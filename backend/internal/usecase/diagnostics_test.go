package usecase

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/bytecode/modbus-mapping-gateway/internal/domain"
	"github.com/bytecode/modbus-mapping-gateway/internal/port"
)

// --- test doubles ---

type fakeLister struct{ devs []domain.DeviceDef }

func (f fakeLister) ListDevices() []domain.DeviceDef { return f.devs }

var testDevices = fakeLister{devs: []domain.DeviceDef{
	{ID: "d1", Name: "dev-one", Endpoint: "x:1"},
	{ID: "d2", Name: "dev-two", Endpoint: "x:2"},
}}

// fakeSnapshotter returns immediately; error and bad points are
// switchable to script success/failure sequences.
type fakeSnapshotter struct {
	mu    sync.Mutex
	err   error
	bad   []string
	calls int
}

func (f *fakeSnapshotter) Snapshot(deviceID string) (*domain.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	snap := &domain.Snapshot{DeviceID: deviceID}
	for _, n := range f.bad {
		snap.Points = append(snap.Points, domain.PointValue{Name: n, Quality: "bad", Error: "missing registers"})
	}
	snap.Points = append(snap.Points, domain.PointValue{Name: "ok_point", Quality: "good"})
	return snap, nil
}

func (f *fakeSnapshotter) setErr(err error) { f.mu.Lock(); f.err = err; f.mu.Unlock() }
func (f *fakeSnapshotter) setBad(names []string) {
	f.mu.Lock()
	f.bad = names
	f.mu.Unlock()
}
func (f *fakeSnapshotter) callCount() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }

// gateSnapshotter blocks inside Snapshot until released; once
// released it stays open (subsequent calls return immediately).
type gateSnapshotter struct {
	mu       sync.Mutex
	calls    int
	inFlight int
	maxConc  int
	gate     chan struct{}
	open     bool
}

func newGateSnapshotter() *gateSnapshotter {
	return &gateSnapshotter{gate: make(chan struct{})}
}

func (g *gateSnapshotter) Snapshot(deviceID string) (*domain.Snapshot, error) {
	g.mu.Lock()
	g.calls++
	g.inFlight++
	if g.inFlight > g.maxConc {
		g.maxConc = g.inFlight
	}
	ch := g.gate
	open := g.open
	g.mu.Unlock()
	if !open {
		<-ch
	}
	g.mu.Lock()
	g.inFlight--
	g.mu.Unlock()
	return &domain.Snapshot{DeviceID: deviceID, Points: []domain.PointValue{{Name: "p", Quality: "good"}}}, nil
}

func (g *gateSnapshotter) release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.open {
		g.open = true
		close(g.gate)
	}
}

func (g *gateSnapshotter) stats() (calls, maxConc int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls, g.maxConc
}

type memDiagStore struct {
	mu    sync.Mutex
	st    domain.DiagState
	saves int
}

func (m *memDiagStore) Load() (domain.DiagState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st, nil
}

func (m *memDiagStore) Save(st domain.DiagState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.st = st
	m.saves++
	return nil
}

func (m *memDiagStore) saved() domain.DiagState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st
}

// --- helpers ---

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func newSvc(snap port.Snapshotter, store *memDiagStore, ringCap int) *DiagnosticsService {
	return NewDiagnosticsService(snap, testDevices, store, ringCap)
}

// --- tests ---

