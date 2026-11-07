//! 契約整合測試，對照 docs/contracts/README.md 與 docs/contracts/fixtures。
//!
//! 樣本一律從檔案讀取，不複製進測試。比對 JSON 時比較解析後的值。
//! 每個測試都會呼叫 hazelminx_protocol 的被測函式，函式仍是 todo!() 時測試必然失敗。

use std::fs;
use std::path::{Path, PathBuf};

use base64::Engine as _;
use base64::engine::general_purpose::{STANDARD, STANDARD_NO_PAD, URL_SAFE, URL_SAFE_NO_PAD};
use ed25519_dalek::{Signer, SigningKey};
use hazelminx_protocol::{
    Capability, DecodeError, Handshake, Incompatible, ProtocolRange, Role, decode,
    decode_handshake, encode, fingerprint, negotiate, new_nonce, supports, transcript, valid_nonce,
};
use serde_json::Value;

/// messages/ 裡屬於握手階段的檔名（不含副檔名）。其餘檔案都是握手後的訊息。
const HANDSHAKE_STEMS: [&str; 6] = [
    "hello",
    "welcome",
    "reject",
    "reject_no_range",
    "auth",
    "ready",
];

// ---------- 共用工具 ----------

fn fixtures_dir() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../docs/contracts/fixtures")
}

fn read_fixture(rel: &str) -> String {
    let path = fixtures_dir().join(rel);
    fs::read_to_string(&path).unwrap_or_else(|e| panic!("cannot read {}: {e}", path.display()))
}

fn fixture_value(rel: &str) -> Value {
    serde_json::from_str(&read_fixture(rel)).unwrap_or_else(|e| panic!("{rel} is not JSON: {e}"))
}

/// 列舉目錄內所有 .json 檔名並排序。列舉而非寫死清單，新增樣本會自動納入測試。
fn json_names(rel_dir: &str) -> Vec<String> {
    let dir = fixtures_dir().join(rel_dir);
    let entries =
        fs::read_dir(&dir).unwrap_or_else(|e| panic!("cannot list {}: {e}", dir.display()));
    let mut names: Vec<String> = entries
        .map(|entry| {
            entry
                .expect("readable directory entry")
                .file_name()
                .into_string()
                .expect("UTF-8 file name")
        })
        .filter(|name| name.ends_with(".json"))
        .collect();
    names.sort();
    assert!(!names.is_empty(), "{rel_dir}/ has no .json fixtures");
    names
}

fn stem(name: &str) -> &str {
    name.strip_suffix(".json").unwrap_or(name)
}

fn is_handshake(name: &str) -> bool {
    HANDSHAKE_STEMS.contains(&stem(name))
}

/// messages/ 裡的握手檔名，並確認六個必備檔都存在，避免檔案遺失時被默默略過。
fn handshake_names() -> Vec<String> {
    let names: Vec<String> = json_names("messages")
        .into_iter()
        .filter(|name| is_handshake(name))
        .collect();
    for expected in HANDSHAKE_STEMS {
        assert!(
            names.iter().any(|name| stem(name) == expected),
            "messages/{expected}.json is missing"
        );
    }
    names
}

fn decode_err(json: &str) -> DecodeError {
    match decode(json) {
        Ok(msg) => panic!("expected decode to fail, got {msg:?}"),
        Err(e) => e,
    }
}

fn decode_handshake_err(json: &str) -> DecodeError {
    match decode_handshake(json) {
        Ok(msg) => panic!("expected decode_handshake to fail, got {msg:?}"),
        Err(e) => e,
    }
}

fn cap(name: &str, version: u32) -> Capability {
    Capability {
        name: name.to_owned(),
        version,
    }
}

fn signature() -> Value {
    fixture_value("signature.json")
}

fn sig_str<'a>(sig: &'a Value, key: &str) -> &'a str {
    sig[key]
        .as_str()
        .unwrap_or_else(|| panic!("signature.json: {key} is not a string"))
}

