package nodes

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
)

// Fixed clock for the tests that need a date. The fixture certificates are
// valid from 2026-01-01 until 2126, except node-expired.crt (ends 2026-06-01).
var pkiFixedNow = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

const pkiFixtureDir = "../../../docs/contracts/fixtures/pki"

// callNoPanic runs fn. The unimplemented PKI functions panic; a panic becomes
// a failure of the calling (sub)test instead of aborting the whole run.
func callNoPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic (function not implemented?): %v", r)
		}
	}()
	fn()
}

// pkiRecordingStore records the inputs of Create calls made by the code under
// test, on top of the in-memory store from harness_test.go.
type pkiRecordingStore struct {
	*memSecrets
	creates []secrets.CreateInput
}

func (r *pkiRecordingStore) Create(ctx context.Context, in secrets.CreateInput) (secrets.Secret, error) {
	r.creates = append(r.creates, in)
	return r.memSecrets.Create(ctx, in)
}

func pkiLoad(t *testing.T, st SecretStore, now func() time.Time) (*PKI, error) {
	t.Helper()
	var p *PKI
	var err error
	callNoPanic(t, func() { p, err = LoadOrCreatePKI(context.Background(), st, now) })
	return p, err
}

// pkiFixedClock returns a now function fixed at pkiFixedNow.
func pkiFixedClock() time.Time { return pkiFixedNow }

// pkiStoredValues reads every secret in st by name, with the consumer that
// the PKI uses.
func pkiStoredValues(t *testing.T, st *memSecrets) map[string][]byte {
	t.Helper()
	ctx := context.Background()
	list, err := st.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, s := range list {
		v, _, err := st.Reveal(ctx, secrets.RevealRequest{ID: s.ID, Consumer: SecretConsumer})
		if err != nil {
			t.Fatalf("reveal %s: %v", s.Name, err)
		}
		out[s.Name] = v
	}
	return out
}

// pkiBundles returns the stored CA bundle and Core bundle of one fresh PKI.
func pkiBundles(t *testing.T) (caBundle, coreBundle []byte) {
	t.Helper()
	st := newMemSecrets()
	if _, err := pkiLoad(t, st, nil); err != nil {
		t.Fatalf("create PKI for fixtures: %v", err)
	}
	vals := pkiStoredValues(t, st)
	return vals[SecretCA], vals[SecretCore]
}

// pkiBlockTypes lists the PEM block types in data, in order.
func pkiBlockTypes(data []byte) []string {
	var types []string
	for rest := data; ; {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			return types
		}
		types = append(types, b.Type)
	}
}

// pkiKeyDER returns the DER of the first PRIVATE KEY block in bundle.
func pkiKeyDER(t *testing.T, bundle []byte) []byte {
	t.Helper()
	for rest := bundle; ; {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			t.Fatal("bundle has no PRIVATE KEY block")
		}
		if b.Type == "PRIVATE KEY" {
			return b.Bytes
		}
	}
}

// pkiParseCert parses a PEM document whose first block is one CERTIFICATE.
func pkiParseCert(t *testing.T, data []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("not a CERTIFICATE PEM: %.40q", data)
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return c
}

// pkiParseKey parses a PEM PKCS#8 Ed25519 private key.
func pkiParseKey(t *testing.T, data []byte) ed25519.PrivateKey {
	t.Helper()
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" {
		t.Fatalf("not a PRIVATE KEY PEM: %.40q", data)
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse PKCS#8: %v", err)
	}
	ed, ok := k.(ed25519.PrivateKey)
	if !ok {
		t.Fatalf("key is %T, want ed25519.PrivateKey", k)
	}
	return ed
}

func pkiReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(pkiFixtureDir, name))
	if err != nil {
		t.Fatalf("read PKI fixture %s: %v", name, err)
	}
	return b
}

// pkiRequireNoKeyLeak fails when err shows the key, in any of the forms a
// careless message could take: the word, base64 of the DER, or the seed in hex.
func pkiRequireNoKeyLeak(t *testing.T, err error, keyDER []byte) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if strings.Contains(msg, "PRIVATE KEY") {
		t.Fatalf("error mentions PRIVATE KEY: %q", msg)
	}
	b64 := base64.StdEncoding.EncodeToString(keyDER)
	if mid := b64[len(b64)/2 : len(b64)/2+32]; strings.Contains(msg, mid) {
		t.Fatalf("error contains base64 of the private key: %q", msg)
	}
	seed := keyDER[len(keyDER)-32:]
	if strings.Contains(msg, hex.EncodeToString(seed[:16])) {
		t.Fatalf("error contains the private key seed: %q", msg)
	}
}

