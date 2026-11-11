// Package protocol is the Go side of the Core<->Agent channel contract
// (engineering/contracts/README.md). The Rust side is agent/crates/protocol; both are
// tested against engineering/contracts/fixtures.
//
// DecodeHandshake reads the five handshake messages and ignores unknown
// fields, so peers of any version can say "incompatible, here is my range".
// Decode reads everything after ready strictly: unknown type, unknown fields
// and missing required fields are errors, and the receiver must not act on
// such a message.
package protocol

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
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
	// CapHostLedger: ledger_pull / ledger_page / ledger_unavailable, the
	// Core's copy of the host change ledger (spec 55.1).
	CapHostLedger = "host.ledger"
	// CapNodeCertificate: certificate_renew / certificate_installed /
	// certificate_refused (certificate rotation, ticket 019).
	CapNodeCertificate = "node.certificate"
)

// certificate_refused codes.
const (
	CertificateInvalid     = "CERTIFICATE_INVALID"
	CertificateWriteFailed = "WRITE_FAILED"
)

// ledger_unavailable codes.
const (
	// LedgerExecutorUnreachable: the agent cannot reach the privileged
	// executor's socket.
	LedgerExecutorUnreachable = "EXECUTOR_UNREACHABLE"
	// LedgerExecutorRefused: the executor refused the export (grant,
	// permission, parameters).
	LedgerExecutorRefused = "EXECUTOR_REFUSED"
	// LedgerExportFailed: the executor could not export (ledger not open,
	// or afterSeq is past the host ledger's head).
	LedgerExportFailed = "EXPORT_FAILED"
)

// reject codes.
const (
	RejectProtocolIncompatible = "PROTOCOL_INCOMPATIBLE"
	RejectHandshakeMalformed   = "HANDSHAKE_MALFORMED"
	RejectIdentityInvalid      = "IDENTITY_INVALID"
	RejectProofInvalid         = "PROOF_INVALID"
	RejectNodeRevoked          = "NODE_REVOKED"
	RejectDuplicateSession     = "DUPLICATE_SESSION"
	// RejectIdentityConflict: the node key is live in two places at once
	// (for example a cloned VM); refused until the node re-enrolls.
	RejectIdentityConflict = "IDENTITY_CONFLICT"
)

// step_rejected codes.
const (
	StepRejectUnsupportedAction      = "UNSUPPORTED_ACTION"
	StepRejectInvalidParams          = "INVALID_PARAMS"
	StepRejectWrongNode              = "WRONG_NODE"
	StepRejectExpired                = "EXPIRED"
	StepRejectStaleControlGeneration = "STALE_CONTROL_GENERATION"
	StepRejectOutcomeUnknown         = "OUTCOME_UNKNOWN"
	StepRejectJournalUnavailable     = "JOURNAL_UNAVAILABLE"
)

// protocol_error codes.
const (
	ErrorUnknownMessage = "UNKNOWN_MESSAGE"
	ErrorMalformed      = "MALFORMED"
)

// Message type strings.
const (
	TypeHello                = "hello"
	TypeWelcome              = "welcome"
	TypeReject               = "reject"
	TypeAuth                 = "auth"
	TypeReady                = "ready"
	TypeHeartbeat            = "heartbeat"
	TypeStepExecute          = "step_execute"
	TypeStepRejected         = "step_rejected"
	TypeStepReconcile        = "step_reconcile"
	TypeReconcileReport      = "reconcile_report"
	TypeEvent                = "event"
	TypeStepProgress         = "step_progress"
	TypeAck                  = "ack"
	TypeProtocolError        = "protocol_error"
	TypeLedgerPull           = "ledger_pull"
	TypeLedgerPage           = "ledger_page"
	TypeLedgerUnavailable    = "ledger_unavailable"
	TypeCertificateRenew     = "certificate_renew"
	TypeCertificateInstalled = "certificate_installed"
	TypeCertificateRefused   = "certificate_refused"
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
	Type            string `json:"type"`
	Protocol        Range  `json:"protocol"`
	AgentVersion    string `json:"agentVersion"`
	NodeCertificate string `json:"nodeCertificate"`
	Nonce           string `json:"nonce"`
	// Kex is the agent's ephemeral X25519 public key (base64url, no padding).
	Kex          string       `json:"kex"`
	Capabilities []Capability `json:"capabilities"`
}

