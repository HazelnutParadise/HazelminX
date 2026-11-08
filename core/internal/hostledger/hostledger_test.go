package hostledger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

func fixture(t *testing.T, rel string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "contracts", "fixtures", rel))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

// The executor (Rust) wrote these lines; Core must accept them as they are.
func TestVerifyAcceptsTheRustFixtures(t *testing.T) {
	var batch struct {
		LedgerID string              `json:"ledger_id"`
		AfterSeq uint64              `json:"after_seq"`
		Lines    []string            `json:"lines"`
		Head     protocol.LedgerHead `json:"head"`
		More     bool                `json:"more"`
	}
	fixture(t, "host-ledger-batch.json", &batch)
	page := &protocol.LedgerPage{LedgerID: batch.LedgerID, AfterSeq: batch.AfterSeq, Lines: batch.Lines, Head: batch.Head, More: batch.More}
	lines, head, err := VerifyPage(nil, page)
	if err != nil {
		t.Fatal(err)
	}
	if head != batch.Head || len(lines) != 2 || lines[1].Seq != 2 {
		t.Fatalf("head %+v lines %d", head, len(lines))
	}
	var msg protocol.LedgerPage
	fixture(t, "messages/ledger_page.json", &msg)
	if _, _, err := VerifyPage(nil, &msg); err != nil {
		t.Fatalf("ledger_page fixture: %v", err)
	}
}

func TestVerifyRejectsEveryKindOfDamage(t *testing.T) {
	var page protocol.LedgerPage
	fixture(t, "messages/ledger_page.json", &page)
	clone := func(f func(p *protocol.LedgerPage)) *protocol.LedgerPage {
		c := page
		c.Lines = append([]string(nil), page.Lines...)
		f(&c)
		return &c
	}
	cases := map[string]*protocol.LedgerPage{
		"edited entry":       clone(func(p *protocol.LedgerPage) { p.Lines[1] = strings.Replace(p.Lines[1], `"active"`, `"failed"`, 1) }),
		"line removed":       clone(func(p *protocol.LedgerPage) { p.Lines = p.Lines[1:] }),
		"lines swapped":      clone(func(p *protocol.LedgerPage) { p.Lines[0], p.Lines[1] = p.Lines[1], p.Lines[0] }),
		"other ledger":       clone(func(p *protocol.LedgerPage) { p.LedgerID = strings.Repeat("0", 32) }),
		"short of head":      clone(func(p *protocol.LedgerPage) { p.Lines = p.Lines[:1] }),
		"wrong start":        clone(func(p *protocol.LedgerPage) { p.AfterSeq = 1 }),
		"extra key":          clone(func(p *protocol.LedgerPage) { p.Lines[0] = strings.Replace(p.Lines[0], `{"hash"`, `{"x":1,"hash"`, 1) }),
		"repeated entry key": clone(func(p *protocol.LedgerPage) { p.Lines[0] = strings.TrimSuffix(p.Lines[0], "}") + `,"entry":{}}` }),
		"trailing data":      clone(func(p *protocol.LedgerPage) { p.Lines[0] += " {}" }),
		"not json":           clone(func(p *protocol.LedgerPage) { p.Lines[0] = "garbage" }),
	}
	for name, p := range cases {
		if _, _, err := VerifyPage(nil, p); !errors.Is(err, ErrChain) {
			t.Errorf("%s: err %v", name, err)
		}
	}
}

// chain builds n valid lines of a ledger (only seq and prev_hash matter to
// Core's check).
func chain(ledgerID string, n int) ([]string, protocol.LedgerHead) {
	prev := protocol.LedgerHead{Hash: GenesisHash(ledgerID)}
	var lines []string
	for i := 1; i <= n; i++ {
		entry := fmt.Sprintf(`{"seq":%d,"prev_hash":"%s","action":"restart_service"}`, i, prev.Hash)
		sum := sha256.Sum256([]byte(entry))
		h := hex.EncodeToString(sum[:])
		lines = append(lines, fmt.Sprintf(`{"hash":"%s","entry":%s}`, h, entry))
		prev = protocol.LedgerHead{Seq: uint64(i), Hash: h}
	}
	return lines, prev
}