// pkiCACert and pkiCoreCert call the slot methods and parse the result.
func pkiCACert(t *testing.T, p *PKI) *x509.Certificate {
	t.Helper()
	var der []byte
	callNoPanic(t, func() { der = p.CACertificatePEM() })
	return pkiParseCert(t, der)
}

func pkiCoreCert(t *testing.T, p *PKI) *x509.Certificate {
	t.Helper()
	var der []byte
	callNoPanic(t, func() { der = p.CoreCertificatePEM() })
	return pkiParseCert(t, der)
}

func pkiTestPKI(t *testing.T) *PKI {
	t.Helper()
	var p *PKI
	callNoPanic(t, func() { p = testPKI(t) })
	return p
}

func pkiFingerprint(t *testing.T, pub ed25519.PublicKey) string {
	t.Helper()
	var fp string
	callNoPanic(t, func() { fp = protocol.Fingerprint(pub) })
	return fp
}

func pkiIssue(t *testing.T, p *PKI, pub ed25519.PublicKey, validity time.Duration) ([]byte, error) {
	t.Helper()
	var out []byte
	var err error
	callNoPanic(t, func() { out, err = p.IssueNodeCertificate(pub, validity) })
	return out, err
}

func pkiVerify(t *testing.T, p *PKI, certPEM []byte, now time.Time) (NodeIdentity, error) {
	t.Helper()
	var id NodeIdentity
	var err error
	callNoPanic(t, func() { id, err = p.VerifyNodeCertificate(certPEM, now) })
	return id, err
}

// pkiSelfCA builds a throwaway Ed25519 CA for tests that need a second CA.
func pkiSelfCA(t *testing.T, now time.Time) (*x509.Certificate, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "pki test throwaway CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, priv
}

// pkiLeafPEM signs a clientAuth node certificate for nodePub with caCert and
// the given common name, so tests can vary the CN and nothing else.
func pkiLeafPEM(t *testing.T, caCert *x509.Certificate, caKey ed25519.PrivateKey, nodePub ed25519.PublicKey, cn string, now time.Time) []byte {
	t.Helper()
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, caCert, nodePub, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// pkiServer starts a TLS server with cfg. Its handler reports the peer
// certificates it saw, or nil when there were none.
func pkiServer(t *testing.T, cfg *tls.Config) (*httptest.Server, chan []*x509.Certificate) {
	t.Helper()
	seen := make(chan []*x509.Certificate, 4)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var peers []*x509.Certificate
		if r.TLS != nil {
			peers = r.TLS.PeerCertificates
		}
		seen <- peers
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = cfg
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, seen
}

// pkiClient is a client that trusts pool and names the core, and that can
// present certs and cap the TLS version.
func pkiClient(t *testing.T, pool *x509.CertPool, certs []tls.Certificate, maxVersion uint16) *http.Client {
	t.Helper()
	tr := &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs:      pool,
		ServerName:   protocol.CoreServerName,
		MinVersion:   tls.VersionTLS12,
		MaxVersion:   maxVersion,
		Certificates: certs,
	}}
	t.Cleanup(tr.CloseIdleConnections)
	return &http.Client{Transport: tr, Timeout: 5 * time.Second}
}

// --- LoadOrCreatePKI ---

// An empty store gets two private-key secrets with the documented names,
// kind, scope and actor, and a PEM bundle of key then certificate.
func TestLoadOrCreatePKI_EmptyStoreCreatesBothSecrets(t *testing.T) {
	st := &pkiRecordingStore{memSecrets: newMemSecrets()}
	if _, err := pkiLoad(t, st, pkiFixedClock); err != nil {
		t.Fatalf("LoadOrCreatePKI: %v", err)
	}
	if len(st.creates) != 2 {
		t.Fatalf("created %d secrets, want 2", len(st.creates))
	}
	names := map[string]bool{}
	for _, in := range st.creates {
		names[in.Name] = true
		if in.Kind != secrets.KindPrivateKey {
			t.Errorf("%s kind = %q, want private-key", in.Name, in.Kind)
		}
		if len(in.Scope) != 1 || in.Scope[0] != SecretConsumer {
			t.Errorf("%s scope = %v, want [%s]", in.Name, in.Scope, SecretConsumer)
		}
		if in.Actor != pkiActor {
			t.Errorf("%s actor = %q, want %q", in.Name, in.Actor, pkiActor)
		}
		if got := pkiBlockTypes(in.Value); strings.Join(got, ",") != "PRIVATE KEY,CERTIFICATE" {
			t.Errorf("%s bundle blocks = %v, want [PRIVATE KEY CERTIFICATE]", in.Name, got)
		}
	}
	if !names[SecretCA] || !names[SecretCore] {
		t.Fatalf("created names = %v, want %s and %s", names, SecretCA, SecretCore)
	}
}

