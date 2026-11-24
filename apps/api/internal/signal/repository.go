package signal

import "context"

type Repository interface {
	// Insert writes the signal and updates the counters in ONE transaction; an assessment replaces the active one.
	Insert(ctx context.Context, s Signal) (replaced *Stance, err error)
	// Get: ErrNotFound for an unknown or malformed id.
	Get(ctx context.Context, id string) (Signal, error)
	// Withdraw marks the signal withdrawn and recounts its contribution; a no-op when already withdrawn.
	Withdraw(ctx context.Context, id string) error
	// SetHidden hides (by = moderator, with an optional reason) or unhides the
	// signal and recounts its contribution; unhiding clears the reason.
	SetHidden(ctx context.Context, id, by, reason string, hidden bool) error
	ListByContribution(ctx context.Context, contributionID string) ([]Signal, error) // including superseded/withdrawn/hidden, by created_at
}
