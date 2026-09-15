package domain

import (
	"math"
	"testing"
)

func TestFloat32ABCDRoundTrip(t *testing.T) {
	pt := PointDef{Type: TypeFloat32ABCD}
	regs, err := EncodeRegisters(pt, 1500.0, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeRegisters(pt, regs)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-1500.0) > 1e-3 {
		t.Fatalf("got %v want 1500", got)
	}
}

func TestFloat32CDABRoundTrip(t *testing.T) {
	pt := PointDef{Type: TypeFloat32CDAB}
	regs, err := EncodeRegisters(pt, 36.5, nil)
	if err != nil {
		t.Fatal(err)
	}
	// CDAB encoding should differ from ABCD word order
	abcd, _ := EncodeRegisters(PointDef{Type: TypeFloat32ABCD}, 36.5, nil)
	if regs[0] == abcd[0] && regs[1] == abcd[1] {
		t.Fatal("cdab should word-swap relative to abcd")
	}
	got, err := DecodeRegisters(pt, regs)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-36.5) > 1e-3 {
		t.Fatalf("got %v want 36.5", got)
	}
}

func TestInt16Uint16Bool(t *testing.T) {
	i16 := PointDef{Type: TypeInt16}
	regs, _ := EncodeRegisters(i16, -5, nil)
	v, _ := DecodeRegisters(i16, regs)
	if v != -5 {
		t.Fatalf("int16 got %v", v)
	}

	u16 := PointDef{Type: TypeUint16}
	regs, _ = EncodeRegisters(u16, 0x00A5, nil)
	v, _ = DecodeRegisters(u16, regs)
	if v != 0x00A5 {
		t.Fatalf("uint16 got %v", v)
	}

	bit := 0
	bb := PointDef{Type: TypeBoolBit, Bit: &bit}
	regs, _ = EncodeRegisters(bb, 1, []uint16{0})
	v, _ = DecodeRegisters(bb, regs)
	if v != 1 {
		t.Fatalf("bool got %v", v)
	}
	regs, _ = EncodeRegisters(bb, 0, regs)
	v, _ = DecodeRegisters(bb, regs)
	if v != 0 {
		t.Fatalf("bool clear got %v", v)
	}
}

func TestScaleOffset(t *testing.T) {
	eng := ApplyScale(120, 0.1, 0)
	if math.Abs(eng-12) > 1e-9 {
		t.Fatalf("apply got %v", eng)
	}
	raw, err := InvertScale(12, 0.1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(raw-120) > 1e-9 {
		t.Fatalf("invert got %v", raw)
	}
	minV, maxV := 0.0, 50.0
	if err := CheckMinMax(12, &minV, &maxV); err != nil {
		t.Fatal(err)
	}
	if err := CheckMinMax(60, &minV, &maxV); err == nil {
		t.Fatal("expected max error")
	}
}
