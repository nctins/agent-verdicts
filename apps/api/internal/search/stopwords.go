package search

import (
	"strings"
	"unicode"
)

// viStopwords are Vietnamese function words in folded form (textfold.Fold),
// dropped from the lexical query so they cannot outrank the words that carry
// the meaning (spec 2026-11-15-role-play-fixes §A.2). "bao" is dropped only
// in "bao nhieu" (how much): on its own it is also bảo/báo. Left out on
// purpose because the folded form is also a content word: da (đã, but also
// đa/đá), the (thế, but also thẻ/thể), ma (mà, but also mã).
//
// ponytail: a fixed list matched on folded text, so it cannot tell là from lá
// or có from cơ, and it only cleans the query: documents still index these
// words. Ceiling: a query made of such a word plus stopwords loses that word.
// Upgrade path: a stopword filter in the index tokenizer (needs a reindex).
var viStopwords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields("la thi va cua cho cac nhung mot khi neu voi trong nhieu gi nao khong co duoc nay o se nhu hay hoac rat") {
		m[w] = true
	}
	return m
}()

// stripStopwords removes viStopwords from a folded query, splitting on what
// the index tokenizer splits on (anything but letters and digits). A query
// with nothing else in it is returned unchanged.
func stripStopwords(folded string) string {
	words := strings.FieldsFunc(folded, notWord)
	var keep []string
	for i, w := range words {
		if !stopwordAt(words, i) {
			keep = append(keep, w)
		}
	}
	if len(keep) == 0 {
		return folded
	}
	return strings.Join(keep, " ")
}

func notWord(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }

// stopwordAt reports whether folded[i] is dropped from the query.
func stopwordAt(folded []string, i int) bool {
	return viStopwords[folded[i]] || (folded[i] == "bao" && i+1 < len(folded) && folded[i+1] == "nhieu")
}
