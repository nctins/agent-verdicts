package trust_test

import (
	"encoding/json"
	"os"
	"testing"

	"agent-verdicts/internal/trust"
)

type stateCases struct {
	State []struct {
		MinFavorable int `json:"min_favorable"`
		Counts       struct {
			Favorable, Unfavorable, Neutral int
			FavorableActors                 int `json:"favorable_actors"`
		} `json:"counts"`
		Want string `json:"want"`
	} `json:"state"`
	Rank []string `json:"rank"`
}

func load(t *testing.T) stateCases {
	t.Helper()
	b, err := os.ReadFile("testdata/state_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var sc stateCases
	if err := json.Unmarshal(b, &sc); err != nil {
		t.Fatal(err)
	}
	return sc
}

func TestStateOf(t *testing.T) {
	for _, tc := range load(t).State {
		c := trust.Counts(tc.Counts)
		if got := trust.StateOf(c, tc.MinFavorable); string(got) != tc.Want {
			t.Errorf("%+v min %d: %s want %s", c, tc.MinFavorable, got, tc.Want)
		}
	}
}

func TestRank(t *testing.T) {
	for i, s := range load(t).Rank {
		if got := trust.Rank(trust.State(s)); got != i {
			t.Errorf("Rank(%s)=%d want %d", s, got, i)
		}
	}
}
