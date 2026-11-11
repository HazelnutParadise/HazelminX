package protocol

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Enrollment (engineering/contracts/README.md, 註冊; decision 0031).

// Agent-listener paths of enrollment.
const (
	EnrollDiscoveryPath = "/agent/v1/enroll/discovery"
	EnrollPath          = "/agent/v1/enroll"
)

const (
	enrollPrefix      = "HZX"
	enrollAlphabet    = "0123456789ABCDEFGHJKMNPQRSTVWXYZ" // Crockford base32
	enrollChars       = 16
	enrollKeyDomain   = "hazelminx-enroll-v1\n"
	enrollMACDomain   = "hazelminx-enroll-discovery-v1\n"
	enrollGroupLength = 4
)

// ErrEnrollmentCode: the text is not an enrollment code.
var ErrEnrollmentCode = errors.New("protocol: not an enrollment code")

// EnrollmentCode is a parsed code. Its zero value is not a valid code.
type EnrollmentCode struct {
	chars string // the 16 characters after the prefix, normalized
}

// ParseEnrollmentCode normalizes text: drops '-' and ASCII spaces,
// uppercases, maps O→0 and I, L→1, then requires "HZX" and 16 characters of
// the Crockford alphabet.
func ParseEnrollmentCode(text string) (EnrollmentCode, error) {
	var b strings.Builder
	for _, r := range text {
		switch {
		case r == '-' || r == ' ' || r == '\t':
			continue
		case r >= 'a' && r <= 'z':
			r -= 'a' - 'A'
		}
		switch r {
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		b.WriteRune(r)
	}
	s := b.String()
	// H, Z and X are not aliased, so the prefix survives normalization.
	if !strings.HasPrefix(s, enrollPrefix) || len(s) != len(enrollPrefix)+enrollChars {
		return EnrollmentCode{}, fmt.Errorf("%w: want %s followed by %d characters", ErrEnrollmentCode, enrollPrefix, enrollChars)
	}
	body := s[len(enrollPrefix):]
	for _, r := range body {
		if !strings.ContainsRune(enrollAlphabet, r) {
			return EnrollmentCode{}, fmt.Errorf("%w: %q is not a code character", ErrEnrollmentCode, r)
		}
	}
	return EnrollmentCode{chars: body}, nil
}

// NewEnrollmentCode draws 80 random bits.
func NewEnrollmentCode() (EnrollmentCode, error) {
	buf := make([]byte, enrollChars)
	if _, err := rand.Read(buf); err != nil {
		return EnrollmentCode{}, fmt.Errorf("protocol: random enrollment code: %w", err)
	}
	out := make([]byte, enrollChars)
	for i, v := range buf {
		out[i] = enrollAlphabet[v&31] // 256 is a multiple of 32: no bias
	}
	return EnrollmentCode{chars: string(out)}, nil
}

// Normalized is "HZX" and the 16 characters, without separators.
func (c EnrollmentCode) Normalized() string { return enrollPrefix + c.chars }

// ID is the public lookup part (the first group).
func (c EnrollmentCode) ID() string { return c.chars[:enrollGroupLength] }

// String is the display form, HZX-XXXX-XXXX-XXXX-XXXX.
func (c EnrollmentCode) String() string {
	parts := []string{enrollPrefix}
	for i := 0; i < len(c.chars); i += enrollGroupLength {
		parts = append(parts, c.chars[i:i+enrollGroupLength])
	}
	return strings.Join(parts, "-")
}

// Key is SHA-256("hazelminx-enroll-v1\n" + Normalized()). The Core stores
// only this value.
func (c EnrollmentCode) Key() []byte {
	sum := sha256.Sum256([]byte(enrollKeyDomain + c.Normalized()))
	return sum[:]
}

// DiscoveryMAC authenticates the CA certificate sent before the agent can
// verify TLS: standard base64 of HMAC-SHA256(key, domain + caPEM).
func DiscoveryMAC(key, caPEM []byte) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(enrollMACDomain))
	m.Write(caPEM)
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

// VerifyDiscoveryMAC compares in constant time.
func VerifyDiscoveryMAC(key, caPEM []byte, mac string) bool {
	got, err := base64.StdEncoding.DecodeString(mac)
	if err != nil {
		return false
	}
	want, _ := base64.StdEncoding.DecodeString(DiscoveryMAC(key, caPEM))
	return hmac.Equal(got, want)
}

// EnrollDiscovery is the answer of GET EnrollDiscoveryPath.
type EnrollDiscovery struct {
	CACertificate string `json:"caCertificate"`
	MAC           string `json:"mac"`
}

// EnrollRequest is the body of POST EnrollPath.
type EnrollRequest struct {
	Code          string `json:"code"`
	NodePublicKey string `json:"nodePublicKey"`
	Hostname      string `json:"hostname"`
	MachineID     string `json:"machineId"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	AgentVersion  string `json:"agentVersion"`
	Group         string `json:"group,omitempty"`
}

// Enrollment answer statuses.
const (
	EnrollPending  = "pending"
	EnrollApproved = "approved"
	EnrollRefused  = "refused"
)

// Enrollment refusal codes.
const (
	EnrollRequestInvalid = "ENROLLMENT_REQUEST_INVALID"
	EnrollCodeInvalid    = "ENROLLMENT_CODE_INVALID"
	EnrollCodeExpired    = "ENROLLMENT_CODE_EXPIRED"
	EnrollCodeUsed       = "ENROLLMENT_CODE_USED"
	EnrollCodeRevoked    = "ENROLLMENT_CODE_REVOKED"
	EnrollGroupMismatch  = "ENROLLMENT_GROUP_MISMATCH"
	EnrollRejected       = "ENROLLMENT_REJECTED"
	EnrollExpired        = "ENROLLMENT_EXPIRED"
	EnrollNodeKeyRevoked = "NODE_KEY_REVOKED"
	EnrollTooManyTries   = "TOO_MANY_ATTEMPTS"
)

// EnrollResponse is every answer of POST EnrollPath. Pending fills
// EnrollmentID and RetryAfterSeconds; approved fills the certificates;
// refused fills Code and Message.
type EnrollResponse struct {
	Status              string `json:"status"`
	EnrollmentID        string `json:"enrollmentId,omitempty"`
	RetryAfterSeconds   int    `json:"retryAfterSeconds,omitempty"`
	NodeID              string `json:"nodeId,omitempty"`
	NodeCertificate     string `json:"nodeCertificate,omitempty"`
	CACertificate       string `json:"caCertificate,omitempty"`
	PrivilegedIssuerKey string `json:"privilegedIssuerKey,omitempty"`
	Code                string `json:"code,omitempty"`
	Message             string `json:"message,omitempty"`
}
