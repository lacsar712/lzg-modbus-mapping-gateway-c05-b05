package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bytecode/modbus-mapping-gateway/internal/domain"
	"github.com/bytecode/modbus-mapping-gateway/internal/usecase"
	"gopkg.in/yaml.v3"
)

const testMapping = `
devices:
  - id: d1
    name: dev-one
    endpoint: fake1:502
    unitId: 1
    timeoutMs: 500
    points:
      - name: rpm
        address: 0
        type: uint16
        writable: false
        scale: 1
        offset: 0
  - id: d2
    name: dev-two
    endpoint: fake2:502
    unitId: 1
    timeoutMs: 500
    points:
      - name: temp
        address: 0
        type: uint16
        writable: false
        scale: 1
        offset: 0
`

type memMappingStore struct{ text string }

func (m *memMappingStore) Load() (domain.MappingConfig, string, error) {
	var cfg domain.MappingConfig
	if err := yaml.Unmarshal([]byte(m.text), &cfg); err != nil {
		return cfg, "", err
	}
	return cfg, m.text, nil
}
func (m *memMappingStore) Save(string) error { return nil }
func (m *memMappingStore) Path() string      { return "mem" }

type fakeModbus struct {
	mu     sync.Mutex
	delay  map[string]time.Duration
	errFor map[string]error
}

func (f *fakeModbus) ReadHoldingRegisters(endpoint string, unitID byte, timeoutMs int, address, quantity uint16) ([]uint16, error) {
	f.mu.Lock()
	d := f.delay[endpoint]
	err := f.errFor[endpoint]
	f.mu.Unlock()
	if d > 0 {
		time.Sleep(d)
	}
	if err != nil {
		return nil, err
	}
	return make([]uint16, quantity), nil
}

func (f *fakeModbus) WriteSingleRegister(endpoint string, unitID byte, timeoutMs int, address, value uint16) error {
	return nil
}

func (f *fakeModbus) WriteMultipleRegisters(endpoint string, unitID byte, timeoutMs int, address uint16, values []uint16) error {
	return nil
}

func (f *fakeModbus) setErr(endpoint string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.errFor == nil {
		f.errFor = map[string]error{}
	}
	f.errFor[endpoint] = err
}

type memDiagStore struct {
	mu sync.Mutex
	st domain.DiagState
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
	return nil
}

type testEnv struct {
	router *gin.Engine
	modbus *fakeModbus
	diag   *usecase.DiagnosticsService
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mb := &fakeModbus{delay: map[string]time.Duration{}, errFor: map[string]error{}}
	svc, err := usecase.NewGatewayService(&memMappingStore{text: testMapping}, mb)
	if err != nil {
		t.Fatalf("gateway service: %v", err)
	}
	diag := usecase.NewDiagnosticsService(svc, svc, &memDiagStore{}, 50)
	srv := NewServer(svc, diag)
	return &testEnv{router: srv.Router(), modbus: mb, diag: diag}
}

func (e *testEnv) login(t *testing.T, user, pass string) string {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, user, pass)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login %s: status %d body %s", user, w.Code, w.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("login resp: %v", err)
	}
	return resp.Token
}

