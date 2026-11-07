package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// keyringSchemaVersion is the format version of the keyring file (34.3).
const keyringSchemaVersion = 1

// keyringFile is the on-disk shape of the master key file.
//
// Lifecycle of a generation: pending (written before any data uses it) →
// active (the only generation new ciphertext is sealed under) → retired
// (kept so older data and backups still decrypt). Generations are never
// removed by this package.
type keyringFile struct {
	SchemaVersion          int        `json:"schemaVersion"`
	Active                 string     `json:"active"`
	Pending                string     `json:"pending,omitempty"`
	Keys                   []keyEntry `json:"keys"`
	RecoveryAcknowledgedAt string     `json:"recoveryAcknowledgedAt,omitempty"`
	RecoveryAcknowledgedBy string     `json:"recoveryAcknowledgedBy,omitempty"`
}

type keyEntry struct {
	ID        string `json:"id"`
	Key       []byte `json:"key"` // base64 in JSON; 32 bytes
	CreatedAt string `json:"createdAt"`
	RetiredAt string `json:"retiredAt,omitempty"`
}

// Keyring is the master key file: generations of 32-byte keys, one active.
// It is written atomically (temp file + rename) with mode 0600 and refuses to
// load a file readable by anyone but its owner.
type Keyring struct {
	path string
	mu   sync.Mutex
	doc  keyringFile
	now  func() time.Time
}

// KeyringExists reports whether a keyring file is present at path. Together
// with store.OpenExisting it distinguishes a fresh install from a lost database.
func KeyringExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// LoadOrCreateKeyring loads the keyring at path, creating one with a single
// active generation when the file does not exist. The parent directory is
// created 0700.
func LoadOrCreateKeyring(path string) (*Keyring, error) {
	k, err := LoadKeyring(path)
	if err == nil {
		return k, nil
	}
	if !errors.Is(err, ErrKeyringMissing) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("secrets: create key dir: %w", err)
	}
	k = &Keyring{path: path, now: time.Now, doc: keyringFile{SchemaVersion: keyringSchemaVersion}}
	id, err := k.addKey()
	if err != nil {
		return nil, err
	}
	k.doc.Active = id
	if err := k.save(); err != nil {
		return nil, err
	}
	return k, nil
}

// LoadKeyring loads an existing keyring. Missing → ErrKeyringMissing;
// group/other permission bits set → ErrKeyringPermissions; unparsable or
// unsupported format → ErrKeyringDamaged.
func LoadKeyring(path string) (*Keyring, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrKeyringMissing, path)
		}
		return nil, fmt.Errorf("secrets: stat key file: %w", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return nil, fmt.Errorf("%w: %s has mode %04o", ErrKeyringPermissions, path, perm)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("secrets: read key file: %w", err)
	}
	var doc keyringFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeyringDamaged, err)
	}
	if doc.SchemaVersion != keyringSchemaVersion {
		return nil, fmt.Errorf("%w: schemaVersion %d, this binary reads %d", ErrKeyringDamaged, doc.SchemaVersion, keyringSchemaVersion)
	}
	k := &Keyring{path: path, doc: doc, now: time.Now}
	if _, ok := k.lookup(doc.Active); !ok {
		return nil, fmt.Errorf("%w: active generation %q is not in the file", ErrKeyringDamaged, doc.Active)
	}
	for _, e := range doc.Keys {
		if len(e.Key) != keyLen {
			return nil, fmt.Errorf("%w: generation %q has %d-byte key, want %d", ErrKeyringDamaged, e.ID, len(e.Key), keyLen)
		}
	}
	return k, nil
}

// Path returns the keyring file path.
func (k *Keyring) Path() string { return k.path }

// ActiveKeyID returns the generation new ciphertext is sealed under.
func (k *Keyring) ActiveKeyID() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.doc.Active
}

// PendingKeyID returns the generation prepared for a rotation that has not
// been activated yet, or "".
func (k *Keyring) PendingKeyID() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.doc.Pending
}

// KeyIDs lists every generation in the file, active first.
func (k *Keyring) KeyIDs() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := []string{k.doc.Active}
	for _, e := range k.doc.Keys {
		if e.ID != k.doc.Active {
			out = append(out, e.ID)
		}
	}
	return out
}

