// Package nodes is the Core end of the Agent channel: node identity
// certificates (this file), the WebSocket hub agents dial, and the step
// runner job handlers use to execute steps on a node.
package nodes

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
)

// Names of the secrets that hold the PKI. Each value is a PEM bundle: one
// "PRIVATE KEY" block (PKCS#8 Ed25519) followed by one "CERTIFICATE" block.
const (
	SecretCA   = "node-pki/ca"
	SecretCore = "node-pki/core"
	// SecretConsumer is the only consumer in both secrets' scope.
	SecretConsumer = "nodes:pki"
	// Actor recorded on the secrets' change log.
	pkiActor = "system:nodes"
)

// Validity of generated certificates.
const (
	CAValidity   = 20 * 365 * 24 * time.Hour
	CoreValidity = 20 * 365 * 24 * time.Hour
	// MaxNodeValidity caps IssueNodeCertificate.
	MaxNodeValidity = 5 * 365 * 24 * time.Hour
)

// Errors of VerifyNodeCertificate. All are wrapped with detail.
var (
	// ErrNodeCertificate: not a single PEM certificate, not Ed25519, not
	// signed by this CA, wrong usage, outside its validity window, or the
	// common name is not the key's fingerprint.
	ErrNodeCertificate = errors.New("nodes: node certificate is not valid for this core")
)

// SecretStore is the part of secrets.Service the PKI needs.
type SecretStore interface {
	List(ctx context.Context) ([]secrets.Secret, error)
	Create(ctx context.Context, in secrets.CreateInput) (secrets.Secret, error)
	Reveal(ctx context.Context, req secrets.RevealRequest) ([]byte, int, error)
}

// NodeIdentity is what a verified node certificate proves.
type NodeIdentity struct {
	// Fingerprint is protocol.Fingerprint of PublicKey (the node ID).
	Fingerprint string
	PublicKey   ed25519.PublicKey
	NotAfter    time.Time
}

// PKI holds the Core CA and the Core identity in memory.
type PKI struct {
	caCert   *x509.Certificate
	caKey    ed25519.PrivateKey
	coreCert *x509.Certificate
	coreKey  ed25519.PrivateKey
	now      func() time.Time
}

// LoadOrCreatePKI reads SecretCA and SecretCore from st, or creates them
// when neither exists. now may be nil (time.Now).
//
// Expected results:
//   - empty store: creates both secrets (kind secrets.KindPrivateKey, scope
//     [SecretConsumer], actor pkiActor) and returns a PKI whose CA is a
//     self-signed Ed25519 CA (IsCA, KeyUsageCertSign) valid CAValidity, and
//     whose Core cert is signed by that CA with EKU serverAuth, DNS SAN and
//     CN protocol.CoreServerName, valid CoreValidity;
//   - second call on the same store: loads the same CA and Core cert (same
//     DER bytes) and creates nothing;
//   - only one of the two secrets exists: error, nothing created (a half
//     PKI is a recovery situation, not a fresh install);
//   - a secret value that does not parse, or a Core cert not signed by the
//     stored CA: error;
//   - private keys never appear in returned errors.
func LoadOrCreatePKI(ctx context.Context, st SecretStore, now func() time.Time) (*PKI, error) {
	if now == nil {
		now = time.Now
	}
	list, err := st.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("nodes: list secrets: %w", err)
	}
	var caID, coreID string
	for _, s := range list {
		switch s.Name {
		case SecretCA:
			caID = s.ID
		case SecretCore:
			coreID = s.ID
		}
	}
	switch {
	case caID != "" && coreID != "":
		return loadPKI(ctx, st, now, caID, coreID)
	case caID != "" || coreID != "":
		return nil, fmt.Errorf("nodes: only one of %s and %s exists; refusing to create the missing half", SecretCA, SecretCore)
	}
	return createPKI(ctx, st, now)
}