func (e *testEnv) do(t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Buffer
	if body != "" {
		rdr = bytes.NewBufferString(body)
	} else {
		rdr = &bytes.Buffer{}
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
}

func TestAuthRequired(t *testing.T) {
	env := newTestEnv(t)
	for _, path := range []string{"/api/diagnostics", "/api/diagnostics/samples", "/api/devices"} {
		if w := env.do(t, http.MethodGet, path, "", ""); w.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s without token = %d, want 401", path, w.Code)
		}
	}
	if w := env.do(t, http.MethodPost, "/api/diagnostics/d1/start", "", `{}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("POST start without token = %d, want 401", w.Code)
	}
}

func TestObserverReadOnlyCannotStart(t *testing.T) {
	env := newTestEnv(t)
	obs := env.login(t, "observer", "obs123456")

	for _, path := range []string{"/api/diagnostics", "/api/diagnostics/samples", "/api/devices?sort=avgDuration", "/api/devices?sort=lastFailure"} {
		if w := env.do(t, http.MethodGet, path, obs, ""); w.Code != http.StatusOK {
			t.Fatalf("observer GET %s = %d, want 200: %s", path, w.Code, w.Body.String())
		}
	}
	if w := env.do(t, http.MethodPost, "/api/diagnostics/d1/start", obs, `{"intervalMs":100}`); w.Code != http.StatusForbidden {
		t.Fatalf("observer start = %d, want 403", w.Code)
	}
	if w := env.do(t, http.MethodPost, "/api/diagnostics/d1/stop", obs, `{}`); w.Code != http.StatusForbidden {
		t.Fatalf("observer stop = %d, want 403", w.Code)
	}
	// observer's failed start attempts must not have started anything
	var ov struct {
		Devices map[string]struct {
			State string `json:"state"`
		} `json:"devices"`
	}
	decodeBody(t, env.do(t, http.MethodGet, "/api/diagnostics", obs, ""), &ov)
	if ov.Devices["d1"].State != "idle" {
		t.Fatalf("d1 state = %s, want idle", ov.Devices["d1"].State)
	}
}

func TestEngineerStartConflictStop(t *testing.T) {
	env := newTestEnv(t)
	eng := env.login(t, "engineer", "mod123456")

	w := env.do(t, http.MethodPost, "/api/diagnostics/d1/start", eng, `{"intervalMs":50}`)
	if w.Code != http.StatusOK {
		t.Fatalf("start = %d: %s", w.Code, w.Body.String())
	}
	var st domain.DeviceSamplerStatus
	decodeBody(t, w, &st)
	if st.State != domain.SamplerRunning || st.IntervalMs != 50 {
		t.Fatalf("status after start = %+v", st)
	}

	// second start (e.g. another engineer) conflicts, no second loop
	w = env.do(t, http.MethodPost, "/api/diagnostics/d1/start", eng, `{"intervalMs":50}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("second start = %d, want 409: %s", w.Code, w.Body.String())
	}

	// stop: running -> stopping -> idle
	w = env.do(t, http.MethodPost, "/api/diagnostics/d1/stop", eng, `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("stop = %d: %s", w.Code, w.Body.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var ov struct {
			Devices map[string]struct {
				State string `json:"state"`
			} `json:"devices"`
		}
		decodeBody(t, env.do(t, http.MethodGet, "/api/diagnostics", eng, ""), &ov)
		if ov.Devices["d1"].State == "idle" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sampler stuck in state %s", ov.Devices["d1"].State)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// stopping an idle sampler conflicts too
	if w := env.do(t, http.MethodPost, "/api/diagnostics/d1/stop", eng, `{}`); w.Code != http.StatusConflict {
		t.Fatalf("stop idle = %d, want 409", w.Code)
	}
	// unknown device
	if w := env.do(t, http.MethodPost, "/api/diagnostics/nope/start", eng, `{}`); w.Code != http.StatusNotFound {
		t.Fatalf("start unknown = %d, want 404", w.Code)
	}
}

func TestManualSnapshotLandsInRingAndKeepsFailures(t *testing.T) {
	env := newTestEnv(t)
	eng := env.login(t, "engineer", "mod123456")

	if w := env.do(t, http.MethodGet, "/api/devices/d1/snapshot", eng, ""); w.Code != http.StatusOK {
		t.Fatalf("snapshot = %d: %s", w.Code, w.Body.String())
	}

	type samplesResp struct {
		Samples []domain.SampleRecord `json:"samples"`
	}
	var sr samplesResp
	decodeBody(t, env.do(t, http.MethodGet, "/api/diagnostics/samples?deviceId=d1", eng, ""), &sr)
	if len(sr.Samples) != 1 || !sr.Samples[0].Success || sr.Samples[0].Source != domain.SourceManual {
		t.Fatalf("samples after manual snapshot = %+v", sr.Samples)
	}

	// modbus starts failing: failure is recorded, old success survives
	env.modbus.setErr("fake1:502", errors.New("modbus exception fc=0x83 code=2"))
	if w := env.do(t, http.MethodGet, "/api/devices/d1/snapshot", eng, ""); w.Code != http.StatusBadGateway {
		t.Fatalf("failing snapshot = %d, want 502", w.Code)
	}
	sr = samplesResp{}
	decodeBody(t, env.do(t, http.MethodGet, "/api/diagnostics/samples?deviceId=d1", eng, ""), &sr)
	if len(sr.Samples) != 2 {
		t.Fatalf("samples = %+v, want 2 records", sr.Samples)
	}
	if sr.Samples[0].Success || sr.Samples[0].Error == "" {
		t.Fatalf("newest record should be the failure: %+v", sr.Samples[0])
	}
	if !sr.Samples[1].Success {
		t.Fatalf("old success must survive: %+v", sr.Samples[1])
	}
}

func TestDevicesSortAndDiagSummary(t *testing.T) {
	env := newTestEnv(t)
	eng := env.login(t, "engineer", "mod123456")

	// d1: slow success; d2: fast failure
	env.modbus.delay["fake1:502"] = 40 * time.Millisecond
	env.modbus.setErr("fake2:502", errors.New("boom"))
	if w := env.do(t, http.MethodGet, "/api/devices/d1/snapshot", eng, ""); w.Code != http.StatusOK {
		t.Fatalf("snapshot d1 = %d", w.Code)
	}
	if w := env.do(t, http.MethodGet, "/api/devices/d2/snapshot", eng, ""); w.Code != http.StatusBadGateway {
		t.Fatalf("snapshot d2 = %d, want 502", w.Code)
	}

	type deviceEntry struct {
		ID   string `json:"id"`
		Diag struct {
			State string `json:"state"`
			Stats struct {
				Total         int    `json:"total"`
				FailCount     int    `json:"failCount"`
				AvgDurationMs int64  `json:"avgDurationMs"`
				LastError     string `json:"lastError"`
			} `json:"stats"`
		} `json:"diag"`
	}
	var resp struct {
		Devices []deviceEntry `json:"devices"`
	}

	decodeBody(t, env.do(t, http.MethodGet, "/api/devices?sort=lastFailure", eng, ""), &resp)
	if len(resp.Devices) != 2 || resp.Devices[0].ID != "d2" {
		t.Fatalf("sort=lastFailure order = %+v", resp.Devices)
	}
	if !strings.Contains(resp.Devices[0].Diag.Stats.LastError, "boom") {
		t.Fatalf("d2 lastError = %q", resp.Devices[0].Diag.Stats.LastError)
	}

	var resp2 struct {
		Devices []deviceEntry `json:"devices"`
	}
	decodeBody(t, env.do(t, http.MethodGet, "/api/devices?sort=avgDuration", eng, ""), &resp2)
	if len(resp2.Devices) != 2 || resp2.Devices[0].ID != "d1" {
		t.Fatalf("sort=avgDuration order = %+v", resp2.Devices)
	}
	if resp2.Devices[0].Diag.Stats.AvgDurationMs < 30 {
		t.Fatalf("d1 avg duration = %d, want >= 30ms", resp2.Devices[0].Diag.Stats.AvgDurationMs)
	}

	// default order preserved + diag attached
	var resp3 struct {
		Devices []deviceEntry `json:"devices"`
	}
	decodeBody(t, env.do(t, http.MethodGet, "/api/devices", eng, ""), &resp3)
	if resp3.Devices[0].ID != "d1" || resp3.Devices[1].ID != "d2" {
		t.Fatalf("default order = %+v", resp3.Devices)
	}
	if resp3.Devices[0].Diag.Stats.Total != 1 || resp3.Devices[1].Diag.Stats.FailCount != 1 {
		t.Fatalf("diag stats = %+v / %+v", resp3.Devices[0].Diag.Stats, resp3.Devices[1].Diag.Stats)
	}

	if w := env.do(t, http.MethodGet, "/api/devices?sort=bogus", eng, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("bogus sort = %d, want 400", w.Code)
	}
}
