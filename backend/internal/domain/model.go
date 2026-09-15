package domain

import (
	"fmt"
	"math"
)

type PointType string

const (
	TypeFloat32ABCD PointType = "float32_abcd"
	TypeFloat32CDAB PointType = "float32_cdab"
	TypeInt16       PointType = "int16"
	TypeUint16      PointType = "uint16"
	TypeBoolBit     PointType = "bool_bit"
)

type PointDef struct {
	Name     string    `yaml:"name" json:"name"`
	Address  uint16    `yaml:"address" json:"address"`
	Type     PointType `yaml:"type" json:"type"`
	Bit      *int      `yaml:"bit,omitempty" json:"bit,omitempty"`
	Writable bool      `yaml:"writable" json:"writable"`
	Scale    float64   `yaml:"scale" json:"scale"`
	Offset   float64   `yaml:"offset" json:"offset"`
	Min      *float64  `yaml:"min,omitempty" json:"min,omitempty"`
	Max      *float64  `yaml:"max,omitempty" json:"max,omitempty"`
}

func (p PointDef) RegisterCount() int {
	switch p.Type {
	case TypeFloat32ABCD, TypeFloat32CDAB:
		return 2
	default:
		return 1
	}
}

func (p PointDef) Normalize() PointDef {
	if p.Scale == 0 {
		p.Scale = 1
	}
	return p
}

func (p PointDef) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("point name required")
	}
	switch p.Type {
	case TypeFloat32ABCD, TypeFloat32CDAB, TypeInt16, TypeUint16, TypeBoolBit:
	default:
		return fmt.Errorf("point %s: unsupported type %s", p.Name, p.Type)
	}
	if p.Type == TypeBoolBit {
		if p.Bit == nil || *p.Bit < 0 || *p.Bit > 15 {
			return fmt.Errorf("point %s: bool_bit requires bit 0-15", p.Name)
		}
	}
	return nil
}

type DeviceDef struct {
	ID        string     `yaml:"id" json:"id"`
	Name      string     `yaml:"name" json:"name"`
	Endpoint  string     `yaml:"endpoint" json:"endpoint"`
	UnitID    byte       `yaml:"unitId" json:"unitId"`
	TimeoutMs int        `yaml:"timeoutMs" json:"timeoutMs"`
	Points    []PointDef `yaml:"points" json:"points"`
}

func (d DeviceDef) Validate() error {
	if d.ID == "" {
		return fmt.Errorf("device id required")
	}
	if d.Endpoint == "" {
		return fmt.Errorf("device %s: endpoint required", d.ID)
	}
	if d.TimeoutMs <= 0 {
		d.TimeoutMs = 2000
	}
	names := map[string]struct{}{}
	for i := range d.Points {
		d.Points[i] = d.Points[i].Normalize()
		if err := d.Points[i].Validate(); err != nil {
			return err
		}
		if _, ok := names[d.Points[i].Name]; ok {
			return fmt.Errorf("device %s: duplicate point %s", d.ID, d.Points[i].Name)
		}
		names[d.Points[i].Name] = struct{}{}
	}
	return nil
}

type MappingConfig struct {
	Devices []DeviceDef `yaml:"devices" json:"devices"`
}

func (c MappingConfig) Validate() error {
	if len(c.Devices) == 0 {
		return fmt.Errorf("at least one device required")
	}
	ids := map[string]struct{}{}
	for i := range c.Devices {
		if err := c.Devices[i].Validate(); err != nil {
			return err
		}
		if _, ok := ids[c.Devices[i].ID]; ok {
			return fmt.Errorf("duplicate device id %s", c.Devices[i].ID)
		}
		ids[c.Devices[i].ID] = struct{}{}
		if c.Devices[i].TimeoutMs <= 0 {
			c.Devices[i].TimeoutMs = 2000
		}
	}
	return nil
}

func (c MappingConfig) FindDevice(id string) (*DeviceDef, error) {
	for i := range c.Devices {
		if c.Devices[i].ID == id {
			d := c.Devices[i]
			return &d, nil
		}
	}
	return nil, fmt.Errorf("device not found: %s", id)
}

func (c MappingConfig) FindPoint(deviceID, name string) (*DeviceDef, *PointDef, error) {
	d, err := c.FindDevice(deviceID)
	if err != nil {
		return nil, nil, err
	}
	for i := range d.Points {
		if d.Points[i].Name == name {
			p := d.Points[i]
			return d, &p, nil
		}
	}
	return nil, nil, fmt.Errorf("point not found: %s/%s", deviceID, name)
}

// ApplyScale: engineering = raw * scale + offset
func ApplyScale(raw float64, scale, offset float64) float64 {
	if scale == 0 {
		scale = 1
	}
	return raw*scale + offset
}

// InvertScale: raw = (eng - offset) / scale
func InvertScale(eng, scale, offset float64) (float64, error) {
	if scale == 0 {
		return 0, fmt.Errorf("scale must not be 0")
	}
	return (eng - offset) / scale, nil
}

func CheckMinMax(eng float64, min, max *float64) error {
	if min != nil && eng < *min {
		return fmt.Errorf("value %.4f below min %.4f", eng, *min)
	}
	if max != nil && eng > *max {
		return fmt.Errorf("value %.4f above max %.4f", eng, *max)
	}
	if math.IsNaN(eng) || math.IsInf(eng, 0) {
		return fmt.Errorf("invalid numeric value")
	}
	return nil
}

type PointValue struct {
	Name     string      `json:"name"`
	Address  uint16      `json:"address"`
	Type     PointType   `json:"type"`
	Writable bool        `json:"writable"`
	Value    interface{} `json:"value"`
	Raw      []uint16    `json:"raw,omitempty"`
	Quality  string      `json:"quality"`
	Error    string      `json:"error,omitempty"`
}

type Snapshot struct {
	DeviceID string       `json:"deviceId"`
	Points   []PointValue `json:"points"`
}

type HealthStatus struct {
	Status          string `json:"status"`
	MappingLoaded   bool   `json:"mappingLoaded"`
	DeviceCount     int    `json:"deviceCount"`
	LastModbusError string `json:"lastModbusError,omitempty"`
	LastErrorAt     string `json:"lastErrorAt,omitempty"`
}
