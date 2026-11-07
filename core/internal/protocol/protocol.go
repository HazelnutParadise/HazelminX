// Package protocol is the Go side of the Core<->Agent channel contract
// (docs/contracts/README.md). The Rust side is agent/crates/protocol; both are
// tested against docs/contracts/fixtures.
//
// DecodeHandshake reads the five handshake messages and ignores unknown
// fields, so peers of any version can say "incompatible, here is my range".
// Decode reads everything after ready strictly: unknown type, unknown fields
// and missing required fields are errors, and the receiver must not act on
// such a message.
package protocol

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Versions this build speaks.
const (
	ProtocolMin = 1
	ProtocolMax = 1
)

// Fixed names of the contract.
const (
	CoreServerName   = "core.hazelminx.internal"
	ConnectPath      = "/agent/v1/connect"
	TranscriptDomain = "hazelminx-session-v1"
	NonceBytes       = 32
)

// Capability names of protocol v1.
const (
	CapJobs      = "jobs"
	CapAgentInfo = "agent.info"
)

// reject codes.
const (
	RejectProtocolIncompatible = "PROTOCOL_INCOMPATIBLE"
	RejectHandshakeMalformed   = "HANDSHAKE_MALFORMED"
	RejectIdentityInvalid      = "IDENTITY_INVALID"
	RejectProofInvalid         = "PROOF_INVALID"
	RejectNodeRevoked          = "NODE_REVOKED"
	RejectDuplicateSession     = "DUPLICATE_SESSION"
)

// step_rejected codes.
const (
	StepRejectUnsupportedAction      = "UNSUPPORTED_ACTION"
	StepRejectInvalidParams          = "INVALID_PARAMS"
	StepRejectWrongNode              = "WRONG_NODE"
	StepRejectExpired                = "EXPIRED"
	StepRejectStaleControlGeneration = "STALE_CONTROL_GENERATION"
	StepRejectOutcomeUnknown         = "OUTCOME_UNKNOWN"
)

// protocol_error codes.
const (
	ErrorUnknownMessage = "UNKNOWN_MESSAGE"
	ErrorMalformed      = "MALFORMED"
)

// Message type strings.
const (
	TypeHello           = "hello"
	TypeWelcome         = "welcome"
	TypeReject          = "reject"
	TypeAuth            = "auth"
	TypeReady           = "ready"
	TypeHeartbeat       = "heartbeat"
	TypeStepExecute     = "step_execute"
	TypeStepRejected    = "step_rejected"
	TypeStepReconcile   = "step_reconcile"
	TypeReconcileReport = "reconcile_report"
	TypeEvent           = "event"
	TypeStepProgress    = "step_progress"
	TypeAck             = "ack"
	TypeProtocolError   = "protocol_error"
)

// Durable event kinds.
const (
	EventStarted   = "started"
	EventCompleted = "completed"
	EventFailed    = "failed"
)

// Range is an inclusive protocol version range.
type Range struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// SupportedRange is the range this build speaks.
func SupportedRange() Range { return Range{Min: ProtocolMin, Max: ProtocolMax} }

// Capability is one {name, version} entry.
type Capability struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// StepFailure is a known failure reported by the agent.
type StepFailure struct {
	Code      string `json:"code"`
	Summary   string `json:"summary"`
	Retryable bool   `json:"retryable"`
}

// Every message struct carries its Type so Encode needs no extra argument.
// Constructors are not needed: callers set Type with the constants above.

type Hello struct {
	Type            string       `json:"type"`
	Protocol        Range        `json:"protocol"`
	AgentVersion    string       `json:"agentVersion"`
	NodeCertificate string       `json:"nodeCertificate"`
	Nonce           string       `json:"nonce"`
	Capabilities    []Capability `json:"capabilities"`
}

type Welcome struct {
	Type             string       `json:"type"`
	ProtocolVersion  int          `json:"protocolVersion"`
	CoreVersion      string       `json:"coreVersion"`
	CoreCertificate  string       `json:"coreCertificate"`
	Nonce            string       `json:"nonce"`
	SessionID        string       `json:"sessionId"`
	Proof            string       `json:"proof"`
	HeartbeatSeconds int          `json:"heartbeatSeconds"`
	Capabilities     []Capability `json:"capabilities"`
}

