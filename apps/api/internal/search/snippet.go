package search

import (
	"strings"
	"unicode"

	"agent-verdicts/internal/platform/textfold"
)

// Snippet returns a window of about max runes (ellipses not counted) around
// the earliest occurrence of any already-folded term; with no match it returns
// the head of text. Matching runs on the folded text, but the window is cut
// from the original, so NFD input (where Fold drops combining marks) works.
func Snippet(text string, foldedTerms []string, max int) string {
	text = strings.TrimSpace(text)
	r := []rune(text)
	n := len(r)
	if max <= 0 || n == 0 {
		return ""
	}
	if n <= max {
		return text
	}

	// Fold rune by rune: a combining mark folds to nothing, others to one rune.
	var folded []rune
	var orig []int // folded index -> original index
	for i, c := range r {
		for _, f := range textfold.Fold(string(c)) {
			folded = append(folded, f)
			orig = append(orig, i)
		}
	}

	mStart, mEnd := 0, 0 // match in original rune indexes
	best := -1
	for _, t := range foldedTerms {
		tr := []rune(t)
		if len(tr) == 0 {
			continue
		}
		if p := indexRunes(folded, tr); p >= 0 && (best < 0 || p < best) {
			best = p
			mStart = orig[p]
			if p+len(tr) < len(orig) {
				mEnd = orig[p+len(tr)]
			} else {
				mEnd = n
			}
		}
	}

	start := mStart - (max-(mEnd-mStart))/2
	start = max2(0, min2(start, n-max))
	end := min2(n, start+max)

	// Snap to whitespace so words are not cut, without losing the match.
	if start > 0 && !unicode.IsSpace(r[start-1]) && !unicode.IsSpace(r[start]) {
		for j := start; j < mStart; j++ {
			if unicode.IsSpace(r[j]) {
				start = j + 1
				break
			}
		}
	}
	if end < n && !unicode.IsSpace(r[end]) && !unicode.IsSpace(r[end-1]) {
		for k := end - 1; k >= mEnd && k > start; k-- {
			if unicode.IsSpace(r[k]) {
				end = k
				break
			}
		}
	}

	out := strings.TrimSpace(string(r[start:end]))
	if start > 0 {
		out = "…" + out
	}
	if end < n {
		out += "…"
	}
	return out
}

func indexRunes(h, n []rune) int {
	for i := 0; i+len(n) <= len(h); i++ {
		ok := true
		for j := range n {
			if h[i+j] != n[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max2(a, b int) int {
	if a > b {
		return a
	}
	return b
}
