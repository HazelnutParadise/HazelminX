//! Core<->Agent channel contract (docs/contracts/README.md).
//!
//! Two decoders exist on purpose:
//! - [`decode_handshake`] reads the five handshake messages and ignores
//!   unknown fields, so agents and Cores of any protocol version can at
//!   least tell each other "incompatible, here is my range".
//! - [`decode`] reads everything after `ready` strictly: unknown `type`,
//!   unknown fields and missing fields are errors, and the receiver must not
//!   act on such a message.
//!
//! The JSON shapes are pinned by `docs/contracts/fixtures`; the Go mirror in
//! `core/internal/protocol` is tested against the same files.

use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};
use time::OffsetDateTime;

/// Lowest and highest protocol version this build speaks.
pub const PROTOCOL_MIN: u32 = 1;
pub const PROTOCOL_MAX: u32 = 1;

/// DNS name in the Core identity certificate. Agents verify this name, not
/// the host or IP in `core_url`.
pub const CORE_SERVER_NAME: &str = "core.hazelminx.internal";
/// WebSocket path on the Core agent listener.
pub const CONNECT_PATH: &str = "/agent/v1/connect";
/// First line of the signed session transcript.
pub const TRANSCRIPT_DOMAIN: &str = "hazelminx-session-v1";
/// Raw nonce length before base64url encoding.
pub const NONCE_BYTES: usize = 32;

/// Capability names of protocol v1.
pub const CAP_JOBS: &str = "jobs";
pub const CAP_AGENT_INFO: &str = "agent.info";
/// `ledger_pull` / `ledger_page` / `ledger_unavailable`: Core keeps a copy of
/// the host change ledger (spec 55.1).
pub const CAP_HOST_LEDGER: &str = "host.ledger";

/// `reject` codes.
pub mod reject_code {
    pub const PROTOCOL_INCOMPATIBLE: &str = "PROTOCOL_INCOMPATIBLE";
    pub const HANDSHAKE_MALFORMED: &str = "HANDSHAKE_MALFORMED";
    pub const IDENTITY_INVALID: &str = "IDENTITY_INVALID";
    pub const PROOF_INVALID: &str = "PROOF_INVALID";
    pub const NODE_REVOKED: &str = "NODE_REVOKED";
    pub const DUPLICATE_SESSION: &str = "DUPLICATE_SESSION";
}

/// `step_rejected` codes.
pub mod step_reject_code {
    pub const UNSUPPORTED_ACTION: &str = "UNSUPPORTED_ACTION";
    pub const INVALID_PARAMS: &str = "INVALID_PARAMS";
    pub const WRONG_NODE: &str = "WRONG_NODE";
    pub const EXPIRED: &str = "EXPIRED";
    pub const STALE_CONTROL_GENERATION: &str = "STALE_CONTROL_GENERATION";
    pub const OUTCOME_UNKNOWN: &str = "OUTCOME_UNKNOWN";
    pub const JOURNAL_UNAVAILABLE: &str = "JOURNAL_UNAVAILABLE";
}

/// `protocol_error` codes.
pub mod protocol_error_code {
    pub const UNKNOWN_MESSAGE: &str = "UNKNOWN_MESSAGE";
    pub const MALFORMED: &str = "MALFORMED";
}

/// `ledger_unavailable` codes.
pub mod ledger_unavailable_code {
    /// The agent cannot reach the privileged executor's socket.
    pub const EXECUTOR_UNREACHABLE: &str = "EXECUTOR_UNREACHABLE";
    /// The executor refused the export (grant, permission, parameters).
    pub const EXECUTOR_REFUSED: &str = "EXECUTOR_REFUSED";
    /// The executor accepted but could not export (ledger not open, or
    /// `afterSeq` is past the host ledger's head).
    pub const EXPORT_FAILED: &str = "EXPORT_FAILED";
}

