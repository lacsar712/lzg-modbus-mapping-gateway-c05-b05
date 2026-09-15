package usecase

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/bytecode/modbus-mapping-gateway/internal/domain"
	"github.com/bytecode/modbus-mapping-gateway/internal/port"
)

// Sampler state machine errors mapped to HTTP 409 by the API layer.
var (
	ErrSamplerConflict   = errors.New("sampler not idle: start conflicts with current state")
	ErrSamplerNotRunning = errors.New("sampler not running")
)

const (
	DefaultRingCap    = 500
	DefaultIntervalMs = 1000
	MinIntervalMs     = 50
	MaxIntervalMs     = 60000
	diagStateVersion  = 1
)

// deviceSampler is the runtime (in-memory) half of the state machine.
type deviceSampler struct {
	state      domain.SamplerState
	intervalMs int
	updatedAt  time.Time
	stopCh     chan struct{}
	doneCh     chan struct{}
}

// DiagnosticsService owns the per-device sampler state machines and
// the shared persistent sample ring. It is deliberately scoped to
// modbus snapshot diagnostics — not a generic metrics/APM pipeline.
type DiagnosticsService struct {
	snapshotter port.Snapshotter
	lister      port.DeviceLister
	store       port.DiagStore

	ring *domain.RingBuffer

	mu       sync.Mutex
	samplers map[string]*deviceSampler
	loadErr  string
}

// NewDiagnosticsService restores the persisted ring and sampler
// states. Recovery policy: a sampler found in running/stopping at
// boot belonged to a dead process — it is reset to idle and NOT
// auto-restarted (an engineer must start it explicitly). The ring
// itself is restored verbatim.
func NewDiagnosticsService(snapshotter port.Snapshotter, lister port.DeviceLister, store port.DiagStore, ringCap int) *DiagnosticsService {
	s := &DiagnosticsService{
		snapshotter: snapshotter,
		lister:      lister,
		store:       store,
		ring:        domain.NewRingBuffer(ringCap),
		samplers:    map[string]*deviceSampler{},
	}
	state, err := store.Load()
	if err != nil {
		// A missing state file just means first boot — start empty
		// silently. A corrupt file must not take the gateway down
		// either: start empty and surface the problem in the overview.
		if !errors.Is(err, os.ErrNotExist) {
			s.loadErr = err.Error()
		}
		return s
	}
	s.ring.Restore(state.Samples, state.NextSeq)
	for id, st := range state.Devices {
		ds := &deviceSampler{intervalMs: st.IntervalMs, updatedAt: time.Now().UTC()}
		switch st.State {
		case domain.SamplerRunning, domain.SamplerStopping:
			// crashed mid-run: crash = stop, no auto-resume
			ds.state = domain.SamplerIdle
		case domain.SamplerIdle:
			ds.state = domain.SamplerIdle
		default:
			ds.state = domain.SamplerIdle
		}
		if ds.intervalMs <= 0 {
			ds.intervalMs = DefaultIntervalMs
		}
		s.samplers[id] = ds
	}
	// persist the recovered (forced-idle) states so a crash loop does
	// not keep re-reading "running" from disk
	s.mu.Lock()
	s.persistLocked()
	s.mu.Unlock()
	return s
}

func (s *DiagnosticsService) deviceExistsLocked(deviceID string) bool {
	for _, d := range s.lister.ListDevices() {
		if d.ID == deviceID {
			return true
		}
	}
	return false
}

func (s *DiagnosticsService) getOrCreateLocked(deviceID string) *deviceSampler {
	ds, ok := s.samplers[deviceID]
	if !ok {
		ds = &deviceSampler{state: domain.SamplerIdle, intervalMs: DefaultIntervalMs, updatedAt: time.Now().UTC()}
		s.samplers[deviceID] = ds
	}
	return ds
}

