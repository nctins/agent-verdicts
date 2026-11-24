package search

import (
	"context"
	"errors"
	"strings"

	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/platform/textfold"

	"golang.org/x/text/unicode/norm"
)

// The BM25 index stores folded text, so a query word also matches every word
// that only differs by its accents ("biến" finds "biện"). A query word typed
// WITH accents is therefore checked against the original text; one typed
// without accents keeps matching every accent variant (spec
// 2026-11-15-role-play-fixes §C5).

// queryToken is one query word after stopword removal: as typed (lower-case,
// NFC) and folded. The two differ exactly when the word carries an accent.
type queryToken struct{ exact, folded string }

// exactWords splits s into lower-case NFC words on the index tokenizer's
// boundaries, so NFD and NFC spellings of a word compare equal.
func exactWords(s string) []string {
	return strings.FieldsFunc(norm.NFC.String(strings.ToLower(s)), notWord)
}

// queryTokens is the query's words without stopwords (all of them when it
// has nothing else, like the lexical query), and whether any carries an accent.
func queryTokens(text string) (toks []queryToken, accented bool) {
	words := exactWords(text)
	folded := make([]string, len(words))
	for i, w := range words {
		folded[i] = textfold.Fold(w)
	}
	for pass := 0; pass < 2 && len(toks) == 0; pass++ { // second pass keeps the stopwords
		for i, w := range words {
			if pass == 0 && stopwordAt(folded, i) {
				continue
			}
			toks = append(toks, queryToken{w, folded[i]})
			accented = accented || w != folded[i]
		}
	}
	return toks, accented
}

// matchesExactly reports whether c contains at least one query token: an
// accented token as that very word, an unaccented one in any accent variant.
func matchesExactly(c contribution.Contribution, toks []queryToken) bool {
	exact, folded := map[string]bool{}, map[string]bool{}
	for _, w := range exactWords(c.Title + "\n" + c.Body) {
		exact[w], folded[textfold.Fold(w)] = true, true
	}
	for _, t := range toks {
		if (t.exact != t.folded && exact[t.exact]) || (t.exact == t.folded && folded[t.folded]) {
			return true
		}
	}
	return false
}

// exactFirst reorders the lexical hits of a query that has accented words:
// hits matching no query token exactly go last (BM25 order otherwise kept),
// or are dropped when drop is set. A query without accents is returned as
// is, with nothing loaded. loaded holds the contributions it had to read, so
// Search does not read them again.
//
// ponytail: it only looks at the BM25 candidates already fetched, so an
// exact match ranked below them by BM25 is never promoted into the list, and
// it reads each candidate (one Get per hit, up to `candidates`) for every
// accented query. An unaccented token is matched on whole words, without the
// index's English stemmer. Upgrade path: a second, accent-preserving indexed
// field, so the index itself scores exact matches.
func (s *Service) exactFirst(ctx context.Context, q Query, ids []string) (ordered []string, loaded map[string]contribution.Contribution, err error) {
	toks, accented := queryTokens(q.Text)
	if !accented {
		return ids, nil, nil
	}
	loaded = make(map[string]contribution.Contribution, len(ids))
	var loose []string
	for _, id := range ids {
		c, err := s.d.Contributions.Get(ctx, q.ProjectID, id)
		if errors.Is(err, contribution.ErrNotFound) {
			continue // deleted since indexing
		}
		if err != nil {
			return nil, nil, err
		}
		loaded[id] = c
		if matchesExactly(c, toks) {
			ordered = append(ordered, id)
		} else if !q.DropAccentMismatch {
			loose = append(loose, id)
		}
	}
	return append(ordered, loose...), loaded, nil
}