func openStore(t *testing.T) *Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewStore(st.DB, nil)
}

// fakeHost answers ledger_pull from a full line list, in pages.
type fakeHost struct {
	mu       sync.Mutex
	ledgerID string
	lines    []string
	head     protocol.LedgerHead
	err      error
	grants   []protocol.ExecutorGrant
}

func (h *fakeHost) PullLedger(_ context.Context, _ string, afterSeq uint64, limit int, grant protocol.ExecutorGrant) (*protocol.LedgerPage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.grants = append(h.grants, grant)
	if h.err != nil {
		return nil, h.err
	}
	if afterSeq > uint64(len(h.lines)) {
		return nil, &nodes.LedgerUnavailableError{Code: protocol.LedgerExportFailed, Message: "after_seq is beyond the ledger head"}
	}
	end := min(int(afterSeq)+limit, len(h.lines))
	return &protocol.LedgerPage{Type: protocol.TypeLedgerPage, LedgerID: h.ledgerID, AfterSeq: afterSeq,
		Lines: append([]string(nil), h.lines[afterSeq:end]...), Head: h.head, More: end < len(h.lines)}, nil
}

type fakeGrants struct{}

func (fakeGrants) Issue(subject string, perms []string, lifetime time.Duration) (protocol.ExecutorGrant, error) {
	return protocol.ExecutorGrant{ID: "grt-test", Subject: subject, Permissions: perms, NotBefore: "2026-11-08T00:00:00Z",
		ExpiresAt: "2026-11-08T00:05:00Z", Signature: "00"}, nil
}