// Snapshot implements RecoveryMaterialSource.
func (k *Keyring) Snapshot() ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return json.Marshal(k.doc)
}

// RecoveryAcknowledged implements RecoveryMaterialSource.
func (k *Keyring) RecoveryAcknowledged() (bool, string, time.Time) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.doc.RecoveryAcknowledgedAt == "" {
		return false, "", time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, k.doc.RecoveryAcknowledgedAt)
	return true, k.doc.RecoveryAcknowledgedBy, t
}

// AcknowledgeRecovery records that actor confirmed holding the recovery
// material. It is the check that must pass before encrypted backups are
// reported as restorable (35.2); the export itself is ticket 078.
func (k *Keyring) AcknowledgeRecovery(actor string) error {
	if actor == "" {
		return errors.New("secrets: acknowledgement needs an actor")
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.doc.RecoveryAcknowledgedAt = k.now().UTC().Format(time.RFC3339)
	k.doc.RecoveryAcknowledgedBy = actor
	return k.save()
}

// Close zeroes key material in memory. The Keyring must not be used afterwards.
func (k *Keyring) Close() {
	k.mu.Lock()
	defer k.mu.Unlock()
	for i := range k.doc.Keys {
		zero(k.doc.Keys[i].Key)
	}
	k.doc.Keys = nil
	k.doc.Active = ""
}

// key returns the material of a generation.
func (k *Keyring) key(id string) ([]byte, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.lookup(id)
}

func (k *Keyring) lookup(id string) ([]byte, bool) {
	for _, e := range k.doc.Keys {
		if e.ID == id {
			return e.Key, true
		}
	}
	return nil, false
}

// addKey appends a fresh generation (not yet active or pending) and returns its id.
func (k *Keyring) addKey() (string, error) {
	key, err := randomKey()
	if err != nil {
		return "", err
	}
	id := "mk_" + newID()
	k.doc.Keys = append(k.doc.Keys, keyEntry{ID: id, Key: key, CreatedAt: k.now().UTC().Format(time.RFC3339)})
	return id, nil
}

// preparePending creates a new generation, marks it pending and persists the
// file: step 1 of a rotation. Data may now be sealed under it. If a pending
// generation already exists (an earlier attempt was interrupted), it is reused.
func (k *Keyring) preparePending() (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.doc.Pending != "" {
		return k.doc.Pending, nil
	}
	id, err := k.addKey()
	if err != nil {
		return "", err
	}
	k.doc.Pending = id
	if err := k.save(); err != nil {
		return "", err
	}
	return id, nil
}

// activatePending makes the pending generation active and retires the old one:
// the final step of a rotation.
func (k *Keyring) activatePending() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.doc.Pending == "" {
		return errors.New("secrets: no pending generation to activate")
	}
	now := k.now().UTC().Format(time.RFC3339)
	for i := range k.doc.Keys {
		if k.doc.Keys[i].ID == k.doc.Active {
			k.doc.Keys[i].RetiredAt = now
		}
	}
	k.doc.Active, k.doc.Pending = k.doc.Pending, ""
	return k.save()
}

// dropPending discards a pending generation that no data refers to.
func (k *Keyring) dropPending() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.doc.Pending == "" {
		return nil
	}
	kept := k.doc.Keys[:0]
	for _, e := range k.doc.Keys {
		if e.ID == k.doc.Pending {
			zero(e.Key)
			continue
		}
		kept = append(kept, e)
	}
	k.doc.Keys, k.doc.Pending = kept, ""
	return k.save()
}

// save writes the file atomically with mode 0600: temp file in the same
// directory, fsync, rename, fsync directory.
func (k *Keyring) save() error {
	raw, err := json.MarshalIndent(k.doc, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(k.path)
	tmp, err := os.CreateTemp(dir, ".master.key.*")
	if err != nil {
		return fmt.Errorf("secrets: write key file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { tmp.Close(); os.Remove(tmpName) }
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, k.path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("secrets: replace key file: %w", err)
	}
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}
