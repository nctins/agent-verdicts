package trust

// State is one of the four mutually exclusive trust states of a claim.
type State string

const (
	StateFavorable            State = "Favorable"
	StateUnfavorable          State = "Unfavorable"
	StateConflict             State = "Conflict"
	StateInsufficientEvidence State = "InsufficientEvidence"
)

// Counts are a contribution's signal counts by stance; FavorableActors is
// the number of distinct actors with an active favorable signal.
type Counts struct{ Favorable, Unfavorable, Neutral, FavorableActors int }

// StateOf applies the trust table ("Trust state" in docs/architecture.md;
// SQL twin: contribution_v, migration 0007). Neutral never affects the
// state; minFavorable < 1 counts as 1.
func StateOf(c Counts, minFavorable int) State {
	switch {
	case c.Favorable > 0 && c.Unfavorable > 0:
		return StateConflict
	case c.Unfavorable == 0 && c.FavorableActors >= max(minFavorable, 1):
		return StateFavorable
	case c.Favorable == 0 && c.Unfavorable > 0:
		return StateUnfavorable
	}
	return StateInsufficientEvidence
}

// Rank: lower is better.
func Rank(s State) int {
	switch s {
	case StateFavorable:
		return 0
	case StateConflict:
		return 1
	case StateInsufficientEvidence:
		return 2
	}
	return 3
}