type Welcome struct {
	Type             string       `json:"type"`
	ProtocolVersion  int          `json:"protocolVersion"`
	CoreVersion      string       `json:"coreVersion"`
	CoreCertificate  string       `json:"coreCertificate"`
	Nonce            string       `json:"nonce"`
	Kex              string       `json:"kex"`
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

// ExecutorGrant is a grant for the privileged executor, carried through the
// agent unchanged. Field names are the executor's own (snake_case). Times
// are RFC 3339 UTC with whole seconds, the form the executor's canonical
// bytes use.
type ExecutorGrant struct {
	ID          string   `json:"id"`
	Subject     string   `json:"subject"`
	Permissions []string `json:"permissions"`
	NotBefore   string   `json:"not_before"`
	ExpiresAt   string   `json:"expires_at"`
	Signature   string   `json:"signature"`
}

// LedgerHead is the last line of a host ledger.
type LedgerHead struct {
	Seq  uint64 `json:"seq"`
	Hash string `json:"hash"`
}

// LedgerPull asks for the host ledger lines after AfterSeq.
type LedgerPull struct {
	Type      string        `json:"type"`
	RequestID string        `json:"requestId"`
	AfterSeq  uint64        `json:"afterSeq"`
	Limit     int           `json:"limit"`
	Grant     ExecutorGrant `json:"grant"`
}

// LedgerPage is one page of raw ledger lines, exactly as stored on the host.
type LedgerPage struct {
	Type      string     `json:"type"`
	RequestID string     `json:"requestId"`
	LedgerID  string     `json:"ledgerId"`
	AfterSeq  uint64     `json:"afterSeq"`
	Lines     []string   `json:"lines"`
	Head      LedgerHead `json:"head"`
	More      bool       `json:"more"`
}

// LedgerUnavailable says why the agent could not produce a page.
type LedgerUnavailable struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

// CertificateRenew hands the agent a new certificate for its existing key.
type CertificateRenew struct {
	Type        string `json:"type"`
	RequestID   string `json:"requestId"`
	Certificate string `json:"certificate"`
}

// CertificateInstalled: the agent verified and stored the new certificate
// and will use it on its next connection.
type CertificateInstalled struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId"`
}

// CertificateRefused: the agent kept its current certificate.
type CertificateRefused struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId"`
	Code      string `json:"code"`
	Message   string `json:"message"`
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
	TypeHeartbeat:            func() any { return &Heartbeat{} },
	TypeStepExecute:          func() any { return &StepExecute{} },
	TypeStepRejected:         func() any { return &StepRejected{} },
	TypeStepReconcile:        func() any { return &StepReconcile{} },
	TypeReconcileReport:      func() any { return &ReconcileReport{} },
	TypeEvent:                func() any { return &Event{} },
	TypeStepProgress:         func() any { return &StepProgress{} },
	TypeAck:                  func() any { return &Ack{} },
	TypeProtocolError:        func() any { return &ProtocolError{} },
	TypeLedgerPull:           func() any { return &LedgerPull{} },
	TypeLedgerPage:           func() any { return &LedgerPage{} },
	TypeLedgerUnavailable:    func() any { return &LedgerUnavailable{} },
	TypeCertificateRenew:     func() any { return &CertificateRenew{} },
	TypeCertificateInstalled: func() any { return &CertificateInstalled{} },
	TypeCertificateRefused:   func() any { return &CertificateRefused{} },
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
	if err := noDuplicateKeys(data); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidFields, typ, err)
	}
	// encoding/json matches keys case-insensitively, so keys are checked
	// against the exact field names first: strictly they must match, and
	// in the lenient handshake anything else is dropped before decoding so
	// "Nonce" can never fill Nonce.
	clean, err := exactKeys(data, t, strict)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidFields, typ, err)
	}
	if err := json.Unmarshal(clean, msg); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidFields, typ, err)
	}
	if ev, ok := msg.(*Event); ok && ev.Kind != EventStarted && ev.Kind != EventCompleted && ev.Kind != EventFailed {
		return nil, fmt.Errorf("%w: event kind %q", ErrInvalidFields, ev.Kind)
	}
	return msg, nil
}

var rawMessageType = reflect.TypeOf(json.RawMessage(nil))