// CACertificatePEM is the trust anchor agents pin (core-ca.crt).
func (p *PKI) CACertificatePEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: p.caCert.Raw})
}

// CoreCertificatePEM is the Core identity certificate sent in welcome.
func (p *PKI) CoreCertificatePEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: p.coreCert.Raw})
}

// SignAsCore signs msg with the Core identity key.
//
// Expected result: the signature verifies with the public key of
// CoreCertificatePEM.
func (p *PKI) SignAsCore(msg []byte) []byte {
	return ed25519.Sign(p.coreKey, msg)
}

// ServerTLSConfig is the TLS configuration of the agent listener: TLS 1.3
// only, the Core identity certificate, client certificates requested but
// not required (tls.VerifyClientCertIfGiven) and verified against the CA.
// Agents behind a TLS-terminating proxy arrive without one; their identity
// is proven in the handshake instead.
//
// Expected results: a client presenting a CA-issued node certificate
// completes the TLS handshake and the peer certificate is visible; a client
// without a certificate also completes; a client presenting a certificate
// from another CA fails the TLS handshake.
func (p *PKI) ServerTLSConfig() *tls.Config {
	pool := x509.NewCertPool()
	pool.AddCert(p.caCert)
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{p.coreCert.Raw},
			PrivateKey:  p.coreKey,
			Leaf:        p.coreCert,
		}},
		ClientAuth: tls.VerifyClientCertIfGiven,
		ClientCAs:  pool,
	}
}

