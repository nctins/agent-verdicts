package search

import "testing"

func TestStripStopwords(t *testing.T) {
	for in, want := range map[string]string{
		"pool bao nhieu thi du khi tai cao": "pool du tai cao",
		"la bao nhieu":                      "la bao nhieu", // nothing left: the query is kept
		"timeout cua http client la gi?":    "timeout http client",
		"refund approver":                   "refund approver",
	} {
		if got := stripStopwords(in); got != want {
			t.Errorf("stripStopwords(%q) = %q, want %q", in, got, want)
		}
	}
}
