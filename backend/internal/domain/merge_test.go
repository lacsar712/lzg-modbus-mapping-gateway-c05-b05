package domain

import "testing"

func TestMergeRangesAdjacent(t *testing.T) {
	in := []RegRange{
		{Start: 0, Count: 2},
		{Start: 2, Count: 1},
		{Start: 10, Count: 2},
		{Start: 20, Count: 1},
	}
	out := MergeRanges(in, 0)
	if len(out) != 3 {
		t.Fatalf("want 3 ranges, got %#v", out)
	}
	if out[0].Start != 0 || out[0].Count != 3 {
		t.Fatalf("first merge %#v", out[0])
	}
}

func TestMergeRangesWithGap(t *testing.T) {
	in := []RegRange{
		{Start: 0, Count: 2},
		{Start: 3, Count: 1},
	}
	out := MergeRanges(in, 1)
	if len(out) != 1 || out[0].Count != 4 {
		t.Fatalf("gap merge %#v", out)
	}
}

func TestBuildPointRanges(t *testing.T) {
	pts := []PointDef{
		{Address: 0, Type: TypeFloat32ABCD},
		{Address: 10, Type: TypeInt16},
	}
	r := BuildPointRanges(pts)
	if len(r) != 2 || r[0].Count != 2 || r[1].Count != 1 {
		t.Fatalf("%#v", r)
	}
}
