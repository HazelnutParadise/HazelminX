package protocol

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fixtureRoot is engineering/contracts/fixtures, relative to this package directory.
const fixtureRoot = "../../../engineering/contracts/fixtures"

// decodeFunc matches the signature of Decode and DecodeHandshake.
type decodeFunc func([]byte) (any, error)

// noPanic runs call. The unimplemented functions panic, so a panic becomes a
// failure of the calling (sub)test only, and the rest of the run continues.
func noPanic(t *testing.T, call func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic (function not implemented?): %v", r)
		}
	}()
	call()
}

// runDecode calls decode on data, turning a panic into a test failure.
func runDecode(t *testing.T, decode decodeFunc, data []byte) (msg any, err error) {
	t.Helper()
	noPanic(t, func() { msg, err = decode(data) })
	return msg, err
}

// readFixture returns the bytes of a file under fixtureRoot.
func readFixture(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureRoot, rel))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	return data
}

// listFixtures returns the .json file names in a fixture directory, sorted,
// and fails when the directory has none.
func listFixtures(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(fixtureRoot, dir))
	if err != nil {
		t.Fatalf("list fixtures in %s: %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		t.Fatalf("no .json fixtures in %s", dir)
	}
	return names
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// requireSameJSON compares two JSON documents by their parsed values.
func requireSameJSON(t *testing.T, got, want []byte) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("parse encoded message: %v", err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("JSON value differs\n got: %s\nwant: %s", got, want)
	}
}