// The CA is self-signed Ed25519 with IsCA and certificate signing, and lasts
// about CAValidity from now.
func TestLoadOrCreatePKI_CAProperties(t *testing.T) {
	p, err := pkiLoad(t, newMemSecrets(), pkiFixedClock)
	if err != nil {
		t.Fatalf("LoadOrCreatePKI: %v", err)
	}
	ca := pkiCACert(t, p)
	if !ca.IsCA {
		t.Error("CA certificate is not marked IsCA")
	}
	if ca.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Error("CA certificate lacks KeyUsageCertSign")
	}
	if ca.PublicKeyAlgorithm != x509.Ed25519 {
		t.Errorf("CA key algorithm = %v, want Ed25519", ca.PublicKeyAlgorithm)
	}
	if ca.Subject.String() != ca.Issuer.String() {
		t.Errorf("CA is not self-issued: subject %s, issuer %s", ca.Subject, ca.Issuer)
	}
	if err := ca.CheckSignatureFrom(ca); err != nil {
		t.Errorf("CA signature does not verify with its own key: %v", err)
	}
	if !pkiFixedNow.After(ca.NotBefore) && !pkiFixedNow.Equal(ca.NotBefore) {
		t.Errorf("CA NotBefore %s is after now", ca.NotBefore)
	}
	want := pkiFixedNow.Add(CAValidity)
	if d := ca.NotAfter.Sub(want); d < -time.Hour || d > time.Hour {
		t.Errorf("CA NotAfter %s, want about %s", ca.NotAfter, want)
	}
}

// The Core certificate is signed by the CA, has only the serverAuth usage,
// and carries the core name as DNS SAN and CN. It lasts about CoreValidity.
func TestLoadOrCreatePKI_CoreCertificateChain(t *testing.T) {
	p, err := pkiLoad(t, newMemSecrets(), pkiFixedClock)
	if err != nil {
		t.Fatalf("LoadOrCreatePKI: %v", err)
	}
	ca := pkiCACert(t, p)
	core := pkiCoreCert(t, p)
	if err := core.CheckSignatureFrom(ca); err != nil {
		t.Fatalf("core certificate is not signed by the CA: %v", err)
	}
	if len(core.ExtKeyUsage) != 1 || core.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
		t.Errorf("ExtKeyUsage = %v, want only serverAuth", core.ExtKeyUsage)
	}
	if core.IsCA {
		t.Error("core certificate is marked IsCA")
	}
	if core.Subject.CommonName != protocol.CoreServerName {
		t.Errorf("CN = %q, want %q", core.Subject.CommonName, protocol.CoreServerName)
	}
	found := false
	for _, n := range core.DNSNames {
		found = found || n == protocol.CoreServerName
	}
	if !found {
		t.Errorf("DNSNames = %v, want %q", core.DNSNames, protocol.CoreServerName)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	if _, err := core.Verify(x509.VerifyOptions{
		Roots:       pool,
		DNSName:     protocol.CoreServerName,
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		CurrentTime: pkiFixedNow,
	}); err != nil {
		t.Errorf("core certificate does not verify for the core name: %v", err)
	}
	want := pkiFixedNow.Add(CoreValidity)
	if d := core.NotAfter.Sub(want); d < -time.Hour || d > time.Hour {
		t.Errorf("core NotAfter %s, want about %s", core.NotAfter, want)
	}
}

