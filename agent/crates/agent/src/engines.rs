//! Container engines on the host (ticket 039.1): answers `engines_probe`
//! by asking the privileged executor for `engine_inspect` with the grant
//! the Core sent. The agent cannot read root's engine sockets or the
//! users' runtime directories itself; it checks the report's shape and
//! relays it.

use hazelminx_privileged::action::Action;
use hazelminx_privileged::client::{Client, ClientError};
use hazelminx_privileged::grant::Grant;
use hazelminx_privileged::protocol::ErrorCode;
use hazelminx_protocol::engines::EngineReport;
use hazelminx_protocol::{ExecutorGrant, Message, engines_unavailable_code as code};
use time::OffsetDateTime;

/// Largest encoded report sent (well under the channel's 1 MiB message
/// limit once escaped into a frame).
pub const MAX_REPORT_BYTES: usize = 384 << 10;

/// The answer to one `engines_probe`.
pub async fn answer(client: &Client, request_id: String, grant: ExecutorGrant) -> Message {
    let unavailable = |c: &str, message: String| Message::EnginesUnavailable {
        request_id: request_id.clone(),
        code: c.to_string(),
        message,
    };
    let grant: Grant = match serde_json::to_value(&grant).and_then(serde_json::from_value) {
        Ok(g) => g,
        Err(e) => return unavailable(code::EXECUTOR_REFUSED, format!("grant is malformed: {e}")),
    };
    let collected_at = OffsetDateTime::now_utc();
    let collected_at = collected_at.replace_nanosecond(0).unwrap_or(collected_at);
    let value = match client
        .execute(&request_id, &grant, &Action::EngineInspect {}, |_, _| {})
        .await
    {
        Ok(v) => v,
        Err(e) => {
            let (c, m) = classify(&e);
            return unavailable(c, m);
        }
    };
    let report: EngineReport = match serde_json::from_value(value) {
        Ok(r) => r,
        Err(e) => {
            return unavailable(code::INSPECT_FAILED, format!("executor returned an unreadable report: {e}"));
        }
    };
    let msg = Message::EnginesReport {
        request_id: request_id.clone(),
        collected_at,
        report: Box::new(report),
    };
    if hazelminx_protocol::encode(&msg).len() > MAX_REPORT_BYTES {
        return unavailable(
            code::INSPECT_FAILED,
            format!("engine report is larger than {MAX_REPORT_BYTES} bytes"),
        );
    }
    msg
}

fn classify(e: &ClientError) -> (&'static str, String) {
    match e {
        ClientError::Io(err) => (
            code::EXECUTOR_UNREACHABLE,
            format!("cannot reach the privileged executor: {err}"),
        ),
        ClientError::Rejected(body) if body.code == ErrorCode::ActionFailed => {
            (code::INSPECT_FAILED, body.message.clone())
        }
        ClientError::Rejected(body) => (
            code::EXECUTOR_REFUSED,
            format!("{:?}: {}", body.code, body.message),
        ),
        ClientError::Protocol(m) => (
            code::INSPECT_FAILED,
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
        let failed = ClientError::Rejected(ErrorBody {
            code: ErrorCode::ActionFailed,
            message: "x".into(),
        });
        assert_eq!(classify(&failed).0, code::INSPECT_FAILED);
        let denied = ClientError::Rejected(ErrorBody {
            code: ErrorCode::PermissionDenied,
            message: "no".into(),
        });
        assert_eq!(classify(&denied).0, code::EXECUTOR_REFUSED);
    }

    #[tokio::test]
    async fn unreachable_executor_is_reported_not_guessed() {
        let c = Client::new(std::path::Path::new("/nonexistent/hazelminx-test.sock"));
        let g = ExecutorGrant {
            id: "g".into(),
            subject: "s".into(),
            not_before: "2026-01-01T00:00:00Z".into(),
            expires_at: "2026-01-01T00:05:00Z".into(),
            permissions: vec!["host.engines.read".into()],
            signature: "00".into(),
        };
        match answer(&c, "r1".into(), g).await {
            Message::EnginesUnavailable { request_id, code: c, .. } => {
                assert_eq!(request_id, "r1");
                assert_eq!(c, code::EXECUTOR_UNREACHABLE);
            }
            other => panic!("{other:?}"),
        }
    }
}