// fixtureWithChange returns a fixture with one edit applied to its top-level
// object. The fixture file itself is not modified.
func fixtureWithChange(t *testing.T, rel string, change func(map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(readFixture(t, rel), &m); err != nil {
		t.Fatalf("parse fixture %s: %v", rel, err)
	}
	change(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal edited fixture %s: %v", rel, err)
	}
	return out
}

// messageCase is the expectation for one messages/ fixture.
type messageCase struct {
	handshake bool         // decoded by DecodeHandshake; otherwise by Decode
	want      reflect.Type // concrete pointer type the decoder must return
}

var messageCases = map[string]messageCase{
	"ack.json":                     {false, reflect.TypeOf(&Ack{})},
	"certificate_installed.json":   {false, reflect.TypeOf(&CertificateInstalled{})},
	"certificate_refused.json":     {false, reflect.TypeOf(&CertificateRefused{})},
	"certificate_renew.json":       {false, reflect.TypeOf(&CertificateRenew{})},
	"auth.json":                    {true, reflect.TypeOf(&Auth{})},
	"engines_probe.json":           {false, reflect.TypeOf(&EnginesProbe{})},
	"engines_report.json":          {false, reflect.TypeOf(&EnginesReport{})},
	"engines_unavailable.json":     {false, reflect.TypeOf(&EnginesUnavailable{})},
	"event_completed.json":         {false, reflect.TypeOf(&Event{})},
	"event_failed.json":            {false, reflect.TypeOf(&Event{})},
	"event_started.json":           {false, reflect.TypeOf(&Event{})},
	"heartbeat.json":               {false, reflect.TypeOf(&Heartbeat{})},
	"host_facts.json":              {false, reflect.TypeOf(&HostFacts{})},
	"metrics_ack.json":             {false, reflect.TypeOf(&MetricsAck{})},
	"metrics_report.json":          {false, reflect.TypeOf(&MetricsReport{})},
	"host_storage.json":            {false, reflect.TypeOf(&HostStorage{})},
	"storage_usage.json":           {false, reflect.TypeOf(&StorageUsage{})},
	"ledger_page.json":             {false, reflect.TypeOf(&LedgerPage{})},
	"ledger_pull.json":             {false, reflect.TypeOf(&LedgerPull{})},
	"ledger_unavailable.json":      {false, reflect.TypeOf(&LedgerUnavailable{})},
	"hello.json":                   {true, reflect.TypeOf(&Hello{})},
	"protocol_error.json":          {false, reflect.TypeOf(&ProtocolError{})},
	"ready.json":                   {true, reflect.TypeOf(&Ready{})},
	"reconcile_report.json":        {false, reflect.TypeOf(&ReconcileReport{})},
	"reconcile_report_failed.json": {false, reflect.TypeOf(&ReconcileReport{})},
	"reject.json":                  {true, reflect.TypeOf(&Reject{})},
	"reject_no_range.json":         {true, reflect.TypeOf(&Reject{})},
	"step_execute.json":            {false, reflect.TypeOf(&StepExecute{})},
	"step_progress.json":           {false, reflect.TypeOf(&StepProgress{})},
	"step_reconcile.json":          {false, reflect.TypeOf(&StepReconcile{})},
	"step_rejected.json":           {false, reflect.TypeOf(&StepRejected{})},
	"step_rejected_no_caps.json":   {false, reflect.TypeOf(&StepRejected{})},
	"welcome.json":                 {true, reflect.TypeOf(&Welcome{})},
}

// Every messages/ fixture decodes to its concrete type and re-encodes to the
// same JSON value. Handshake types go through DecodeHandshake, the rest
// through Decode.
func TestDecodeFixtures_Messages(t *testing.T) {
	files := listFixtures(t, "messages")
	for _, f := range files {
		if _, ok := messageCases[f]; !ok {
			t.Errorf("messages/%s has no expectation in messageCases", f)
		}
	}
	for name := range messageCases {
		if !contains(files, name) {
			t.Errorf("messageCases lists %s but the fixture is missing", name)
		}
	}
	for _, name := range files {
		c, ok := messageCases[name]
		if !ok {
			continue
		}
		t.Run(name, func(t *testing.T) {
			data := readFixture(t, "messages/"+name)
			decode := Decode
			if c.handshake {
				decode = DecodeHandshake
			}
			got, err := runDecode(t, decode, data)
			if err != nil {
				t.Fatalf("decode failed: %v", err)
			}
			if reflect.TypeOf(got) != c.want {
				t.Fatalf("decoded to %T, want %v", got, c.want)
			}
			enc, err := Encode(got)
			if err != nil {
				t.Fatalf("encode failed: %v", err)
			}
			requireSameJSON(t, enc, data)
		})
	}
}

// Handshake fixtures are post-handshake types for Decode, so Decode must
// reject them with ErrUnknownType.
func TestDecode_HandshakeFixturesAreUnknownType(t *testing.T) {
	for _, name := range listFixtures(t, "messages") {
		c, ok := messageCases[name]
		if !ok || !c.handshake {
			continue
		}
		t.Run(name, func(t *testing.T) {
			_, err := runDecode(t, Decode, readFixture(t, "messages/"+name))
			if !errors.Is(err, ErrUnknownType) {
				t.Fatalf("Decode error = %v, want ErrUnknownType", err)
			}
		})
	}
}

// Post-handshake fixtures are not handshake types, so DecodeHandshake must
// reject them with ErrUnknownType. step_execute.json is the named example.
func TestDecodeHandshake_PostHandshakeFixturesAreUnknownType(t *testing.T) {
	for _, name := range listFixtures(t, "messages") {
		c, ok := messageCases[name]
		if !ok || c.handshake {
			continue
		}
		t.Run(name, func(t *testing.T) {
			_, err := runDecode(t, DecodeHandshake, readFixture(t, "messages/"+name))
			if !errors.Is(err, ErrUnknownType) {
				t.Fatalf("DecodeHandshake error = %v, want ErrUnknownType", err)
			}
		})
	}
}

// Lenient handshake fixtures carry unknown fields. DecodeHandshake must accept
// them, and the protocol range or supported range must come through.
var lenientWant = map[string]reflect.Type{
	"hello_future_agent.json":                reflect.TypeOf(&Hello{}),
	"hello_case_variant_key_is_ignored.json": reflect.TypeOf(&Hello{}),
	"reject_future_core.json":                reflect.TypeOf(&Reject{}),
	"welcome_capability_future_field.json":   reflect.TypeOf(&Welcome{}),
}

func TestDecodeHandshake_LenientFixtures(t *testing.T) {
	files := listFixtures(t, "handshake-lenient")
	for name := range lenientWant {
		if !contains(files, name) {
			t.Errorf("handshake-lenient/%s is expected but missing", name)
		}
	}
	for _, name := range files {
		want, ok := lenientWant[name]
		if !ok {
			t.Errorf("handshake-lenient/%s has no expectation", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			msg, err := runDecode(t, DecodeHandshake, readFixture(t, "handshake-lenient/"+name))
			if err != nil {
				t.Fatalf("DecodeHandshake rejected a lenient message: %v", err)
			}
			if reflect.TypeOf(msg) != want {
				t.Fatalf("decoded to %T, want %v", msg, want)
			}
			if h, ok := msg.(*Hello); ok && name == "hello_future_agent.json" {
				if wantRange := (Range{Min: 2, Max: 3}); h.Protocol != wantRange {
					t.Fatalf("Protocol = %+v, want %+v", h.Protocol, wantRange)
				}
			}
			if r, ok := msg.(*Reject); ok {
				if r.Supported == nil || *r.Supported != (Range{Min: 4, Max: 5}) {
					t.Fatalf("Supported = %+v, want {4 5}", r.Supported)
				}
			}
		})
	}
}

// specialInvalid lists the invalid fixtures that do not expect
// ErrInvalidFields. Every other invalid/*.json must fail with ErrInvalidFields.
var specialInvalid = map[string]error{
	"unknown_type.json":         ErrUnknownType,
	"handshake_in_session.json": ErrUnknownType,
	"missing_type.json":         ErrMalformed,
}

// Every invalid fixture must be rejected by Decode with the matching sentinel,
// and ErrorCode must give the code the receiver answers with.
func TestDecode_InvalidFixtures(t *testing.T) {
	files := listFixtures(t, "invalid")
	for name := range specialInvalid {
		if !contains(files, name) {
			t.Errorf("invalid/%s is expected but missing", name)
		}
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			wantErr := ErrInvalidFields
			if e, ok := specialInvalid[name]; ok {
				wantErr = e
			}
			wantCode := "MALFORMED"
			if wantErr == ErrUnknownType {
				wantCode = "UNKNOWN_MESSAGE"
			}
			msg, err := runDecode(t, Decode, readFixture(t, "invalid/"+name))
			if err == nil {
				t.Fatalf("Decode accepted an invalid message (got %T)", msg)
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("Decode error = %v, want %v", err, wantErr)
			}
			if got := ErrorCode(err); got != wantCode {
				t.Fatalf("ErrorCode = %q, want %q", got, wantCode)
			}
		})
	}
}