// A second call on the same store loads the same material and creates
// nothing.
func TestLoadOrCreatePKI_SecondCallLoadsSameMaterial(t *testing.T) {
	st := &pkiRecordingStore{memSecrets: newMemSecrets()}
	p1, err := pkiLoad(t, st, pkiFixedClock)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	p2, err := pkiLoad(t, st, pkiFixedClock)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(st.creates) != 2 {
		t.Errorf("creates after two calls = %d, want 2", len(st.creates))
	}
	list, _ := st.List(context.Background())
	if len(list) != 2 {
		t.Errorf("secrets in store = %d, want 2", len(list))
	}
	var ca1, ca2, core1, core2 []byte
	callNoPanic(t, func() {
		ca1, ca2 = p1.CACertificatePEM(), p2.CACertificatePEM()
		core1, core2 = p1.CoreCertificatePEM(), p2.CoreCertificatePEM()
	})
	if !bytes.Equal(ca1, ca2) {
		t.Error("second call returned a different CA certificate")
	}
	if !bytes.Equal(core1, core2) {
		t.Error("second call returned a different Core certificate")
	}
}

// Exactly one of the two secrets exists: error, and nothing new is created.
func TestLoadOrCreatePKI_RefusesHalfPKI(t *testing.T) {
	caBundle, coreBundle := pkiBundles(t)
	cases := []struct {
		name  string
		which string
		value []byte
	}{
		{"only CA present", SecretCA, caBundle},
		{"only Core present", SecretCore, coreBundle},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := &pkiRecordingStore{memSecrets: newMemSecrets()}
			if _, err := st.memSecrets.Create(context.Background(), secrets.CreateInput{
				Kind: secrets.KindPrivateKey, Name: c.which, Value: c.value, Scope: []string{SecretConsumer},
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := pkiLoad(t, st, pkiFixedClock); err == nil {
				t.Fatal("LoadOrCreatePKI accepted a half PKI")
			}
			if len(st.creates) != 0 {
				t.Errorf("created %d secrets on a half PKI, want none", len(st.creates))
			}
			list, _ := st.List(context.Background())
			if len(list) != 1 {
				t.Errorf("secrets in store = %d, want 1", len(list))
			}
		})
	}
}