fn sig_protocol_version(sig: &Value) -> u32 {
    let raw = sig["protocolVersion"]
        .as_u64()
        .expect("signature.json: protocolVersion is an integer");
    u32::try_from(raw).expect("protocolVersion fits in u32")
}

fn sig_bytes32(sig: &Value, key: &str) -> [u8; 32] {
    let raw = hex::decode(sig_str(sig, key))
        .unwrap_or_else(|e| panic!("signature.json: {key} is not hex: {e}"));
    let len = raw.len();
    raw.try_into()
        .unwrap_or_else(|_| panic!("signature.json: {key} is {len} bytes, want 32"))
}

/// 以 signature.json 的輸入呼叫 transcript，角色由參數決定。
fn sig_transcript(role: Role, sig: &Value) -> String {
    transcript(
        role,
        sig_str(sig, "agentNonce"),
        sig_str(sig, "coreNonce"),
        sig_protocol_version(sig),
        sig_str(sig, "nodeFingerprint"),
        sig_str(sig, "sessionId"),
    )
}

/// Ed25519 簽章，結果為標準 base64，與 proof 欄位格式相同。
fn sign_standard_b64(seed: &[u8; 32], text: &str) -> String {
    let key = SigningKey::from_bytes(seed);
    STANDARD.encode(key.sign(text.as_bytes()).to_bytes())
}

// ---------- messages/ 解碼與 round trip ----------

#[test]
fn messages_handshake_fixtures_decode_and_round_trip() {
    for name in handshake_names() {
        let text = read_fixture(&format!("messages/{name}"));
        let original: Value = serde_json::from_str(&text).expect("fixture is JSON");
        let msg = decode_handshake(&text)
            .unwrap_or_else(|e| panic!("messages/{name}: decode_handshake failed: {e}"));
        let back: Value = serde_json::from_str(&encode(&msg)).expect("encoded message is JSON");
        assert_eq!(back, original, "messages/{name}: re-encoded value differs");
    }
}

#[test]
fn messages_post_handshake_fixtures_decode_and_round_trip() {
    let names: Vec<String> = json_names("messages")
        .into_iter()
        .filter(|name| !is_handshake(name))
        .collect();
    assert!(
        !names.is_empty(),
        "messages/ has no post-handshake fixtures"
    );
    for name in names {
        let text = read_fixture(&format!("messages/{name}"));
        let original: Value = serde_json::from_str(&text).expect("fixture is JSON");
        let msg = decode(&text).unwrap_or_else(|e| panic!("messages/{name}: decode failed: {e}"));
        let back: Value = serde_json::from_str(&encode(&msg)).expect("encoded message is JSON");
        assert_eq!(back, original, "messages/{name}: re-encoded value differs");
    }
}

#[test]
fn decode_handshake_step_execute_fixture_is_unknown_type() {
    let text = read_fixture("messages/step_execute.json");
    let err = decode_handshake_err(&text);
    assert!(
        matches!(err, DecodeError::UnknownType(_)),
        "step_execute is a post-handshake type, got {err:?}"
    );
}

#[test]
fn decode_handshake_rejects_every_post_handshake_fixture_as_unknown_type() {
    let names: Vec<String> = json_names("messages")
        .into_iter()
        .filter(|name| !is_handshake(name))
        .collect();
    for name in names {
        let text = read_fixture(&format!("messages/{name}"));
        let err = decode_handshake_err(&text);
        assert!(
            matches!(err, DecodeError::UnknownType(_)),
            "messages/{name}: decode_handshake should say UnknownType, got {err:?}"
        );
    }
}

// ---------- handshake-lenient/ ----------

#[test]
fn decode_handshake_lenient_fixtures_all_decode() {
    for name in json_names("handshake-lenient") {
        let text = read_fixture(&format!("handshake-lenient/{name}"));
        if let Err(e) = decode_handshake(&text) {
            panic!("handshake-lenient/{name}: should decode, got {e}");
        }
    }
}