// Start transitions idle -> running and spawns exactly one sampling
// loop. A second start (same device, any user) returns
// ErrSamplerConflict and never spawns another loop.
func (s *DiagnosticsService) Start(deviceID string, intervalMs int) (domain.DeviceSamplerStatus, error) {
	if intervalMs <= 0 {
		intervalMs = DefaultIntervalMs
	}
	if intervalMs < MinIntervalMs || intervalMs > MaxIntervalMs {
		return domain.DeviceSamplerStatus{}, fmt.Errorf("intervalMs must be between %d and %d", MinIntervalMs, MaxIntervalMs)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.deviceExistsLocked(deviceID) {
		return domain.DeviceSamplerStatus{}, fmt.Errorf("device not found: %s", deviceID)
	}
	ds := s.getOrCreateLocked(deviceID)
	if ds.state != domain.SamplerIdle {
		return domain.DeviceSamplerStatus{}, ErrSamplerConflict
	}
	ds.state = domain.SamplerRunning
	ds.intervalMs = intervalMs
	ds.updatedAt = time.Now().UTC()
	ds.stopCh = make(chan struct{})
	ds.doneCh = make(chan struct{})
	s.persistLocked()
	go s.runLoop(deviceID, ds)
	return s.statusOf(deviceID), nil
}

// Stop transitions running -> stopping; the loop exits after any
// in-flight sample is discarded. Stopping an already-stopping sampler
// is acknowledged idempotently; stopping an idle one conflicts.
func (s *DiagnosticsService) Stop(deviceID string) (domain.DeviceSamplerStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ds, ok := s.samplers[deviceID]
	if !ok || ds.state == domain.SamplerIdle {
		return domain.DeviceSamplerStatus{}, ErrSamplerNotRunning
	}
	if ds.state == domain.SamplerStopping {
		return s.statusOf(deviceID), nil
	}
	ds.state = domain.SamplerStopping
	ds.updatedAt = time.Now().UTC()
	close(ds.stopCh)
	s.persistLocked()
	return s.statusOf(deviceID), nil
}

func (s *DiagnosticsService) runLoop(deviceID string, ds *deviceSampler) {
	defer func() {
		s.mu.Lock()
		ds.state = domain.SamplerIdle
		ds.updatedAt = time.Now().UTC()
		ds.stopCh = nil
		s.persistLocked()
		s.mu.Unlock()
		close(ds.doneCh)
	}()
	for {
		select {
		case <-ds.stopCh:
			return
		default:
		}
		// device removed from mapping by a reload: stop the loop
		// instead of spinning failure records forever.
		s.mu.Lock()
		exists := s.deviceExistsLocked(deviceID)
		s.mu.Unlock()
		if !exists {
			return
		}
		s.recordSample(deviceID, domain.SourceSampler)
		select {
		case <-ds.stopCh:
			return
		case <-time.After(time.Duration(ds.intervalMs) * time.Millisecond):
		}
	}
}

// runSnapshot executes one snapshot and converts the outcome into a
// ring record (without appending it).
func (s *DiagnosticsService) runSnapshot(deviceID string, source domain.SampleSource) (*domain.Snapshot, domain.SampleRecord, error) {
	started := time.Now()
	snap, snapErr := s.snapshotter.Snapshot(deviceID)
	dur := time.Since(started)

	rec := domain.SampleRecord{
		DeviceID:   deviceID,
		Source:     source,
		StartedAt:  started.UTC().Format(time.RFC3339Nano),
		DurationMs: dur.Milliseconds(),
	}
	if snapErr != nil {
		rec.Success = false
		rec.Error = snapErr.Error()
	} else {
		rec.Success = true
		for _, p := range snap.Points {
			if p.Quality != "good" {
				rec.BadPoints = append(rec.BadPoints, p.Name)
			}
		}
	}
	return snap, rec, snapErr
}

// recordSample runs one snapshot and appends the outcome to the ring.
// Sampler-sourced records are discarded if the sampler left the
// running state while the snapshot was in flight (stopping must not
// record further successes — or any result at all).
func (s *DiagnosticsService) recordSample(deviceID string, source domain.SampleSource) (*domain.SampleRecord, error) {
	_, rec, snapErr := s.runSnapshot(deviceID, source)

	s.mu.Lock()
	defer s.mu.Unlock()
	if source == domain.SourceSampler {
		ds, ok := s.samplers[deviceID]
		if !ok || ds.state != domain.SamplerRunning {
			return nil, nil // stopping/stopped mid-flight: discard
		}
	}
	rec.Seq = s.ring.Append(rec)
	s.persistLocked()
	return &rec, snapErr
}

// RecordManualSnapshot runs a manual snapshot and records it in the
// ring (source=manual). Manual samples are always recorded, even
// while the sampler is stopping — the discard rule applies to the
// sampler loop only. Returns the snapshot for the API response.
func (s *DiagnosticsService) RecordManualSnapshot(deviceID string) (*domain.Snapshot, *domain.SampleRecord, error) {
	snap, rec, snapErr := s.runSnapshot(deviceID, domain.SourceManual)
	s.mu.Lock()
	rec.Seq = s.ring.Append(rec)
	s.persistLocked()
	s.mu.Unlock()
	return snap, &rec, snapErr
}

// Status returns the current sampler status for one device.
func (s *DiagnosticsService) Status(deviceID string) domain.DeviceSamplerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusOf(deviceID)
}

