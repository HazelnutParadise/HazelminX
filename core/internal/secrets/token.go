package secrets

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

// tokenDigestPrefix names the algorithm so a later migration can tell digests apart.
const tokenDigestPrefix = "sha256:"

// HashToken returns the verification digest to store for a token that only
// ever needs to be checked, never sent anywhere (35.2 雜湊優先). The token
// itself is not retained.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return tokenDigestPrefix + hex.EncodeToString(sum[:])
}

// VerifyToken reports whether token matches a digest from HashToken, in
// constant time over the digest bytes.
func VerifyToken(token, digest string) bool {
	if !strings.HasPrefix(digest, tokenDigestPrefix) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(HashToken(token)), []byte(digest)) == 1
}