// A stored value that does not parse is an error, and the error does not
// show private key material.
func TestLoadOrCreatePKI_RejectsUnparseableSecret(t *testing.T) {
	_, coreBundle := pkiBundles(t)
	cases := []struct {
		name  string
		value []byte
	}{
		{"value is not PEM", []byte("not a pem bundle")},
		{"PRIVATE KEY block with corrupt body", []byte("-----BEGIN PRIVATE KEY-----\n!!!not base64!!!\n-----END PRIVATE KEY-----\n")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := &pkiRecordingStore{memSecrets: newMemSecrets()}
			if _, err := st.memSecrets.Create(context.Background(), secrets.CreateInput{
				Kind: secrets.KindPrivateKey, Name: SecretCA, Value: c.value, Scope: []string{SecretConsumer},
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := st.memSecrets.Create(context.Background(), secrets.CreateInput{
				Kind: secrets.KindPrivateKey, Name: SecretCore, Value: coreBundle, Scope: []string{SecretConsumer},
			}); err != nil {
				t.Fatal(err)
			}
			_, err := pkiLoad(t, st, pkiFixedClock)
			if err == nil {
				t.Fatal("LoadOrCreatePKI accepted an unparseable secret")
			}
			if strings.Contains(err.Error(), "PRIVATE KEY") {
				t.Fatalf("error mentions PRIVATE KEY: %q", err)
			}
			if len(st.creates) != 0 {
				t.Errorf("created %d secrets, want none", len(st.creates))
			}
		})
	}
}

// The stored CA and a Core certificate from another PKI do not match: error,
// and no key material in the error.
func TestLoadOrCreatePKI_RejectsCoreNotSignedByStoredCA(t *testing.T) {
	caA, _ := pkiBundles(t)
	_, coreB := pkiBundles(t)
	st := &pkiRecordingStore{memSecrets: newMemSecrets()}
	for _, in := range []secrets.CreateInput{
		{Kind: secrets.KindPrivateKey, Name: SecretCA, Value: caA, Scope: []string{SecretConsumer}},
		{Kind: secrets.KindPrivateKey, Name: SecretCore, Value: coreB, Scope: []string{SecretConsumer}},
	} {
		if _, err := st.memSecrets.Create(context.Background(), in); err != nil {
			t.Fatal(err)
		}
	}
	_, err := pkiLoad(t, st, pkiFixedClock)
	if err == nil {
		t.Fatal("LoadOrCreatePKI accepted a Core certificate not signed by the stored CA")
	}
	pkiRequireNoKeyLeak(t, err, pkiKeyDER(t, caA))
	pkiRequireNoKeyLeak(t, err, pkiKeyDER(t, coreB))
	if len(st.creates) != 0 {
		t.Errorf("created %d secrets, want none", len(st.creates))
	}
}

// --- accessors ---

// CACertificatePEM is exactly one CERTIFICATE block, and it is the CA the PKI
// signs with.
func TestPKI_CACertificatePEM(t *testing.T) {
	p := pkiTestPKI(t)
	var der []byte
	callNoPanic(t, func() { der = p.CACertificatePEM() })
	if got := pkiBlockTypes(der); strings.Join(got, ",") != "CERTIFICATE" {
		t.Fatalf("CA PEM blocks = %v, want exactly [CERTIFICATE]", got)
	}
	ca := pkiParseCert(t, der)
	if !ca.IsCA {
		t.Error("CA PEM is not a CA certificate")
	}
	core := pkiCoreCert(t, p)
	if err := core.CheckSignatureFrom(ca); err != nil {
		t.Errorf("core certificate is not signed by the CA PEM: %v", err)
	}
}

// CoreCertificatePEM is exactly one CERTIFICATE block for the core name.
func TestPKI_CoreCertificatePEM(t *testing.T) {
	p := pkiTestPKI(t)
	var der []byte
	callNoPanic(t, func() { der = p.CoreCertificatePEM() })
	if got := pkiBlockTypes(der); strings.Join(got, ",") != "CERTIFICATE" {
		t.Fatalf("Core PEM blocks = %v, want exactly [CERTIFICATE]", got)
	}
	core := pkiParseCert(t, der)
	if core.Subject.CommonName != protocol.CoreServerName {
		t.Errorf("CN = %q, want %q", core.Subject.CommonName, protocol.CoreServerName)
	}
}

// SignAsCore output verifies with the public key in CoreCertificatePEM, and a
// different message does not.
func TestSignAsCore_VerifiesWithCoreCertificateKey(t *testing.T) {
	p := pkiTestPKI(t)
	core := pkiCoreCert(t, p)
	pub, ok := core.PublicKey.(ed25519.PublicKey)
	if !ok {
		t.Fatalf("core public key is %T, want ed25519.PublicKey", core.PublicKey)
	}
	msg := []byte("hazelminx-session-v1\ncore\nexample\n")
	var sig []byte
	callNoPanic(t, func() { sig = p.SignAsCore(msg) })
	if !ed25519.Verify(pub, msg, sig) {
		t.Fatal("SignAsCore signature does not verify with the Core certificate key")
	}
	if ed25519.Verify(pub, append(msg, '!'), sig) {
		t.Fatal("signature verifies a different message")
	}
}

// --- ServerTLSConfig ---

// The listener config is TLS 1.3 only, presents the Core identity, asks for
// but does not require client certs, and verifies them against the CA.
func TestServerTLSConfig_Settings(t *testing.T) {
	p := pkiTestPKI(t)
	var cfg *tls.Config
	callNoPanic(t, func() { cfg = p.ServerTLSConfig() })
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %#x, want TLS 1.3", cfg.MinVersion)
	}
	if cfg.ClientAuth != tls.VerifyClientCertIfGiven {
		t.Errorf("ClientAuth = %v, want VerifyClientCertIfGiven", cfg.ClientAuth)
	}
	if cfg.ClientCAs == nil {
		t.Error("ClientCAs is nil")
	}
	if len(cfg.Certificates) != 1 || len(cfg.Certificates[0].Certificate) == 0 {
		t.Fatalf("Certificates = %d entries, want one core identity", len(cfg.Certificates))
	}
	leaf := pkiParseCert(t, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cfg.Certificates[0].Certificate[0]}))
	if leaf.Subject.CommonName != protocol.CoreServerName {
		t.Errorf("served certificate CN = %q, want %q", leaf.Subject.CommonName, protocol.CoreServerName)
	}
}

