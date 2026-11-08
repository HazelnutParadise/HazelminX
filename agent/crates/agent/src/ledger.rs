//! Core's copy of the host change ledger (spec 55.1): answers `ledger_pull`
//! by asking the privileged executor for a page (`ledger_export`) with the
//! grant the Core sent, and relays the raw lines unchanged. The agent cannot
//! read the ledger itself (it is root-only); it only passes pages along.

use hazelminx_privileged::action::Action;
use hazelminx_privileged::client::{Client, ClientError};
use hazelminx_privileged::grant::Grant;
use hazelminx_privileged::ledger::sync::LedgerBatch;
use hazelminx_privileged::protocol::ErrorCode;
use hazelminx_protocol::{ExecutorGrant, LedgerHead, Message, ledger_unavailable_code as code};

/// Line bytes asked for per page. A line's quotes are escaped once in the
/// message and again in the frame, so a page grows up to four times on the
/// wire; this keeps it well under the channel's 1 MiB message limit.
pub const MAX_PAGE_LINE_BYTES: u32 = 192 << 10;
/// Largest encoded `ledger_page` body sent; a bigger one is fetched again
/// with half the byte budget.
pub const MAX_PAGE_MESSAGE_BYTES: usize = 480 << 10;
/// Most lines asked for per page (the executor's own cap is 500).
pub const MAX_PAGE_LINES: u32 = 500;

/// The answer to one `ledger_pull`.
pub async fn answer(
    client: &Client,
    request_id: String,
    after_seq: u64,
    limit: u32,
    grant: ExecutorGrant,
) -> Message {
    let unavailable = |c: &str, message: String| Message::LedgerUnavailable {
        request_id: request_id.clone(),
        code: c.to_string(),
        message,
    };
    let grant: Grant = match serde_json::to_value(&grant).and_then(serde_json::from_value) {
        Ok(g) => g,
        Err(e) => {
            return unavailable(code::EXECUTOR_REFUSED, format!("grant is malformed: {e}"));
        }
    };
    let limit = limit.clamp(1, MAX_PAGE_LINES);
    let mut max_bytes = MAX_PAGE_LINE_BYTES;
    loop {
        let action = Action::LedgerExport {
            after_seq,
            limit: Some(limit),
            max_bytes: Some(max_bytes),
        };
        let value = match client
            .execute(&request_id, &grant, &action, |_, _| {})
            .await
        {
            Ok(v) => v,
            Err(e) => {
                let (c, m) = classify(&e);
                return unavailable(c, m);
            }
        };
        let batch: LedgerBatch = match serde_json::from_value(value) {
            Ok(b) => b,
            Err(e) => {
                return unavailable(
                    code::EXPORT_FAILED,
                    format!("executor returned an unreadable page: {e}"),
                );
            }
        };
        let page = Message::LedgerPage {
            request_id: request_id.clone(),
            ledger_id: batch.ledger_id,
            after_seq: batch.after_seq,
            lines: batch.lines,
            head: LedgerHead {
                seq: batch.head.seq,
                hash: batch.head.hash,
            },
            more: batch.more,
        };
        let size = hazelminx_protocol::encode(&page).len();
        let one_line = matches!(&page, Message::LedgerPage { lines, .. } if lines.len() <= 1);
        if size <= MAX_PAGE_MESSAGE_BYTES || one_line {
            return page;
        }
        max_bytes /= 2;
    }
}

/// Maps a client error to a `ledger_unavailable` code and message. The
/// message carries the executor's own wording, which never contains
/// secrets (ticket 012).
fn classify(e: &ClientError) -> (&'static str, String) {
    match e {
        ClientError::Io(err) => (
            code::EXECUTOR_UNREACHABLE,
            format!("cannot reach the privileged executor: {err}"),
        ),
        ClientError::Rejected(body) if body.code == ErrorCode::ActionFailed => {
            (code::EXPORT_FAILED, body.message.clone())
        }
        ClientError::Rejected(body) => (
            code::EXECUTOR_REFUSED,
            format!("{:?}: {}", body.code, body.message),
        ),
        ClientError::Protocol(m) => (
            code::EXPORT_FAILED,
            format!("executor answered unexpectedly: {m}"),
        ),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use hazelminx_privileged::protocol::ErrorBody;

    #[test]
    fn errors_map_to_codes() {
        let io = ClientError::Io(std::io::Error::from(std::io::ErrorKind::NotFound));
        assert_eq!(classify(&io).0, code::EXECUTOR_UNREACHABLE);
        let refused = ClientError::Rejected(ErrorBody {
            code: ErrorCode::GrantInvalid,
            message: "grant signature does not verify".into(),
        });
        let (c, m) = classify(&refused);
        assert_eq!(c, code::EXECUTOR_REFUSED);
        assert!(m.contains("GrantInvalid") && m.contains("signature"));
        let failed = ClientError::Rejected(ErrorBody {
            code: ErrorCode::ActionFailed,
            message: "after_seq 9 is beyond the ledger head 2".into(),
        });
        assert_eq!(classify(&failed).0, code::EXPORT_FAILED);
    }

    #[tokio::test]
    async fn unreachable_executor_is_reported_not_hung() {
        let dir = tempfile::tempdir().unwrap();
        let client = Client::new(dir.path().join("missing.sock"));
        let g = ExecutorGrant {
            id: "g".into(),
            subject: "s".into(),
            permissions: vec!["host.ledger.read".into()],
            not_before: "2026-11-08T00:00:00Z".into(),
            expires_at: "2026-11-08T00:05:00Z".into(),
            signature: "00".into(),
        };
        match answer(&client, "r1".into(), 0, 10, g.clone()).await {
            Message::LedgerUnavailable {
                request_id,
                code: c,
                ..
            } => {
                assert_eq!(request_id, "r1");
                assert_eq!(c, code::EXECUTOR_UNREACHABLE);
            }
            other => panic!("{other:?}"),
        }
        let bad = ExecutorGrant {
            not_before: "yesterday".into(),
            ..g
        };
        assert!(matches!(
            answer(&client, "r2".into(), 0, 10, bad).await,
            Message::LedgerUnavailable { code: c, .. } if c == code::EXECUTOR_REFUSED
        ));
    }
}
