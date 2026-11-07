//! Socket frames. One request per connection, newline-delimited JSON:
//! the client writes one request line; the server answers with zero or more
//! `output` frames followed by exactly one terminal frame (`result`,
//! `error`, `health` or `revoked`).

use crate::command::StreamKind;
use crate::grant::Grant;
use base64::Engine;
use base64::engine::general_purpose::STANDARD as BASE64;
use serde::{Deserialize, Serialize};

/// Largest accepted request line. Actions carry parameters, not payloads;
/// bulk data never crosses this socket (SEC-009).
pub const MAX_REQUEST_BYTES: usize = 64 * 1024;
pub const PROTOCOL_VERSION: u32 = 1;

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum Request {
    Action {
        request_id: String,
        grant: Grant,
        /// Parsed into [`crate::action::Action`] after the grant is accepted,
        /// so a bad parameter is reported as such rather than as a malformed
        /// request.
        action: serde_json::Value,
    },
    Health,
    Revoke {
        grant_id: String,
    },
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ErrorCode {
    Malformed,
    RequestTooLarge,
    UnauthorizedCaller,
    GrantInvalid,
    PermissionDenied,
    UnknownAction,
    InvalidParam,
    Busy,
    ActionFailed,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ErrorBody {
    pub code: ErrorCode,
    pub message: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum Response {
    Output {
        stream: StreamKind,
        /// Base64 (standard alphabet) of the raw bytes.
        data: String,
    },
    Result {
        request_id: String,
        result: serde_json::Value,
    },
    Error {
        request_id: Option<String>,
        error: ErrorBody,
    },
    Health {
        protocol_version: u32,
        version: String,
        active_actions: usize,
        max_actions: usize,
    },
    Revoked {
        grant_id: String,
    },
}

impl Response {
    pub fn error(request_id: Option<String>, code: ErrorCode, message: impl Into<String>) -> Self {
        Response::Error {
            request_id,
            error: ErrorBody {
                code,
                message: message.into(),
            },
        }
    }

    pub fn output(stream: StreamKind, data: &[u8]) -> Self {
        Response::Output {
            stream,
            data: BASE64.encode(data),
        }
    }

    /// Decodes an `Output` frame's bytes.
    pub fn output_bytes(data: &str) -> Option<Vec<u8>> {
        BASE64.decode(data).ok()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn output_frame_round_trips_binary() {
        let bytes: Vec<u8> = (0..=255u8).collect();
        let Response::Output { data, .. } = Response::output(StreamKind::Stderr, &bytes) else {
            panic!()
        };
        assert_eq!(Response::output_bytes(&data).unwrap(), bytes);
        assert!(Response::output_bytes("not base64!").is_none());
    }

    #[test]
    fn request_wire_shape() {
        let r: Request = serde_json::from_str(r#"{"type":"health"}"#).unwrap();
        assert_eq!(r, Request::Health);
        let r: Request = serde_json::from_str(r#"{"type":"revoke","grant_id":"g"}"#).unwrap();
        assert_eq!(
            r,
            Request::Revoke {
                grant_id: "g".into()
            }
        );
        assert!(serde_json::from_str::<Request>(r#"{"type":"shell","cmd":"id"}"#).is_err());
    }
}
