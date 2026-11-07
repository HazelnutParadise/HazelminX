package secrets

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
)

// newID returns 16 random bytes as lowercase base32 without padding (26 chars).
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secrets: crypto/rand unavailable: " + err.Error())
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
}