func TestSyncCopiesInPagesAndContinues(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	lines, head := chain("aa11", 7)
	host := &fakeHost{ledgerID: "aa11", lines: lines[:5], head: protocol.LedgerHead{Seq: 5, Hash: mustHash(lines[4])}}
	s := &Syncer{Store: st, Grants: fakeGrants{}, Pull: host, PageLimit: 2}

	got, err := s.SyncNode(ctx, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Seq != 5 || len(host.grants) != 3 {
		t.Fatalf("head %+v after %d pulls", got, len(host.grants))
	}
	g := host.grants[0]
	if g.Subject != GrantSubject || len(g.Permissions) != 1 || g.Permissions[0] != GrantPermission {
		t.Fatalf("grant %+v", g)
	}
	c, ok, _ := st.Get(ctx, "node-a")
	if !ok || c.LedgerID != "aa11" || c.Head.Seq != 5 || c.LastSyncedAt == nil || c.LastErrorCode != "" {
		t.Fatalf("copy %+v", c)
	}

	// The host writes two more lines; the next sync only fetches those.
	host.mu.Lock()
	host.lines, host.head = lines, head
	host.grants = nil
	host.mu.Unlock()
	got, err = s.SyncNode(ctx, "node-a")
	if err != nil || got != head || len(host.grants) != 1 {
		t.Fatalf("head %+v err %v pulls %d", got, err, len(host.grants))
	}
	stored, err := st.Lines(ctx, "node-a", 0, 100)
	if err != nil || len(stored) != 7 {
		t.Fatalf("stored %d err %v", len(stored), err)
	}
	for i, l := range stored {
		if l.Raw != lines[i] {
			t.Fatalf("line %d stored as %q", i+1, l.Raw)
		}
	}
	// Nothing new: one pull, no change.
	host.grants = nil
	if got, err := s.SyncNode(ctx, "node-a"); err != nil || got != head {
		t.Fatalf("idle sync: %+v %v", got, err)
	}
}

func mustHash(line string) string {
	var l struct {
		Hash string `json:"hash"`
	}
	_ = json.Unmarshal([]byte(line), &l)
	return l.Hash
}

func TestSyncRecordsWhyItFailedAndStoresNothingBad(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	lines, head := chain("aa11", 3)
	host := &fakeHost{ledgerID: "aa11", lines: lines, head: head}
	s := &Syncer{Store: st, Grants: fakeGrants{}, Pull: host}
	if _, err := s.SyncNode(ctx, "n"); err != nil {
		t.Fatal(err)
	}

	expectError := func(name, code string) {
		t.Helper()
		if _, err := s.SyncNode(ctx, "n"); err == nil {
			t.Fatalf("%s: no error", name)
		}
		c, _, _ := st.Get(ctx, "n")
		if c.LastErrorCode != code || c.Head != head {
			t.Fatalf("%s: copy %+v", name, c)
		}
		n, _ := st.Lines(ctx, "n", 0, 100)
		if len(n) != 3 {
			t.Fatalf("%s: %d lines stored", name, len(n))
		}
	}

	// The host's ledger was recreated: same node, new ledger id.
	other, otherHead := chain("bb22", 4)
	host.ledgerID, host.lines, host.head = "bb22", other, otherHead
	expectError("replaced", CodeLedgerReplaced)

	// The host's ledger is shorter than Core's copy (rolled back).
	host.ledgerID, host.lines, host.head = "aa11", lines[:2], protocol.LedgerHead{Seq: 2, Hash: mustHash(lines[1])}
	expectError("shorter", protocol.LedgerExportFailed)

	// A line was edited after Core copied it.
	more, moreHead := chain("aa11", 4)
	more[3] = strings.Replace(more[3], "restart_service", "remove_package", 1)
	host.lines, host.head = more, moreHead
	expectError("edited", CodeChainBroken)

	host.err = nodes.ErrLedgerUnsupported
	expectError("old agent", CodeAgentUnsupported)

	// Offline is normal and is not recorded as a failure.
	host.err = nodes.ErrNodeOffline
	if _, err := s.SyncNode(ctx, "n"); !errors.Is(err, nodes.ErrNodeOffline) {
		t.Fatal(err)
	}
	if c, _, _ := st.Get(ctx, "n"); c.LastErrorCode != CodeAgentUnsupported {
		t.Fatalf("offline overwrote the last error: %+v", c)
	}

	// Recovery clears the error.
	host.err = nil
	host.lines, host.head = more[:3], head
	if _, err := s.SyncNode(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if c, _, _ := st.Get(ctx, "n"); c.LastErrorCode != "" || c.LastErrorAt != nil {
		t.Fatalf("error not cleared: %+v", c)
	}
}

func TestFirstSyncFailureIsRecordedWithoutACopy(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	host := &fakeHost{err: &nodes.LedgerUnavailableError{Code: protocol.LedgerExecutorUnreachable, Message: "connect: no such file"}}
	s := &Syncer{Store: st, Grants: fakeGrants{}, Pull: host}
	if _, err := s.SyncNode(ctx, "n"); err == nil {
		t.Fatal("no error")
	}
	c, ok, _ := st.Get(ctx, "n")
	if !ok || c.LedgerID != "" || c.LastErrorCode != protocol.LedgerExecutorUnreachable {
		t.Fatalf("copy %+v ok %v", c, ok)
	}
	// Once the executor is reachable, the first page creates the copy.
	lines, head := chain("cc33", 2)
	host.err, host.ledgerID, host.lines, host.head = nil, "cc33", lines, head
	if got, err := s.SyncNode(ctx, "n"); err != nil || got != head {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestStoredLinesCannotBeChangedOrDeleted(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	lines, head := chain("aa11", 2)
	if _, err := st.Append(ctx, "n", &protocol.LedgerPage{LedgerID: "aa11", Lines: lines, Head: head}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE host_ledger_lines SET line = 'x' WHERE seq = 1`); err == nil {
		t.Fatal("update allowed")
	}
	if _, err := st.db.ExecContext(ctx, `DELETE FROM host_ledger_lines`); err == nil {
		t.Fatal("delete allowed")
	}
	if _, err := st.db.ExecContext(ctx, `DELETE FROM host_ledgers`); err == nil {
		t.Fatal("deleting the copy with lines allowed")
	}
}

func TestConcurrentSyncsOfOneNodeDoNotDoubleStore(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	lines, head := chain("aa11", 50)
	host := &fakeHost{ledgerID: "aa11", lines: lines, head: head}
	s := &Syncer{Store: st, Grants: fakeGrants{}, Pull: host, PageLimit: 7}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Go(func() {
			if _, err := s.SyncNode(ctx, "n"); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if n, _ := st.Lines(ctx, "n", 0, 1000); len(n) != 50 {
		t.Fatalf("%d lines", len(n))
	}
}