#[test]
fn decode_handshake_future_hello_keeps_its_newer_protocol_range() {
    let text = read_fixture("handshake-lenient/hello_future_agent.json");
    let msg = decode_handshake(&text).unwrap_or_else(|e| panic!("future hello: {e}"));
    match msg {
        Handshake::Hello { protocol, .. } => {
            assert_eq!(protocol, ProtocolRange { min: 2, max: 3 });
        }
        other => panic!("expected a hello, got {other:?}"),
    }
}

// ---------- invalid/ ----------

#[test]
fn decode_invalid_fixtures_fail_with_expected_error_kind_and_code() {
    for name in json_names("invalid") {
        let text = read_fixture(&format!("invalid/{name}"));
        let err = decode_err(&text);
        let (kind_ok, want_code) = match stem(&name) {
            "unknown_type" | "handshake_in_session" => (
                matches!(err, DecodeError::UnknownType(_)),
                "UNKNOWN_MESSAGE",
            ),
            "missing_type" => (matches!(err, DecodeError::Malformed(_)), "MALFORMED"),
            _ => (
                matches!(err, DecodeError::InvalidFields { .. }),
                "MALFORMED",
            ),
        };
        assert!(kind_ok, "invalid/{name}: wrong error kind, got {err:?}");
        assert_eq!(
            err.protocol_error_code(),
            want_code,
            "invalid/{name}: wrong protocol_error code"
        );
    }
}

#[test]
fn decode_handshake_hello_without_nonce_is_invalid_fields() {
    let mut hello = fixture_value("messages/hello.json");
    let removed = hello
        .as_object_mut()
        .expect("hello fixture is an object")
        .remove("nonce");
    assert!(removed.is_some(), "hello fixture has no nonce to remove");
    let err = decode_handshake_err(&hello.to_string());
    assert!(
        matches!(err, DecodeError::InvalidFields { .. }),
        "hello without nonce should be InvalidFields, got {err:?}"
    );
}

#[test]
fn decode_malformed_input_is_malformed() {
    for input in ["not json", "[]", r#"{"type":5}"#, r#"{"seq":1}"#] {
        let err = decode_err(input);
        assert!(
            matches!(err, DecodeError::Malformed(_)),
            "decode({input:?}) should be Malformed, got {err:?}"
        );
    }
}

#[test]
fn decode_handshake_malformed_input_is_malformed() {
    for input in ["not json", "[]", r#"{"type":5}"#, r#"{"nonce":"x"}"#] {
        let err = decode_handshake_err(input);
        assert!(
            matches!(err, DecodeError::Malformed(_)),
            "decode_handshake({input:?}) should be Malformed, got {err:?}"
        );
    }
}

#[test]
fn protocol_error_code_is_unknown_message_only_for_unknown_type() {
    let unknown = decode_err(&read_fixture("invalid/unknown_type.json"));
    assert_eq!(unknown.protocol_error_code(), "UNKNOWN_MESSAGE");

    let malformed = decode_err(&read_fixture("invalid/missing_type.json"));
    assert_eq!(malformed.protocol_error_code(), "MALFORMED");

    let invalid = decode_err(&read_fixture("invalid/ack_negative_seq.json"));
    assert_eq!(invalid.protocol_error_code(), "MALFORMED");
}

// ---------- negotiate ----------

#[test]
fn negotiate_picks_highest_common_version() {
    let cases = [
        (
            ProtocolRange { min: 1, max: 1 },
            ProtocolRange { min: 1, max: 1 },
            1,
        ),
        (
            ProtocolRange { min: 1, max: 3 },
            ProtocolRange { min: 2, max: 5 },
            3,
        ),
        (
            ProtocolRange { min: 1, max: 2 },
            ProtocolRange { min: 2, max: 2 },
            2,
        ),
        (
            ProtocolRange { min: 2, max: 5 },
            ProtocolRange { min: 1, max: 3 },
            3,
        ),
        (
            ProtocolRange { min: 1, max: 3 },
            ProtocolRange { min: 1, max: 1 },
            1,
        ),
    ];
    for (ours, theirs, want) in cases {
        assert_eq!(
            negotiate(ours, theirs),
            Ok(want),
            "negotiate({ours:?}, {theirs:?})"
        );
    }
}

