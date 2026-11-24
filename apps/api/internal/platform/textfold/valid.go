package textfold

import (
	"strings"
	"unicode/utf8"
)

// Valid reports whether s can be stored in or compared against a Postgres
// text value: valid UTF-8 and no NUL byte (U+0000 is valid UTF-8, but text
// rejects it with SQLSTATE 22021). Every domain entry point that takes free
// text from a request checks it and returns invalid_argument.
func Valid(s string) bool { return utf8.ValidString(s) && !strings.ContainsRune(s, 0) }
