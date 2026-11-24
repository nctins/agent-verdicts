package main

import (
	"encoding/json"
	"math"
	"testing"
)

func TestScore(t *testing.T) {
	cases := []struct {
		name         string
		ranked, gold []string
		r1, r5, mrr  float64
	}{
		{"gold first", []string{"a", "b", "c"}, []string{"a"}, 1, 1, 1},
		{"gold third", []string{"x", "y", "a", "z"}, []string{"a"}, 0, 1, 1.0 / 3},
		{"gold sixth: outside top 5", []string{"1", "2", "3", "4", "5", "a"}, []string{"a"}, 0, 0, 1.0 / 6},
		{"missing", []string{"x", "y"}, []string{"a"}, 0, 0, 0},
		{"empty ranking", nil, []string{"a"}, 0, 0, 0},
		// two gold: one at rank 2, one outside top 5 → r5 = 1/2, mrr from the first hit
		{"two gold, one found", []string{"x", "b", "y", "z", "w", "v", "a"}, []string{"a", "b"}, 0, 0.5, 0.5},
		{"two gold, both found", []string{"b", "a"}, []string{"a", "b"}, 1, 1, 1},
	}
	for _, c := range cases {
		r1, r5, mrr := score(c.ranked, c.gold)
		if !near(r1, c.r1) || !near(r5, c.r5) || !near(mrr, c.mrr) {
			t.Errorf("%s: score = (%v, %v, %v), want (%v, %v, %v)", c.name, r1, r5, mrr, c.r1, c.r5, c.mrr)
		}
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// The embedded dataset must keep the shape and minimums of spec §11.
func TestDatasetShape(t *testing.T) {
	var d dataset
	if err := json.Unmarshal(datasetJSON, &d); err != nil {
		t.Fatal(err)
	}
	if d.Note == "" {
		t.Error("missing note")
	}
	ids := map[string]contrib{}
	long := 0
	for _, c := range d.Contributions {
		if _, dup := ids[c.ID]; dup {
			t.Errorf("duplicate id %s", c.ID)
		}
		ids[c.ID] = c
		if n := len(c.Body); n >= 8000 && n <= 30000 {
			long++
		}
	}
	vivi := 0
	for _, q := range d.Queries {
		switch q.Kind {
		case "K", "P", "X", "T":
		default:
			t.Errorf("%q: kind %q", q.Query, q.Kind)
		}
		if len(q.GoldIDs) == 0 {
			t.Errorf("%q: no gold", q.Query)
		}
		for _, g := range q.GoldIDs {
			if _, ok := ids[g]; !ok {
				t.Errorf("%q: unknown gold %s", q.Query, g)
			}
		}
		if isViVi(q, ids) {
			vivi++
		}
	}
	if len(d.Queries) < 90 || vivi < 30 || long < 5 {
		t.Errorf("queries=%d vi→vi=%d long=%d; want ≥90, ≥30, ≥5", len(d.Queries), vivi, long)
	}
}