// Handshake behaviour over a real TLS listener.
func TestServerTLSConfig_Handshake(t *testing.T) {
	t.Run("node certificate from this CA is accepted and visible to the handler", func(t *testing.T) {
		p := pkiTestPKI(t)
		var cfg *tls.Config
		callNoPanic(t, func() { cfg = p.ServerTLSConfig() })
		srv, seen := pkiServer(t, cfg)

		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		certPEM, err := pkiIssue(t, p, pub, 24*time.Hour)
		if err != nil {
			t.Fatalf("IssueNodeCertificate: %v", err)
		}
		node := testNode{key: priv, cert: certPEM, fp: pkiFingerprint(t, pub)}
		client := pkiClient(t, pkiCAPool(t, p), []tls.Certificate{node.tlsCert(t)}, 0)
		resp, err := client.Get(srv.URL)
		if err != nil {
			t.Fatalf("handshake with a CA-issued node certificate failed: %v", err)
		}
		resp.Body.Close()
		select {
		case peers := <-seen:
			if len(peers) != 1 {
				t.Fatalf("handler saw %d peer certificates, want 1", len(peers))
			}
			if got := pkiFingerprint(t, peers[0].PublicKey.(ed25519.PublicKey)); got != node.fp {
				t.Errorf("peer fingerprint = %s, want %s", got, node.fp)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("handler not reached")
		}
	})

	t.Run("client without a certificate is accepted", func(t *testing.T) {
		p := pkiTestPKI(t)
		var cfg *tls.Config
		callNoPanic(t, func() { cfg = p.ServerTLSConfig() })
		srv, seen := pkiServer(t, cfg)
		client := pkiClient(t, pkiCAPool(t, p), nil, 0)
		resp, err := client.Get(srv.URL)
		if err != nil {
			t.Fatalf("handshake without a client certificate failed: %v", err)
		}
		resp.Body.Close()
		select {
		case peers := <-seen:
			if len(peers) != 0 {
				t.Fatalf("handler saw %d peer certificates, want none", len(peers))
			}
		case <-time.After(5 * time.Second):
			t.Fatal("handler not reached")
		}
	})

	t.Run("client certificate from another PKI fails the handshake", func(t *testing.T) {
		p := pkiTestPKI(t)
		other := pkiTestPKI(t)
		var cfg *tls.Config
		callNoPanic(t, func() { cfg = p.ServerTLSConfig() })
		srv, _ := pkiServer(t, cfg)
		foreign := pkiForeignNode(t, other)
		client := pkiClient(t, pkiCAPool(t, p), []tls.Certificate{foreign.tlsCert(t)}, 0)
		if resp, err := client.Get(srv.URL); err == nil {
			resp.Body.Close()
			t.Fatal("handshake with a certificate from another CA succeeded")
		}
	})

	t.Run("TLS 1.2 client is refused", func(t *testing.T) {
		p := pkiTestPKI(t)
		var cfg *tls.Config
		callNoPanic(t, func() { cfg = p.ServerTLSConfig() })
		srv, _ := pkiServer(t, cfg)
		client := pkiClient(t, pkiCAPool(t, p), nil, tls.VersionTLS12)
		if resp, err := client.Get(srv.URL); err == nil {
			resp.Body.Close()
			t.Fatal("TLS 1.2 handshake succeeded; the listener must be TLS 1.3 only")
		}
	})
}

// pkiForeignNode is a node certificate issued by p, returned as a testNode.
func pkiForeignNode(t *testing.T, p *PKI) testNode {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certPEM, err := pkiIssue(t, p, pub, 24*time.Hour)
	if err != nil {
		t.Fatalf("IssueNodeCertificate: %v", err)
	}
	return testNode{key: priv, cert: certPEM, fp: pkiFingerprint(t, pub)}
}

// pkiCAPool returns a pool holding the CA of p, wrapped so a slot panic fails the subtest.
func pkiCAPool(t *testing.T, p *PKI) *x509.CertPool {
	t.Helper()
	var der []byte
	callNoPanic(t, func() { der = p.CACertificatePEM() })
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(der)
	return pool
}

// --- IssueNodeCertificate ---

// An issued certificate verifies, names its key by fingerprint, is a
// clientAuth-only leaf, starts about five minutes in the past and runs for the
// requested validity.
func TestIssueNodeCertificate_RoundTrip(t *testing.T) {
	p := pkiTestPKI(t)
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const validity = 24 * time.Hour
	before := time.Now()
	certPEM, err := pkiIssue(t, p, pub, validity)
	after := time.Now()
	if err != nil {
		t.Fatalf("IssueNodeCertificate: %v", err)
	}
	fp := pkiFingerprint(t, pub)

	id, err := pkiVerify(t, p, certPEM, time.Now())
	if err != nil {
		t.Fatalf("VerifyNodeCertificate rejects its own issue: %v", err)
	}
	if id.Fingerprint != fp {
		t.Errorf("fingerprint = %s, want %s", id.Fingerprint, fp)
	}
	if !bytes.Equal(id.PublicKey, pub) {
		t.Error("identity public key differs from the issued key")
	}

	cert := pkiParseCert(t, certPEM)
	if cert.Subject.CommonName != fp {
		t.Errorf("CN = %q, want fingerprint %q", cert.Subject.CommonName, fp)
	}
	if len(cert.ExtKeyUsage) != 1 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Errorf("ExtKeyUsage = %v, want only clientAuth", cert.ExtKeyUsage)
	}
	if cert.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		t.Error("KeyUsage lacks DigitalSignature")
	}
	if got, ok := cert.PublicKey.(ed25519.PublicKey); !ok || !bytes.Equal(got, pub) {
		t.Error("certificate public key differs from the issued key")
	}
	if lo, hi := before.Add(-6*time.Minute), after.Add(-4*time.Minute); cert.NotBefore.Before(lo) || cert.NotBefore.After(hi) {
		t.Errorf("NotBefore %s, want about five minutes before now", cert.NotBefore)
	}
	if lo, hi := before.Add(validity-time.Minute), after.Add(validity+time.Minute); cert.NotAfter.Before(lo) || cert.NotAfter.After(hi) {
		t.Errorf("NotAfter %s, want about now plus %s", cert.NotAfter, validity)
	}
}