type Reject struct {
	Type      string `json:"type"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Supported *Range `json:"supported,omitempty"`
}

type Auth struct {
	Type  string `json:"type"`
	Proof string `json:"proof"`
}

type Ready struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
}

type Heartbeat struct {
	Type string    `json:"type"`
	At   time.Time `json:"at"`
}

type StepExecute struct {
	Type              string                     `json:"type"`
	RequestID         string                     `json:"requestId"`
	JobID             string                     `json:"jobId"`
	Step              int                        `json:"step"`
	StepName          string                     `json:"stepName"`
	Action            string                     `json:"action"`
	ActionVersion     int                        `json:"actionVersion"`
	Params            map[string]json.RawMessage `json:"params"`
	TargetNode        string                     `json:"targetNode"`
	ControlGeneration int64                      `json:"controlGeneration"`
	ExpiresAt         time.Time                  `json:"expiresAt"`
	Retryable         bool                       `json:"retryable"`
	Irreversible      bool                       `json:"irreversible"`
}

type StepRejected struct {
	Type      string       `json:"type"`
	RequestID string       `json:"requestId"`
	JobID     string       `json:"jobId"`
	Step      int          `json:"step"`
	Code      string       `json:"code"`
	Message   string       `json:"message"`
	Supported []Capability `json:"supported,omitempty"`
}

type StepReconcile struct {
	Type       string `json:"type"`
	RequestID  string `json:"requestId"`
	JobID      string `json:"jobId"`
	Step       int    `json:"step"`
	TargetNode string `json:"targetNode"`
}

type ReconcileReport struct {
	Type      string       `json:"type"`
	RequestID string       `json:"requestId"`
	JobID     string       `json:"jobId"`
	Step      int          `json:"step"`
	Started   bool         `json:"started"`
	Completed bool         `json:"completed"`
	Failed    bool         `json:"failed"`
	Evidence  []string     `json:"evidence,omitempty"`
	Error     *StepFailure `json:"error,omitempty"`
}

type Event struct {
	Type      string          `json:"type"`
	Seq       uint64          `json:"seq"`
	Kind      string          `json:"kind"`
	JobID     string          `json:"jobId"`
	Step      int             `json:"step"`
	RequestID string          `json:"requestId"`
	At        time.Time       `json:"at"`
	Evidence  []string        `json:"evidence,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *StepFailure    `json:"error,omitempty"`
}

type StepProgress struct {
	Type        string    `json:"type"`
	JobID       string    `json:"jobId"`
	Step        int       `json:"step"`
	At          time.Time `json:"at"`
	Progress    *int      `json:"progress,omitempty"`
	Log         []string  `json:"log"`
	LogsDropped uint64    `json:"logsDropped"`
}

type Ack struct {
	Type string `json:"type"`
	Seq  uint64 `json:"seq"`
}

type ProtocolError struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Decode errors. Use errors.Is; the wrapped message carries the detail.
var (
	// ErrMalformed: not JSON, not an object, or no string "type".
	ErrMalformed = errors.New("protocol: malformed message")
	// ErrUnknownType: "type" is not a message of this phase.
	ErrUnknownType = errors.New("protocol: unknown message type")
	// ErrInvalidFields: known type, but fields are missing, unknown or of the
	// wrong shape.
	ErrInvalidFields = errors.New("protocol: invalid message fields")
	// ErrIncompatible: no common protocol version.
	ErrIncompatible = errors.New("protocol: no common protocol version")
)

// ErrorCode is the protocol_error code a receiver answers a decode error with.
func ErrorCode(err error) string {
	if errors.Is(err, ErrUnknownType) {
		return ErrorUnknownMessage
	}
	return ErrorMalformed
}

// DecodeHandshake decodes hello, welcome, reject, auth or ready, ignoring
// unknown fields. It returns a pointer to the concrete struct (*Hello, …).
//
// Expected results:
//   - every messages/{hello,welcome,reject,reject_no_range,auth,ready}.json
//     fixture decodes, and Encode of the result gives the same JSON value;
//   - handshake-lenient/*.json decodes (unknown fields ignored);
//   - a post-handshake type such as step_execute → ErrUnknownType;
//   - non-JSON, non-object, missing or non-string "type" → ErrMalformed;
//   - a known type with a required field missing (any field without
//     omitempty in the struct) → ErrInvalidFields.
func DecodeHandshake(data []byte) (any, error) {
	return decode(data, handshakeTypes, false)
}

// Decode decodes a post-handshake message strictly. It returns a pointer to
// the concrete struct (*Heartbeat, *StepExecute, …).
//
// Expected results:
//   - every non-handshake fixture in messages/ decodes, and Encode of the
//     result gives the same JSON value;
//   - invalid/unknown_type.json and invalid/handshake_in_session.json →
//     ErrUnknownType;
//   - invalid/missing_type.json → ErrMalformed;
//   - every other invalid/*.json → ErrInvalidFields (unknown field, missing
//     required field, params not an object, event kind not one of
//     started/completed/failed, negative seq).
//   - A required field is any field without omitempty; "params" must be a
//     JSON object (it may be empty).
func Decode(data []byte) (any, error) {
	return decode(data, sessionTypes, true)
}

var handshakeTypes = map[string]func() any{
	TypeHello:   func() any { return &Hello{} },
	TypeWelcome: func() any { return &Welcome{} },
	TypeReject:  func() any { return &Reject{} },
	TypeAuth:    func() any { return &Auth{} },
	TypeReady:   func() any { return &Ready{} },
}