/// A grant for the privileged executor, carried through the agent unchanged.
/// Field names are the executor's own (snake_case), not this protocol's.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ExecutorGrant {
    pub id: String,
    pub subject: String,
    pub permissions: Vec<String>,
    /// RFC 3339 UTC, whole seconds.
    pub not_before: String,
    pub expires_at: String,
    /// Hex Ed25519 signature over the executor's canonical grant bytes.
    pub signature: String,
}

/// Last line of a host ledger: its sequence number and hash.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct LedgerHead {
    pub seq: u64,
    pub hash: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub struct ProtocolRange {
    pub min: u32,
    pub max: u32,
}

/// No `deny_unknown_fields`: capabilities appear in the handshake, which
/// must tolerate fields a newer peer adds. The agent never decodes the
/// strict messages that carry capabilities (`step_rejected` flows agent to
/// core), and the Core checks those strictly.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Capability {
    pub name: String,
    pub version: u32,
}

/// Known failure of a step, as the agent reports it.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct StepFailure {
    pub code: String,
    pub summary: String,
    pub retryable: bool,
}

/// Handshake messages. Unknown fields are ignored (see module docs).
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(
    tag = "type",
    rename_all = "snake_case",
    rename_all_fields = "camelCase"
)]
pub enum Handshake {
    Hello {
        protocol: ProtocolRange,
        agent_version: String,
        /// PEM of the node certificate issued by the Core CA.
        node_certificate: String,
        nonce: String,
        /// Ephemeral X25519 public key, base64url without padding.
        kex: String,
        capabilities: Vec<Capability>,
    },
    Welcome {
        protocol_version: u32,
        core_version: String,
        /// PEM of the Core identity certificate.
        core_certificate: String,
        nonce: String,
        kex: String,
        session_id: String,
        /// Core signature over the `core` transcript, standard base64.
        proof: String,
        heartbeat_seconds: u32,
        capabilities: Vec<Capability>,
    },
    Reject {
        code: String,
        message: String,
        #[serde(default, skip_serializing_if = "Option::is_none")]
        supported: Option<ProtocolRange>,
    },
    Auth {
        /// Node signature over the `agent` transcript, standard base64.
        proof: String,
    },
    Ready {
        session_id: String,
    },
}

/// Durable event kinds. Only these are journaled and acknowledged.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum EventKind {
    Started,
    Completed,
    Failed,
}

/// Messages after `ready`. Strict: unknown fields and types are errors.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(
    tag = "type",
    rename_all = "snake_case",
    rename_all_fields = "camelCase",
    deny_unknown_fields
)]
pub enum Message {
    Heartbeat {
        #[serde(with = "time::serde::rfc3339")]
        at: OffsetDateTime,
    },
    StepExecute {
        request_id: String,
        job_id: String,
        step: u32,
        step_name: String,
        action: String,
        action_version: u32,
        params: Map<String, Value>,
        target_node: String,
        control_generation: u64,
        #[serde(with = "time::serde::rfc3339")]
        expires_at: OffsetDateTime,
        retryable: bool,
        irreversible: bool,
    },
    StepRejected {
        request_id: String,
        job_id: String,
        step: u32,
        code: String,
        message: String,
        #[serde(default, skip_serializing_if = "Option::is_none")]
        supported: Option<Vec<Capability>>,
    },
    StepReconcile {
        request_id: String,
        job_id: String,
        step: u32,
        target_node: String,
    },
    ReconcileReport {
        request_id: String,
        job_id: String,
        step: u32,
        started: bool,
        completed: bool,
        failed: bool,
        #[serde(default, skip_serializing_if = "Vec::is_empty")]
        evidence: Vec<String>,
        #[serde(default, skip_serializing_if = "Option::is_none")]
        error: Option<StepFailure>,
    },
    Event {
        seq: u64,
        kind: EventKind,
        job_id: String,
        step: u32,
        request_id: String,
        #[serde(with = "time::serde::rfc3339")]
        at: OffsetDateTime,
        #[serde(default, skip_serializing_if = "Vec::is_empty")]
        evidence: Vec<String>,
        #[serde(default, skip_serializing_if = "Option::is_none")]
        result: Option<Value>,
        #[serde(default, skip_serializing_if = "Option::is_none")]
        error: Option<StepFailure>,
    },
    StepProgress {
        job_id: String,
        step: u32,
        #[serde(with = "time::serde::rfc3339")]
        at: OffsetDateTime,
        #[serde(default, skip_serializing_if = "Option::is_none")]
        progress: Option<u8>,
        log: Vec<String>,
        logs_dropped: u64,
    },
    Ack {
        seq: u64,
    },
    ProtocolError {
        code: String,
        message: String,
    },
    /// Core asks for the host ledger lines after `after_seq`.
    LedgerPull {
        request_id: String,
        after_seq: u64,
        limit: u32,
        grant: ExecutorGrant,
    },
    /// One page of raw ledger lines, exactly as stored on the host.
    LedgerPage {
        request_id: String,
        ledger_id: String,
        after_seq: u64,
        lines: Vec<String>,
        head: LedgerHead,
        more: bool,
    },
    /// The agent could not produce a page.
    LedgerUnavailable {
        request_id: String,
        code: String,
        message: String,
    },
}

