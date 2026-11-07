package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestKeyringCreateLoadAndPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	path := filepath.Join(dir, "master.key")
	if KeyringExists(path) {
		t.Fatal("exists before creation")
	}
	if _, err := LoadKeyring(path); !errors.Is(err, ErrKeyringMissing) {
		t.Fatalf("load missing: %v", err)
	}
	k, err := LoadOrCreateKeyring(path)
	if err != nil {
		t.Fatal(err)
	}
	active := k.ActiveKeyID()
	if active == "" || len(k.KeyIDs()) != 1 {
		t.Fatalf("new keyring active=%q ids=%v", active, k.KeyIDs())
	}
	k.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode %o, want 0600", info.Mode().Perm())
	}
	if d, _ := os.Stat(dir); d.Mode().Perm() != 0o700 {
		t.Fatalf("key dir mode %o, want 0700", d.Mode().Perm())
	}
	k2, err := LoadOrCreateKeyring(path)
	if err != nil {
		t.Fatal(err)
	}
	if k2.ActiveKeyID() != active {
		t.Fatal("reload generated a different key")
	}
	k2.Close()

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeyring(path); !errors.Is(err, ErrKeyringPermissions) {
		t.Fatalf("world-readable key file: %v", err)
	}
	if _, err := LoadOrCreateKeyring(path); !errors.Is(err, ErrKeyringPermissions) {
		t.Fatalf("LoadOrCreate must not replace a badly-permissioned file: %v", err)
	}
	os.Chmod(path, 0o600)

	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeyring(path); !errors.Is(err, ErrKeyringDamaged) {
		t.Fatalf("damaged key file: %v", err)
	}
	if _, err := LoadOrCreateKeyring(path); !errors.Is(err, ErrKeyringDamaged) {
		t.Fatalf("LoadOrCreate must not replace a damaged file: %v", err)
	}
}

func TestKeyringCloseZeroesMaterial(t *testing.T) {
	k, err := LoadOrCreateKeyring(filepath.Join(t.TempDir(), "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	key, ok := k.key(k.ActiveKeyID())
	if !ok || len(key) != keyLen {
		t.Fatal("no active key")
	}
	k.Close()
	for _, b := range key {
		if b != 0 {
			t.Fatal("key material not zeroed on Close")
		}
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	key, _ := randomKey()
	ct, err := seal(key, []byte("plain"), aad("sec_a", 1))
	if err != nil {
		t.Fatal(err)
	}
	pt, err := open(key, ct, aad("sec_a", 1))
	if err != nil || string(pt) != "plain" {
		t.Fatalf("round trip: %q %v", pt, err)
	}
	if _, err := open(key, ct, aad("sec_a", 2)); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("wrong aad: %v", err)
	}
	other, _ := randomKey()
	if _, err := open(other, ct, aad("sec_a", 1)); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("wrong key: %v", err)
	}
	if _, err := open(key, ct[:5], aad("sec_a", 1)); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("truncated: %v", err)
	}
}