// IssueNodeCertificate signs a node certificate for pub: CN = fingerprint,
// EKU clientAuth only, KeyUsageDigitalSignature, random 128-bit serial,
// NotBefore = now - 5 minutes (clock skew), NotAfter = now + validity.
// Returns PEM. Used by enrollment (ticket 019) and tests.
//
// Expected results: the result passes VerifyNodeCertificate with the same
// fingerprint; validity <= 0 or > MaxNodeValidity → error; a key that is not
// 32 bytes → error.
func (p *PKI) IssueNodeCertificate(pub ed25519.PublicKey, validity time.Duration) ([]byte, error) {
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("nodes: node public key is %d bytes, want %d", len(pub), ed25519.PublicKeySize)
	}
	if validity <= 0 || validity > MaxNodeValidity {
		return nil, fmt.Errorf("nodes: validity %s is outside (0, %s]", validity, MaxNodeValidity)
	}
	serial, err := newSerial()
	if err != nil {
		return nil, err
	}
	now := p.now().UTC()
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: protocol.Fingerprint(pub)},
		NotBefore:    now.Add(-pkiClockSkew),
		NotAfter:     now.Add(validity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, p.caCert, pub, p.caKey)
	if err != nil {
		return nil, fmt.Errorf("nodes: sign node certificate: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

// VerifyNodeCertificate checks a PEM node certificate at time now.
//
// Expected results (fixtures in docs/contracts/fixtures/pki, verified
// against a PKI built from ca.crt for the test):
//   - node.crt → identity with the fingerprint of node.key's public key;
//   - node-rogue.crt (other CA) → ErrNodeCertificate;
//   - node-expired.crt at 2026-11-08 → ErrNodeCertificate;
//   - core.crt (serverAuth, not clientAuth) → ErrNodeCertificate;
//   - two concatenated certificates, garbage, empty → ErrNodeCertificate;
//   - a CA-signed clientAuth cert whose CN is not its key's fingerprint →
//     ErrNodeCertificate.
func (p *PKI) VerifyNodeCertificate(certPEM []byte, now time.Time) (NodeIdentity, error) {
	block, rest := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return NodeIdentity{}, fmt.Errorf("%w: not a PEM certificate", ErrNodeCertificate)
	}
	if len(bytes.TrimSpace(rest)) != 0 || !bytes.HasPrefix(bytes.TrimSpace(certPEM), []byte("-----BEGIN CERTIFICATE-----")) {
		return NodeIdentity{}, fmt.Errorf("%w: expected exactly one PEM certificate block", ErrNodeCertificate)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return NodeIdentity{}, fmt.Errorf("%w: parse certificate: %v", ErrNodeCertificate, err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(p.caCert)
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:       pool,
		CurrentTime: now.UTC(),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		return NodeIdentity{}, fmt.Errorf("%w: %v", ErrNodeCertificate, err)
	}
	pub, ok := cert.PublicKey.(ed25519.PublicKey)
	if !ok {
		return NodeIdentity{}, fmt.Errorf("%w: public key is not Ed25519", ErrNodeCertificate)
	}
	fp := protocol.Fingerprint(pub)
	if cert.Subject.CommonName != fp {
		return NodeIdentity{}, fmt.Errorf("%w: common name is not the key fingerprint", ErrNodeCertificate)
	}
	return NodeIdentity{Fingerprint: fp, PublicKey: pub, NotAfter: cert.NotAfter.UTC()}, nil
}

// newPKIFromParts builds a PKI from already-parsed material. Tests use it
// with the fixture CA and Core keys.
func newPKIFromParts(caCert *x509.Certificate, caKey ed25519.PrivateKey, coreCert *x509.Certificate, coreKey ed25519.PrivateKey, now func() time.Time) *PKI {
	if now == nil {
		now = time.Now
	}
	return &PKI{caCert: caCert, caKey: caKey, coreCert: coreCert, coreKey: coreKey, now: now}
}

const (
	pkiCACommonName = "HazelminX Core CA"
	// pkiClockSkew is how far NotBefore is set in the past, for clock skew.
	pkiClockSkew = 5 * time.Minute
)

// newSerial returns a random 128-bit certificate serial number.
func newSerial() (*big.Int, error) {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("nodes: random serial: %w", err)
	}
	return n, nil
}

// createPKI generates the CA and the Core identity, then stores both secrets.
// The CA is created first; if the second Create fails the store is left with
// one half, which LoadOrCreatePKI then refuses (there is no delete in
// SecretStore to roll back).
func createPKI(ctx context.Context, st SecretStore, now func() time.Time) (*PKI, error) {
	t := now().UTC()

	caPub, caKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("nodes: generate CA key: %w", err)
	}
	caSerial, err := newSerial()
	if err != nil {
		return nil, err
	}
	caTpl := &x509.Certificate{
		SerialNumber:          caSerial,
		Subject:               pkix.Name{CommonName: pkiCACommonName},
		NotBefore:             t.Add(-pkiClockSkew),
		NotAfter:              t.Add(CAValidity),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, caPub, caKey)
	if err != nil {
		return nil, fmt.Errorf("nodes: create CA certificate: %w", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, fmt.Errorf("nodes: parse CA certificate: %w", err)
	}

	corePub, coreKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("nodes: generate Core key: %w", err)
	}
	coreSerial, err := newSerial()
	if err != nil {
		return nil, err
	}
	coreTpl := &x509.Certificate{
		SerialNumber: coreSerial,
		Subject:      pkix.Name{CommonName: protocol.CoreServerName},
		DNSNames:     []string{protocol.CoreServerName},
		NotBefore:    t.Add(-pkiClockSkew),
		NotAfter:     t.Add(CoreValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	coreDER, err := x509.CreateCertificate(rand.Reader, coreTpl, caCert, corePub, caKey)
	if err != nil {
		return nil, fmt.Errorf("nodes: create Core certificate: %w", err)
	}
	coreCert, err := x509.ParseCertificate(coreDER)
	if err != nil {
		return nil, fmt.Errorf("nodes: parse Core certificate: %w", err)
	}

	caBundle, err := encodePKIBundle(caKey, caDER)
	if err != nil {
		return nil, err
	}
	coreBundle, err := encodePKIBundle(coreKey, coreDER)
	if err != nil {
		return nil, err
	}
	for _, in := range []secrets.CreateInput{
		{Kind: secrets.KindPrivateKey, Name: SecretCA, Value: caBundle, Scope: []string{SecretConsumer}, Actor: pkiActor},
		{Kind: secrets.KindPrivateKey, Name: SecretCore, Value: coreBundle, Scope: []string{SecretConsumer}, Actor: pkiActor},
	} {
		if _, err := st.Create(ctx, in); err != nil {
			return nil, fmt.Errorf("nodes: create %s: %w", in.Name, err)
		}
	}
	return newPKIFromParts(caCert, caKey, coreCert, coreKey, now), nil
}

// loadPKI reveals and parses the stored CA and Core, and checks that the Core
// certificate is signed by the stored CA for the server usage.
func loadPKI(ctx context.Context, st SecretStore, now func() time.Time, caID, coreID string) (*PKI, error) {
	caBundle, err := revealPKISecret(ctx, st, SecretCA, caID)
	if err != nil {
		return nil, err
	}
	defer clear(caBundle)
	coreBundle, err := revealPKISecret(ctx, st, SecretCore, coreID)
	if err != nil {
		return nil, err
	}
	defer clear(coreBundle)

	caCert, caKey, err := parsePKIBundle(SecretCA, caBundle)
	if err != nil {
		return nil, err
	}
	coreCert, coreKey, err := parsePKIBundle(SecretCore, coreBundle)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	if _, err := coreCert.Verify(x509.VerifyOptions{
		Roots:       pool,
		CurrentTime: now().UTC(),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		return nil, fmt.Errorf("nodes: stored Core certificate is not valid under the stored CA: %w", err)
	}
	return newPKIFromParts(caCert, caKey, coreCert, coreKey, now), nil
}

// revealPKISecret returns the current value of one PKI secret. The caller
// clears the result.
func revealPKISecret(ctx context.Context, st SecretStore, name, id string) ([]byte, error) {
	v, _, err := st.Reveal(ctx, secrets.RevealRequest{ID: id, Consumer: SecretConsumer})
	if err != nil {
		return nil, fmt.Errorf("nodes: reveal %s: %w", name, err)
	}
	return v, nil
}

// parsePKIBundle parses one stored bundle: a PKCS#8 Ed25519 "PRIVATE KEY"
// block, then one "CERTIFICATE" block, and nothing else. The key must be the
// one certified by the certificate. Errors name the secret but never its content.
func parsePKIBundle(name string, bundle []byte) (*x509.Certificate, ed25519.PrivateKey, error) {
	keyBlock, rest := pem.Decode(bundle)
	if keyBlock == nil || keyBlock.Type != "PRIVATE KEY" {
		return nil, nil, fmt.Errorf("nodes: PKI secret %s is malformed: want a key block first", name)
	}
	defer clear(keyBlock.Bytes)
	certBlock, rest := pem.Decode(rest)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, nil, fmt.Errorf("nodes: PKI secret %s is malformed: want one certificate block after the key", name)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("nodes: PKI secret %s has an unreadable key: %v", name, err)
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, nil, fmt.Errorf("nodes: PKI secret %s key is not Ed25519", name)
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("nodes: PKI secret %s has an unreadable certificate: %v", name, err)
	}
	certKey, ok := cert.PublicKey.(ed25519.PublicKey)
	if !ok || !bytes.Equal(certKey, key.Public().(ed25519.PublicKey)) {
		return nil, nil, fmt.Errorf("nodes: PKI secret %s certificate does not match its key", name)
	}
	return cert, key, nil
}

// encodePKIBundle builds the stored form: PKCS#8 key block, then certificate.
func encodePKIBundle(key ed25519.PrivateKey, certDER []byte) ([]byte, error) {
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("nodes: marshal key: %w", err)
	}
	defer clear(pkcs8)
	bundle := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	return append(bundle, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})...), nil
}
