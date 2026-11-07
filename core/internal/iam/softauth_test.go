package iam

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// softAuthenticator is a software WebAuthn authenticator for tests. It plays
// the part of the browser and the authenticator: register stands in for
// navigator.credentials.create() and assert for navigator.credentials.get().
// It produces ES256 (P-256) credentials with the "none" attestation format.
type softAuthenticator struct {
	key        *ecdsa.PrivateKey
	credID     []byte // 16 random bytes
	userHandle []byte // taken from the registration options' user.id
	rpID       string // RP ID recorded at registration
	signCount  uint32 // incremented before every assertion
	// forceRPID, when not empty, is the RP ID hashed into assertions. It
	// simulates a credential that was created for another domain.
	forceRPID string
}

// Authenticator data flag bits (WebAuthn section 6.1).
const (
	softFlagUP byte = 0x01 // user present
	softFlagUV byte = 0x04 // user verified
	softFlagAT byte = 0x40 // attested credential data included
)

// COSE key labels and values (RFC 8152 and RFC 8812).
const (
	softCOSELabelKty = 1
	softCOSELabelAlg = 3
	softCOSELabelCrv = -1
	softCOSELabelX   = -2
	softCOSELabelY   = -3
	softCOSEKtyEC2   = 2
	softCOSEAlgES256 = -7
	softCOSECrvP256  = 1
)

func newSoftAuthenticator(t *testing.T) *softAuthenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate soft authenticator key: %v", err)
	}
	credID := make([]byte, 16)
	if _, err := rand.Read(credID); err != nil {
		t.Fatalf("generate soft authenticator credential ID: %v", err)
	}
	return &softAuthenticator{key: key, credID: credID}
}

// softOptions holds the fields of a WebAuthn options object that the
// authenticator reads. The same struct serves creation and request options.
type softOptions struct {
	PublicKey struct {
		Challenge string `json:"challenge"`
		RPID      string `json:"rpId"`
		RP        struct {
			ID string `json:"id"`
		} `json:"rp"`
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	} `json:"publicKey"`
}

func softParseOptions(t *testing.T, optionsJSON []byte) softOptions {
	t.Helper()
	var opts softOptions
	if err := json.Unmarshal(optionsJSON, &opts); err != nil {
		t.Fatalf("parse WebAuthn options: %v", err)
	}
	if opts.PublicKey.Challenge == "" {
		t.Fatalf("WebAuthn options carry no challenge")
	}
	return opts
}

func softDecodeBase64URL(t *testing.T, field, value string) []byte {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(value, "="))
	if err != nil {
		t.Fatalf("decode %s: %v", field, err)
	}
	return raw
}

func softEncodeBase64URL(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

// softClientDataJSON builds the client data the browser would collect. The
// challenge is echoed exactly as the server sent it.
func softClientDataJSON(t *testing.T, ceremony, challenge, origin string) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"type":        ceremony,
		"challenge":   challenge,
		"origin":      origin,
		"crossOrigin": false,
	})
	if err != nil {
		t.Fatalf("marshal client data: %v", err)
	}
	return data
}

// softAuthenticatorData lays out rpIdHash, flags, signCount and, when
// present, the attested credential data.
func softAuthenticatorData(rpID string, flags byte, signCount uint32, attested []byte) []byte {
	hash := sha256.Sum256([]byte(rpID))
	data := make([]byte, 0, 37+len(attested))
	data = append(data, hash[:]...)
	data = append(data, flags)
	data = binary.BigEndian.AppendUint32(data, signCount)
	return append(data, attested...)
}

// softCOSEPublicKey encodes the authenticator's P-256 public key as a COSE_Key.
func softCOSEPublicKey(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	em, err := cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		t.Fatalf("create CBOR encoder: %v", err)
	}
	data, err := em.Marshal(map[int]any{
		softCOSELabelKty: softCOSEKtyEC2,
		softCOSELabelAlg: softCOSEAlgES256,
		softCOSELabelCrv: softCOSECrvP256,
		softCOSELabelX:   key.PublicKey.X.FillBytes(make([]byte, 32)),
		softCOSELabelY:   key.PublicKey.Y.FillBytes(make([]byte, 32)),
	})
	if err != nil {
		t.Fatalf("encode COSE public key: %v", err)
	}
	return data
}