var sessionTypes = map[string]func() any{
	TypeHeartbeat:       func() any { return &Heartbeat{} },
	TypeStepExecute:     func() any { return &StepExecute{} },
	TypeStepRejected:    func() any { return &StepRejected{} },
	TypeStepReconcile:   func() any { return &StepReconcile{} },
	TypeReconcileReport: func() any { return &ReconcileReport{} },
	TypeEvent:           func() any { return &Event{} },
	TypeStepProgress:    func() any { return &StepProgress{} },
	TypeAck:             func() any { return &Ack{} },
	TypeProtocolError:   func() any { return &ProtocolError{} },
}

func decode(data []byte, types map[string]func() any, strict bool) (any, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("%w: not a JSON object", ErrMalformed)
	}
	var typ string
	if raw, ok := fields["type"]; !ok || !bytes.HasPrefix(bytes.TrimSpace(raw), []byte(`"`)) || json.Unmarshal(raw, &typ) != nil {
		return nil, fmt.Errorf("%w: no string \"type\" field", ErrMalformed)
	}
	mk, ok := types[typ]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownType, typ)
	}
	msg := mk()
	// Every field without omitempty is required, and null does not count.
	t := reflect.TypeOf(msg).Elem()
	for i := 0; i < t.NumField(); i++ {
		name, opts, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if strings.Contains(opts, "omitempty") {
			continue
		}
		if raw, ok := fields[name]; !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, fmt.Errorf("%w: %s needs %q", ErrInvalidFields, typ, name)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if strict {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(msg); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidFields, typ, err)
	}
	if ev, ok := msg.(*Event); ok && ev.Kind != EventStarted && ev.Kind != EventCompleted && ev.Kind != EventFailed {
		return nil, fmt.Errorf("%w: event kind %q", ErrInvalidFields, ev.Kind)
	}
	return msg, nil
}

// Encode marshals a message struct as compact JSON.
func Encode(msg any) ([]byte, error) { return json.Marshal(msg) }

// Negotiate picks the highest version both ranges contain.
//
// Expected results: {1,1}+{1,1} → 1; {1,3}+{2,5} → 3; {1,2}+{2,2} → 2;
// {2,3}+{1,1} → ErrIncompatible; a range with Min < 1 or Min > Max on either
// side → ErrIncompatible.
func Negotiate(ours, theirs Range) (int, error) {
	valid := func(r Range) bool { return r.Min >= 1 && r.Min <= r.Max }
	lo, hi := max(ours.Min, theirs.Min), min(ours.Max, theirs.Max)
	if !valid(ours) || !valid(theirs) || lo > hi {
		return 0, fmt.Errorf("%w: ours %d..%d, theirs %d..%d", ErrIncompatible, ours.Min, ours.Max, theirs.Min, theirs.Max)
	}
	return hi, nil
}

// Role is the signer of a transcript: "core" or "agent".
type Role string

const (
	RoleCore  Role = "core"
	RoleAgent Role = "agent"
)

// Transcript is the exact text each side signs (docs/contracts/README.md).
//
// Expected result: with the inputs of fixtures/signature.json, RoleCore
// gives coreTranscript and RoleAgent gives agentTranscript byte for byte.
func Transcript(role Role, agentNonce, coreNonce string, protocolVersion int, nodeFingerprint, sessionID string) []byte {
	return fmt.Appendf(nil, "%s\n%s\n%s\n%s\n%d\n%s\n%s\n", TranscriptDomain, role, agentNonce, coreNonce, protocolVersion, nodeFingerprint, sessionID)
}

// Fingerprint is the lowercase hex SHA-256 of a raw 32-byte Ed25519 key.
//
// Expected result: nodePublicKeyHex of fixtures/signature.json gives
// nodeFingerprint.
func Fingerprint(publicKey []byte) string {
	sum := sha256.Sum256(publicKey)
	return hex.EncodeToString(sum[:])
}

// NewNonce returns 32 random bytes, base64url without padding.
func NewNonce() (string, error) {
	b := make([]byte, NonceBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ValidNonce reports whether s is base64url without padding and decodes to
// exactly 32 bytes.
//
// Expected results: fixture nonces and NewNonce() → true; padded,
// standard-alphabet (+ or /), 31 or 33 bytes, empty → false.
func ValidNonce(s string) bool {
	b, err := base64.RawURLEncoding.Strict().DecodeString(s)
	return err == nil && len(b) == NonceBytes
}

// Supports reports whether caps has name at version or higher.
//
// Expected results: [jobs v1] supports (jobs,1) → true, (jobs,2) → false,
// (agent.info,1) → false; nil → false.
func Supports(caps []Capability, name string, version int) bool {
	for _, c := range caps {
		if c.Name == name && c.Version >= version {
			return true
		}
	}
	return false
}