// Validity at exactly MaxNodeValidity is allowed.
func TestIssueNodeCertificate_MaxValidityAllowed(t *testing.T) {
	p := pkiTestPKI(t)
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pkiIssue(t, p, pub, MaxNodeValidity); err != nil {
		t.Fatalf("validity equal to MaxNodeValidity refused: %v", err)
	}
}

// Invalid requests are errors: validity out of range, or a key that is not
// 32 bytes.
func TestIssueNodeCertificate_Rejects(t *testing.T) {
	p := pkiTestPKI(t)
	good, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		pub      ed25519.PublicKey
		validity time.Duration
	}{
		{"zero validity", good, 0},
		{"negative validity", good, -time.Hour},
		{"validity above MaxNodeValidity", good, MaxNodeValidity + time.Hour},
		{"key of 31 bytes", make(ed25519.PublicKey, 31), 24 * time.Hour},
		{"key of 33 bytes", make(ed25519.PublicKey, 33), 24 * time.Hour},
		{"empty key", nil, 24 * time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := pkiIssue(t, p, c.pub, c.validity); err == nil {
				t.Fatal("IssueNodeCertificate accepted an invalid request")
			}
		})
	}
}

// Two issues for the same key get different serial numbers.
func TestIssueNodeCertificate_SerialsDiffer(t *testing.T) {
	p := pkiTestPKI(t)
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	a, err := pkiIssue(t, p, pub, 24*time.Hour)
	if err != nil {
		t.Fatalf("first issue: %v", err)
	}
	b, err := pkiIssue(t, p, pub, 24*time.Hour)
	if err != nil {
		t.Fatalf("second issue: %v", err)
	}
	if pkiParseCert(t, a).SerialNumber.Cmp(pkiParseCert(t, b).SerialNumber) == 0 {
		t.Fatal("two issued certificates share a serial number")
	}
}

// --- VerifyNodeCertificate ---

// pkiFixturePKI builds a PKI from the fixture CA and Core material. The fixture
// CA private key is not in the samples, so the PKI can verify but not issue.
func pkiFixturePKI(t *testing.T) *PKI {
	t.Helper()
	ca := pkiParseCert(t, pkiReadFixture(t, "ca.crt"))
	core := pkiParseCert(t, pkiReadFixture(t, "core.crt"))
	coreKey := pkiParseKey(t, pkiReadFixture(t, "core.key"))
	return newPKIFromParts(ca, nil, core, coreKey, nil)
}

// fixtureNodePub is the public key of node.key, checked against node.crt.
func pkiFixtureNodePub(t *testing.T) ed25519.PublicKey {
	t.Helper()
	pub := pkiParseKey(t, pkiReadFixture(t, "node.key")).Public().(ed25519.PublicKey)
	if !bytes.Equal(pub, pkiParseCert(t, pkiReadFixture(t, "node.crt")).PublicKey.(ed25519.PublicKey)) {
		t.Fatal("fixture node.key does not match node.crt")
	}
	return pub
}