// register answers a navigator.credentials.create() call. optionsJSON is the
// CredentialCreation JSON the server sent, and the result is the credential
// JSON the browser would POST back.
func (a *softAuthenticator) register(t *testing.T, optionsJSON []byte, origin string) []byte {
	t.Helper()
	opts := softParseOptions(t, optionsJSON)
	a.rpID = opts.PublicKey.RP.ID
	if a.rpID == "" {
		t.Fatalf("registration options carry no rp.id")
	}
	a.userHandle = softDecodeBase64URL(t, "user.id", opts.PublicKey.User.ID)
	if len(a.userHandle) == 0 {
		t.Fatalf("registration options carry no user.id")
	}

	clientData := softClientDataJSON(t, "webauthn.create", opts.PublicKey.Challenge, origin)

	// Attested credential data: AAGUID (16 zero bytes), credential ID length
	// and ID, then the COSE public key.
	attested := make([]byte, 16)
	attested = binary.BigEndian.AppendUint16(attested, uint16(len(a.credID)))
	attested = append(attested, a.credID...)
	attested = append(attested, softCOSEPublicKey(t, a.key)...)

	authData := softAuthenticatorData(a.rpID, softFlagUP|softFlagUV|softFlagAT, 0, attested)

	em, err := cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		t.Fatalf("create CBOR encoder: %v", err)
	}
	attestationObject, err := em.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": authData,
	})
	if err != nil {
		t.Fatalf("encode attestation object: %v", err)
	}

	id := softEncodeBase64URL(a.credID)
	body, err := json.Marshal(map[string]any{
		"id":    id,
		"rawId": id,
		"type":  "public-key",
		"response": map[string]any{
			"attestationObject": softEncodeBase64URL(attestationObject),
			"clientDataJSON":    softEncodeBase64URL(clientData),
		},
	})
	if err != nil {
		t.Fatalf("marshal registration response: %v", err)
	}
	return body
}

// assert answers a navigator.credentials.get() call. optionsJSON is the
// CredentialAssertion JSON the server sent, and the result is the assertion
// JSON the browser would POST back.
func (a *softAuthenticator) assert(t *testing.T, optionsJSON []byte, origin string) []byte {
	t.Helper()
	if len(a.userHandle) == 0 {
		t.Fatalf("assert called before register")
	}
	opts := softParseOptions(t, optionsJSON)

	rpID := opts.PublicKey.RPID
	if rpID == "" {
		rpID = a.rpID
	}
	if a.forceRPID != "" {
		rpID = a.forceRPID
	}
	if rpID == "" {
		t.Fatalf("no RP ID available for the assertion")
	}

	clientData := softClientDataJSON(t, "webauthn.get", opts.PublicKey.Challenge, origin)

	a.signCount++
	authData := softAuthenticatorData(rpID, softFlagUP|softFlagUV, a.signCount, nil)

	// ES256 signs authenticatorData || SHA-256(clientDataJSON) and the
	// signature is DER encoded.
	clientHash := sha256.Sum256(clientData)
	signed := make([]byte, 0, len(authData)+len(clientHash))
	signed = append(signed, authData...)
	signed = append(signed, clientHash[:]...)
	digest := sha256.Sum256(signed)
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}

	id := softEncodeBase64URL(a.credID)
	body, err := json.Marshal(map[string]any{
		"id":    id,
		"rawId": id,
		"type":  "public-key",
		"response": map[string]any{
			"authenticatorData": softEncodeBase64URL(authData),
			"clientDataJSON":    softEncodeBase64URL(clientData),
			"signature":         softEncodeBase64URL(sig),
			"userHandle":        softEncodeBase64URL(a.userHandle),
		},
	})
	if err != nil {
		t.Fatalf("marshal assertion response: %v", err)
	}
	return body
}

// softTestUser is the relying party's view of one account in these tests.
type softTestUser struct {
	id    []byte
	creds []webauthn.Credential
}