// Input that is not an object with a string "type" is ErrMalformed, for both
// decoders.
func TestDecode_MalformedInput(t *testing.T) {
	inputs := []struct {
		name string
		data string
	}{
		{"not JSON", `not json`},
		{"empty input", ``},
		{"JSON array", `[]`},
		{"JSON string", `"hello"`},
		{"type is a number", `{"type":5}`},
		{"type is null", `{"type":null}`},
		{"type missing", `{"seq":1}`},
	}
	decoders := []struct {
		name   string
		decode decodeFunc
	}{
		{"Decode", Decode},
		{"DecodeHandshake", DecodeHandshake},
	}
	for _, d := range decoders {
		for _, in := range inputs {
			t.Run(d.name+"/"+in.name, func(t *testing.T) {
				_, err := runDecode(t, d.decode, []byte(in.data))
				if !errors.Is(err, ErrMalformed) {
					t.Fatalf("error = %v, want ErrMalformed", err)
				}
			})
		}
	}
}

// A handshake message with a required field removed is ErrInvalidFields.
func TestDecodeHandshake_MissingRequiredField(t *testing.T) {
	cases := []struct {
		name string
		file string
		drop string
	}{
		{"hello without nonce", "messages/hello.json", "nonce"},
		{"welcome without proof", "messages/welcome.json", "proof"},
		{"auth without proof", "messages/auth.json", "proof"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := fixtureWithChange(t, c.file, func(m map[string]any) { delete(m, c.drop) })
			_, err := runDecode(t, DecodeHandshake, data)
			if !errors.Is(err, ErrInvalidFields) {
				t.Fatalf("error = %v, want ErrInvalidFields", err)
			}
		})
	}
}

// A post-handshake message with a required field removed is ErrInvalidFields.
// step_progress without log is the named case.
func TestDecode_MissingRequiredField(t *testing.T) {
	cases := []struct {
		name string
		file string
		drop string
	}{
		{"step_progress without log", "messages/step_progress.json", "log"},
		{"heartbeat without at", "messages/heartbeat.json", "at"},
		{"ack without seq", "messages/ack.json", "seq"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := fixtureWithChange(t, c.file, func(m map[string]any) { delete(m, c.drop) })
			_, err := runDecode(t, Decode, data)
			if !errors.Is(err, ErrInvalidFields) {
				t.Fatalf("error = %v, want ErrInvalidFields", err)
			}
		})
	}
}

// Decode is strict: an unknown field in a post-handshake message is
// ErrInvalidFields.
func TestDecode_UnknownFieldIsInvalid(t *testing.T) {
	cases := []struct {
		name string
		file string
	}{
		{"ack with extra field", "messages/ack.json"},
		{"step_reconcile with extra field", "messages/step_reconcile.json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := fixtureWithChange(t, c.file, func(m map[string]any) { m["extra"] = true })
			_, err := runDecode(t, Decode, data)
			if !errors.Is(err, ErrInvalidFields) {
				t.Fatalf("error = %v, want ErrInvalidFields", err)
			}
		})
	}
}

