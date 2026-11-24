package search

import (
	"cmp"
	"slices"
)

const rrfK = 60

// Fuse merges two ranked id lists with weighted reciprocal rank fusion.
// A duplicate id inside one list counts only at its first rank; ties keep
// first-appearance order (lex list scanned first). Every id appears once.
func Fuse(lex, vec []string, wLex, wVec float64) []string {
	score := map[string]float64{}
	var order []string
	add := func(list []string, w float64) {
		seen := map[string]bool{}
		for i, id := range list {
			if seen[id] {
				continue
			}
			seen[id] = true
			if _, ok := score[id]; !ok {
				order = append(order, id)
			}
			score[id] += w / float64(rrfK+i+1)
		}
	}
	add(lex, wLex)
	add(vec, wVec)
	// stable: ties keep first-appearance order
	slices.SortStableFunc(order, func(a, b string) int { return cmp.Compare(score[b], score[a]) })
	return order
}
