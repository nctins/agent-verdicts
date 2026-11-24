// Package search holds pure (DB-free) helpers for chunking, rank fusion and snippets.
package search

import "strings"

const (
	chunkMax     = 1600 // target maximum for normal packing, in runes
	chunkOverlap = 200  // runes carried from one chunk into the next
)

// Chunk splits title+"\n\n"+body into chunks of at most ~1600 runes, each
// (after the first) starting with the trailing >=200 runes of the previous one.
// Sentences end at ". ", "? ", "! " or "\n"; a sentence longer than the limit
// is hard-cut by rune. A blank body yields a single chunk: the title.
func Chunk(title, body string) []string {
	if strings.TrimSpace(body) == "" {
		return []string{title}
	}
	segs := splitSentences([]rune(title + "\n\n" + body))

	var chunks []string
	var cur []rune       // current chunk text
	var curSegs [][]rune // segments of cur (including carried ones)
	fresh := 0           // segments in cur that are new (not carry)
	flush := func() {
		chunks = append(chunks, string(cur))
		cur, curSegs, fresh = nil, nil, 0
	}
	start := func(carry [][]rune) {
		for _, c := range carry {
			cur = append(cur, c...)
			curSegs = append(curSegs, c)
		}
	}
	for _, s := range segs {
		if len(s) > chunkMax {
			// Hard cut: fill the current chunk, then continue in new ones.
			for len(s) > 0 {
				room := chunkMax - len(cur)
				if room <= 0 || room > len(s) {
					room = min2(len(s), max2(room, 0))
				}
				if room == 0 {
					carry := carryOver(curSegs)
					flush()
					start(carry)
					continue
				}
				cur = append(cur, s[:room]...)
				curSegs = append(curSegs, s[:room])
				fresh++
				s = s[room:]
				if len(s) > 0 {
					carry := carryOver(curSegs)
					flush()
					start(carry)
				}
			}
			continue
		}
		if len(cur)+len(s) > chunkMax && fresh > 0 {
			carry := carryOver(curSegs)
			flush()
			start(carry)
		}
		if len(cur)+len(s) > chunkMax+chunkOverlap { // carry too big next to this segment
			cur = cur[len(cur)-chunkOverlap:]
			curSegs = [][]rune{cur}
		}
		cur = append(cur, s...)
		curSegs = append(curSegs, s)
		fresh++
	}
	if fresh > 0 {
		flush()
	}
	return chunks
}

// carryOver picks trailing whole segments totalling >=200 runes; when that
// would exceed 2x the overlap it falls back to the last 200 runes.
func carryOver(segs [][]rune) [][]rune {
	n, i := 0, len(segs)
	for i > 0 && n < chunkOverlap {
		i--
		n += len(segs[i])
	}
	if n <= 2*chunkOverlap {
		return segs[i:]
	}
	var all []rune
	for _, s := range segs[i:] {
		all = append(all, s...)
	}
	return [][]rune{all[len(all)-chunkOverlap:]}
}

// splitSentences splits without losing a rune: separators stay with the
// segment they end.
func splitSentences(r []rune) [][]rune {
	var out [][]rune
	start := 0
	for i, c := range r {
		end := c == '\n' || (c == ' ' && i > start && (r[i-1] == '.' || r[i-1] == '?' || r[i-1] == '!'))
		if end {
			out = append(out, r[start:i+1])
			start = i + 1
		}
	}
	if start < len(r) {
		out = append(out, r[start:])
	}
	return out
}