// A known type whose field has the wrong JSON shape is ErrInvalidFields.
// The params null case is a judgment call: params must be a JSON object, and
// null is not one.
func TestDecode_WrongShapeIsInvalid(t *testing.T) {
	cases := []struct {
		name   string
		decode decodeFunc
		file   string
		change func(map[string]any)
	}{
		{"ack seq as string", Decode, "messages/ack.json", func(m map[string]any) { m["seq"] = "1" }},
		{"step_progress progress as string", Decode, "messages/step_progress.json", func(m map[string]any) { m["progress"] = "40" }},
		{"step_execute params null", Decode, "messages/step_execute.json", func(m map[string]any) { m["params"] = nil }},
		{"hello protocol as number", DecodeHandshake, "messages/hello.json", func(m map[string]any) { m["protocol"] = 1 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := fixtureWithChange(t, c.file, c.change)
			_, err := runDecode(t, c.decode, data)
			if !errors.Is(err, ErrInvalidFields) {
				t.Fatalf("error = %v, want ErrInvalidFields", err)
			}
		})
	}
}

// ErrorCode maps an unknown-type error to UNKNOWN_MESSAGE and every other
// error to MALFORMED. The wrapped case checks errors.Is through the chain.
func TestErrorCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"ErrUnknownType", ErrUnknownType, "UNKNOWN_MESSAGE"},
		{"wrapped ErrUnknownType", fmt.Errorf("decode: %w", ErrUnknownType), "UNKNOWN_MESSAGE"},
		{"ErrMalformed", ErrMalformed, "MALFORMED"},
		{"ErrInvalidFields", ErrInvalidFields, "MALFORMED"},
		{"unrelated error", errors.New("boom"), "MALFORMED"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ErrorCode(c.err); got != c.want {
				t.Fatalf("ErrorCode = %q, want %q", got, c.want)
			}
		})
	}
}

// Negotiate picks the highest version in the intersection, and fails with
// ErrIncompatible when there is none or a range is malformed.
func TestNegotiate(t *testing.T) {
	cases := []struct {
		name         string
		ours, theirs Range
		want         int
		wantErr      bool
	}{
		{"same single version", Range{1, 1}, Range{1, 1}, 1, false},
		{"highest common version of wider range", Range{1, 3}, Range{2, 5}, 3, false},
		{"argument order does not change result", Range{2, 5}, Range{1, 3}, 3, false},
		{"single overlap at the upper bound", Range{1, 2}, Range{2, 2}, 2, false},
		{"disjoint, theirs above ours", Range{2, 3}, Range{1, 1}, 0, true},
		{"disjoint, ours above theirs", Range{1, 1}, Range{2, 3}, 0, true},
		{"our min below 1", Range{0, 1}, Range{1, 1}, 0, true},
		{"their min below 1", Range{1, 1}, Range{0, 1}, 0, true},
		{"our min above max", Range{3, 1}, Range{1, 3}, 0, true},
		{"their min above max", Range{1, 1}, Range{3, 1}, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got int
			var err error
			noPanic(t, func() { got, err = Negotiate(c.ours, c.theirs) })
			if c.wantErr {
				if !errors.Is(err, ErrIncompatible) {
					t.Fatalf("error = %v, want ErrIncompatible", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("version = %d, want %d", got, c.want)
			}
		})
	}
}

// ValidNonce accepts unpadded base64url of exactly 32 bytes. The padded,
// standard-alphabet, wrong-length, and empty cases must be rejected.
func TestValidNonce(t *testing.T) {
	s := loadSignature(t)
	raw, err := base64.RawURLEncoding.DecodeString(s.AgentNonce)
	if err != nil || len(raw) != NonceBytes {
		t.Fatalf("fixture agentNonce is not 32 bytes of unpadded base64url: %v", err)
	}
	standard := strings.NewReplacer("-", "+", "_", "/").Replace(s.AgentNonce)
	if !strings.ContainsAny(standard, "+/") {
		t.Fatal("fixture agentNonce has no - or _, so the standard-alphabet case would prove nothing")
	}
	cases := []struct {
		name string
		s    string
		want bool
	}{
		{"fixture agentNonce", s.AgentNonce, true},
		{"fixture coreNonce", s.CoreNonce, true},
		{"32 zero bytes", base64.RawURLEncoding.EncodeToString(make([]byte, 32)), true},
		{"padded 32 bytes", base64.URLEncoding.EncodeToString(raw), false},
		{"standard alphabet", standard, false},
		{"31 bytes", base64.RawURLEncoding.EncodeToString(make([]byte, 31)), false},
		{"33 bytes", base64.RawURLEncoding.EncodeToString(make([]byte, 33)), false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got bool
			noPanic(t, func() { got = ValidNonce(c.s) })
			if got != c.want {
				t.Fatalf("ValidNonce(%q) = %v, want %v", c.s, got, c.want)
			}
		})
	}
}