#[test]
fn negotiate_disjoint_ranges_returns_incompatible_with_both_ranges() {
    let ours = ProtocolRange { min: 2, max: 3 };
    let theirs = ProtocolRange { min: 1, max: 1 };
    assert_eq!(
        negotiate(ours, theirs),
        Err(Incompatible { ours, theirs }),
        "ours 2..3 vs theirs 1..1"
    );
    assert_eq!(
        negotiate(theirs, ours),
        Err(Incompatible {
            ours: theirs,
            theirs: ours
        }),
        "ours 1..1 vs theirs 2..3"
    );
}

#[test]
fn negotiate_invalid_range_on_either_side_is_error() {
    let cases = [
        (
            ProtocolRange { min: 0, max: 1 },
            ProtocolRange { min: 1, max: 1 },
        ),
        (
            ProtocolRange { min: 1, max: 1 },
            ProtocolRange { min: 0, max: 1 },
        ),
        (
            ProtocolRange { min: 0, max: 0 },
            ProtocolRange { min: 0, max: 0 },
        ),
        (
            ProtocolRange { min: 2, max: 1 },
            ProtocolRange { min: 1, max: 3 },
        ),
        (
            ProtocolRange { min: 1, max: 3 },
            ProtocolRange { min: 3, max: 2 },
        ),
    ];
    for (ours, theirs) in cases {
        assert!(
            negotiate(ours, theirs).is_err(),
            "negotiate({ours:?}, {theirs:?}) should fail on an invalid range"
        );
    }
}

// ---------- transcript / fingerprint / 簽章 ----------

#[test]
fn transcript_core_role_matches_fixture_bytes() {
    let sig = signature();
    assert_eq!(
        sig_transcript(Role::Core, &sig),
        sig_str(&sig, "coreTranscript"),
        "core transcript"
    );
}

#[test]
fn transcript_agent_role_matches_fixture_bytes() {
    let sig = signature();
    assert_eq!(
        sig_transcript(Role::Agent, &sig),
        sig_str(&sig, "agentTranscript"),
        "agent transcript"
    );
}

#[test]
fn fingerprint_of_node_public_key_matches_fixture() {
    let sig = signature();
    let public_key = sig_bytes32(&sig, "nodePublicKeyHex");
    assert_eq!(
        fingerprint(&public_key),
        sig_str(&sig, "nodeFingerprint"),
        "fingerprint of nodePublicKeyHex"
    );
}

#[test]
fn agent_proof_is_node_key_signature_over_agent_transcript() {
    let sig = signature();
    let seed = sig_bytes32(&sig, "nodeSeedHex");
    let key = SigningKey::from_bytes(&seed);
    assert_eq!(
        hex::encode(key.verifying_key().to_bytes()),
        sig_str(&sig, "nodePublicKeyHex"),
        "nodeSeedHex must derive nodePublicKeyHex"
    );
    let text = sig_transcript(Role::Agent, &sig);
    assert_eq!(
        sign_standard_b64(&seed, &text),
        sig_str(&sig, "agentProof"),
        "agentProof"
    );
}

#[test]
fn core_proof_is_core_key_signature_over_core_transcript() {
    let sig = signature();
    let seed = sig_bytes32(&sig, "coreSeedHex");
    let text = sig_transcript(Role::Core, &sig);
    assert_eq!(
        sign_standard_b64(&seed, &text),
        sig_str(&sig, "coreProof"),
        "coreProof"
    );
}

#[test]
fn hello_fixture_nonce_is_agent_nonce_of_signature_fixture() {
    let sig = signature();
    let text = read_fixture("messages/hello.json");
    let msg = decode_handshake(&text).unwrap_or_else(|e| panic!("hello.json: {e}"));
    match msg {
        Handshake::Hello { nonce, .. } => assert_eq!(nonce, sig_str(&sig, "agentNonce")),
        other => panic!("expected a hello, got {other:?}"),
    }
}