func TestStartConflictKeepsSingleLoop(t *testing.T) {
	gate := newGateSnapshotter()
	svc := newSvc(gate, &memDiagStore{}, 10)

	if _, err := svc.Start("d1", 50); err != nil {
		t.Fatalf("first start: %v", err)
	}
	// wait until the loop is parked inside its first snapshot
	waitFor(t, "first in-flight snapshot", func() bool {
		c, _ := gate.stats()
		return c == 1
	})

	// second start (e.g. another engineer) must conflict
	if _, err := svc.Start("d1", 50); !errors.Is(err, ErrSamplerConflict) {
		t.Fatalf("second start err = %v, want ErrSamplerConflict", err)
	}
	// give a hypothetical second loop time to run: no new calls may appear
	time.Sleep(150 * time.Millisecond)
	calls, maxConc := gate.stats()
	if calls != 1 || maxConc != 1 {
		t.Fatalf("calls=%d maxConc=%d, want exactly 1 call / 1 concurrent loop", calls, maxConc)
	}

	gate.release()
	waitFor(t, "sample recorded after release", func() bool {
		return svc.ring.Size() >= 1
	})
	if _, err := svc.Stop("d1"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !svc.waitIdle("d1", 2*time.Second) {
		t.Fatal("sampler did not reach idle")
	}
}

func TestStoppingDiscardsInFlightSuccess(t *testing.T) {
	gate := newGateSnapshotter()
	svc := newSvc(gate, &memDiagStore{}, 10)

	if _, err := svc.Start("d1", 50); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, "in-flight snapshot", func() bool {
		c, _ := gate.stats()
		return c == 1
	})

	st, err := svc.Stop("d1")
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if st.State != domain.SamplerStopping {
		t.Fatalf("state after stop = %s, want stopping", st.State)
	}

	// the in-flight snapshot now completes successfully — it must NOT
	// enter the ring because the sampler is stopping
	gate.release()
	if !svc.waitIdle("d1", 2*time.Second) {
		t.Fatal("sampler did not reach idle")
	}
	if got := svc.ring.Size(); got != 0 {
		recs := svc.ring.List("", 0)
		t.Fatalf("ring size = %d, want 0 (stopping must not record success): %+v", got, recs)
	}
}

func TestSamplerRecordsFailuresAndSuccesses(t *testing.T) {
	snap := &fakeSnapshotter{err: errors.New("modbus exception fc=0x83 code=2")}
	svc := newSvc(snap, &memDiagStore{}, 100)

	if _, err := svc.Start("d1", 50); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, "failure sample in ring", func() bool {
		recs := svc.ring.List("d1", 0)
		return len(recs) >= 1 && !recs[0].Success
	})

	// device recovers: successes must not delete the old failures
	snap.setErr(nil)
	waitFor(t, "success sample in ring", func() bool {
		for _, r := range svc.ring.List("d1", 0) {
			if r.Success {
				return true
			}
		}
		return false
	})
	if _, err := svc.Stop("d1"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	svc.waitIdle("d1", 2*time.Second)

	var fails, succs int
	var failRec *domain.SampleRecord
	recs := svc.ring.List("d1", 0)
	for i := range recs {
		if recs[i].Success {
			succs++
		} else {
			fails++
			failRec = &recs[i]
		}
	}
	if fails == 0 || succs == 0 {
		t.Fatalf("want both failures and successes in ring, got fails=%d succs=%d", fails, succs)
	}
	if failRec.Error == "" || failRec.Source != domain.SourceSampler {
		t.Fatalf("failure record = %+v", failRec)
	}
}

func TestManualSnapshotRecordedWithBadPoints(t *testing.T) {
	snap := &fakeSnapshotter{}
	snap.setBad([]string{"temperature"})
	svc := newSvc(snap, &memDiagStore{}, 10)

	got, rec, err := svc.RecordManualSnapshot("d1")
	if err != nil {
		t.Fatalf("manual snapshot: %v", err)
	}
	if got == nil || rec == nil {
		t.Fatal("expected snapshot and record")
	}
	if !rec.Success || rec.Source != domain.SourceManual {
		t.Fatalf("record = %+v", rec)
	}
	if len(rec.BadPoints) != 1 || rec.BadPoints[0] != "temperature" {
		t.Fatalf("bad points = %v, want [temperature]", rec.BadPoints)
	}
	if rec.DurationMs < 0 {
		t.Fatalf("duration = %d", rec.DurationMs)
	}

	// now fail: failure joins the ring, success stays
	snap.setErr(errors.New("dial tcp: connection refused"))
	snap.setBad(nil)
	if _, _, err := svc.RecordManualSnapshot("d1"); err == nil {
		t.Fatal("expected snapshot error")
	}
	recs := svc.ring.List("d1", 0)
	if len(recs) != 2 {
		t.Fatalf("ring size = %d, want 2", len(recs))
	}
	var sawSuccess, sawFail bool
	for _, r := range recs {
		if r.Success {
			sawSuccess = true
		} else if r.Error == "dial tcp: connection refused" {
			sawFail = true
		}
	}
	if !sawSuccess || !sawFail {
		t.Fatalf("ring must keep success AND failure, got %+v", recs)
	}
}

