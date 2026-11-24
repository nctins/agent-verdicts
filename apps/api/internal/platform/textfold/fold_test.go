package textfold_test

import (
	"testing"
	"unicode/utf8"

	"agent-verdicts/internal/platform/textfold"
)

func TestFold(t *testing.T) {
	cases := map[string]string{
		"Hoàn Tiền": "hoan tien", "Đường đi": "duong di", "ABC": "abc", "": "",
		"Đồng ý":                 "dong y",
		"TIẾNG VIỆT có DẤU":      "tieng viet co dau",
		"Thông tin lỗi thời":     "thong tin loi thoi",
		"ưu tiên, ỳ ạch, Ốc":     "uu tien, y ach, oc",
		"Rate Limit 100 req/min": "rate limit 100 req/min",
		// NFD input: combining marks are dropped.
		"ngân hàng": "ngan hang",
		"ờ ự":     "o u",
	}
	for in, want := range cases {
		if got := textfold.Fold(in); got != want {
			t.Errorf("Fold(%q)=%q want %q", in, got, want)
		}
	}
}

func TestFold_PreservesRuneCountForNFC(t *testing.T) {
	for _, s := range []string{"Đồng ý", "Đường đi", "TIẾNG VIỆT có DẤU", "ưu tiên, ỳ ạch, Ốc", "Hoàn Tiền ĐĐ đ"} {
		if got, want := utf8.RuneCountInString(textfold.Fold(s)), utf8.RuneCountInString(s); got != want {
			t.Errorf("rune count of Fold(%q)=%d want %d", s, got, want)
		}
	}
}

func TestValid(t *testing.T) {
	for s, want := range map[string]bool{"": true, "Đồng ý": true, "a\x00b": false, "bad \xff": false} {
		if got := textfold.Valid(s); got != want {
			t.Errorf("Valid(%q) = %v, want %v", s, got, want)
		}
	}
}