#[test]
fn welcome_fixture_proof_is_core_proof_of_signature_fixture() {
    let sig = signature();
    let text = read_fixture("messages/welcome.json");
    let msg = decode_handshake(&text).unwrap_or_else(|e| panic!("welcome.json: {e}"));
    match msg {
        Handshake::Welcome { proof, .. } => assert_eq!(proof, sig_str(&sig, "coreProof")),
        other => panic!("expected a welcome, got {other:?}"),
    }
}

#[test]
fn auth_fixture_proof_is_agent_proof_of_signature_fixture() {
    let sig = signature();
    let text = read_fixture("messages/auth.json");
    let msg = decode_handshake(&text).unwrap_or_else(|e| panic!("auth.json: {e}"));
    match msg {
        Handshake::Auth { proof } => assert_eq!(proof, sig_str(&sig, "agentProof")),
        other => panic!("expected an auth, got {other:?}"),
    }
}

// ---------- nonce ----------

#[test]
fn new_nonce_is_fresh_on_each_call_and_valid() {
    let first = new_nonce();
    let second = new_nonce();
    assert_ne!(first, second, "two nonces must differ");
    assert!(valid_nonce(&first), "first nonce {first:?} is not valid");
    assert!(valid_nonce(&second), "second nonce {second:?} is not valid");
}

#[test]
fn valid_nonce_accepts_fixture_nonces() {
    let sig = signature();
    assert!(valid_nonce(sig_str(&sig, "agentNonce")));
    assert!(valid_nonce(sig_str(&sig, "coreNonce")));
}

#[test]
fn valid_nonce_rejects_padded_base64url() {
    // 32 bytes 的 base64url 帶 padding 會以 '=' 結尾。
    let padded = URL_SAFE.encode([7u8; 32]);
    assert!(padded.ends_with('='), "sanity: test input must be padded");
    assert!(!valid_nonce(&padded), "padded nonce {padded:?} accepted");
}

#[test]
fn valid_nonce_rejects_standard_alphabet() {
    // 0xFB 的位元組會產生 '+' 與 '/'，不是 base64url 字母表。
    let no_pad = STANDARD_NO_PAD.encode([0xfbu8; 32]);
    assert!(
        no_pad.contains('+') && no_pad.contains('/'),
        "sanity: input must use the standard alphabet"
    );
    assert!(
        !valid_nonce(&no_pad),
        "standard alphabet {no_pad:?} accepted"
    );

    let padded = STANDARD.encode([0xfbu8; 32]);
    assert!(!valid_nonce(&padded), "standard padded {padded:?} accepted");
}

#[test]
fn valid_nonce_rejects_wrong_length_and_empty() {
    let short = URL_SAFE_NO_PAD.encode([1u8; 31]);
    let long = URL_SAFE_NO_PAD.encode([1u8; 33]);
    assert!(!valid_nonce(&short), "31-byte nonce {short:?} accepted");
    assert!(!valid_nonce(&long), "33-byte nonce {long:?} accepted");
    assert!(!valid_nonce(""), "empty string accepted");
}

// ---------- supports ----------

#[test]
fn supports_requires_requested_version_or_higher() {
    let caps = [cap("jobs", 1)];
    assert!(supports(&caps, "jobs", 1));
    assert!(!supports(&caps, "jobs", 2));
    assert!(!supports(&caps, "agent.info", 1));
}

#[test]
fn supports_newer_capability_satisfies_older_request() {
    let caps = [cap("jobs", 3)];
    assert!(supports(&caps, "jobs", 1));
    assert!(supports(&caps, "jobs", 3));
    assert!(!supports(&caps, "jobs", 4));
}

#[test]
fn supports_matches_by_name_among_several_capabilities() {
    let caps = [cap("jobs", 1), cap("agent.info", 1)];
    assert!(supports(&caps, "agent.info", 1));
    assert!(supports(&caps, "jobs", 1));
}

#[test]
fn supports_empty_capability_list_is_false() {
    assert!(!supports(&[], "jobs", 1));
}
