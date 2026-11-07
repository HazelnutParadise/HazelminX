package iam

import "errors"

// Recovery codes rescue a login when the second-factor device is lost
// (IAM-002). Each is shown once, stored only as an argon2id hash and marked
// used the moment it is accepted. The format follows the Setup design board:
// two groups of four characters, e.g. "7KQ2-9MXD".
const (
	RecoveryCodeCount = 10
	// RecoveryAlphabet has no 0/O or 1/I so codes survive being read aloud
	// or written down. 32 symbols → 5 bits each, 40 bits per code.
	RecoveryAlphabet   = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	recoveryCodeLength = 8
)

var errRecoveryNotImplemented = errors.New("iam: recovery codes not implemented")

// GenerateRecoveryCodes returns n distinct codes formatted "XXXX-XXXX",
// each character drawn uniformly from RecoveryAlphabet with crypto/rand
// (rejection sampling, no modulo bias). n <= 0 returns an empty slice and
// no error. Errors only when the system random source fails.
func GenerateRecoveryCodes(n int) ([]string, error) {
	return nil, errRecoveryNotImplemented
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
	return "", false
}