func (s *DiagnosticsService) statusOf(deviceID string) domain.DeviceSamplerStatus {
	ds, ok := s.samplers[deviceID]
	if !ok {
		return domain.DeviceSamplerStatus{State: domain.SamplerIdle, IntervalMs: DefaultIntervalMs, UpdatedAt: ""}
	}
	return domain.DeviceSamplerStatus{
		State:      ds.state,
		IntervalMs: ds.intervalMs,
		UpdatedAt:  ds.updatedAt.UTC().Format(time.RFC3339Nano),
	}
}

// Samples lists ring records newest-first.
func (s *DiagnosticsService) Samples(deviceID string, limit int) []domain.SampleRecord {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	return s.ring.List(deviceID, limit)
}

// DeviceDiag is the per-device diagnostics summary attached to the
// device list.
type DeviceDiag struct {
	State      domain.SamplerState `json:"state"`
	IntervalMs int                 `json:"intervalMs"`
	Stats      domain.DeviceStats  `json:"stats"`
}

// DeviceSummary pairs a device definition with its diagnostics.
type DeviceSummary struct {
	Device domain.DeviceDef `json:"device"`
	Diag   DeviceDiag       `json:"diag"`
}

// Overview is the diagnostics dashboard payload.
type Overview struct {
	RingSize  int                   `json:"ringSize"`
	RingCap   int                   `json:"ringCap"`
	LoadError string                `json:"loadError,omitempty"`
	Devices   map[string]DeviceDiag `json:"devices"`
}

func (s *DiagnosticsService) Overview() Overview {
	s.mu.Lock()
	defer s.mu.Unlock()
	ov := Overview{
		RingSize:  s.ring.Size(),
		RingCap:   s.ring.Cap(),
		LoadError: s.loadErr,
		Devices:   map[string]DeviceDiag{},
	}
	for _, d := range s.lister.ListDevices() {
		st := s.statusOf(d.ID)
		ov.Devices[d.ID] = DeviceDiag{
			State:      st.State,
			IntervalMs: st.IntervalMs,
			Stats:      s.ring.StatsFor(d.ID),
		}
	}
	return ov
}

// DeviceSummaries returns devices (config order by default) with diag
// summaries, optionally sorted: "lastFailure" (most recent failure
// first) or "avgDuration" (slowest average first). Devices without
// matching data sort last; ties keep config order.
func (s *DiagnosticsService) DeviceSummaries(sortBy string) ([]DeviceSummary, error) {
	if sortBy != "" && sortBy != "lastFailure" && sortBy != "avgDuration" {
		return nil, fmt.Errorf("unsupported sort %q (want lastFailure|avgDuration)", sortBy)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	devs := s.lister.ListDevices()
	out := make([]DeviceSummary, 0, len(devs))
	for _, d := range devs {
		st := s.statusOf(d.ID)
		out = append(out, DeviceSummary{
			Device: d,
			Diag: DeviceDiag{
				State:      st.State,
				IntervalMs: st.IntervalMs,
				Stats:      s.ring.StatsFor(d.ID),
			},
		})
	}
	switch sortBy {
	case "lastFailure":
		sort.SliceStable(out, func(i, j int) bool {
			a, b := out[i].Diag.Stats.LastErrorAt, out[j].Diag.Stats.LastErrorAt
			if a == "" || b == "" {
				return a != "" // devices with a failure first
			}
			return a > b // RFC3339 UTC: lexicographic == chronological
		})
	case "avgDuration":
		sort.SliceStable(out, func(i, j int) bool {
			ai, aj := out[i].Diag.Stats, out[j].Diag.Stats
			if ai.Total == 0 || aj.Total == 0 {
				return ai.Total != 0 // devices with samples first
			}
			return ai.AvgDurationMs > aj.AvgDurationMs
		})
	}
	return out, nil
}

func (s *DiagnosticsService) persistLocked() {
	state := domain.DiagState{
		Version: diagStateVersion,
		NextSeq: s.ring.NextSeq(),
		Devices: map[string]domain.DeviceSamplerStatus{},
		Samples: s.ring.Snapshot(),
	}
	for id, ds := range s.samplers {
		state.Devices[id] = domain.DeviceSamplerStatus{
			State:      ds.state,
			IntervalMs: ds.intervalMs,
			UpdatedAt:  ds.updatedAt.UTC().Format(time.RFC3339Nano),
		}
	}
	// best-effort: a persistence hiccup must not break sampling;
	// the error surfaces via the next load cycle / overview.
	_ = s.store.Save(state)
}

// waitIdle blocks until the device's sampler reaches idle or the
// timeout elapses. Used by tests and graceful shutdown.
func (s *DiagnosticsService) waitIdle(deviceID string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.Status(deviceID).State == domain.SamplerIdle {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}
