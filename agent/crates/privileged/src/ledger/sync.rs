//! Core's copy of the ledger (spec 55.1: "Core 也保存一份").
//!
//! The executor hands out raw ledger lines in pages through the
//! `ledger_export` action; the agent relays them to Core, which verifies
//! each page against the head it already holds and appends it. Lines travel
//! as the exact bytes on disk, so Core verifies the hash chain without
//! re-serializing (`json.RawMessage` in Go).
//!
//! On the Core↔Agent channel the Core sends `ledger_pull` with a grant for
//! `host.ledger.read`; the agent calls `ledger_export` with it and answers
//! `ledger_page` (the [`LedgerBatch`] fields) or `ledger_unavailable`
//! (engineering/contracts/README.md). Core's check is `core/internal/hostledger`;
//! [`LedgerBatch::verify`] is the same rule, and both run against
//! `engineering/contracts/fixtures/host-ledger-batch.json`.

use super::entry::Entry;
use super::store::{genesis_hash, sha256_hex};
use serde::{Deserialize, Serialize};
use serde_json::value::RawValue;

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Head {
    pub seq: u64,
    pub hash: String,
}

/// One page of raw lines, as `ledger_export` returns it.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct LedgerBatch {
    pub ledger_id: String,
    pub after_seq: u64,
    /// Lines `after_seq + 1 ..`, each exactly as stored (no newline).
    pub lines: Vec<String>,
    /// The ledger's head at export time (may be past the last line).
    pub head: Head,
    pub more: bool,
}

#[derive(Deserialize)]
struct RawLine<'a> {
    hash: String,
    #[serde(borrow)]
    entry: &'a RawValue,
}

impl LedgerBatch {
    /// Core's check of a page: `prev` is the head Core holds for this
    /// ledger (`None` for the first page). Returns the new head, or why the
    /// page cannot be appended.
    pub fn verify(&self, prev: Option<&Head>) -> Result<Head, String> {
        let mut head = match prev {
            Some(h) => h.clone(),
            None => Head {
                seq: 0,
                hash: genesis_hash(&self.ledger_id),
            },
        };
        if head.seq != self.after_seq {
            return Err(format!(
                "page starts after {} but the copy ends at {}",
                self.after_seq, head.seq
            ));
        }
        for (i, line) in self.lines.iter().enumerate() {
            let raw: RawLine = serde_json::from_str(line)
                .map_err(|e| format!("line {} is not a ledger line: {e}", i + 1))?;
            if sha256_hex(raw.entry.get().as_bytes()) != raw.hash {
                return Err(format!("line {} hash does not match", i + 1));
            }
            let entry: Entry = serde_json::from_str(raw.entry.get())
                .map_err(|e| format!("line {} entry does not parse: {e}", i + 1))?;
            if entry.seq != head.seq + 1 || entry.prev_hash != head.hash {
                return Err(format!("line {} does not continue the chain", i + 1));
            }
            head = Head {
                seq: entry.seq,
                hash: raw.hash,
            };
        }
        if !self.more && head != self.head {
            return Err("last page does not end at the reported head".into());
        }
        Ok(head)
    }

    /// Parsed entries (after [`Self::verify`]).
    pub fn entries(&self) -> Result<Vec<Entry>, String> {
        self.lines
            .iter()
            .map(|line| {
                let raw: RawLine = serde_json::from_str(line).map_err(|e| e.to_string())?;
                serde_json::from_str(raw.entry.get()).map_err(|e| e.to_string())
            })
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ledger::entry::Phase;
    use crate::ledger::store::Ledger;
    use crate::ledger::store::tests::{sample, sec_for, tight_tempdir};

    #[test]
    fn core_rejects_gaps_splices_and_edits() {
        let root = tight_tempdir();
        let (l, _) = Ledger::open(&root.path().join("ledger"), &sec_for(root.path())).unwrap();
        l.append(vec![
            sample(Phase::Intent, "a.service"),
            sample(Phase::Result, "a.service"),
            sample(Phase::Intent, "b.service"),
        ])
        .unwrap();
        let all = l.export(0, 10, None).unwrap();
        let head = all.verify(None).unwrap();
        assert_eq!(head, l.head());
        assert_eq!(all.entries().unwrap().len(), 3);

        // Page that skips what Core has not seen.
        let later = l.export(2, 10, None).unwrap();
        assert!(later.verify(None).is_err());
        let first = l.export(0, 1, None).unwrap();
        let h1 = first.verify(None).unwrap();
        assert!(later.verify(Some(&h1)).is_err());

        // Edited line.
        let mut edited = all.clone();
        edited.lines[1] = edited.lines[1].replacen("\"active\"", "\"failed\"", 1);
        assert!(edited.verify(None).is_err());

        // Lines from another ledger.
        let mut other = all.clone();
        other.ledger_id = "00".repeat(16);
        assert!(other.verify(None).is_err());

        // Missing tail while claiming to be complete.
        let mut short = all.clone();
        short.lines.pop();
        assert!(short.verify(None).is_err());
    }

    #[test]
    fn wire_shape_matches_the_contract_fixture() {
        let text =
            include_str!("../../../../../engineering/contracts/fixtures/host-ledger-batch.json");
        let batch: LedgerBatch = serde_json::from_str(text).unwrap();
        let head = batch.verify(None).unwrap();
        assert_eq!(head, batch.head);
        let entries = batch.entries().unwrap();
        assert_eq!(entries[0].phase, Phase::Intent);
        assert_eq!(entries[1].intent_seq, Some(1));
    }
}