/// Why a message could not be decoded. `UnknownType` lets the receiver answer
/// `UNKNOWN_MESSAGE` instead of `MALFORMED`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum DecodeError {
    /// Not JSON, not an object, or no string `type` field.
    Malformed(String),
    /// `type` is not a message of this phase.
    UnknownType(String),
    /// Known `type`, but fields are missing, unknown or of the wrong shape.
    InvalidFields {
        message_type: String,
        detail: String,
    },
}

impl std::fmt::Display for DecodeError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            DecodeError::Malformed(d) => write!(f, "malformed message: {d}"),
            DecodeError::UnknownType(t) => write!(f, "unknown message type {t:?}"),
            DecodeError::InvalidFields {
                message_type,
                detail,
            } => write!(f, "invalid {message_type} message: {detail}"),
        }
    }
}

impl std::error::Error for DecodeError {}

impl DecodeError {
    /// The `protocol_error` code a receiver should answer with.
    pub fn protocol_error_code(&self) -> &'static str {
        match self {
            DecodeError::UnknownType(_) => protocol_error_code::UNKNOWN_MESSAGE,
            _ => protocol_error_code::MALFORMED,
        }
    }
}

/// No common protocol version.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Incompatible {
    pub ours: ProtocolRange,
    pub theirs: ProtocolRange,
}

impl std::fmt::Display for Incompatible {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "no common protocol version: ours {}..{}, theirs {}..{}",
            self.ours.min, self.ours.max, self.theirs.min, self.theirs.max
        )
    }
}

impl std::error::Error for Incompatible {}

/// Which side signs a transcript.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Role {
    Core,
    Agent,
}

impl Role {
    pub fn as_str(self) -> &'static str {
        match self {
            Role::Core => "core",
            Role::Agent => "agent",
        }
    }
}

/// The range this build speaks.
pub fn supported_range() -> ProtocolRange {
    ProtocolRange {
        min: PROTOCOL_MIN,
        max: PROTOCOL_MAX,
    }
}

const HANDSHAKE_TYPES: &[&str] = &["hello", "welcome", "reject", "auth", "ready"];
const SESSION_TYPES: &[&str] = &[
    "heartbeat",
    "step_execute",
    "step_rejected",
    "step_reconcile",
    "reconcile_report",
    "event",
    "step_progress",
    "ack",
    "protocol_error",
    "ledger_pull",
    "ledger_page",
    "ledger_unavailable",
];

/// The string `type` of a JSON object, or `Malformed`.
fn message_type(json: &str) -> Result<String, DecodeError> {
    let v: Value = serde_json::from_str(json).map_err(|e| DecodeError::Malformed(e.to_string()))?;
    match v.get("type") {
        Some(Value::String(t)) if v.is_object() => Ok(t.clone()),
        _ if !v.is_object() => Err(DecodeError::Malformed("not a JSON object".into())),
        _ => Err(DecodeError::Malformed("no string \"type\" field".into())),
    }
}

