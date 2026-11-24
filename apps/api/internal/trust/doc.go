// Package trust derives a claim's trust state from its signal counts by
// stance: StateOf returns one of four States and Rank orders them
// (Favorable > Conflict > InsufficientEvidence > Unfavorable). The state is
// never stored as a column; neutral signals are counted but never change
// it. The view contribution_v implements the same table in SQL for
// filtering and sorting, and testdata/state_cases.json checks both.
package trust
