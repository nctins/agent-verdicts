// Package id generates unique identifiers (UUID v4) used as primary keys
// for persistent entities.
package id

import (
	"crypto/rand"
	"fmt"
)

// New returns a new UUID v4 string generated with crypto/rand.
func New() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("id: failed to read random bytes: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10

	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