/// Decodes a handshake message, ignoring unknown fields.
///
/// Expected results:
/// - every `messages/{hello,welcome,reject,reject_no_range,auth,ready}.json`
///   fixture decodes, and re-encoding gives the same JSON value;
/// - `handshake-lenient/*.json` decodes (unknown fields ignored);
/// - a post-handshake type such as `step_execute` is `UnknownType`;
/// - non-JSON, non-object, missing or non-string `type` is `Malformed`;
/// - a known type with a missing required field is `InvalidFields`.
pub fn decode_handshake(json: &str) -> Result<Handshake, DecodeError> {
    let message_type = message_type(json)?;
    if !HANDSHAKE_TYPES.contains(&message_type.as_str()) {
        return Err(DecodeError::UnknownType(message_type));
    }
    serde_json::from_str(json).map_err(|e| DecodeError::InvalidFields {
        message_type,
        detail: e.to_string(),
    })
}

/// Decodes a post-handshake message strictly.
///
/// Expected results:
/// - every non-handshake fixture in `messages/` decodes, and re-encoding
///   gives the same JSON value;
/// - `invalid/unknown_type.json` and `invalid/handshake_in_session.json`
///   are `UnknownType`;
/// - `invalid/missing_type.json` is `Malformed`;
/// - every other `invalid/*.json` is `InvalidFields` (unknown field,
///   missing field, params not an object, unknown event kind, negative seq).
pub fn decode(json: &str) -> Result<Message, DecodeError> {
    let message_type = message_type(json)?;
    if !SESSION_TYPES.contains(&message_type.as_str()) {
        return Err(DecodeError::UnknownType(message_type));
    }
    serde_json::from_str(json).map_err(|e| DecodeError::InvalidFields {
        message_type,
        detail: e.to_string(),
    })
}

/// Encodes any message as compact JSON.
pub fn encode<T: Serialize>(msg: &T) -> String {
    serde_json::to_string(msg).expect("protocol messages always serialize")
}

/// Picks the highest version both ranges contain.
///
/// Expected results:
/// - {1,1} with {1,1} → Ok(1); {1,3} with {2,5} → Ok(3); {1,2} with {2,2} → Ok(2);
/// - {2,3} with {1,1} → Err(Incompatible{ours, theirs}) with both ranges as given;
/// - an empty or invalid range (min 0, or min > max) on either side → Err.
pub fn negotiate(ours: ProtocolRange, theirs: ProtocolRange) -> Result<u32, Incompatible> {
    let valid = |r: ProtocolRange| r.min >= 1 && r.min <= r.max;
    let lo = ours.min.max(theirs.min);
    let hi = ours.max.min(theirs.max);
    if valid(ours) && valid(theirs) && lo <= hi {
        Ok(hi)
    } else {
        Err(Incompatible { ours, theirs })
    }
}

/// Everything a handshake signature covers.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TranscriptInput<'a> {
    pub agent_nonce: &'a str,
    pub core_nonce: &'a str,
    pub agent_kex: &'a str,
    pub core_kex: &'a str,
    pub protocol_version: u32,
    pub node_fingerprint: &'a str,
    pub session_id: &'a str,
    /// [`hello_digest`] of the hello exactly as sent and received.
    pub hello_digest: &'a str,
    /// [`welcome_digest`] of the welcome's other fields.
    pub welcome_digest: &'a str,
}

/// The exact text each side signs (docs/contracts/README.md). It covers
/// both nonces, both key-exchange keys, the whole hello and the welcome's
/// parameters, so a proxy that changes any of them breaks the signatures.
pub fn transcript(role: Role, t: &TranscriptInput<'_>) -> String {
    format!(
        "{TRANSCRIPT_DOMAIN}\n{}\n{}\n{}\n{}\n{}\n{}\n{}\n{}\n{}\n{}\n",
        role.as_str(),
        t.agent_nonce,
        t.core_nonce,
        t.agent_kex,
        t.core_kex,
        t.protocol_version,
        t.node_fingerprint,
        t.session_id,
        t.hello_digest,
        t.welcome_digest
    )
}