func TestReloadRestoresRingAndRecoversRunningToIdle(t *testing.T) {
	store := &memDiagStore{}
	snapA := &fakeSnapshotter{}
	svcA := newSvc(snapA, store, 10)

	// one success + one failure in the ring
	if _, _, err := svcA.RecordManualSnapshot("d1"); err != nil {
		t.Fatalf("manual: %v", err)
	}
	snapA.setErr(errors.New("modbus timeout"))
	if _, _, err := svcA.RecordManualSnapshot("d1"); err == nil {
		t.Fatal("expected error")
	}
	snapA.setErr(nil)

	// sampler running at "crash" (huge interval: exactly one loop sample)
	if _, err := svcA.Start("d1", MaxIntervalMs); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, "sampler record persisted", func() bool {
		return svcA.ring.Size() == 3
	})
	if st := store.saved().Devices["d1"]; st.State != domain.SamplerRunning {
		t.Fatalf("persisted state = %s, want running", st.State)
	}

	// simulate restart: new service over the same store
	snapB := &fakeSnapshotter{}
	svcB := newSvc(snapB, store, 10)

	// ring survived
	recs := svcB.ring.List("d1", 0)
	if len(recs) != 3 {
		t.Fatalf("restored ring size = %d, want 3", len(recs))
	}
	var sawFail bool
	for _, r := range recs {
		if !r.Success && r.Error == "modbus timeout" && r.Source == domain.SourceManual {
			sawFail = true
		}
	}
	if !sawFail {
		t.Fatalf("failure record lost across restart: %+v", recs)
	}

	// running sampler recovered to idle, NOT auto-resumed
	if st := svcB.Status("d1"); st.State != domain.SamplerIdle {
		t.Fatalf("recovered state = %s, want idle", st.State)
	}
	callsBefore := snapB.callCount()
	time.Sleep(150 * time.Millisecond)
	if got := snapB.callCount(); got != callsBefore {
		t.Fatalf("sampler auto-resumed after restart: snapshot calls %d -> %d", callsBefore, got)
	}
	if got := svcB.ring.Size(); got != 3 {
		t.Fatalf("ring grew after restart without explicit start: %d", got)
	}

	// seq continuity across restart
	_, rec, err := svcB.RecordManualSnapshot("d1")
	if err != nil {
		t.Fatalf("manual after restart: %v", err)
	}
	if rec.Seq != 4 {
		t.Fatalf("seq after restart = %d, want 4", rec.Seq)
	}

	// cleanup svcA's parked loop
	if _, err := svcA.Stop("d1"); err != nil {
		t.Fatalf("stop svcA: %v", err)
	}
	svcA.waitIdle("d1", 2*time.Second)
}

func TestStopIdleConflictsAndStartUnknownDevice(t *testing.T) {
	svc := newSvc(&fakeSnapshotter{}, &memDiagStore{}, 10)
	if _, err := svc.Stop("d1"); !errors.Is(err, ErrSamplerNotRunning) {
		t.Fatalf("stop idle err = %v, want ErrSamplerNotRunning", err)
	}
	if _, err := svc.Start("nope", 100); err == nil {
		t.Fatal("start unknown device should fail")
	}
	if _, err := svc.Start("d1", 1); err == nil {
		t.Fatal("interval below min should fail")
	}
}

