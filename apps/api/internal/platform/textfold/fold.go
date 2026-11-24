package textfold

import (
	"strings"
	"unicode"
)

// Fold lower-cases s and strips Vietnamese diacritics so "dong y" matches
// "Đồng ý" — whether the text arrives precomposed (NFC: one code point per
// accented letter) or decomposed (NFD: base letter + combining marks, as
// macOS and some clients send it).
//
// Precomposed (NFC) input maps exactly 1 rune to 1 rune, so rune indexes in
// the folded text align with the original (used for snippets). NFD input is
// NOT length-preserving: combining marks are dropped. Inputs here are NFC.
func Fold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		if unicode.Is(unicode.Mn, r) { // combining mark of decomposed text
			continue
		}
		if base, ok := vietBase[r]; ok {
			b.WriteRune(base)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

var vietBase = func() map[rune]rune {
	m := map[rune]rune{'đ': 'd'}
	for base, variants := range map[rune]string{
		'a': "àáảãạăằắẳẵặâầấẩẫậ",
		'e': "èéẻẽẹêềếểễệ",
		'i': "ìíỉĩị",
		'o': "òóỏõọôồốổỗộơờớởỡợ",
		'u': "ùúủũụưừứửữự",
		'y': "ỳýỷỹỵ",
	} {
		for _, r := range variants {
			m[r] = base
		}
	}
	return m
}()
