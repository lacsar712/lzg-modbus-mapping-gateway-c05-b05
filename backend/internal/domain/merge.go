package domain

import "sort"

// RegRange is a contiguous holding-register read window.
type RegRange struct {
	Start uint16
	Count uint16
}

// MergeRanges merges overlapping/adjacent register windows (gap <= maxGap).
func MergeRanges(ranges []RegRange, maxGap uint16) []RegRange {
	if len(ranges) == 0 {
		return nil
	}
	cp := append([]RegRange(nil), ranges...)
	sort.Slice(cp, func(i, j int) bool {
		if cp[i].Start == cp[j].Start {
			return cp[i].Count > cp[j].Count
		}
		return cp[i].Start < cp[j].Start
	})
	out := []RegRange{cp[0]}
	for _, r := range cp[1:] {
		last := &out[len(out)-1]
		lastEnd := uint32(last.Start) + uint32(last.Count)
		start := uint32(r.Start)
		end := start + uint32(r.Count)
		if start <= lastEnd+uint32(maxGap) {
			if end > lastEnd {
				last.Count = uint16(end - uint32(last.Start))
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// BuildPointRanges creates per-point register windows.
func BuildPointRanges(points []PointDef) []RegRange {
	ranges := make([]RegRange, 0, len(points))
	for _, p := range points {
		ranges = append(ranges, RegRange{
			Start: p.Address,
			Count: uint16(p.RegisterCount()),
		})
	}
	return ranges
}
