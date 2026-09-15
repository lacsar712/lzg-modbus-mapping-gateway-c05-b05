package usecase

import (
	"fmt"
	"sync"
	"time"

	"github.com/bytecode/modbus-mapping-gateway/internal/domain"
	"github.com/bytecode/modbus-mapping-gateway/internal/port"
)

type GatewayService struct {
	store  port.MappingStore
	modbus port.ModbusClient

	mu              sync.RWMutex
	cfg             domain.MappingConfig
	yamlText        string
	lastModbusError string
	lastErrorAt     time.Time
}

func NewGatewayService(store port.MappingStore, modbus port.ModbusClient) (*GatewayService, error) {
	s := &GatewayService{store: store, modbus: modbus}
	if err := s.Reload(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *GatewayService) Reload() error {
	cfg, text, err := s.store.Load()
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid mapping: %w", err)
	}
	// normalize timeout
	for i := range cfg.Devices {
		if cfg.Devices[i].TimeoutMs <= 0 {
			cfg.Devices[i].TimeoutMs = 2000
		}
		for j := range cfg.Devices[i].Points {
			cfg.Devices[i].Points[j] = cfg.Devices[i].Points[j].Normalize()
		}
	}
	s.mu.Lock()
	s.cfg = cfg
	s.yamlText = text
	s.mu.Unlock()
	return nil
}

func (s *GatewayService) ReloadFromText(text string) error {
	// try parse+validate without destroying current cfg on failure
	tmpStore := &memStore{text: text}
	cfg, _, err := tmpStore.Load()
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid mapping: %w", err)
	}
	if err := s.store.Save(text); err != nil {
		return err
	}
	return s.Reload()
}

type memStore struct{ text string }

func (m *memStore) Load() (domain.MappingConfig, string, error) {
	return parseYAML(m.text)
}
func (m *memStore) Save(string) error { return nil }
func (m *memStore) Path() string      { return "memory" }

func (s *GatewayService) Config() domain.MappingConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *GatewayService) YAMLText() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.yamlText
}

func (s *GatewayService) Health() domain.HealthStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := domain.HealthStatus{
		Status:        "ok",
		MappingLoaded: len(s.cfg.Devices) > 0,
		DeviceCount:   len(s.cfg.Devices),
	}
	if s.lastModbusError != "" {
		st.LastModbusError = s.lastModbusError
		st.LastErrorAt = s.lastErrorAt.Format(time.RFC3339)
	}
	return st
}

func (s *GatewayService) recordErr(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	s.lastModbusError = err.Error()
	s.lastErrorAt = time.Now()
	s.mu.Unlock()
}

func (s *GatewayService) ListDevices() []domain.DeviceDef {
	cfg := s.Config()
	out := make([]domain.DeviceDef, len(cfg.Devices))
	copy(out, cfg.Devices)
	return out
}

func (s *GatewayService) GetPoints(deviceID string) ([]domain.PointDef, error) {
	cfg := s.Config()
	d, err := cfg.FindDevice(deviceID)
	if err != nil {
		return nil, err
	}
	return d.Points, nil
}

func (s *GatewayService) GetPoint(deviceID, name string) (*domain.DeviceDef, *domain.PointDef, error) {
	return s.Config().FindPoint(deviceID, name)
}

func (s *GatewayService) Snapshot(deviceID string) (*domain.Snapshot, error) {
	cfg := s.Config()
	d, err := cfg.FindDevice(deviceID)
	if err != nil {
		return nil, err
	}
	ranges := domain.MergeRanges(domain.BuildPointRanges(d.Points), 4)
	regMap := map[uint16]uint16{}
	for _, r := range ranges {
		vals, err := s.modbus.ReadHoldingRegisters(d.Endpoint, d.UnitID, d.TimeoutMs, r.Start, r.Count)
		if err != nil {
			s.recordErr(err)
			return nil, fmt.Errorf("modbus read %d/%d: %w", r.Start, r.Count, err)
		}
		for i, v := range vals {
			regMap[r.Start+uint16(i)] = v
		}
	}

	snap := &domain.Snapshot{DeviceID: deviceID, Points: make([]domain.PointValue, 0, len(d.Points))}
	for _, p := range d.Points {
		pv := domain.PointValue{
			Name:     p.Name,
			Address:  p.Address,
			Type:     p.Type,
			Writable: p.Writable,
			Quality:  "good",
		}
		raw := make([]uint16, p.RegisterCount())
		ok := true
		for i := 0; i < p.RegisterCount(); i++ {
			v, exists := regMap[p.Address+uint16(i)]
			if !exists {
				ok = false
				break
			}
			raw[i] = v
		}
		if !ok {
			pv.Quality = "bad"
			pv.Error = "missing registers"
			snap.Points = append(snap.Points, pv)
			continue
		}
		pv.Raw = raw
		decoded, err := domain.DecodeRegisters(p, raw)
		if err != nil {
			pv.Quality = "bad"
			pv.Error = err.Error()
			snap.Points = append(snap.Points, pv)
			continue
		}
		eng := domain.ApplyScale(decoded, p.Scale, p.Offset)
		pv.Value = domain.FormatValue(p, eng)
		snap.Points = append(snap.Points, pv)
	}
	return snap, nil
}

func (s *GatewayService) WritePoint(deviceID, name string, engValue float64) error {
	d, p, err := s.GetPoint(deviceID, name)
	if err != nil {
		return err
	}
	if !p.Writable {
		return fmt.Errorf("point %s is read-only", name)
	}
	if err := domain.CheckMinMax(engValue, p.Min, p.Max); err != nil {
		return err
	}
	raw, err := domain.InvertScale(engValue, p.Scale, p.Offset)
	if err != nil {
		return err
	}

	var existing []uint16
	if p.Type == domain.TypeBoolBit {
		regs, err := s.modbus.ReadHoldingRegisters(d.Endpoint, d.UnitID, d.TimeoutMs, p.Address, 1)
		if err != nil {
			s.recordErr(err)
			return err
		}
		existing = regs
	}

	regs, err := domain.EncodeRegisters(*p, raw, existing)
	if err != nil {
		return err
	}
	if len(regs) == 1 {
		if err := s.modbus.WriteSingleRegister(d.Endpoint, d.UnitID, d.TimeoutMs, p.Address, regs[0]); err != nil {
			s.recordErr(err)
			return err
		}
		return nil
	}
	if err := s.modbus.WriteMultipleRegisters(d.Endpoint, d.UnitID, d.TimeoutMs, p.Address, regs); err != nil {
		s.recordErr(err)
		return err
	}
	return nil
}
