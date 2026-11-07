package iam

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
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

// NewTOTPSecret returns TOTPSecretSize random bytes from crypto/rand.
// Errors only when the system random source fails.
func NewTOTPSecret() ([]byte, error) {
	secret := make([]byte, TOTPSecretSize)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	return secret, nil
}

// TOTPStep is the RFC 6238 time step of t: floor(unix seconds / 30).
// Times before the epoch are not expected; return the floor anyway.
func TOTPStep(t time.Time) int64 {
	sec := t.Unix()
	step := sec / int64(TOTPPeriod/time.Second)
	if sec%int64(TOTPPeriod/time.Second) != 0 && sec < 0 {
		step--
	}
	return step
}

// TOTPCode is the HOTP value of secret at counter step: HMAC-SHA1 over the
// 8-byte big-endian counter, RFC 4226 dynamic truncation, modulo 10^6,
// left-padded with zeros to exactly TOTPDigits characters.
func TOTPCode(secret []byte, step int64) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))

	mac := hmac.New(sha1.New, secret)
	mac.Write(counter[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	truncated := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	value := truncated % 1000000
	return fmt.Sprintf("%0*d", TOTPDigits, value)
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
// lastStep 0 means "never used". Step 0 (the first 30 seconds of 1970) is
// therefore never accepted; no real clock is there.
func VerifyTOTP(secret []byte, code string, now time.Time, lastStep int64) (step int64, ok bool) {
	clean := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, code)
	if len(clean) != TOTPDigits {
		return 0, false
	}
	for i := 0; i < len(clean); i++ {
		if clean[i] < '0' || clean[i] > '9' {
			return 0, false
		}
	}

	base := TOTPStep(now)
	for delta := -TOTPSkewSteps; delta <= TOTPSkewSteps; delta++ {
		candidate := base + int64(delta)
		if candidate <= lastStep {
			continue
		}
		want := TOTPCode(secret, candidate)
		if subtle.ConstantTimeCompare([]byte(want), []byte(clean)) == 1 {
			return candidate, true
		}
	}
	return 0, false
}

// EncodeTOTPSecret renders a seed the way authenticator apps expect for
// manual entry: RFC 4648 base32, upper case, no padding.
func EncodeTOTPSecret(secret []byte) string {
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
}

// TOTPURI is the otpauth:// URI encoded into the enrolment QR code:
//
//	otpauth://totp/<issuer>:<account>?secret=<base32>&issuer=<issuer>&algorithm=SHA1&digits=6&period=30
//
// issuer and account are path- and query-escaped (spaces as %20 in the
// label, a ':' inside issuer or account escaped so the label stays
// unambiguous).
func TOTPURI(issuer, account string, secret []byte) string {
	issuerEsc := strings.ReplaceAll(url.PathEscape(issuer), ":", "%3A")
	accountEsc := strings.ReplaceAll(url.PathEscape(account), ":", "%3A")
	label := issuerEsc + ":" + accountEsc

	query := url.Values{}
	query.Set("secret", EncodeTOTPSecret(secret))
	query.Set("issuer", issuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", strconv.Itoa(TOTPDigits))
	query.Set("period", strconv.Itoa(int(TOTPPeriod/time.Second)))

	return "otpauth://totp/" + label + "?" + query.Encode()
}
