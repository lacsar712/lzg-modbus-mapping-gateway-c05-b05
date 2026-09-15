package domain

import "sync"

// SamplerState is the diagnostics sampler state machine state.
// Transitions: idle -> running -> stopping -> idle. Only a running
// sampler spawns a sampling loop; a crashed process never leaves a
// state other than idle behind (see DiagnosticsService recovery).
type SamplerState string

const (
	SamplerIdle     SamplerState = "idle"
	SamplerRunning  SamplerState = "running"
	SamplerStopping SamplerState = "stopping"
)

// SampleSource tells who triggered the sample: a manual snapshot
// request or the device sampler loop.
type SampleSource string

const (
	SourceManual  SampleSource = "manual"
	SourceSampler SampleSource = "sampler"
)

// SampleRecord is one sampling attempt persisted in the ring buffer.
type SampleRecord struct {
	Seq        uint64       `json:"seq"`
	DeviceID   string       `json:"deviceId"`
	Source     SampleSource `json:"source"`
	StartedAt  string       `json:"startedAt"` // RFC3339Nano
	DurationMs int64        `json:"durationMs"`
	Success    bool         `json:"success"`
	BadPoints  []string     `json:"badPoints,omitempty"` // points with quality != good
	Error      string       `json:"error,omitempty"`     // modbus / snapshot error
}

// DeviceSamplerStatus is the persisted per-device sampler state.
type DeviceSamplerStatus struct {
	State      SamplerState `json:"state"`
	IntervalMs int          `json:"intervalMs"`
	UpdatedAt  string       `json:"updatedAt"` // RFC3339Nano of last transition
}

// DiagState is the on-disk diagnostics state (ring + sampler states).
type DiagState struct {
	Version int                            `json:"version"`
	NextSeq uint64                         `json:"nextSeq"`
	Devices map[string]DeviceSamplerStatus `json:"devices,omitempty"`
	Samples []SampleRecord                 `json:"samples,omitempty"` // oldest -> newest
}

// RingBuffer is a fixed-capacity, append-only FIFO ring of sample
// records. Eviction happens only when capacity is exceeded (oldest
// first) — a success never removes older failures.
type RingBuffer struct {
	mu      sync.Mutex
	cap     int
	buf     []SampleRecord // circular storage
	start   int            // index of oldest element
	size    int
	nextSeq uint64
}

func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = 500
	}
	return &RingBuffer{cap: capacity, buf: make([]SampleRecord, capacity), nextSeq: 1}
}

// Append assigns the next sequence number and appends the record,
// evicting the oldest record when full. Returns the assigned seq.
func (r *RingBuffer) Append(rec SampleRecord) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec.Seq = r.nextSeq
	r.nextSeq++
	if r.size < r.cap {
		r.buf[(r.start+r.size)%r.cap] = rec
		r.size++
	} else {
		r.buf[r.start] = rec
		r.start = (r.start + 1) % r.cap
	}
	return rec.Seq
}

// List returns records newest-first, optionally filtered by deviceID
// ("" = all devices) and limited to limit (<=0 = no limit).
func (r *RingBuffer) List(deviceID string, limit int) []SampleRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]SampleRecord, 0, r.size)
	for i := r.size - 1; i >= 0; i-- {
		rec := r.buf[(r.start+i)%r.cap]
		if deviceID != "" && rec.DeviceID != deviceID {
			continue
		}
		out = append(out, rec)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (r *RingBuffer) Size() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.size
}

func (r *RingBuffer) Cap() int { return r.cap }

// Snapshot returns records oldest-first (persistence order).
func (r *RingBuffer) Snapshot() []SampleRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]SampleRecord, 0, r.size)
	for i := 0; i < r.size; i++ {
		out = append(out, r.buf[(r.start+i)%r.cap])
	}
	return out
}

// NextSeq returns the sequence number the next append will use.
func (r *RingBuffer) NextSeq() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nextSeq
}

// Restore replaces ring contents from persisted state. Samples must be
// oldest-first. Capacity overflow keeps the newest records.
func (r *RingBuffer) Restore(samples []SampleRecord, nextSeq uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.size = 0
	r.start = 0
	var maxSeq uint64
	for _, rec := range samples {
		if rec.Seq >= maxSeq {
			maxSeq = rec.Seq
		}
		if r.size < r.cap {
			r.buf[(r.start+r.size)%r.cap] = rec
			r.size++
		} else {
			r.buf[r.start] = rec
			r.start = (r.start + 1) % r.cap
		}
	}
	if nextSeq > maxSeq {
		r.nextSeq = nextSeq
	} else {
		r.nextSeq = maxSeq + 1
	}
	if r.nextSeq == 0 {
		r.nextSeq = 1
	}
}

// DeviceStats aggregates ring records for one device.
type DeviceStats struct {
	DeviceID      string `json:"deviceId"`
	Total         int    `json:"total"`
	SuccessCount  int    `json:"successCount"`
	FailCount     int    `json:"failCount"`
	BadPointTotal int    `json:"badPointTotal"`
	AvgDurationMs int64  `json:"avgDurationMs"`
	LastSampleAt  string `json:"lastSampleAt,omitempty"`
	LastSource    string `json:"lastSource,omitempty"`
	LastSuccess   *bool  `json:"lastSuccess,omitempty"`
	LastError     string `json:"lastError,omitempty"`
	LastErrorAt   string `json:"lastErrorAt,omitempty"`
}

// StatsFor computes stats over the current ring contents (newest
// record wins for the Last* fields).
func (r *RingBuffer) StatsFor(deviceID string) DeviceStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := DeviceStats{DeviceID: deviceID}
	var durSum int64
	for i := 0; i < r.size; i++ {
		rec := r.buf[(r.start+i)%r.cap]
		if rec.DeviceID != deviceID {
			continue
		}
		st.Total++
		durSum += rec.DurationMs
		st.BadPointTotal += len(rec.BadPoints)
		if rec.Success {
			st.SuccessCount++
		} else {
			st.FailCount++
		}
		if rec.Error != "" {
			st.LastError = rec.Error
			st.LastErrorAt = rec.StartedAt
		}
		// iterate oldest -> newest so the last write wins
		st.LastSampleAt = rec.StartedAt
		st.LastSource = string(rec.Source)
		ok := rec.Success
		st.LastSuccess = &ok
	}
	if st.Total > 0 {
		st.AvgDurationMs = durSum / int64(st.Total)
	}
	return st
}
