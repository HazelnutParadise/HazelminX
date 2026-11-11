package grants

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
)

type grantFixture struct {
	IssuerSeedHex      string                 `json:"issuerSeedHex"`
	IssuerPublicKeyPem string                 `json:"issuerPublicKeyPem"`
	Grant              protocol.ExecutorGrant `json:"grant"`
	Canonical          string                 `json:"canonical"`
}

func readFixture(t *testing.T) grantFixture {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "engineering", "contracts", "fixtures", "grant.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f grantFixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// The Rust executor signed the fixture; Go must produce the same canonical
// bytes, the same signature (Ed25519 is deterministic) and the same public
// key PEM, or the executor would refuse every Core grant.
func TestCanonicalAndSignatureMatchRustFixture(t *testing.T) {
	f := readFixture(t)
	seed, err := hex.DecodeString(f.IssuerSeedHex)
	if err != nil {
		t.Fatal(err)
	}
	iss := New(ed25519.NewKeyFromSeed(seed), nil)
	if got := string(Canonical(f.Grant)); got != f.Canonical {
		t.Fatalf("canonical:\n%q\nwant\n%q", got, f.Canonical)
	}
	g := f.Grant
	g.Signature = ""
	iss.Sign(&g)
	if g.Signature != f.Grant.Signature {
		t.Fatalf("signature %s, want %s", g.Signature, f.Grant.Signature)
	}
	if got := string(iss.PublicKeyPEM()); got != f.IssuerPublicKeyPem {
		t.Fatalf("public key PEM:\n%s\nwant\n%s", got, f.IssuerPublicKeyPem)
	}
}

func TestIssueProducesExecutorShapedGrants(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(nil)
	now := time.Date(2026, 11, 8, 9, 30, 0, 987654321, time.FixedZone("x", 8*3600))
	iss := New(key, func() time.Time { return now })
	g, err := iss.Issue("system:ledger-sync", []string{"host.ledger.read"}, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^grt-[0-9a-f]{32}$`).MatchString(g.ID) {
		t.Fatalf("id %q", g.ID)
	}
	if g.NotBefore != "2026-11-08T01:30:00Z" || g.ExpiresAt != "2026-11-08T01:35:00Z" {
		t.Fatalf("times %s %s", g.NotBefore, g.ExpiresAt)
	}
	sig, _ := hex.DecodeString(g.Signature)
	if !ed25519.Verify(key.Public().(ed25519.PublicKey), Canonical(g), sig) {
		t.Fatal("signature does not verify")
	}
	other, _ := iss.Issue("system:ledger-sync", []string{"host.ledger.read"}, time.Minute)
	if other.ID == g.ID {
		t.Fatal("ids repeat")
	}
	for name, call := range map[string]func() error{
		"subject with space": func() error { _, e := iss.Issue("a b", []string{"x"}, time.Minute); return e },
		"no permissions":     func() error { _, e := iss.Issue("s", nil, time.Minute); return e },
		"newline permission": func() error { _, e := iss.Issue("s", []string{"x\ny"}, time.Minute); return e },
		"too long":           func() error { _, e := iss.Issue("s", []string{"x"}, MaxLifetime+time.Second); return e },
		"zero lifetime":      func() error { _, e := iss.Issue("s", []string{"x"}, 0); return e },
	} {
		if err := call(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err %v", name, err)
		}
	}
}

type memSecrets struct {
	mu   sync.Mutex
	list []secrets.Secret
	vals map[string][]byte
}

func (m *memSecrets) List(context.Context) ([]secrets.Secret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]secrets.Secret(nil), m.list...), nil
}

func (m *memSecrets) Create(_ context.Context, in secrets.CreateInput) (secrets.Secret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := secrets.Secret{ID: fmt.Sprintf("sec_%d", len(m.list)+1), Kind: in.Kind, Name: in.Name, Scope: in.Scope}
	m.list = append(m.list, s)
	m.vals[s.ID] = append([]byte(nil), in.Value...)
	return s, nil
}

func (m *memSecrets) Reveal(_ context.Context, req secrets.RevealRequest) ([]byte, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.list {
		if s.ID == req.ID {
			if len(s.Scope) != 1 || s.Scope[0] != req.Consumer {
				return nil, 0, secrets.ErrOutOfScope
			}
			return append([]byte(nil), m.vals[s.ID]...), 1, nil
		}
	}
	return nil, 0, secrets.ErrNotFound
}

func TestLoadOrCreateKeepsOneKey(t *testing.T) {
	st := &memSecrets{vals: map[string][]byte{}}
	a, err := LoadOrCreate(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadOrCreate(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(a.PublicKeyPEM()) != string(b.PublicKeyPEM()) {
		t.Fatal("second load produced a different key")
	}
	if len(st.list) != 1 || st.list[0].Kind != secrets.KindPrivateKey || st.list[0].Scope[0] != SecretConsumer {
		t.Fatalf("secrets %+v", st.list)
	}
	st.vals[st.list[0].ID] = []byte("not a key")
	_, err = LoadOrCreate(context.Background(), st, nil)
	if err == nil || strings.Contains(err.Error(), "not a key") {
		t.Fatalf("bad value: err %v", err)
	}
}