func (u *softTestUser) WebAuthnID() []byte                         { return u.id }
func (u *softTestUser) WebAuthnName() string                       { return "soft-test-user" }
func (u *softTestUser) WebAuthnDisplayName() string                { return "Soft Test User" }
func (u *softTestUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

const (
	softTestRPID   = "panel.example.test"
	softTestOrigin = "https://panel.example.test"
)

func softTestWebAuthn(t *testing.T) *webauthn.WebAuthn {
	t.Helper()
	w, err := webauthn.New(&webauthn.Config{
		RPID:          softTestRPID,
		RPDisplayName: "HazelminX",
		RPOrigins:     []string{softTestOrigin},
	})
	if err != nil {
		t.Fatalf("create webauthn relying party: %v", err)
	}
	return w
}

// softRegistered runs a full registration through the public go-webauthn API
// and returns the relying party, the user holding the new credential, and the
// authenticator that owns the key.
func softRegistered(t *testing.T) (*webauthn.WebAuthn, *softTestUser, *softAuthenticator) {
	t.Helper()
	w := softTestWebAuthn(t)
	user := &softTestUser{id: []byte("soft-test-user-id-0001")}
	a := newSoftAuthenticator(t)

	creation, session, err := w.BeginRegistration(user)
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	optionsJSON, err := json.Marshal(creation)
	if err != nil {
		t.Fatalf("marshal creation options: %v", err)
	}

	body := a.register(t, optionsJSON, softTestOrigin)
	parsed, err := protocol.ParseCredentialCreationResponseBytes(body)
	if err != nil {
		t.Fatalf("ParseCredentialCreationResponseBytes: %v", err)
	}
	cred, err := w.CreateCredential(user, *session, parsed)
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	user.creds = append(user.creds, *cred)
	return w, user, a
}

// softBeginLogin starts an assertion and returns the session together with
// the options JSON the server would send to the browser.
func softBeginLogin(t *testing.T, w *webauthn.WebAuthn, user *softTestUser) (*webauthn.SessionData, []byte) {
	t.Helper()
	assertion, session, err := w.BeginLogin(user)
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	optionsJSON, err := json.Marshal(assertion)
	if err != nil {
		t.Fatalf("marshal assertion options: %v", err)
	}
	return session, optionsJSON
}

// softValidate parses an assertion response and validates it against session.
func softValidate(t *testing.T, w *webauthn.WebAuthn, user *softTestUser, session *webauthn.SessionData, body []byte) (*webauthn.Credential, error) {
	t.Helper()
	parsed, err := protocol.ParseCredentialRequestResponseBytes(body)
	if err != nil {
		t.Fatalf("ParseCredentialRequestResponseBytes: %v", err)
	}
	return w.ValidateLogin(user, *session, parsed)
}

func TestSoftAuthenticatorRoundTrip(t *testing.T) {
	w, user, a := softRegistered(t)

	for want := uint32(1); want <= 2; want++ {
		session, opts := softBeginLogin(t, w, user)
		body := a.assert(t, opts, softTestOrigin)
		cred, err := softValidate(t, w, user, session, body)
		if err != nil {
			t.Fatalf("ValidateLogin for assertion %d: %v", want, err)
		}
		if cred.Authenticator.SignCount != want {
			t.Fatalf("SignCount after assertion %d = %d, want %d", want, cred.Authenticator.SignCount, want)
		}
		// The relying party stores the updated credential, as it would after a login.
		user.creds[0] = *cred
	}

	t.Run("RejectsWrongOrigin", func(t *testing.T) {
		w, user, a := softRegistered(t)

		session, opts := softBeginLogin(t, w, user)
		body := a.assert(t, opts, "https://evil.example")
		if cred, err := softValidate(t, w, user, session, body); err == nil {
			t.Fatalf("ValidateLogin accepted an assertion from origin https://evil.example (SignCount %d)", cred.Authenticator.SignCount)
		}

		// Control: the same authenticator with the right origin must pass.
		session, opts = softBeginLogin(t, w, user)
		body = a.assert(t, opts, softTestOrigin)
		if _, err := softValidate(t, w, user, session, body); err != nil {
			t.Fatalf("control assertion with the right origin failed: %v", err)
		}
	})

	t.Run("RejectsCredentialForOtherRPID", func(t *testing.T) {
		w, user, a := softRegistered(t)

		a.forceRPID = "other.example"
		session, opts := softBeginLogin(t, w, user)
		body := a.assert(t, opts, softTestOrigin)
		if cred, err := softValidate(t, w, user, session, body); err == nil {
			t.Fatalf("ValidateLogin accepted an assertion hashed for other.example (SignCount %d)", cred.Authenticator.SignCount)
		}

		// Control: without the forced RP ID the same flow must pass.
		a.forceRPID = ""
		session, opts = softBeginLogin(t, w, user)
		body = a.assert(t, opts, softTestOrigin)
		if _, err := softValidate(t, w, user, session, body); err != nil {
			t.Fatalf("control assertion without forceRPID failed: %v", err)
		}
	})

	t.Run("RejectsReusedSession", func(t *testing.T) {
		w, user, a := softRegistered(t)

		firstSession, firstOpts := softBeginLogin(t, w, user)
		firstBody := a.assert(t, firstOpts, softTestOrigin)
		cred, err := softValidate(t, w, user, firstSession, firstBody)
		if err != nil {
			t.Fatalf("first ValidateLogin failed: %v", err)
		}
		user.creds[0] = *cred

		// A second ceremony issues a new challenge. The assertion signs that
		// challenge, but it is checked against the first session.
		secondSession, secondOpts := softBeginLogin(t, w, user)
		secondBody := a.assert(t, secondOpts, softTestOrigin)
		if cred, err := softValidate(t, w, user, firstSession, secondBody); err == nil {
			t.Fatalf("ValidateLogin accepted a challenge that does not belong to the session (SignCount %d)", cred.Authenticator.SignCount)
		}

		// Control: the same assertion validates against its own session.
		if _, err := softValidate(t, w, user, secondSession, secondBody); err != nil {
			t.Fatalf("control assertion with its own session failed: %v", err)
		}
	})
}
