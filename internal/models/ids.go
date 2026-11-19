package models

import (
	"crypto/rand"
	"encoding/hex"
)

// NewID returns a random id with the given prefix, e.g. "c-1a2b3c4d".
func NewID(prefix string) string {
	randomBytes := make([]byte, 4)
	_, _ = rand.Read(randomBytes)
	return prefix + "-" + hex.EncodeToString(randomBytes)
}