/// Lowercase hex SHA-256 of the hello text.
pub fn hello_digest(raw: &str) -> String {
    use sha2::{Digest, Sha256};
    hex::encode(Sha256::digest(raw.as_bytes()))
}

/// Lowercase hex SHA-256 of the welcome fields the transcript does not list
/// one by one, length-prefixed so it does not depend on JSON formatting:
/// lp("hazelminx-welcome-v1"), lp(coreVersion), be32(heartbeatSeconds),
/// be32(len(caps)), then lp(name) and be32(version) per capability, where
/// lp(x) = be32(len(x)) || x.
pub fn welcome_digest(core_version: &str, heartbeat_seconds: u32, caps: &[Capability]) -> String {
    use sha2::{Digest, Sha256};
    let mut h = Sha256::new();
    let lp = |h: &mut Sha256, x: &str| {
        h.update((x.len() as u32).to_be_bytes());
        h.update(x.as_bytes());
    };
    lp(&mut h, "hazelminx-welcome-v1");
    lp(&mut h, core_version);
    h.update(heartbeat_seconds.to_be_bytes());
    h.update((caps.len() as u32).to_be_bytes());
    for c in caps {
        lp(&mut h, &c.name);
        h.update(c.version.to_be_bytes());
    }
    hex::encode(h.finalize())
}

/// Node fingerprint: lowercase hex SHA-256 of the raw 32-byte Ed25519 key.
///
/// Expected result: `nodePublicKeyHex` of `fixtures/signature.json` gives
/// `nodeFingerprint`.
pub fn fingerprint(public_key: &[u8; 32]) -> String {
    use sha2::{Digest, Sha256};
    hex::encode(Sha256::digest(public_key))
}

/// A fresh nonce: 32 random bytes, base64url without padding.
pub fn new_nonce() -> String {
    use base64::Engine;
    let bytes: [u8; NONCE_BYTES] = rand::random();
    base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(bytes)
}

/// True when `s` is base64url without padding and decodes to exactly 32 bytes.
///
/// Expected results: fixture nonces and `new_nonce()` → true; padded,
/// standard-alphabet (`+`/`/`), 31 or 33 bytes, empty → false.
pub fn valid_nonce(s: &str) -> bool {
    use base64::Engine;
    matches!(
        base64::engine::general_purpose::URL_SAFE_NO_PAD.decode(s),
        Ok(b) if b.len() == NONCE_BYTES
    )
}

/// True when `caps` has `name` at `version` or higher.
///
/// Expected results: [jobs v1] supports (jobs,1) → true, (jobs,2) → false,
/// (agent.info,1) → false; empty list → false.
pub fn supports(caps: &[Capability], name: &str, version: u32) -> bool {
    caps.iter().any(|c| c.name == name && c.version >= version)
}

/// The key exchange failed: malformed peer key or no shared secret.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct KexError(pub String);

impl std::fmt::Display for KexError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "key exchange failed: {}", self.0)
    }
}

impl std::error::Error for KexError {}

/// A session frame failed authentication or ordering; close the session.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FrameError(pub String);

impl std::fmt::Display for FrameError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "session frame rejected: {}", self.0)
    }
}

impl std::error::Error for FrameError {}

/// An ephemeral X25519 key and its public key encoded for hello/welcome.
pub fn new_kex() -> (x25519_dalek::StaticSecret, String) {
    let secret = x25519_dalek::StaticSecret::random_from_rng(rand::rngs::OsRng);
    let public = kex_public(&secret);
    (secret, public)
}

/// The encoded public key of a kex secret.
pub fn kex_public(secret: &x25519_dalek::StaticSecret) -> String {
    use base64::Engine;
    base64::engine::general_purpose::URL_SAFE_NO_PAD
        .encode(x25519_dalek::PublicKey::from(secret).as_bytes())
}

