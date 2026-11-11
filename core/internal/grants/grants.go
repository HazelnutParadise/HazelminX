// Package grants issues the signed authorization grants the privileged
// executor (`hazelminx-privileged`, ticket 012) requires on every request.
//
// The issuer key is an Ed25519 key kept in the secret store. Its public half
// is what the executor trusts (`/etc/hazelminx/privileged/issuer.pub`); the
// installer and node enrollment (tickets 013 and 019) put it there.
//
// The signed content is the executor's canonical form, one field per line:
//
//	hazelminx-grant-v1
//	{id}
//	{subject}
//	{not_before}
//	{expires_at}
//	{permissions joined with ","}
//
// with times in RFC 3339 UTC and whole seconds. engineering/contracts/fixtures/grant.json
// is a grant signed by the Rust side with a fixed key; this package must
// produce the same bytes and the same signature.
package grants

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
)

const (
	// SecretIssuer holds the issuer key: one PKCS#8 "PRIVATE KEY" PEM block.
	SecretIssuer = "privileged-grants/issuer"
	// SecretConsumer is the only consumer in the secret's scope.
	SecretConsumer = "grants:issuer"
	actor          = "system:grants"

	canonicalPrefix = "hazelminx-grant-v1"
	// MaxLifetime is the executor's cap; longer grants are refused there.
	MaxLifetime = 24 * time.Hour
	maxTokenLen = 256
)

// SecretStore is the part of secrets.Service the issuer needs.
type SecretStore interface {
	List(ctx context.Context) ([]secrets.Secret, error)
	Create(ctx context.Context, in secrets.CreateInput) (secrets.Secret, error)
	Reveal(ctx context.Context, req secrets.RevealRequest) ([]byte, int, error)
}

// ErrInvalid: a subject or permission is not a token, or the lifetime is
// not between one second and MaxLifetime.
var ErrInvalid = errors.New("grants: invalid grant request")

// Issuer signs grants.
type Issuer struct {
	key ed25519.PrivateKey
	now func() time.Time
}

// New wraps an existing key; now may be nil (time.Now). Tests use it with
// the fixture key.
func New(key ed25519.PrivateKey, now func() time.Time) *Issuer {
	if now == nil {
		now = time.Now
	}
	return &Issuer{key: key, now: now}
}

// LoadOrCreate reads SecretIssuer from st, or creates a new key when it does
// not exist. now may be nil.
//
// Expected results: an empty store gets one secret (kind private-key, scope
// [SecretConsumer]); a second call loads the same key and creates nothing; a
// value that is not a PKCS#8 Ed25519 key is an error that does not include
// the value.
func LoadOrCreate(ctx context.Context, st SecretStore, now func() time.Time) (*Issuer, error) {
	list, err := st.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("grants: list secrets: %w", err)
	}
	for _, s := range list {
		if s.Name != SecretIssuer {
			continue
		}
		v, _, err := st.Reveal(ctx, secrets.RevealRequest{ID: s.ID, Consumer: SecretConsumer})
		if err != nil {
			return nil, fmt.Errorf("grants: reveal %s: %w", SecretIssuer, err)
		}
		defer clear(v)
		key, err := parseKey(v)
		if err != nil {
			return nil, err
		}
		return New(key, now), nil
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("grants: generate issuer key: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("grants: encode issuer key: %w", err)
	}
	defer clear(der)
	value := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	defer clear(value)
	if _, err := st.Create(ctx, secrets.CreateInput{Kind: secrets.KindPrivateKey, Name: SecretIssuer, Value: value,
		Scope: []string{SecretConsumer}, Actor: actor}); err != nil {
		return nil, fmt.Errorf("grants: create %s: %w", SecretIssuer, err)
	}
	return New(key, now), nil
}

func parseKey(value []byte) (ed25519.PrivateKey, error) {
	block, rest := pem.Decode(value)
	if block == nil || block.Type != "PRIVATE KEY" || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, fmt.Errorf("grants: secret %s is not one PRIVATE KEY block", SecretIssuer)
	}
	defer clear(block.Bytes)
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("grants: secret %s does not parse as PKCS#8", SecretIssuer)
	}
	key, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("grants: secret %s is not an Ed25519 key", SecretIssuer)
	}
	return key, nil
}

// PublicKeyPEM is the SPKI "PUBLIC KEY" PEM the executor loads as issuer.pub.
func (i *Issuer) PublicKeyPEM() []byte {
	der, err := x509.MarshalPKIXPublicKey(i.key.Public())
	if err != nil {
		panic("grants: marshal ed25519 public key: " + err.Error()) // cannot happen for Ed25519
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

// Issue signs a grant for subject with perms, valid from now (whole
// seconds) for lifetime.
//
// Expected results: id is "grt-" plus 32 hex characters; not_before and
// expires_at are RFC 3339 UTC without fractions; signature is the hex
// Ed25519 signature over Canonical; a subject or permission that is not a
// token, no permissions, or a lifetime outside 1 s..MaxLifetime is ErrInvalid.
func (i *Issuer) Issue(subject string, perms []string, lifetime time.Duration) (protocol.ExecutorGrant, error) {
	if !IsToken(subject) || len(perms) == 0 || lifetime < time.Second || lifetime > MaxLifetime {
		return protocol.ExecutorGrant{}, ErrInvalid
	}
	for _, p := range perms {
		if !IsToken(p) {
			return protocol.ExecutorGrant{}, ErrInvalid
		}
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return protocol.ExecutorGrant{}, fmt.Errorf("grants: random id: %w", err)
	}
	nb := i.now().UTC().Truncate(time.Second)
	g := protocol.ExecutorGrant{
		ID:          "grt-" + hex.EncodeToString(id[:]),
		Subject:     subject,
		Permissions: append([]string(nil), perms...),
		NotBefore:   nb.Format(time.RFC3339),
		ExpiresAt:   nb.Add(lifetime.Truncate(time.Second)).Format(time.RFC3339),
	}
	i.Sign(&g)
	return g, nil
}

// Sign sets g.Signature over Canonical(g).
func (i *Issuer) Sign(g *protocol.ExecutorGrant) {
	g.Signature = hex.EncodeToString(ed25519.Sign(i.key, Canonical(*g)))
}

// Canonical is the executor's signed form of g.
func Canonical(g protocol.ExecutorGrant) []byte {
	return []byte(canonicalPrefix + "\n" + g.ID + "\n" + g.Subject + "\n" + g.NotBefore + "\n" +
		g.ExpiresAt + "\n" + strings.Join(g.Permissions, ",") + "\n")
}

// IsToken matches the executor's charset for id, subject and permissions:
// 1..256 bytes of [A-Za-z0-9._:@-].
func IsToken(s string) bool {
	if s == "" || len(s) > maxTokenLen {
		return false
	}
	for _, c := range []byte(s) {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '_' || c == ':' || c == '@' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}
