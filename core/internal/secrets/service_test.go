package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

type fixture struct {
	dir     string
	dbPath  string
	keyPath string
	st      *store.Store
	kr      *Keyring
	svc     *Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	f := &fixture{dir: dir, dbPath: filepath.Join(dir, "core.db"), keyPath: filepath.Join(dir, "master.key")}
	f.open(t)
	return f
}

func (f *fixture) open(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, f.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	kr, err := LoadOrCreateKeyring(f.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := Open(ctx, st, kr)
	if err != nil {
		st.Close()
		t.Fatalf("secrets.Open: %v", err)
	}
	f.st, f.kr, f.svc = st, kr, svc
	t.Cleanup(func() { st.Close(); kr.Close() })
}

// reopen closes everything and opens it again, as a restart would.
func (f *fixture) reopen(t *testing.T) {
	t.Helper()
	f.st.Close()
	f.kr.Close()
	f.open(t)
}

func (f *fixture) create(t *testing.T, name string, value string, scope ...string) Secret {
	t.Helper()
	s, err := f.svc.Create(context.Background(), CreateInput{Kind: KindPassword, Name: name, Value: []byte(value), Scope: scope, Actor: "user:owner"})
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return s
}

func (f *fixture) reveal(t *testing.T, id, consumer string, version int) string {
	t.Helper()
	v, _, err := f.svc.Reveal(context.Background(), RevealRequest{ID: id, Version: version, Consumer: consumer})
	if err != nil {
		t.Fatalf("reveal %s for %s: %v", id, consumer, err)
	}
	return string(v)
}

// checkpointedFiles returns the database (after a WAL checkpoint) and the
// keyring file, for byte-level "value must not appear" checks.
func (f *fixture) checkpointedDB(t *testing.T) []byte {
	t.Helper()
	if _, err := f.st.DB.ExecContext(context.Background(), `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(f.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCreateAndRevealWithinScope(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.create(t, "db-password", "hunter2-plain", "service:db", "service:app")
	if s.Version != 1 || s.KeyID != f.kr.ActiveKeyID() || s.ID == "" {
		t.Fatalf("unexpected metadata %+v", s)
	}
	if got := f.reveal(t, s.ID, "service:db", 0); got != "hunter2-plain" {
		t.Fatalf("revealed %q", got)
	}
	if _, _, err := f.svc.Reveal(ctx, RevealRequest{ID: s.ID, Consumer: "service:other"}); !errors.Is(err, ErrOutOfScope) {
		t.Fatalf("out-of-scope consumer got %v, want ErrOutOfScope", err)
	}
	if _, _, err := f.svc.Reveal(ctx, RevealRequest{ID: s.ID, Consumer: ""}); !errors.Is(err, ErrOutOfScope) {
		t.Fatalf("empty consumer got %v, want ErrOutOfScope", err)
	}
	if _, _, err := f.svc.Reveal(ctx, RevealRequest{ID: "sec_nope", Consumer: "service:db"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id got %v", err)
	}
	got, err := f.svc.Get(ctx, s.ID)
	if err != nil || got.Name != "db-password" || len(got.Scope) != 2 {
		t.Fatalf("get: %+v %v", got, err)
	}
}

func TestCreateValidation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Create(ctx, CreateInput{Kind: "cookie", Name: "a", Value: []byte("x"), Actor: "u"}); !errors.Is(err, ErrInvalidKind) {
		t.Fatalf("bad kind: %v", err)
	}
	if _, err := f.svc.Create(ctx, CreateInput{Kind: KindGeneric, Name: "a", Actor: "u"}); !errors.Is(err, ErrEmptyValue) {
		t.Fatalf("empty value: %v", err)
	}
	f.create(t, "dup", "v")
	if _, err := f.svc.Create(ctx, CreateInput{Kind: KindGeneric, Name: "dup", Value: []byte("x"), Actor: "u"}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("duplicate name: %v", err)
	}
}

func TestRotateKeepsPinnedVersionReadable(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.create(t, "token", "v1-value", "service:a")
	s2, err := f.svc.Rotate(ctx, s.ID, []byte("v2-value"), "user:owner")
	if err != nil {
		t.Fatal(err)
	}
	if s2.Version != 2 {
		t.Fatalf("version after rotate %d", s2.Version)
	}
	if got := f.reveal(t, s.ID, "service:a", 0); got != "v2-value" {
		t.Fatalf("current = %q", got)
	}
	if got := f.reveal(t, s.ID, "service:a", 1); got != "v1-value" {
		t.Fatalf("pinned v1 = %q", got)
	}
	if _, _, err := f.svc.Reveal(ctx, RevealRequest{ID: s.ID, Version: 3, Consumer: "service:a"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing version: %v", err)
	}
	if _, err := f.svc.Rotate(ctx, s.ID, nil, "user:owner"); !errors.Is(err, ErrEmptyValue) {
		t.Fatalf("empty rotate: %v", err)
	}
}

func TestExpiredSecretIsRefused(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	f.svc.now = func() time.Time { return now }
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	s, err := f.svc.Create(ctx, CreateInput{Kind: KindAPIToken, Name: "t", Value: []byte("v"), Scope: []string{"c"}, ExpiresAt: &future, Actor: "u"})
	if err != nil {
		t.Fatal(err)
	}
	f.reveal(t, s.ID, "c", 0)
	if err := f.svc.SetExpiry(ctx, s.ID, &past, "u"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.svc.Reveal(ctx, RevealRequest{ID: s.ID, Consumer: "c"}); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired reveal: %v", err)
	}
	if err := f.svc.SetExpiry(ctx, s.ID, nil, "u"); err != nil {
		t.Fatal(err)
	}
	f.reveal(t, s.ID, "c", 0)
}

func TestScopeChangeAndDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.create(t, "k", "v", "service:a")
	if err := f.svc.SetScope(ctx, s.ID, []string{"service:b"}, "u"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.svc.Reveal(ctx, RevealRequest{ID: s.ID, Consumer: "service:a"}); !errors.Is(err, ErrOutOfScope) {
		t.Fatalf("old consumer after scope change: %v", err)
	}
	f.reveal(t, s.ID, "service:b", 0)
	if err := f.svc.Delete(ctx, s.ID, "u"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Get(ctx, s.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted get: %v", err)
	}
	if _, _, err := f.svc.Reveal(ctx, RevealRequest{ID: s.ID, Consumer: "service:b"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted reveal: %v", err)
	}
	list, err := f.svc.List(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("list after delete: %v %v", list, err)
	}
	// The name can be reused; the change record of the old one remains.
	f.create(t, "k", "v2", "service:c")
	ch, err := f.svc.Changes(ctx, s.ID)
	if err != nil || len(ch) == 0 || ch[len(ch)-1].Action != "deleted" {
		t.Fatalf("changes of deleted secret: %+v %v", ch, err)
	}
}

// DAT-004: the change record, and the whole database file, never contain a
// value — not the old one, not the new one.
func TestChangeRecordNeverContainsValues(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	const old, newer = "OLDVALUE-7f3a9c", "NEWVALUE-21be44"
	s := f.create(t, "api-key", old, "service:a")
	if _, err := f.svc.Rotate(ctx, s.ID, []byte(newer), "user:owner"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SetScope(ctx, s.ID, []string{"service:a", "service:b"}, "user:owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.RotateMasterKey(ctx, "user:owner"); err != nil {
		t.Fatal(err)
	}
	changes, err := f.svc.Changes(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantActions := []string{"created", "rotated", "scope-changed", "rewrapped"}
	if len(changes) != len(wantActions) {
		t.Fatalf("got %d changes %+v, want %v", len(changes), changes, wantActions)
	}
	for i, c := range changes {
		if c.Action != wantActions[i] {
			t.Fatalf("change %d action %q, want %q", i, c.Action, wantActions[i])
		}
		if c.SecretName != "api-key" || c.Actor != "user:owner" || c.OccurredAt.IsZero() {
			t.Fatalf("change %d missing snapshot fields: %+v", i, c)
		}
	}
	if *changes[1].FromVersion != 1 || *changes[1].ToVersion != 2 {
		t.Fatalf("rotated change versions: %+v", changes[1])
	}
	db := f.checkpointedDB(t)
	for _, v := range []string{old, newer} {
		if bytes.Contains(db, []byte(v)) {
			t.Fatalf("database file contains the value %q in clear", v)
		}
	}
	// The change rows themselves, read back as text, must not carry values.
	rows, err := f.st.DB.QueryContext(ctx, `SELECT secret_id, secret_name, action, actor, coalesce(key_id,'') FROM secret_changes`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cols [5]string
		if err := rows.Scan(&cols[0], &cols[1], &cols[2], &cols[3], &cols[4]); err != nil {
			t.Fatal(err)
		}
		for _, c := range cols {
			if strings.Contains(c, old) || strings.Contains(c, newer) {
				t.Fatalf("change row contains a value: %v", cols)
			}
		}
	}
}

// A ciphertext moved to another row must not decrypt there.
func TestCiphertextIsBoundToItsRow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.create(t, "a", "value-a", "c")
	b := f.create(t, "b", "value-b", "c")
	var ctA []byte
	if err := f.st.DB.QueryRowContext(ctx, `SELECT ciphertext FROM secret_versions WHERE secret_id=? AND version=1`, a.ID).Scan(&ctA); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.DB.ExecContext(ctx, `UPDATE secret_versions SET ciphertext=? WHERE secret_id=? AND version=1`, ctA, b.ID); err != nil {
		t.Fatal(err)
	}
	_, _, err := f.svc.Reveal(ctx, RevealRequest{ID: b.ID, Consumer: "c"})
	if !errors.Is(err, ErrDecrypt) {
		t.Fatalf("swapped ciphertext revealed or wrong error: %v", err)
	}
	if strings.Contains(err.Error(), "value-a") || strings.Contains(err.Error(), "value-b") {
		t.Fatalf("error echoes a value: %v", err)
	}
}

func TestMasterKeyRotationKeepsEverythingReadable(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s1 := f.create(t, "one", "one-v1", "c")
	if _, err := f.svc.Rotate(ctx, s1.ID, []byte("one-v2"), "u"); err != nil {
		t.Fatal(err)
	}
	s2 := f.create(t, "two", "two-v1", "c")
	s3 := f.create(t, "three", "three-v1", "c")
	if err := f.svc.Delete(ctx, s3.ID, "u"); err != nil {
		t.Fatal(err)
	}
	oldKey := f.kr.ActiveKeyID()

	res, err := f.svc.RotateMasterKey(ctx, "user:owner")
	if err != nil {
		t.Fatal(err)
	}
	if res.OldKeyID != oldKey || res.NewKeyID == oldKey || res.NewKeyID != f.kr.ActiveKeyID() {
		t.Fatalf("rotation result %+v, keyring active %s", res, f.kr.ActiveKeyID())
	}
	if res.Rewrapped != 4 { // one@1, one@2, two@1, three@1 (deleted secrets stay readable for their record)
		t.Fatalf("rewrapped %d versions, want 4", res.Rewrapped)
	}
	if f.kr.PendingKeyID() != "" {
		t.Fatal("pending generation left behind after a successful rotation")
	}
	if got := f.reveal(t, s1.ID, "c", 0); got != "one-v2" {
		t.Fatalf("one current %q", got)
	}
	if got := f.reveal(t, s1.ID, "c", 1); got != "one-v1" {
		t.Fatalf("one v1 %q", got)
	}
	if got := f.reveal(t, s2.ID, "c", 0); got != "two-v1" {
		t.Fatalf("two %q", got)
	}
	var stale int
	if err := f.st.DB.QueryRowContext(ctx, `SELECT count(*) FROM secret_versions WHERE key_id<>?`, res.NewKeyID).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Fatalf("%d versions still under the old generation", stale)
	}
	var oldStatus, newStatus string
	if err := f.st.DB.QueryRowContext(ctx, `SELECT status FROM master_keys WHERE key_id=?`, oldKey).Scan(&oldStatus); err != nil {
		t.Fatal(err)
	}
	if err := f.st.DB.QueryRowContext(ctx, `SELECT status FROM master_keys WHERE key_id=?`, res.NewKeyID).Scan(&newStatus); err != nil {
		t.Fatal(err)
	}
	if oldStatus != "retired" || newStatus != "active" {
		t.Fatalf("master_keys old=%s new=%s", oldStatus, newStatus)
	}
	rep, err := f.svc.VerifyAll(ctx)
	if err != nil || rep.Checked != 4 || len(rep.Failures) != 0 {
		t.Fatalf("verify after rotation: %+v %v", rep, err)
	}
	// New values go under the new generation; everything survives a restart.
	f.reopen(t)
	s4 := f.create(t, "four", "four-v1", "c")
	if s4.KeyID != res.NewKeyID {
		t.Fatalf("new secret sealed under %s, want %s", s4.KeyID, res.NewKeyID)
	}
	if got := f.reveal(t, s1.ID, "c", 1); got != "one-v1" {
		t.Fatalf("after restart one v1 %q", got)
	}
}

// DAT-004 / 35.2: a backup taken before the rotation must still decrypt with
// the keyring as it is after the rotation; a keyring that lost the old
// generation is reported before anything is served.
func TestOldBackupDecryptsAfterRotation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.create(t, "legacy", "legacy-value", "c")
	backup := filepath.Join(t.TempDir(), "backup.db")
	if err := os.WriteFile(backup, f.checkpointedDB(t), 0o600); err != nil {
		t.Fatal(err)
	}
	oldKey := f.kr.ActiveKeyID()
	if _, err := f.svc.RotateMasterKey(ctx, "u"); err != nil {
		t.Fatal(err)
	}

	bst, err := store.Open(ctx, backup)
	if err != nil {
		t.Fatal(err)
	}
	defer bst.Close()
	bsvc, err := Open(ctx, bst, f.kr)
	if err != nil {
		t.Fatalf("open backup with post-rotation keyring: %v", err)
	}
	v, _, err := bsvc.Reveal(ctx, RevealRequest{ID: s.ID, Consumer: "c"})
	if err != nil || string(v) != "legacy-value" {
		t.Fatalf("backup reveal: %q %v", v, err)
	}
	// Opening the backup must not have moved it to the new generation behind
	// our back, nor changed which generation the keyring treats as active.
	var backupKey string
	if err := bst.DB.QueryRowContext(ctx, `SELECT key_id FROM secret_versions WHERE secret_id=?`, s.ID).Scan(&backupKey); err != nil {
		t.Fatal(err)
	}
	if backupKey != oldKey {
		t.Fatalf("backup rows now under %s, want untouched %s", backupKey, oldKey)
	}
	if f.kr.ActiveKeyID() == oldKey {
		t.Fatal("keyring active generation reverted")
	}

	// A keyring without the old generation: refuse up front, name the gap.
	fresh, err := LoadOrCreateKeyring(filepath.Join(t.TempDir(), "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	bst2, err := store.Open(ctx, backup)
	if err != nil {
		t.Fatal(err)
	}
	defer bst2.Close()
	_, err = Open(ctx, bst2, fresh)
	if !errors.Is(err, ErrKeyNotFound) || !strings.Contains(err.Error(), oldKey) {
		t.Fatalf("open backup with a keyring lacking %s: %v", oldKey, err)
	}
}

// DAT-004: a failure during re-wrapping leaves the old generation active and
// every value readable; the next attempt succeeds.
func TestMasterKeyRotationFailureRollsBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.create(t, "k", "value", "c")
	oldKey := f.kr.ActiveKeyID()
	boom := errors.New("disk full")
	f.svc.rotateHook = func(step string) error {
		if step == "rewrapped" {
			return boom
		}
		return nil
	}
	_, err := f.svc.RotateMasterKey(ctx, "u")
	if !errors.Is(err, boom) {
		t.Fatalf("expected injected failure, got %v", err)
	}
	if f.kr.ActiveKeyID() != oldKey {
		t.Fatalf("active generation changed to %s after failed rotation", f.kr.ActiveKeyID())
	}
	if f.kr.PendingKeyID() != "" {
		t.Fatal("failed rotation left a pending generation in the keyring")
	}
	var keyID string
	if err := f.st.DB.QueryRowContext(ctx, `SELECT key_id FROM secret_versions WHERE secret_id=?`, s.ID).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if keyID != oldKey {
		t.Fatalf("version re-wrapped to %s despite rollback", keyID)
	}
	var n int
	if err := f.st.DB.QueryRowContext(ctx, `SELECT count(*) FROM master_keys`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("master_keys has %d rows after rollback, want 1", n)
	}
	if got := f.reveal(t, s.ID, "c", 0); got != "value" {
		t.Fatalf("value after failed rotation %q", got)
	}
	ch, _ := f.svc.Changes(ctx, s.ID)
	for _, c := range ch {
		if c.Action == "rewrapped" {
			t.Fatal("change record claims a rewrap that was rolled back")
		}
	}
	// Restart (so the on-disk keyring is what counts) and retry.
	f.reopen(t)
	res, err := f.svc.RotateMasterKey(ctx, "u")
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if res.OldKeyID != oldKey || f.kr.ActiveKeyID() != res.NewKeyID {
		t.Fatalf("retry result %+v", res)
	}
	if got := f.reveal(t, s.ID, "c", 0); got != "value" {
		t.Fatalf("value after retry %q", got)
	}
}

// Crash between step 1 (pending written) and step 2 (transaction committed):
// the pending generation is unused and is dropped at the next Open.
func TestInterruptedRotationBeforeCommitIsDropped(t *testing.T) {
	f := newFixture(t)
	s := f.create(t, "k", "value", "c")
	oldKey := f.kr.ActiveKeyID()
	pending, err := f.kr.preparePending()
	if err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	if f.kr.ActiveKeyID() != oldKey || f.kr.PendingKeyID() != "" {
		t.Fatalf("after reopen active=%s pending=%s", f.kr.ActiveKeyID(), f.kr.PendingKeyID())
	}
	for _, id := range f.kr.KeyIDs() {
		if id == pending {
			t.Fatal("unused pending generation kept in keyring")
		}
	}
	if got := f.reveal(t, s.ID, "c", 0); got != "value" {
		t.Fatalf("value %q", got)
	}
}

// Crash between step 2 (transaction committed) and step 3 (keyring activated):
// the data already refers to the pending generation, so Open activates it.
func TestInterruptedRotationAfterCommitIsCompleted(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.create(t, "k", "value", "c")
	oldKey := f.kr.ActiveKeyID()
	crash := errors.New("power lost")
	f.svc.rotateHook = func(step string) error {
		if step == "committed" {
			return crash
		}
		return nil
	}
	if _, err := f.svc.RotateMasterKey(ctx, "u"); !errors.Is(err, crash) {
		t.Fatalf("expected injected crash, got %v", err)
	}
	// On disk now: keyring active=old, pending=new; database rows under new.
	onDisk, err := LoadKeyring(f.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if onDisk.ActiveKeyID() != oldKey || onDisk.PendingKeyID() == "" {
		t.Fatalf("precondition: on-disk keyring active=%s pending=%s", onDisk.ActiveKeyID(), onDisk.PendingKeyID())
	}
	pending := onDisk.PendingKeyID()
	onDisk.Close()

	f.reopen(t)
	if f.kr.ActiveKeyID() != pending || f.kr.PendingKeyID() != "" {
		t.Fatalf("after reopen active=%s pending=%s, want active=%s", f.kr.ActiveKeyID(), f.kr.PendingKeyID(), pending)
	}
	var status string
	if err := f.st.DB.QueryRowContext(ctx, `SELECT status FROM master_keys WHERE key_id=?`, pending).Scan(&status); err != nil || status != "active" {
		t.Fatalf("master_keys for %s: %q %v", pending, status, err)
	}
	if err := f.st.DB.QueryRowContext(ctx, `SELECT status FROM master_keys WHERE key_id=?`, oldKey).Scan(&status); err != nil || status != "retired" {
		t.Fatalf("master_keys for old %s: %q %v", oldKey, status, err)
	}
	if got := f.reveal(t, s.ID, "c", 0); got != "value" {
		t.Fatalf("value %q", got)
	}
	s2 := f.create(t, "k2", "v", "c")
	if s2.KeyID != pending {
		t.Fatalf("new secret under %s, want %s", s2.KeyID, pending)
	}
}

func TestVerifyAllReportsDamageByLocationOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.create(t, "k", "sensitive-value", "c")
	if _, err := f.st.DB.ExecContext(ctx, `UPDATE secret_versions SET ciphertext=X'00' WHERE secret_id=?`, s.ID); err != nil {
		t.Fatal(err)
	}
	rep, err := f.svc.VerifyAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Checked != 1 || len(rep.Failures) != 1 || !strings.Contains(rep.Failures[0], s.ID+"@1") {
		t.Fatalf("report %+v", rep)
	}
	// A rotation must not proceed over data it cannot read.
	if _, err := f.svc.RotateMasterKey(ctx, "u"); err == nil {
		t.Fatal("rotation succeeded over undecryptable data")
	}
	if f.kr.PendingKeyID() != "" {
		t.Fatal("pending generation left after refused rotation")
	}
}

func TestRecoveryAcknowledgement(t *testing.T) {
	f := newFixture(t)
	if ok, _, _ := f.kr.RecoveryAcknowledged(); ok {
		t.Fatal("fresh keyring must not claim recovery material was taken")
	}
	if err := f.kr.AcknowledgeRecovery(""); err == nil {
		t.Fatal("acknowledgement without an actor accepted")
	}
	if err := f.kr.AcknowledgeRecovery("user:owner"); err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	ok, by, at := f.kr.RecoveryAcknowledged()
	if !ok || by != "user:owner" || at.IsZero() {
		t.Fatalf("after restart: %v %q %v", ok, by, at)
	}
	var src RecoveryMaterialSource = f.kr
	snap, err := src.Snapshot()
	if err != nil || !bytes.Contains(snap, []byte(f.kr.ActiveKeyID())) {
		t.Fatalf("snapshot: %v", err)
	}
}

// Writes racing a master-key rotation end up either before it (and are
// re-wrapped) or after it (sealed under the new generation); nothing is left
// under the retired generation.
func TestWritesDuringMasterKeyRotationAreNotLeftBehind(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first := f.create(t, "seed", "seed-value", "c")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := f.svc.Create(ctx, CreateInput{Kind: KindGeneric, Name: fmt.Sprintf("racer-%d", i), Value: []byte("v"), Scope: []string{"c"}, Actor: "u"}); err != nil {
				t.Errorf("create %d: %v", i, err)
			}
			if _, err := f.svc.Rotate(ctx, first.ID, []byte(fmt.Sprintf("seed-v%d", i)), "u"); err != nil {
				t.Errorf("rotate %d: %v", i, err)
			}
		}(i)
	}
	res, err := f.svc.RotateMasterKey(ctx, "u")
	if err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	var stale int
	if err := f.st.DB.QueryRowContext(ctx, `SELECT count(*) FROM secret_versions WHERE key_id<>?`, res.NewKeyID).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Fatalf("%d versions left under the retired generation", stale)
	}
	rep, err := f.svc.VerifyAll(ctx)
	if err != nil || len(rep.Failures) != 0 || rep.Checked != 1+8+8 {
		t.Fatalf("verify: %+v %v", rep, err)
	}
}