// NewNonce returns two different 32-byte base64url values with no padding,
// and ValidNonce accepts them.
func TestNewNonce(t *testing.T) {
	var a, b string
	var errA, errB error
	noPanic(t, func() {
		a, errA = NewNonce()
		b, errB = NewNonce()
	})
	if errA != nil || errB != nil {
		t.Fatalf("NewNonce errors: %v, %v", errA, errB)
	}
	if a == b {
		t.Fatalf("two calls returned the same nonce %q", a)
	}
	for _, n := range []string{a, b} {
		raw, err := base64.RawURLEncoding.DecodeString(n)
		if err != nil {
			t.Fatalf("nonce %q is not unpadded base64url: %v", n, err)
		}
		if len(raw) != NonceBytes {
			t.Fatalf("nonce decodes to %d bytes, want %d", len(raw), NonceBytes)
		}
		var ok bool
		noPanic(t, func() { ok = ValidNonce(n) })
		if !ok {
			t.Fatalf("ValidNonce rejects NewNonce output %q", n)
		}
	}
}

// Supports reports whether caps has the name at the requested version or
// higher.
func TestSupports(t *testing.T) {
	cases := []struct {
		name    string
		caps    []Capability
		capName string
		version int
		want    bool
	}{
		{"exact version", []Capability{{Name: CapJobs, Version: 1}}, CapJobs, 1, true},
		{"requested version above declared", []Capability{{Name: CapJobs, Version: 1}}, CapJobs, 2, false},
		{"declared version above requested", []Capability{{Name: CapJobs, Version: 2}}, CapJobs, 1, true},
		{"other capability name", []Capability{{Name: CapJobs, Version: 1}}, CapAgentInfo, 1, false},
		{"name found in later entry", []Capability{{Name: CapJobs, Version: 1}, {Name: CapAgentInfo, Version: 1}}, CapAgentInfo, 1, true},
		{"nil list", nil, CapJobs, 1, false},
		{"empty list", []Capability{}, CapJobs, 1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got bool
			noPanic(t, func() { got = Supports(c.caps, c.capName, c.version) })
			if got != c.want {
				t.Fatalf("Supports = %v, want %v", got, c.want)
			}
		})
	}
}

// signatureFixture mirrors fixtures/signature.json, read from the file.
type signatureFixture struct {
	AgentNonce       string `json:"agentNonce"`
	AgentKex         string `json:"agentKex"`
	CoreKex          string `json:"coreKex"`
	AgentKexSeedHex  string `json:"agentKexSeedHex"`
	CoreKexSeedHex   string `json:"coreKexSeedHex"`
	KeyCoreToAgent   string `json:"keyCoreToAgentHex"`
	KeyAgentToCore   string `json:"keyAgentToCoreHex"`
	HelloText        string `json:"helloText"`
	HelloDigestHex   string `json:"helloDigest"`
	WelcomeDigestHex string `json:"welcomeDigest"`
	AgentProof       string `json:"agentProof"`
	AgentTranscript  string `json:"agentTranscript"`
	CoreNonce        string `json:"coreNonce"`
	CoreProof        string `json:"coreProof"`
	CoreSeedHex      string `json:"coreSeedHex"`
	CoreTranscript   string `json:"coreTranscript"`
	NodeFingerprint  string `json:"nodeFingerprint"`
	NodePublicKeyHex string `json:"nodePublicKeyHex"`
	NodeSeedHex      string `json:"nodeSeedHex"`
	ProtocolVersion  int    `json:"protocolVersion"`
	SessionID        string `json:"sessionId"`
}

func (s signatureFixture) input() TranscriptInput {
	return TranscriptInput{AgentNonce: s.AgentNonce, CoreNonce: s.CoreNonce, AgentKex: s.AgentKex, CoreKex: s.CoreKex,
		ProtocolVersion: s.ProtocolVersion, NodeFingerprint: s.NodeFingerprint, SessionID: s.SessionID,
		HelloDigest: s.HelloDigestHex, WelcomeDigest: s.WelcomeDigestHex}
}

func loadSignature(t *testing.T) signatureFixture {
	t.Helper()
	var s signatureFixture
	if err := json.Unmarshal(readFixture(t, "signature.json"), &s); err != nil {
		t.Fatalf("parse signature.json: %v", err)
	}
	return s
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("fixture hex %q: %v", s, err)
	}
	return b
}

