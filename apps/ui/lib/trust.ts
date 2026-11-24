export type TrustState = "Favorable" | "Unfavorable" | "Conflict" | "InsufficientEvidence";
export type Counts = { favorable: number; unfavorable: number; neutral: number; favorable_actors: number };

// Port of api/internal/trust/state.go; neutral never changes the state.
export function stateOf(c: Counts, minFavorable: number): TrustState {
  if (c.favorable > 0 && c.unfavorable > 0) return "Conflict";
  if (c.unfavorable === 0 && c.favorable_actors >= Math.max(minFavorable, 1)) return "Favorable";
  if (c.favorable === 0 && c.unfavorable > 0) return "Unfavorable";
  return "InsufficientEvidence";
}

const order: TrustState[] = ["Favorable", "Conflict", "InsufficientEvidence", "Unfavorable"];
// Smaller is better (Go trust.Rank).
export function rank(s: TrustState): number {
  const i = order.indexOf(s);
  return i < 0 ? order.length - 1 : i;
}
