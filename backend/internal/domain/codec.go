package domain

import (
	"encoding/binary"
	"fmt"
	"math"
)

// DecodeRegisters converts holding registers into a raw engineering number (before scale/offset).
func DecodeRegisters(pt PointDef, regs []uint16) (float64, error) {
	need := pt.RegisterCount()
	if len(regs) < need {
		return 0, fmt.Errorf("need %d registers, got %d", need, len(regs))
	}
	switch pt.Type {
	case TypeFloat32ABCD:
		b := make([]byte, 4)
		binary.BigEndian.PutUint16(b[0:2], regs[0])
		binary.BigEndian.PutUint16(b[2:4], regs[1])
		bits := binary.BigEndian.Uint32(b)
		return float64(math.Float32frombits(bits)), nil
	case TypeFloat32CDAB:
		b := make([]byte, 4)
		// CD AB: first register is low word
		binary.BigEndian.PutUint16(b[0:2], regs[1])
		binary.BigEndian.PutUint16(b[2:4], regs[0])
		bits := binary.BigEndian.Uint32(b)
		return float64(math.Float32frombits(bits)), nil
	case TypeInt16:
		return float64(int16(regs[0])), nil
	case TypeUint16:
		return float64(regs[0]), nil
	case TypeBoolBit:
		bit := 0
		if pt.Bit != nil {
			bit = *pt.Bit
		}
		if (regs[0]>>uint(bit))&1 == 1 {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("unsupported type %s", pt.Type)
	}
}

// EncodeRegisters converts a raw value (after inverse scale) into holding registers.
func EncodeRegisters(pt PointDef, raw float64, existing []uint16) ([]uint16, error) {
	switch pt.Type {
	case TypeFloat32ABCD:
		f := float32(raw)
		bits := math.Float32bits(f)
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, bits)
		return []uint16{
			binary.BigEndian.Uint16(b[0:2]),
			binary.BigEndian.Uint16(b[2:4]),
		}, nil
	case TypeFloat32CDAB:
		f := float32(raw)
		bits := math.Float32bits(f)
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, bits)
		// store as CD AB
		return []uint16{
			binary.BigEndian.Uint16(b[2:4]),
			binary.BigEndian.Uint16(b[0:2]),
		}, nil
	case TypeInt16:
		v := int16(math.Round(raw))
		return []uint16{uint16(v)}, nil
	case TypeUint16:
		if raw < 0 || raw > 65535 {
			return nil, fmt.Errorf("uint16 out of range: %v", raw)
		}
		return []uint16{uint16(math.Round(raw))}, nil
	case TypeBoolBit:
		bit := 0
		if pt.Bit != nil {
			bit = *pt.Bit
		}
		base := uint16(0)
		if len(existing) > 0 {
			base = existing[0]
		}
		if raw != 0 {
			base |= 1 << uint(bit)
		} else {
			base &^= 1 << uint(bit)
		}
		return []uint16{base}, nil
	default:
		return nil, fmt.Errorf("unsupported type %s", pt.Type)
	}
}

// FormatValue returns JSON-friendly typed value.
func FormatValue(pt PointDef, eng float64) interface{} {
	switch pt.Type {
	case TypeBoolBit:
		return eng != 0
	case TypeInt16, TypeUint16:
		return int64(math.Round(eng))
	default:
		return eng
	}
}