// node.crt verifies at the fixture date and yields the fingerprint of node.key.
func TestVerifyNodeCertificate_FixtureNodeCertificate(t *testing.T) {
	p := pkiFixturePKI(t)
	nodePub := pkiFixtureNodePub(t)
	data := pkiReadFixture(t, "node.crt")
	id, err := pkiVerify(t, p, data, pkiFixedNow)
	if err != nil {
		t.Fatalf("node.crt rejected: %v", err)
	}
	if want := pkiFingerprint(t, nodePub); id.Fingerprint != want {
		t.Errorf("fingerprint = %s, want %s", id.Fingerprint, want)
	}
	if !bytes.Equal(id.PublicKey, nodePub) {
		t.Error("identity public key differs from node.key")
	}
	if want := pkiParseCert(t, data).NotAfter; !id.NotAfter.Equal(want) {
		t.Errorf("NotAfter = %s, want %s", id.NotAfter, want)
	}
}

// Node certificates that must be refused at the fixture date.
func TestVerifyNodeCertificate_FixtureRejects(t *testing.T) {
	p := pkiFixturePKI(t)
	node := pkiReadFixture(t, "node.crt")
	core := pkiReadFixture(t, "core.crt")
	cases := []struct {
		name string
		data []byte
	}{
		{"node-rogue.crt (signed by another CA)", pkiReadFixture(t, "node-rogue.crt")},
		{"core.crt (serverAuth, not clientAuth)", core},
		{"two node certificates concatenated", append(append([]byte{}, node...), node...)},
		{"node certificate followed by core certificate", append(append([]byte{}, node...), core...)},
		{"garbage", []byte("this is not a certificate")},
		{"empty", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := pkiVerify(t, p, c.data, pkiFixedNow)
			if !errors.Is(err, ErrNodeCertificate) {
				t.Fatalf("error = %v, want ErrNodeCertificate", err)
			}
		})
	}
}

// node-expired.crt is refused after its NotAfter and accepted inside it.
func TestVerifyNodeCertificate_FixtureExpiry(t *testing.T) {
	p := pkiFixturePKI(t)
	nodePub := pkiFixtureNodePub(t)
	expired := pkiReadFixture(t, "node-expired.crt")

	t.Run("refused at 2026-11-08", func(t *testing.T) {
		_, err := pkiVerify(t, p, expired, pkiFixedNow)
		if !errors.Is(err, ErrNodeCertificate) {
			t.Fatalf("error = %v, want ErrNodeCertificate", err)
		}
	})
	t.Run("accepted at 2026-03-01", func(t *testing.T) {
		at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
		id, err := pkiVerify(t, p, expired, at)
		if err != nil {
			t.Fatalf("node-expired.crt rejected inside its validity: %v", err)
		}
		if want := pkiFingerprint(t, nodePub); id.Fingerprint != want {
			t.Errorf("fingerprint = %s, want %s", id.Fingerprint, want)
		}
	})
}

// A CA-signed clientAuth certificate whose CN is not its key's fingerprint is
// refused. A control with the right CN is accepted, so the refusal is caused
// by the CN and nothing else.
func TestVerifyNodeCertificate_CommonNameMustBeFingerprint(t *testing.T) {
	caCert, caKey := pkiSelfCA(t, pkiFixedNow)
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p := newPKIFromParts(caCert, caKey, pkiParseCert(t, pkiReadFixture(t, "core.crt")), pkiParseKey(t, pkiReadFixture(t, "core.key")), nil)
	fp := pkiFingerprint(t, pub)

	t.Run("control: CN equals fingerprint", func(t *testing.T) {
		id, err := pkiVerify(t, p, pkiLeafPEM(t, caCert, caKey, pub, fp, pkiFixedNow), pkiFixedNow)
		if err != nil {
			t.Fatalf("control rejected: %v", err)
		}
		if id.Fingerprint != fp {
			t.Errorf("fingerprint = %s, want %s", id.Fingerprint, fp)
		}
	})
	t.Run("CN is not the fingerprint", func(t *testing.T) {
		_, err := pkiVerify(t, p, pkiLeafPEM(t, caCert, caKey, pub, "not-"+fp, pkiFixedNow), pkiFixedNow)
		if !errors.Is(err, ErrNodeCertificate) {
			t.Fatalf("error = %v, want ErrNodeCertificate", err)
		}
	})
}
