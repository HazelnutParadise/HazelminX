//! 註冊碼與 CA 發現 MAC 的契約測試，對照 fixtures/enrollment.json（Go 端讀同一個檔）。

use hazelminx_protocol::enroll::{EnrollmentCode, discovery_mac, verify_discovery_mac};
use serde_json::Value;
use std::path::Path;

fn fixture() -> Value {
    let p = Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../../../engineering/contracts/fixtures/enrollment.json");
    serde_json::from_str(&std::fs::read_to_string(p).unwrap()).unwrap()
}

fn s<'a>(v: &'a Value, k: &str) -> &'a str {
    v[k].as_str()
        .unwrap_or_else(|| panic!("fixture has no string {k}"))
}

#[test]
fn code_key_and_mac_match_fixture() {
    let f = fixture();
    let code = EnrollmentCode::parse(s(&f, "code")).unwrap();
    assert_eq!(code.normalized(), s(&f, "normalized"));
    assert_eq!(code.id(), s(&f, "codeId"));
    assert_eq!(code.to_string(), s(&f, "code"));
    assert_eq!(hex::encode(code.key()), s(&f, "key"));
    let ca = s(&f, "caCertificate").as_bytes();
    assert_eq!(discovery_mac(&code.key(), ca), s(&f, "mac"));
    assert!(verify_discovery_mac(&code.key(), ca, s(&f, "mac")));
    let mut changed = ca.to_vec();
    changed.push(b'\n');
    assert!(!verify_discovery_mac(&code.key(), &changed, s(&f, "mac")));
    let other = EnrollmentCode::parse(s(&f["aliases"], "input")).unwrap();
    assert!(!verify_discovery_mac(&other.key(), ca, s(&f, "mac")));
    assert!(!verify_discovery_mac(&code.key(), ca, "not base64!"));
}

#[test]
fn normalization_and_invalid_codes() {
    let f = fixture();
    for v in f["equivalent"].as_array().unwrap() {
        let c = EnrollmentCode::parse(v.as_str().unwrap()).unwrap();
        assert_eq!(c.normalized(), s(&f, "normalized"), "{v}");
    }
    let a = EnrollmentCode::parse(s(&f["aliases"], "input")).unwrap();
    assert_eq!(a.normalized(), s(&f["aliases"], "normalized"));
    for v in f["invalid"].as_array().unwrap() {
        assert!(
            EnrollmentCode::parse(v.as_str().unwrap()).is_err(),
            "{v} was accepted"
        );
    }
}
