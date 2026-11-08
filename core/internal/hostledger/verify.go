// Package hostledger keeps Core's copy of each host's change ledger (spec
// 55.1, UNX-001). The host is the source: its privileged executor writes the
// ledger and exports raw lines; the agent relays them in ledger_page
// messages; this package checks each page against the copy it already holds
// and appends it.
//
// A line is {"hash":"<hex>","entry":{…}}. hash is the SHA-256 of the entry
// bytes exactly as written, entry.seq counts 1, 2, 3, … and entry.prev_hash
// is the previous line's hash; the first line chains to
// sha256("hazelminx-ledger-v1\n" + ledgerID). The rules match
// agent/crates/privileged/src/ledger/store.rs and are checked against the
// same fixture.
package hostledger

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// ErrChain: a page does not continue the copy (wrong start, edited line,
// gap, reordering, other ledger, or a head that does not match).
var ErrChain = errors.New("hostledger: page does not continue the ledger copy")

// Line is one verified ledger line.
type Line struct {
	Seq  uint64
	Hash string
	Raw  string
}

// GenesisHash is what the first line's prev_hash must be.
func GenesisHash(ledgerID string) string {
	sum := sha256.Sum256([]byte("hazelminx-ledger-v1\n" + ledgerID))
	return hex.EncodeToString(sum[:])
}

// VerifyPage checks page against prev, the head of the copy (nil for a copy
// that has no lines yet), and returns the lines and the new head.
//
// Expected results: lines that continue prev with matching hashes give the
// new head; a page that starts elsewhere, any edited, missing, reordered or
// foreign line, a line with unknown or repeated keys, or a last page whose
// end is not page.Head is ErrChain naming the line.
func VerifyPage(prev *protocol.LedgerHead, page *protocol.LedgerPage) ([]Line, protocol.LedgerHead, error) {
	head := protocol.LedgerHead{Seq: 0, Hash: GenesisHash(page.LedgerID)}
	if prev != nil {
		head = *prev
	}
	if page.AfterSeq != head.Seq {
		return nil, head, fmt.Errorf("%w: page starts after %d but the copy ends at %d", ErrChain, page.AfterSeq, head.Seq)
	}
	out := make([]Line, 0, len(page.Lines))
	for i, raw := range page.Lines {
		n := i + 1
		hash, entry, err := splitLine([]byte(raw))
		if err != nil {
			return nil, head, fmt.Errorf("%w: line %d: %v", ErrChain, n, err)
		}
		sum := sha256.Sum256(entry)
		if hex.EncodeToString(sum[:]) != hash {
			return nil, head, fmt.Errorf("%w: line %d: hash does not match its entry", ErrChain, n)
		}
		var e struct {
			Seq      *uint64 `json:"seq"`
			PrevHash *string `json:"prev_hash"`
		}
		if err := json.Unmarshal(entry, &e); err != nil || e.Seq == nil || e.PrevHash == nil {
			return nil, head, fmt.Errorf("%w: line %d: entry has no seq or prev_hash", ErrChain, n)
		}
		if *e.Seq != head.Seq+1 || *e.PrevHash != head.Hash {
			return nil, head, fmt.Errorf("%w: line %d does not continue the chain", ErrChain, n)
		}
		head = protocol.LedgerHead{Seq: *e.Seq, Hash: hash}
		out = append(out, Line{Seq: head.Seq, Hash: hash, Raw: raw})
	}
	if !page.More && head != page.Head {
		return nil, head, fmt.Errorf("%w: last page ends at %d, host reports head %d", ErrChain, head.Seq, page.Head.Seq)
	}
	return out, head, nil
}

// splitLine returns the hash and the exact entry bytes of a line, which must
// be an object with exactly the keys "hash" (string) and "entry" (object),
// each once.
func splitLine(line []byte) (string, []byte, error) {
	dec := json.NewDecoder(bytes.NewReader(line))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return "", nil, errors.New("not a JSON object")
	}
	var hash string
	var entry json.RawMessage
	seen := map[string]bool{}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return "", nil, errors.New("not a JSON object")
		}
		key, _ := t.(string)
		if seen[key] {
			return "", nil, fmt.Errorf("key %q repeats", key)
		}
		seen[key] = true
		switch key {
		case "hash":
			if err := dec.Decode(&hash); err != nil {
				return "", nil, errors.New("hash is not a string")
			}
		case "entry":
			if err := dec.Decode(&entry); err != nil {
				return "", nil, errors.New("entry is not JSON")
			}
		default:
			return "", nil, fmt.Errorf("unknown key %q", key)
		}
	}
	if _, err := dec.Token(); err != nil {
		return "", nil, errors.New("not a JSON object")
	}
	if _, err := dec.Token(); err != io.EOF {
		return "", nil, errors.New("trailing data after the object")
	}
	if !seen["hash"] || !seen["entry"] || !bytes.HasPrefix(entry, []byte("{")) {
		return "", nil, errors.New("needs a string hash and an object entry")
	}
	return hash, entry, nil
}
