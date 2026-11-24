package authentication

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Password login for Human actors (spec 2026-11-11 §2). Agents keep bearer
// tokens; this file only covers the password half.

const (
	MinPasswordLen = 10
	MaxPasswordLen = 128
	passwordSalt   = 16
	passwordKeyLen = 32
)

// passwordIterations is the PBKDF2 work factor for new hashes. Each hash
// stores its own count, so raising this later does not break old hashes.
var passwordIterations = 600_000

var (
	ErrInvalidUsername  = errors.New("authentication: username must be 3-32 characters of a-z, 0-9, '.', '_' or '-'")
	ErrPasswordTooShort = fmt.Errorf("authentication: password must be at least %d characters", MinPasswordLen)
	ErrPasswordTooLong  = fmt.Errorf("authentication: password must be at most %d characters", MaxPasswordLen)
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9._-]{3,32}$`)

// NormalizeUsername trims and lower-cases s, then checks the allowed shape.
func NormalizeUsername(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if !usernamePattern.MatchString(s) {
		return "", ErrInvalidUsername
	}
	return s, nil
}

// ValidatePassword enforces length only (NIST 800-63B): no composition rules.
func ValidatePassword(p string) error {
	switch n := utf8.RuneCountInString(p); {
	case n < MinPasswordLen:
		return ErrPasswordTooShort
	case n > MaxPasswordLen:
		return ErrPasswordTooLong
	}
	return nil
}

// HashPassword returns "pbkdf2-sha256$<iter>$<salt b64>$<key b64>".
func HashPassword(p string) (string, error) {
	salt := make([]byte, passwordSalt)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("authentication: password salt: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, p, salt, passwordIterations, passwordKeyLen)
	if err != nil {
		return "", fmt.Errorf("authentication: hash password: %w", err)
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", passwordIterations, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// checkPassword reports whether p matches encoded. Malformed hashes never match.
func checkPassword(encoded, p string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, p, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash is checked against when a username does not exist, so a miss
// costs the same time as a wrong password and does not reveal which
// usernames exist.
var dummyHash = func() string {
	h, err := HashPassword("not-a-real-password")
	if err != nil {
		panic(err)
	}
	return h
}()

// passwordAlphabet drops look-alikes (0/o, 1/l/i) so a password read aloud
// or copied from a screen is typed right the first time.
const passwordAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// GeneratePassword returns a temporary password like "abcd-efgh-jkmn-pqrs"
// (16 random characters, ~79 bits).
func GeneratePassword() string {
	c := GenerateCode(16)
	return c[0:4] + "-" + c[4:8] + "-" + c[8:12] + "-" + c[12:16]
}

// GenerateCode returns n random characters from the unambiguous alphabet —
// short one-time codes such as the first-run setup code.
func GenerateCode(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("authentication: read random: " + err.Error())
	}
	for i, c := range b {
		// ponytail: modulo bias over 31 symbols is ~1 bit over 16 chars;
		// fine for codes and temporary passwords that get replaced.
		b[i] = passwordAlphabet[int(c)%len(passwordAlphabet)]
	}
	return string(b)
}
