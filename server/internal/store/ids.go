package store

import (
	"crypto/rand"
	"encoding/hex"
)

// NewID returns prefix + 16 random bytes in hex, e.g. "usr_3f2a…".
func NewID(prefix string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error
	return prefix + hex.EncodeToString(b)
}
