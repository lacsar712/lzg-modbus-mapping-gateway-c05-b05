package domain

import (
	"fmt"
	"testing"
)

func TestRingAppendKeepsFailuresAfterSuccess(t *testing.T) {
	r := NewRingBuffer(10)
	// failures first
	for i := 0; i < 3; i++ {
		r.Append(SampleRecord{DeviceID: "d1", Source: SourceSampler, Success: false, Error: fmt.Sprintf("err-%d", i)})
	}
	// then successes — must not evict the failures
	for i := 0; i < 3; i++ {
		r.Append(SampleRecord{DeviceID: "d1", Source: SourceManual, Success: true})
	}
	if r.Size() != 6 {
		t.Fatalf("size = %d, want 6", r.Size())
	}
	all := r.List("d1", 0)
	fails := 0
	for _, rec := range all {
		if !rec.Success {
			fails++
		}
	}
	if fails != 3 {
		t.Fatalf("failures kept = %d, want 3 (success must not drop old failures)", fails)
	}
	// newest first
	if all[0].Source != SourceManual {
		t.Fatalf("newest record source = %s, want manual", all[0].Source)
	}
}

func TestRingFIFOEvictionAtCapacity(t *testing.T) {
	r := NewRingBuffer(4)
	for i := 0; i < 6; i++ {
		r.Append(SampleRecord{DeviceID: "d1", Success: i%2 == 0, Error: fmt.Sprintf("e%d", i)})
	}
	if r.Size() != 4 {
		t.Fatalf("size = %d, want 4", r.Size())
	}
	got := r.Snapshot() // oldest -> newest
	if got[0].Error != "e2" || got[3].Error != "e5" {
		t.Fatalf("FIFO eviction wrong: oldest=%s newest=%s, want e2..e5", got[0].Error, got[3].Error)
	}
	// seq is monotonic and survives eviction
	if got[0].Seq != 3 || got[3].Seq != 6 {
		t.Fatalf("seq = %d..%d, want 3..6", got[0].Seq, got[3].Seq)
	}
}

func TestRingRestoreAndSeqContinuity(t *testing.T) {
	r := NewRingBuffer(3)
	samples := []SampleRecord{
		{Seq: 7, DeviceID: "d1", Error: "old"},
		{Seq: 8, DeviceID: "d1", Success: true},
		{Seq: 9, DeviceID: "d2", Error: "new"},
	}
	r.Restore(samples, 10)
	if r.Size() != 3 {
		t.Fatalf("size = %d, want 3", r.Size())
	}
	seq := r.Append(SampleRecord{DeviceID: "d2", Success: true})
	if seq != 10 {
		t.Fatalf("next seq after restore = %d, want 10", seq)
	}
	// capacity smaller than restored count: keep newest
	r2 := NewRingBuffer(2)
	r2.Restore(samples, 10)
	got := r2.Snapshot()
	if len(got) != 2 || got[0].Seq != 8 || got[1].Seq != 9 {
		t.Fatalf("restore over capacity kept %+v, want seq 8,9", got)
	}
}

func TestRingStatsFor(t *testing.T) {
	r := NewRingBuffer(10)
	r.Append(SampleRecord{DeviceID: "d1", Source: SourceManual, Success: true, DurationMs: 10, StartedAt: "2026-09-15T01:00:00Z"})
	r.Append(SampleRecord{DeviceID: "d1", Source: SourceSampler, Success: false, DurationMs: 30, StartedAt: "2026-09-15T01:00:01Z", Error: "modbus timeout", BadPoints: nil})
	r.Append(SampleRecord{DeviceID: "d1", Source: SourceSampler, Success: true, DurationMs: 20, StartedAt: "2026-09-15T01:00:02Z", BadPoints: []string{"temperature"}})
	r.Append(SampleRecord{DeviceID: "d2", Source: SourceManual, Success: true, DurationMs: 99})

	st := r.StatsFor("d1")
	if st.Total != 3 || st.SuccessCount != 2 || st.FailCount != 1 {
		t.Fatalf("stats counts = %+v", st)
	}
	if st.AvgDurationMs != 20 {
		t.Fatalf("avg duration = %d, want 20", st.AvgDurationMs)
	}
	if st.LastError != "modbus timeout" || st.LastErrorAt != "2026-09-15T01:00:01Z" {
		t.Fatalf("last error = %q @ %q", st.LastError, st.LastErrorAt)
	}
	if st.LastSampleAt != "2026-09-15T01:00:02Z" || st.LastSource != string(SourceSampler) {
		t.Fatalf("last sample = %q by %q", st.LastSampleAt, st.LastSource)
	}
	if st.LastSuccess == nil || !*st.LastSuccess {
		t.Fatalf("last success = %v, want true", st.LastSuccess)
	}
	if st.BadPointTotal != 1 {
		t.Fatalf("bad point total = %d, want 1", st.BadPointTotal)
	}

	empty := r.StatsFor("nope")
	if empty.Total != 0 || empty.LastSuccess != nil {
		t.Fatalf("empty stats = %+v", empty)
	}
}