// exactKeys re-encodes data keeping only keys that are exactly a json tag
// of t (recursively for nested structs and slices of structs). Unknown keys
// are an error when strict and dropped otherwise. Maps and RawMessage
// values (params, result) pass through untouched.
func exactKeys(data []byte, t reflect.Type, strict bool) ([]byte, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t == rawMessageType, t.Kind() == reflect.Map:
		return data, nil
	case t.Kind() == reflect.Slice:
		var items []json.RawMessage
		if json.Unmarshal(data, &items) != nil {
			return data, nil // wrong shape: let json.Unmarshal report it
		}
		for i, it := range items {
			c, err := exactKeys(it, t.Elem(), strict)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			items[i] = c
		}
		return json.Marshal(items)
	case t.Kind() == reflect.Struct && t != reflect.TypeOf(time.Time{}):
		var obj map[string]json.RawMessage
		if json.Unmarshal(data, &obj) != nil || obj == nil {
			return data, nil
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
			fields[name] = t.Field(i).Type
		}
		out := map[string]json.RawMessage{}
		for k, v := range obj {
			ft, ok := fields[k]
			if !ok {
				if strict {
					return nil, fmt.Errorf("unknown field %q", k)
				}
				continue
			}
			c, err := exactKeys(v, ft, strict)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			out[k] = c
		}
		return json.Marshal(out)
	}
	return data, nil
}

// noDuplicateKeys rejects any JSON object (at any depth) that repeats a key;
// encoding/json would silently keep the last one.
func noDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var walk func() error
	walk = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch tok {
		case json.Delim('{'):
			seen := map[string]bool{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return err
				}
				k := kt.(string)
				if seen[k] {
					return fmt.Errorf("duplicate key %q", k)
				}
				seen[k] = true
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		case json.Delim('['):
			for dec.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		}
		return nil
	}
	return walk()
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

// TranscriptInput is everything a handshake signature covers.
type TranscriptInput struct {
	AgentNonce, CoreNonce string
	AgentKex, CoreKex     string
	ProtocolVersion       int
	NodeFingerprint       string
	SessionID             string
	// HelloDigest is HelloDigest of the hello exactly as sent and received.
	HelloDigest string
	// WelcomeDigest is WelcomeDigest of the welcome's other fields.
	WelcomeDigest string
}

// Transcript is the exact text each side signs (engineering/contracts/README.md).
// It covers both nonces, both key-exchange keys, the whole hello and the
// welcome's parameters, so a proxy that changes any of them breaks the
// signatures.
//
// Expected result: with the inputs of fixtures/signature.json, RoleCore
// gives coreTranscript and RoleAgent gives agentTranscript byte for byte.
func Transcript(role Role, in TranscriptInput) []byte {
	return fmt.Appendf(nil, "%s\n%s\n%s\n%s\n%s\n%s\n%d\n%s\n%s\n%s\n%s\n", TranscriptDomain, role,
		in.AgentNonce, in.CoreNonce, in.AgentKex, in.CoreKex, in.ProtocolVersion, in.NodeFingerprint, in.SessionID, in.HelloDigest, in.WelcomeDigest)
}

// HelloDigest is the lowercase hex SHA-256 of the hello text.
func HelloDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// WelcomeDigest is the lowercase hex SHA-256 of the welcome fields the
// transcript does not list one by one, in a length-prefixed encoding that
// does not depend on JSON formatting: lp("hazelminx-welcome-v1"),
// lp(coreVersion), be32(heartbeatSeconds), be32(len(caps)), then lp(name)
// and be32(version) per capability, where lp(x) = be32(len(x)) || x.
func WelcomeDigest(coreVersion string, heartbeatSeconds int, caps []Capability) string {
	h := sha256.New()
	u32 := func(v int) {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(v))
		h.Write(b[:])
	}
	lp := func(x string) {
		u32(len(x))
		h.Write([]byte(x))
	}
	lp("hazelminx-welcome-v1")
	lp(coreVersion)
	u32(heartbeatSeconds)
	u32(len(caps))
	for _, c := range caps {
		lp(c.Name)
		u32(c.Version)
	}
	return hex.EncodeToString(h.Sum(nil))
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

// ErrFrame: a session frame failed authentication or ordering. The
// receiver must close the session; nothing in the frame may be used.
var ErrFrame = errors.New("protocol: session frame rejected")

// ErrKex: a key-exchange public key is malformed or yields no shared secret.
var ErrKex = errors.New("protocol: key exchange failed")

// NewKex draws an ephemeral X25519 key and returns it with its public key
// encoded for hello/welcome.
func NewKex() (*ecdh.PrivateKey, string, error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	return k, base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}

