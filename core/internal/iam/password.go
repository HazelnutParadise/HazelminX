package iam

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// PasswordParams tunes argon2id. DefaultPasswordParams follows the OWASP
// 2023 minimum (m=64 MiB, t=3, p=4 is heavier; we use 19 MiB/t=2/p=1 as the
// recommended lower bound) so a login costs tens of milliseconds, not seconds.
type PasswordParams struct {
	Memory  uint32 // KiB
	Time    uint32
	Threads uint8
	KeyLen  uint32
	SaltLen uint32
}

// DefaultPasswordParams is used in production.
var DefaultPasswordParams = PasswordParams{Memory: 19 * 1024, Time: 2, Threads: 1, KeyLen: 32, SaltLen: 16}

// TestPasswordParams keeps the test suite fast. Never use outside tests.
var TestPasswordParams = PasswordParams{Memory: 256, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}

// MinPasswordLength is the only password rule this ticket enforces
// (INS-005 forbids fixed defaults; a length floor stops trivially guessed
// owner passwords without inventing complexity rules the spec does not ask for).
const MinPasswordLength = 12

// HashPassword returns a self-describing PHC string:
// $argon2id$v=19$m=...,t=...,p=...$<salt>$<hash>.
func HashPassword(password string, p PasswordParams) (string, error) {
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", p.Memory, p.Time, p.Threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks password against an encoded hash in constant time.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("iam: unsupported password hash")
	}
	var p PasswordParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads); err != nil {
		return false, fmt.Errorf("iam: bad hash params: %w", err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// ValidatePassword applies the length floor.
func ValidatePassword(pw string) error {
	if len(pw) < MinPasswordLength {
		return fmt.Errorf("%w: password must be at least %d characters", ErrValidation, MinPasswordLength)
	}
	return nil
}

// randomToken returns n random bytes, base64url without padding.
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