// Transcript gives the exact bytes each side signs, for the signature.json
// inputs: coreTranscript for RoleCore and agentTranscript for RoleAgent.
func TestTranscript_MatchesFixture(t *testing.T) {
	s := loadSignature(t)
	cases := []struct {
		name string
		role Role
		want string
	}{
		{"RoleCore gives coreTranscript", RoleCore, s.CoreTranscript},
		{"RoleAgent gives agentTranscript", RoleAgent, s.AgentTranscript},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []byte
			noPanic(t, func() {
				got = Transcript(c.role, s.input())
			})
			if !bytes.Equal(got, []byte(c.want)) {
				t.Fatalf("transcript differs\n got: %q\nwant: %q", got, c.want)
			}
		})
	}
}

// Fingerprint is the lowercase hex SHA-256 of the raw 32-byte public key.
func TestFingerprint_MatchesFixture(t *testing.T) {
	s := loadSignature(t)
	pub := mustHex(t, s.NodePublicKeyHex)
	if len(pub) != ed25519.PublicKeySize {
		t.Fatalf("fixture nodePublicKeyHex is %d bytes, want %d", len(pub), ed25519.PublicKeySize)
	}
	t.Run("fixture public key gives nodeFingerprint", func(t *testing.T) {
		var got string
		noPanic(t, func() { got = Fingerprint(pub) })
		if got != s.NodeFingerprint {
			t.Fatalf("Fingerprint = %s, want %s", got, s.NodeFingerprint)
		}
	})
	t.Run("is lowercase hex SHA-256 of the raw key", func(t *testing.T) {
		sum := sha256.Sum256(pub)
		want := hex.EncodeToString(sum[:])
		var got string
		noPanic(t, func() { got = Fingerprint(pub) })
		if got != want {
			t.Fatalf("Fingerprint = %s, want SHA-256 %s", got, want)
		}
	})
}

// The Ed25519 keys from the fixture seeds sign the Transcript output, and the
// base64 result equals the fixture proofs. Ed25519 is deterministic, so the
// signatures must match exactly.
func TestSignatures_FixtureKeysSignTranscripts(t *testing.T) {
	s := loadSignature(t)
	nodeSeed := mustHex(t, s.NodeSeedHex)
	coreSeed := mustHex(t, s.CoreSeedHex)
	if len(nodeSeed) != ed25519.SeedSize || len(coreSeed) != ed25519.SeedSize {
		t.Fatalf("fixture seeds must be %d bytes", ed25519.SeedSize)
	}
	nodePriv := ed25519.NewKeyFromSeed(nodeSeed)
	corePriv := ed25519.NewKeyFromSeed(coreSeed)

	t.Run("agent proof is the node key over the agent transcript", func(t *testing.T) {
		if !bytes.Equal(nodePriv.Public().(ed25519.PublicKey), mustHex(t, s.NodePublicKeyHex)) {
			t.Fatal("fixture nodeSeedHex does not derive nodePublicKeyHex")
		}
		var transcript []byte
		noPanic(t, func() {
			transcript = Transcript(RoleAgent, s.input())
		})
		sig := ed25519.Sign(nodePriv, transcript)
		if got := base64.StdEncoding.EncodeToString(sig); got != s.AgentProof {
			t.Fatalf("agent proof = %s, want %s", got, s.AgentProof)
		}
	})
	t.Run("core proof is the core key over the core transcript", func(t *testing.T) {
		var transcript []byte
		noPanic(t, func() {
			transcript = Transcript(RoleCore, s.input())
		})
		sig := ed25519.Sign(corePriv, transcript)
		if got := base64.StdEncoding.EncodeToString(sig); got != s.CoreProof {
			t.Fatalf("core proof = %s, want %s", got, s.CoreProof)
		}
	})
}