// SessionKeys are the per-direction MAC keys of one session.
type SessionKeys struct {
	Send, Recv []byte
}

// DeriveKeys computes the X25519 shared secret with the peer's kex and
// expands it with HKDF-SHA256 into one key per direction, bound to the
// session id. role is the caller's side: the core sends with the
// core→agent key.
//
// Expected results: with the seeds of fixtures/signature.json, both sides
// derive keyCoreToAgentHex and keyAgentToCoreHex; a peer key that is not
// 32 bytes of base64url, or a low-order point, is ErrKex.
func DeriveKeys(role Role, priv *ecdh.PrivateKey, peerKex, sessionID string) (SessionKeys, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(peerKex)
	if err != nil || len(raw) != 32 {
		return SessionKeys{}, fmt.Errorf("%w: peer key is not 32 bytes of base64url", ErrKex)
	}
	pub, err := ecdh.X25519().NewPublicKey(raw)
	if err != nil {
		return SessionKeys{}, fmt.Errorf("%w: %v", ErrKex, err)
	}
	shared, err := priv.ECDH(pub) // rejects the all-zero result of low-order points
	if err != nil {
		return SessionKeys{}, fmt.Errorf("%w: %v", ErrKex, err)
	}
	c2a, err := hkdf.Key(sha256.New, shared, nil, KeyInfo(RoleCore, sessionID), 32)
	if err != nil {
		return SessionKeys{}, err
	}
	a2c, err := hkdf.Key(sha256.New, shared, nil, KeyInfo(RoleAgent, sessionID), 32)
	if err != nil {
		return SessionKeys{}, err
	}
	if role == RoleCore {
		return SessionKeys{Send: c2a, Recv: a2c}, nil
	}
	return SessionKeys{Send: a2c, Recv: c2a}, nil
}

// KeyInfo is the HKDF info string for messages sent by from.
func KeyInfo(from Role, sessionID string) string {
	dir := "core->agent"
	if from == RoleAgent {
		dir = "agent->core"
	}
	return TranscriptDomain + " " + dir + " " + sessionID
}

// Frame is the envelope of every message after ready: N counts frames per
// direction from 1, Body is the message JSON as a string, MAC is
// HMAC-SHA256(key, "hazelminx-frame-v1\n" || big-endian N || Body).
type Frame struct {
	N    uint64 `json:"n"`
	Body string `json:"body"`
	MAC  string `json:"mac"`
}

func frameMAC(key []byte, n uint64, body []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("hazelminx-frame-v1\n"))
	var nb [8]byte
	binary.BigEndian.PutUint64(nb[:], n)
	m.Write(nb[:])
	m.Write(body)
	return m.Sum(nil)
}

// SealFrame wraps body as frame number n.
func SealFrame(key []byte, n uint64, body []byte) ([]byte, error) {
	return json.Marshal(Frame{N: n, Body: string(body), MAC: base64.StdEncoding.EncodeToString(frameMAC(key, n, body))})
}

// OpenFrame checks a frame and returns its body. The frame must be a JSON
// object with exactly n, body and mac; n must equal want; the MAC must
// match. Any failure is ErrFrame.
//
// Expected results: fixtures/frame.json opens with its key and n; a changed
// body, a changed MAC, n off by one in either direction, an extra or
// missing field → ErrFrame.
func OpenFrame(key []byte, want uint64, data []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || len(fields) != 3 || noDuplicateKeys(data) != nil {
		return nil, fmt.Errorf("%w: not a frame object", ErrFrame)
	}
	var f Frame
	for k, v := range fields {
		var err error
		switch k {
		case "n":
			err = json.Unmarshal(v, &f.N)
		case "body":
			err = json.Unmarshal(v, &f.Body)
		case "mac":
			err = json.Unmarshal(v, &f.MAC)
		default:
			err = errors.New("unknown field")
		}
		if err != nil {
			return nil, fmt.Errorf("%w: field %q", ErrFrame, k)
		}
	}
	if f.N != want {
		return nil, fmt.Errorf("%w: frame %d, expected %d", ErrFrame, f.N, want)
	}
	mac, err := base64.StdEncoding.Strict().DecodeString(f.MAC)
	if err != nil || !hmac.Equal(mac, frameMAC(key, f.N, []byte(f.Body))) {
		return nil, fmt.Errorf("%w: MAC mismatch on frame %d", ErrFrame, f.N)
	}
	return []byte(f.Body), nil
}
