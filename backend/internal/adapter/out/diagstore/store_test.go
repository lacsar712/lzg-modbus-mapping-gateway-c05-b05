package diagstore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bytecode/modbus-mapping-gateway/internal/domain"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diag", "diagnostics.json")
	s := New(path)

	want := domain.DiagState{
		Version: 1,
		NextSeq: 3,
		Devices: map[string]domain.DeviceSamplerStatus{
			"d1": {State: domain.SamplerRunning, IntervalMs: 1000, UpdatedAt: "2026-09-15T01:00:00Z"},
		},
		Samples: []domain.SampleRecord{
			{Seq: 1, DeviceID: "d1", Source: domain.SourceManual, Success: true, DurationMs: 12, StartedAt: "2026-09-15T01:00:00Z"},
			{Seq: 2, DeviceID: "d1", Source: domain.SourceSampler, Success: false, Error: "modbus timeout", BadPoints: []string{"temperature"}, StartedAt: "2026-09-15T01:00:01Z"},
		},
	}
	if err := s.Save(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.NextSeq != 3 || len(got.Samples) != 2 || got.Devices["d1"].State != domain.SamplerRunning {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if got.Samples[1].Error != "modbus timeout" || got.Samples[1].BadPoints[0] != "temperature" {
		t.Fatalf("sample mismatch: %+v", got.Samples[1])
	}
	// tmp file must not linger
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp file should be renamed away: %v", err)
	}
}

func TestLoadMissingAndCorrupt(t *testing.T) {
	dir := t.TempDir()
	if _, err := New(filepath.Join(dir, "nope.json")).Load(); err == nil {
		t.Fatal("missing file should error")
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(bad).Load(); err == nil {
		t.Fatal("corrupt file should error")
	}
}