// The handshake fixtures carry the same nonces and proofs as signature.json.
func TestHandshakeFixtures_CarrySignatureValues(t *testing.T) {
	s := loadSignature(t)
	t.Run("hello nonce equals agentNonce", func(t *testing.T) {
		msg, err := runDecode(t, DecodeHandshake, readFixture(t, "messages/hello.json"))
		if err != nil {
			t.Fatalf("decode hello.json: %v", err)
		}
		h, ok := msg.(*Hello)
		if !ok {
			t.Fatalf("decoded to %T, want *Hello", msg)
		}
		if h.Nonce != s.AgentNonce {
			t.Fatalf("nonce = %s, want %s", h.Nonce, s.AgentNonce)
		}
	})
	t.Run("welcome nonce equals coreNonce and proof equals coreProof", func(t *testing.T) {
		msg, err := runDecode(t, DecodeHandshake, readFixture(t, "messages/welcome.json"))
		if err != nil {
			t.Fatalf("decode welcome.json: %v", err)
		}
		w, ok := msg.(*Welcome)
		if !ok {
			t.Fatalf("decoded to %T, want *Welcome", msg)
		}
		if w.Nonce != s.CoreNonce {
			t.Fatalf("nonce = %s, want %s", w.Nonce, s.CoreNonce)
		}
		if w.Proof != s.CoreProof {
			t.Fatalf("proof = %s, want %s", w.Proof, s.CoreProof)
		}
	})
	t.Run("auth proof equals agentProof", func(t *testing.T) {
		msg, err := runDecode(t, DecodeHandshake, readFixture(t, "messages/auth.json"))
		if err != nil {
			t.Fatalf("decode auth.json: %v", err)
		}
		a, ok := msg.(*Auth)
		if !ok {
			t.Fatalf("decoded to %T, want *Auth", msg)
		}
		if a.Proof != s.AgentProof {
			t.Fatalf("proof = %s, want %s", a.Proof, s.AgentProof)
		}
	})
}

