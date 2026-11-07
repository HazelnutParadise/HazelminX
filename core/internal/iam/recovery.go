package iam

import (
	"crypto/rand"
	"strings"
)

// Recovery codes rescue a login when the second-factor device is lost
// (IAM-002). Each is shown once, stored only as a keyed hash (mfa.go) and
// marked used the moment it is accepted. The format follows the Setup design board:
// two groups of four characters, e.g. "7KQ2-9MXD".
const (
	RecoveryCodeCount = 10
	// RecoveryAlphabet has no 0/O or 1/I so codes survive being read aloud
	// or written down. 32 symbols → 5 bits each, 40 bits per code.
	RecoveryAlphabet   = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	recoveryCodeLength = 8
)

// GenerateRecoveryCodes returns n distinct codes formatted "XXXX-XXXX",
// each character drawn uniformly from RecoveryAlphabet with crypto/rand
// (rejection sampling, no modulo bias). n <= 0 returns an empty slice and
// no error. Errors only when the system random source fails.
func GenerateRecoveryCodes(n int) ([]string, error) {
	if n <= 0 {
		return []string{}, nil
	}

	seen := make(map[string]struct{}, n)
	codes := make([]string, 0, n)
	for len(codes) < n {
		var b strings.Builder
		b.Grow(recoveryCodeLength + 1)
		for i := 0; i < recoveryCodeLength; i++ {
			if i == 4 {
				b.WriteByte('-')
			}
			idx, err := recoveryRandomIndex(len(RecoveryAlphabet))
			if err != nil {
				return nil, err
			}
			b.WriteByte(RecoveryAlphabet[idx])
		}
		code := b.String()
		if _, dup := seen[code]; dup {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	return codes, nil
}

// recoveryRandomIndex returns a uniformly distributed index in [0,n) using
// rejection sampling so no modulo bias is introduced.
func recoveryRandomIndex(n int) (int, error) {
	limit := 256 - (256 % n)
	var buf [1]byte
	for {
		if _, err := rand.Read(buf[:]); err != nil {
			return 0, err
		}
		if int(buf[0]) < limit {
			return int(buf[0]) % n, nil
		}
	}
}

// NormalizeRecoveryCode turns what a user typed into the canonical 8
// characters that are hashed: trims surrounding space, removes every '-'
// and inner space, upper-cases ASCII letters. ok=false when the result is
// not exactly 8 characters of RecoveryAlphabet (so "0"/"O"/"1"/"I" and
// non-ASCII input are rejected before any hashing work).
//
// Examples: "7kq2-9mxd" → "7KQ29MXD"; " 7KQ2 9MXD " → "7KQ29MXD";
// "7KQ29MX" → ok=false; "7KQ2-9MX0" → ok=false.
func NormalizeRecoveryCode(in string) (canonical string, ok bool) {
	trimmed := strings.TrimSpace(in)

	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		switch {
		case r == '-' || r == ' ':
			continue
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
		default:
			b.WriteRune(r)
		}
	}
	canonical = b.String()

	if len(canonical) != recoveryCodeLength {
		return "", false
	}
	for i := 0; i < len(canonical); i++ {
		if strings.IndexByte(RecoveryAlphabet, canonical[i]) < 0 {
			return "", false
		}
	}
	return canonical, true
}
