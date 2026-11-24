// Package reads counts how often agents read contributions over MCP (spec
// 2026-11-21-claim-read-counts): per-actor daily detail for a project's
// DetailDays, then monthly totals kept forever. Only project managers see
// the counts; they are not evidence that a claim is right.
package reads

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Kind is what the read was.
type Kind int16

const (
	Observe   Kind = 1 // observe_trust_signal: about to rely on it
	Reference Kind = 2 // reference_contribution: opened it
	SearchHit Kind = 3 // shown in search results; no actor is kept
)

// Bounds and default of a project's detail retention, in days.
const (
	MinDetailDays     = 30
	MaxDetailDays     = 3650
	DefaultDetailDays = 180
)

// BytesPerRow is the size of one daily row with its indexes, for the size
// estimate: 169 measured on 808k rows (2026-11-21).
const BytesPerRow = 170

// Timeout bounds recording, so a slow write never fails or stalls a read.
const Timeout = 200 * time.Millisecond

var ErrInvalidArgument = errors.New("invalid argument")

// ValidateDetailDays checks a retention a manager asked for.
func ValidateDetailDays(days int) error {
	if days < MinDetailDays || days > MaxDetailDays {
		return fmt.Errorf("%w: detail_days must be %d-%d", ErrInvalidArgument, MinDetailDays, MaxDetailDays)
	}
	return nil
}

// DetailFrom is the oldest day that still has daily detail once the rollup
// has run: whole months older than today-days are rolled up.
func DetailFrom(today time.Time, days int) time.Time {
	d := today.AddDate(0, 0, -days)
	return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// Count is reads and distinct readers of one kind.
type Count struct{ Reads, Readers int }

// Item is one contribution that was read in the period.
type Item struct {
	ID, Title, Kind    string
	State              *string // nil for questions
	Superseded         bool
	Observe, Reference Count
	SearchHits         int
}

// Period is the reads of every contribution on one day (or month).
type Period struct {
	Start                         time.Time
	Observe, Reference, SearchHit int
}

// Stats is a project's reads in [From, To]. Readers include monthly totals
// for months before the daily detail, so they are an upper bound there.
type Stats struct {
	DetailDays int
	Daily      []Period // days in range that still have detail
	Monthly    []Period // rolled-up months overlapping the range
	Items      []Item   // most observed first
	NeverRead  int      // live claims (not superseded, not hidden) with no observe or reference
}

// Retention is a project's detail retention and the size it implies.
type Retention struct {
	DetailDays     int
	RowsPerDay     float64 // daily rows written, averaged over the last 30 days
	EstimatedBytes int64
}

type Repository interface {
	// Record adds one read of kind k by actor to each id (an id may repeat);
	// ids outside the project or written by actor are skipped. One statement.
	Record(ctx context.Context, projectID, actorID string, k Kind, ids []string) error
	// Rollup moves whole months older than each project's detail retention
	// into monthly totals, in one transaction. today is a UTC date.
	Rollup(ctx context.Context, today time.Time) error
	Stats(ctx context.Context, projectID string, from, to time.Time) (Stats, error)
	// SetDetailDays stores a validated retention; today anchors the 30-day average.
	SetDetailDays(ctx context.Context, projectID string, days int, today time.Time) (Retention, error)
}
