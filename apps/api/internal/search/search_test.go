package search_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"agent-verdicts/internal/platform/textfold"
	"agent-verdicts/internal/search"
)

func uniqueSentences(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "Sentence number %d is here. ", i)
	}
	return strings.TrimSpace(b.String())
}

// merge joins chunks by the longest suffix/prefix overlap (text must be unique enough).
func merge(chunks []string) (string, []int) {
	out := []rune(chunks[0])
	var overlaps []int
	for _, c := range chunks[1:] {
		cr := []rune(c)
		best := 0
		for k := min(len(out), len(cr)); k > 0; k-- {
			if slices.Equal(out[len(out)-k:], cr[:k]) {
				best = k
				break
			}
		}
		overlaps = append(overlaps, best)
		out = append(out, cr[best:]...)
	}
	return string(out), overlaps
}

func TestChunkSentences(t *testing.T) {
	body := uniqueSentences(500) // ~13k runes
	title := "My title"
	chunks := search.Chunk(title, body)
	if len(chunks) < 2 {
		t.Fatalf("want several chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if n := utf8.RuneCountInString(c); n > 1800 {
			t.Errorf("chunk %d has %d runes", i, n)
		}
	}
	got, overlaps := merge(chunks)
	if got != title+"\n\n"+body {
		t.Fatalf("reconstruction differs")
	}
	for i, o := range overlaps {
		if o < 1 {
			t.Errorf("chunk %d/%d share nothing", i, i+1)
		}
	}
	if strings.Contains(strings.Join(chunks[1:], ""), title+"\n\n") {
		t.Error("title must prefix first chunk only")
	}
}

func TestChunkHardCut(t *testing.T) {
	var rs []rune
	for i := 0; i < 10000; i++ {
		rs = append(rs, rune('a'+(i*7+i/13)%26), rune(0x4e00+i%500)) // no whitespace, non-repeating
	}
	body := string(rs)
	chunks := search.Chunk("t", body)
	for i, c := range chunks {
		if n := utf8.RuneCountInString(c); n > 1800 {
			t.Errorf("chunk %d has %d runes", i, n)
		}
	}
	got, overlaps := merge(chunks)
	if got != "t\n\n"+body {
		t.Fatal("reconstruction differs")
	}
	for _, o := range overlaps {
		if o < 200 {
			t.Errorf("overlap %d < 200", o)
		}
	}
}

func TestChunkSmallAndEmpty(t *testing.T) {
	if got := search.Chunk("T", ""); !slices.Equal(got, []string{"T"}) {
		t.Errorf("empty body: %q", got)
	}
	if got := search.Chunk("T", " \n\t "); !slices.Equal(got, []string{"T"}) {
		t.Errorf("blank body: %q", got)
	}
	if got := search.Chunk("T", "hello. world"); !slices.Equal(got, []string{"T\n\nhello. world"}) {
		t.Errorf("small: %q", got)
	}
}

func TestFuse(t *testing.T) {
	got := search.Fuse([]string{"a", "b"}, []string{"b", "c"}, 0.5, 1.0)
	if !slices.Equal(got, []string{"b", "c", "a"}) { // b: .5/62+1/61; c: 1/62; a: .5/61
		t.Errorf("got %v", got)
	}
	// ties: lex scanned first
	got = search.Fuse([]string{"x"}, []string{"y"}, 1, 1)
	if !slices.Equal(got, []string{"x", "y"}) {
		t.Errorf("tie: %v", got)
	}
	// duplicates count at first rank only; every id exactly once
	got = search.Fuse([]string{"a", "a", "b"}, nil, 1, 1)
	if !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("dup: %v", got)
	}
	if got := search.Fuse(nil, nil, 1, 1); len(got) != 0 {
		t.Errorf("empty: %v", got)
	}
}

func TestFoldKeepsRuneCountNFC(t *testing.T) {
	s := "Hoàn tiền Đồng ý ƯỚC lượng"
	if utf8.RuneCountInString(textfold.Fold(s)) != utf8.RuneCountInString(s) {
		t.Fatal("Fold changed rune count for NFC")
	}
}

func TestSnippet(t *testing.T) {
	long := strings.Repeat("lorem ipsum dolor sit amet ", 40)
	cases := []struct {
		name, text string
		terms      []string
		max        int
		check      func(string) bool
	}{
		{"accent NFC", long + "chính sách hoàn tiền áp dụng " + long, []string{"hoan tien"}, 60,
			func(s string) bool {
				return strings.Contains(s, "hoàn tiền") && strings.HasPrefix(s, "…") && strings.HasSuffix(s, "…")
			}},
		{"accent NFD", long + "chi\u0301nh sa\u0301ch hoa\u0300n tie\u0302\u0300n a\u0301p du\u0323ng " + long, []string{"hoan tien"}, 40,
			func(s string) bool {
				// original decomposed word kept, window cut mid-text on both sides
				return strings.Contains(s, "hoa\u0300n tie\u0302\u0300n") && strings.HasPrefix(s, "…") && strings.HasSuffix(s, "…")
			}},
		{"empty text", "", []string{"a"}, 10, func(s string) bool { return s == "" }},
		{"max zero", "abc", []string{"a"}, 0, func(s string) bool { return s == "" }},
		{"max negative", "abc", []string{"a"}, -5, func(s string) bool { return s == "" }},
		{"short unchanged", "  short text  ", []string{"text"}, 100, func(s string) bool { return s == "short text" }},
		{"no terms -> head", long, nil, 30,
			func(s string) bool {
				return strings.HasPrefix(s, "lorem") && strings.HasSuffix(s, "…") && !strings.HasPrefix(s, "…")
			}},
		{"no match -> head", long, []string{"zzz"}, 30, func(s string) bool { return strings.HasPrefix(s, "lorem") }},
		{"empty term ignored", long, []string{""}, 30, func(s string) bool { return strings.HasPrefix(s, "lorem") }},
		{"term at start", "needle " + long, []string{"needle"}, 40,
			func(s string) bool { return strings.HasPrefix(s, "needle") && strings.HasSuffix(s, "…") }},
		{"term at end", long + "needle", []string{"needle"}, 40,
			func(s string) bool { return strings.HasSuffix(s, "needle") && strings.HasPrefix(s, "…") }},
		{"earliest of terms", long + "beta " + long + "alpha", []string{"alpha", "beta"}, 40,
			func(s string) bool { return strings.Contains(s, "beta") }},
		{"no whitespace hard cut", strings.Repeat("x", 100) + "needle" + strings.Repeat("y", 100), []string{"needle"}, 30,
			func(s string) bool { return strings.Contains(s, "needle") }},
	}
	for _, c := range cases {
		got := search.Snippet(c.text, c.terms, c.max)
		if !c.check(got) {
			t.Errorf("%s: got %q", c.name, got)
		}
		if c.max > 0 {
			if n := utf8.RuneCountInString(strings.Trim(got, "…")); n > c.max {
				t.Errorf("%s: %d runes > max", c.name, n)
			}
		}
	}
}
