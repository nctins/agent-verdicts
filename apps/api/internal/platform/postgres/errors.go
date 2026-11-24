package postgres

import (
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// isNoRow reports whether err means "no such row": no rows, or a key that
// cannot name any row: not a valid uuid (22P02), or not even encodable as
// text (22021: NUL byte or invalid UTF-8). The domain rejects bad free text
// itself (textfold.Valid); this is the backstop for ids and keys.
func isNoRow(err error) bool {
	c := pgCode(err)
	return errors.Is(err, pgx.ErrNoRows) || c == "22P02" || c == "22021"
}

func isUniqueViolation(err error) bool { return pgCode(err) == "23505" }

// utcPtr normalizes an optional timestamp read from the database to UTC.
func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