/// Per-direction MAC keys of one session.
#[derive(Clone, PartialEq, Eq)]
pub struct SessionKeys {
    pub send: [u8; 32],
    pub recv: [u8; 32],
}

impl std::fmt::Debug for SessionKeys {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("SessionKeys(..)")
    }
}

/// HKDF info for messages sent by `from`.
pub fn key_info(from: Role, session_id: &str) -> String {
    let dir = match from {
        Role::Core => "core->agent",
        Role::Agent => "agent->core",
    };
    format!("{TRANSCRIPT_DOMAIN} {dir} {session_id}")
}

/// X25519 with the peer's kex, then HKDF-SHA256 into one key per
/// direction bound to the session id; `role` is the caller's side.
pub fn derive_keys(
    role: Role,
    secret: &x25519_dalek::StaticSecret,
    peer_kex: &str,
    session_id: &str,
) -> Result<SessionKeys, KexError> {
    use base64::Engine;
    let raw = base64::engine::general_purpose::URL_SAFE_NO_PAD
        .decode(peer_kex)
        .map_err(|_| KexError("peer key is not base64url".into()))?;
    let raw: [u8; 32] = raw
        .try_into()
        .map_err(|_| KexError("peer key is not 32 bytes".into()))?;
    let shared = secret.diffie_hellman(&x25519_dalek::PublicKey::from(raw));
    if !shared.was_contributory() {
        return Err(KexError("peer key is a low-order point".into()));
    }
    let hk = hkdf::Hkdf::<sha2::Sha256>::new(None, shared.as_bytes());
    let mut c2a = [0u8; 32];
    let mut a2c = [0u8; 32];
    hk.expand(key_info(Role::Core, session_id).as_bytes(), &mut c2a)
        .expect("32 bytes is a valid HKDF length");
    hk.expand(key_info(Role::Agent, session_id).as_bytes(), &mut a2c)
        .expect("32 bytes is a valid HKDF length");
    Ok(match role {
        Role::Core => SessionKeys {
            send: c2a,
            recv: a2c,
        },
        Role::Agent => SessionKeys {
            send: a2c,
            recv: c2a,
        },
    })
}

#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Frame {
    n: u64,
    body: String,
    mac: String,
}

fn frame_mac(key: &[u8; 32], n: u64, body: &str) -> hmac::Hmac<sha2::Sha256> {
    use hmac::Mac;
    let mut m = <hmac::Hmac<sha2::Sha256> as Mac>::new_from_slice(key).expect("any key length");
    m.update(b"hazelminx-frame-v1\n");
    m.update(&n.to_be_bytes());
    m.update(body.as_bytes());
    m
}

/// Wraps a message body as frame number `n` (docs/contracts/README.md).
pub fn seal_frame(key: &[u8; 32], n: u64, body: &str) -> String {
    use base64::Engine;
    use hmac::Mac;
    let mac = frame_mac(key, n, body).finalize().into_bytes();
    serde_json::to_string(&Frame {
        n,
        body: body.to_string(),
        mac: base64::engine::general_purpose::STANDARD.encode(mac),
    })
    .expect("frames always serialize")
}

/// Checks a frame (exactly n, body, mac; n == want; MAC matches in constant
/// time) and returns its body.
pub fn open_frame(key: &[u8; 32], want: u64, frame: &str) -> Result<String, FrameError> {
    use base64::Engine;
    use hmac::Mac;
    let f: Frame =
        serde_json::from_str(frame).map_err(|e| FrameError(format!("not a frame: {e}")))?;
    if f.n != want {
        return Err(FrameError(format!("frame {}, expected {want}", f.n)));
    }
    let mac = base64::engine::general_purpose::STANDARD
        .decode(&f.mac)
        .map_err(|_| FrameError("mac is not base64".into()))?;
    frame_mac(key, f.n, &f.body)
        .verify_slice(&mac)
        .map_err(|_| FrameError(format!("MAC mismatch on frame {}", f.n)))?;
    Ok(f.body)
}