func kexFromSeed(t *testing.T, seedHex string) *ecdh.PrivateKey {
	t.Helper()
	seed, err := hex.DecodeString(seedHex)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ecdh.X25519().NewPrivateKey(seed)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// Both sides derive the same pair of per-direction keys as the fixture, and
// each side's send key is the other's receive key.
func TestDeriveKeysMatchesFixtureFromBothSides(t *testing.T) {
	s := loadSignature(t)
	agent, core := kexFromSeed(t, s.AgentKexSeedHex), kexFromSeed(t, s.CoreKexSeedHex)
	if base64.RawURLEncoding.EncodeToString(agent.PublicKey().Bytes()) != s.AgentKex || base64.RawURLEncoding.EncodeToString(core.PublicKey().Bytes()) != s.CoreKex {
		t.Fatal("fixture kex public keys do not match their seeds")
	}
	ak, err := DeriveKeys(RoleAgent, agent, s.CoreKex, s.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	ck, err := DeriveKeys(RoleCore, core, s.AgentKex, s.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(ck.Send) != s.KeyCoreToAgent || hex.EncodeToString(ak.Send) != s.KeyAgentToCore {
		t.Fatalf("send keys differ from fixture")
	}
	if !bytes.Equal(ak.Recv, ck.Send) || !bytes.Equal(ck.Recv, ak.Send) {
		t.Fatal("one side's send key is not the other's receive key")
	}
	// A different session id gives different keys.
	other, _ := DeriveKeys(RoleCore, core, s.AgentKex, s.SessionID+"x")
	if bytes.Equal(other.Send, ck.Send) {
		t.Fatal("keys are not bound to the session id")
	}
}

func TestDeriveKeysRejectsBadPeerKeys(t *testing.T) {
	priv, _, err := NewKex()
	if err != nil {
		t.Fatal(err)
	}
	for name, peer := range map[string]string{
		"empty":      "",
		"short":      base64.RawURLEncoding.EncodeToString(make([]byte, 31)),
		"padded":     base64.URLEncoding.EncodeToString(make([]byte, 32)),
		"low order":  base64.RawURLEncoding.EncodeToString(make([]byte, 32)), // all-zero point
		"not base64": "!!!!",
	} {
		if _, err := DeriveKeys(RoleCore, priv, peer, "ses_x"); !errors.Is(err, ErrKex) {
			t.Errorf("%s: got %v, want ErrKex", name, err)
		}
	}
}

type framesFixture struct {
	KeyCoreToAgent string  `json:"keyCoreToAgentHex"`
	KeyAgentToCore string  `json:"keyAgentToCoreHex"`
	CoreToAgent    []Frame `json:"coreToAgent"`
	AgentToCore    []Frame `json:"agentToCore"`
}

func TestFramesMatchFixtureAndRejectTampering(t *testing.T) {
	var f framesFixture
	if err := json.Unmarshal(readFixture(t, "frames.json"), &f); err != nil {
		t.Fatal(err)
	}
	c2a, _ := hex.DecodeString(f.KeyCoreToAgent)
	a2c, _ := hex.DecodeString(f.KeyAgentToCore)
	for i, fr := range f.CoreToAgent {
		sealed, err := SealFrame(c2a, fr.N, []byte(fr.Body))
		if err != nil {
			t.Fatal(err)
		}
		var got Frame
		if err := json.Unmarshal(sealed, &got); err != nil || got != fr {
			t.Fatalf("frame %d: sealed %s, fixture %+v", i, sealed, fr)
		}
		body, err := OpenFrame(c2a, uint64(i+1), sealed)
		if err != nil || string(body) != fr.Body {
			t.Fatalf("open frame %d: %v", i, err)
		}
		if _, err := Decode(body); err != nil {
			t.Fatalf("frame %d body does not decode: %v", i, err)
		}
	}
	good, _ := json.Marshal(f.AgentToCore[0])
	if _, err := OpenFrame(a2c, 1, good); err != nil {
		t.Fatal(err)
	}
	tamper := func(fn func(m map[string]any)) []byte {
		var m map[string]any
		_ = json.Unmarshal(good, &m)
		fn(m)
		b, _ := json.Marshal(m)
		return b
	}
	cases := map[string]struct {
		key  []byte
		want uint64
		data []byte
	}{
		"wrong direction key":  {c2a, 1, good},
		"replayed (n too low)": {a2c, 2, good},
		"skipped (n too high)": {a2c, 0, good},
		"changed body":         {a2c, 1, tamper(func(m map[string]any) { m["body"] = `{"type":"heartbeat","at":"2026-11-08T03:04:06Z"}` })},
		"changed mac":          {a2c, 1, tamper(func(m map[string]any) { m["mac"] = "AAAA" })},
		"extra field":          {a2c, 1, tamper(func(m map[string]any) { m["x"] = 1 })},
		"missing mac":          {a2c, 1, tamper(func(m map[string]any) { delete(m, "mac") })},
		"not json":             {a2c, 1, []byte("hello")},
	}
	for name, c := range cases {
		if _, err := OpenFrame(c.key, c.want, c.data); !errors.Is(err, ErrFrame) {
			t.Errorf("%s: got %v, want ErrFrame", name, err)
		}
	}
}

// In the lenient handshake a key that differs only in case is ignored, so
// it can never fill the real field.
func TestHandshakeIgnoresCaseVariantKeys(t *testing.T) {
	m, err := DecodeHandshake(readFixture(t, "handshake-lenient/hello_case_variant_key_is_ignored.json"))
	if err != nil {
		t.Fatal(err)
	}
	if h := m.(*Hello); h.Nonce != loadSignature(t).AgentNonce {
		t.Fatalf("nonce %q was filled from a case-variant key", h.Nonce)
	}
}

// The hello digest is over the exact text, and the hello text decodes to
// the hello fixture; the welcome digest is computed from the welcome
// fixture's fields with the length-prefixed encoding.
func TestHelloAndWelcomeDigestsMatchFixture(t *testing.T) {
	s := loadSignature(t)
	if HelloDigest([]byte(s.HelloText)) != s.HelloDigestHex {
		t.Fatal("hello digest differs from fixture")
	}
	var fromText, fromFile any
	_ = json.Unmarshal([]byte(s.HelloText), &fromText)
	_ = json.Unmarshal(readFixture(t, "messages/hello.json"), &fromFile)
	if !reflect.DeepEqual(fromText, fromFile) {
		t.Fatal("helloText is not the hello fixture")
	}
	m, err := DecodeHandshake(readFixture(t, "messages/welcome.json"))
	if err != nil {
		t.Fatal(err)
	}
	w := m.(*Welcome)
	if got := WelcomeDigest(w.CoreVersion, w.HeartbeatSeconds, w.Capabilities); got != s.WelcomeDigestHex {
		t.Fatalf("welcome digest %s, fixture %s", got, s.WelcomeDigestHex)
	}
	// Each covered field changes the digest.
	for name, d := range map[string]string{
		"core version": WelcomeDigest(w.CoreVersion+"x", w.HeartbeatSeconds, w.Capabilities),
		"heartbeat":    WelcomeDigest(w.CoreVersion, w.HeartbeatSeconds+1, w.Capabilities),
		"capabilities": WelcomeDigest(w.CoreVersion, w.HeartbeatSeconds, append(w.Capabilities, Capability{Name: "x", Version: 1})),
		"cap boundary": WelcomeDigest(w.CoreVersion, w.HeartbeatSeconds, []Capability{{Name: "jobs", Version: 1}, {Name: "", Version: 0}}),
	} {
		if d == s.WelcomeDigestHex {
			t.Errorf("%s change does not change the digest", name)
		}
	}
}

func TestFrameWithDuplicateKeyIsRejected(t *testing.T) {
	key := make([]byte, 32)
	good, _ := SealFrame(key, 1, []byte(`{"type":"ack","seq":1}`))
	dup := append(bytes.TrimSuffix(good, []byte("}")), []byte(`,"n":1}`)...)
	if _, err := OpenFrame(key, 1, dup); !errors.Is(err, ErrFrame) {
		t.Fatalf("duplicate n accepted: %v", err)
	}
}
