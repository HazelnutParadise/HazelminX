package iam

import (
	"errors"
	"time"
)

// TOTP (RFC 6238 over RFC 4226 HOTP, HMAC-SHA1, 6 digits, 30-second steps)
// is what every common authenticator app speaks. The service keeps the seed
// in the secret store and remembers the last accepted step per credential,
// so a code can be used once (replay) and clocks may drift by one step
// either way (IAM-011, SET-004 clock skew).
const (
	TOTPDigits     = 6
	TOTPPeriod     = 30 * time.Second
	TOTPSkewSteps  = 1  // accept now-1 .. now+1
	TOTPSecretSize = 20 // bytes, the RFC 4226 recommended 160-bit seed
)

var errTOTPNotImplemented = errors.New("iam: totp not implemented")

// NewTOTPSecret returns TOTPSecretSize random bytes from crypto/rand.
// Errors only when the system random source fails.
func NewTOTPSecret() ([]byte, error) {
	return nil, errTOTPNotImplemented
}

// TOTPStep is the RFC 6238 time step of t: floor(unix seconds / 30).
// Times before the epoch are not expected; return the floor anyway.
func TOTPStep(t time.Time) int64 {
	return 0
}

// TOTPCode is the HOTP value of secret at counter step: HMAC-SHA1 over the
// 8-byte big-endian counter, RFC 4226 dynamic truncation, modulo 10^6,
// left-padded with zeros to exactly TOTPDigits characters.
func TOTPCode(secret []byte, step int64) string {
	return ""
}

// VerifyTOTP checks code against secret at now.
//   - code may contain spaces (apps show "123 456"); after removing spaces it
//     must be exactly TOTPDigits ASCII digits, otherwise ok=false.
//   - candidate steps are TOTPStep(now)-TOTPSkewSteps .. +TOTPSkewSteps;
//     a step <= lastStep is never accepted (replay), even if the code matches.
//   - comparison is constant time per candidate.
//   - on success it returns the matched step, which the caller stores as the
//     new lastStep; on failure step is 0.
//
// lastStep 0 means "never used".
func VerifyTOTP(secret []byte, code string, now time.Time, lastStep int64) (step int64, ok bool) {
	return 0, false
}

// EncodeTOTPSecret renders a seed the way authenticator apps expect for
// manual entry: RFC 4648 base32, upper case, no padding.
func EncodeTOTPSecret(secret []byte) string {
	return ""
}

// TOTPURI is the otpauth:// URI encoded into the enrolment QR code:
//
//	otpauth://totp/<issuer>:<account>?secret=<base32>&issuer=<issuer>&algorithm=SHA1&digits=6&period=30
//
// issuer and account are path- and query-escaped (spaces as %20 in the
// label, a ':' inside issuer or account escaped so the label stays
// unambiguous).
func TOTPURI(issuer, account string, secret []byte) string {
	return ""
}