// errDiagStore simulates a broken/missing state file.
type errDiagStore struct{ err error }

func (e *errDiagStore) Load() (domain.DiagState, error) { return domain.DiagState{}, e.err }
func (e *errDiagStore) Save(domain.DiagState) error     { return nil }

func TestLoadErrorSurfacedOnlyForCorruptState(t *testing.T) {
	// first boot (missing file): silent, no loadError
	fresh := NewDiagnosticsService(&fakeSnapshotter{}, testDevices, &errDiagStore{err: os.ErrNotExist}, 10)
	if ov := fresh.Overview(); ov.LoadError != "" {
		t.Fatalf("fresh boot loadError = %q, want empty", ov.LoadError)
	}

	// corrupt file: surfaced, but the service keeps working
	broken := NewDiagnosticsService(&fakeSnapshotter{}, testDevices, &errDiagStore{err: errors.New("diag state parse: unexpected EOF")}, 10)
	if ov := broken.Overview(); ov.LoadError == "" {
		t.Fatal("corrupt state file should surface loadError")
	}
	if _, _, err := broken.RecordManualSnapshot("d1"); err != nil {
		t.Fatalf("service must keep working with corrupt state file: %v", err)
	}
	if broken.ring.Size() != 1 {
		t.Fatalf("ring size = %d, want 1", broken.ring.Size())
	}
}

func TestDeviceSummariesSort(t *testing.T) {
	snap := &fakeSnapshotter{}
	svc := newSvc(snap, &memDiagStore{}, 10)

	// d1: slow success; d2: fast failure (more recent)
	svc.recordSampleWith("d1", true, 100, "", time.Now().Add(-2*time.Minute))
	svc.recordSampleWith("d2", false, 10, "boom", time.Now().Add(-1*time.Minute))

	// default: config order
	sums, err := svc.DeviceSummaries("")
	if err != nil {
		t.Fatalf("summaries: %v", err)
	}
	if sums[0].Device.ID != "d1" || sums[1].Device.ID != "d2" {
		t.Fatalf("default order = %s,%s", sums[0].Device.ID, sums[1].Device.ID)
	}

	// lastFailure: d2 (only failure) first
	sums, err = svc.DeviceSummaries("lastFailure")
	if err != nil {
		t.Fatalf("summaries: %v", err)
	}
	if sums[0].Device.ID != "d2" || sums[1].Device.ID != "d1" {
		t.Fatalf("lastFailure order = %s,%s, want d2,d1", sums[0].Device.ID, sums[1].Device.ID)
	}

	// avgDuration: d1 (100ms) before d2 (10ms)
	sums, err = svc.DeviceSummaries("avgDuration")
	if err != nil {
		t.Fatalf("summaries: %v", err)
	}
	if sums[0].Device.ID != "d1" || sums[1].Device.ID != "d2" {
		t.Fatalf("avgDuration order = %s,%s, want d1,d2", sums[0].Device.ID, sums[1].Device.ID)
	}
	if sums[0].Diag.Stats.AvgDurationMs != 100 || sums[1].Diag.Stats.AvgDurationMs != 10 {
		t.Fatalf("avg durations = %d,%d", sums[0].Diag.Stats.AvgDurationMs, sums[1].Diag.Stats.AvgDurationMs)
	}

	if _, err := svc.DeviceSummaries("bogus"); err == nil {
		t.Fatal("unsupported sort should fail")
	}
}

// recordSampleWith injects a record directly (test shortcut for stats).
func (s *DiagnosticsService) recordSampleWith(deviceID string, success bool, durMs int64, errText string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ring.Append(domain.SampleRecord{
		DeviceID:   deviceID,
		Source:     domain.SourceSampler,
		StartedAt:  at.UTC().Format(time.RFC3339Nano),
		DurationMs: durMs,
		Success:    success,
		Error:      errText,
	})
	s.persistLocked()
}
