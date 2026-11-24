package session

import "time"

// SetNowForTest replaces s's clock so tests can age sessions
// deterministically instead of sleeping. Production code never calls this.
func SetNowForTest(s *Store, now func() time.Time) { s.now = now }
