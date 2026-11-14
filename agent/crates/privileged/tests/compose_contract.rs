//! The Compose manifest digest, the action bindings and a bound grant match
//! what Core computes (engineering/contracts/fixtures/compose-manifest.json,
//! written by core/internal/composedeploy/fixturegen).

use ed25519_dalek::SigningKey;
use hazelminx_privileged::action::Action;
use hazelminx_privileged::compose_manifest::ComposeManifest;
use hazelminx_privileged::grant::Grant;
use serde_json::Value;
use std::path::Path;

fn fixture() -> Value {
    let p = Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../../../engineering/contracts/fixtures/compose-manifest.json");
    serde_json::from_str(&std::fs::read_to_string(p).unwrap()).unwrap()
}

#[test]
fn manifest_digest_and_bindings_match_core() {
    let f = fixture();
    let m: ComposeManifest = serde_json::from_value(f["manifest"].clone()).unwrap();
    assert_eq!(m.digest_text(), f["digestText"].as_str().unwrap());
    assert_eq!(m.digest(), f["digest"].as_str().unwrap());
    let check = Action::ComposeCheck {
        manifest: m.clone(),
    };
    let deploy = Action::ComposeDeploy { manifest: m };
    assert_eq!(check.binding().unwrap(), f["bindingCheck"].as_str().unwrap());
    assert_eq!(
        deploy.binding().unwrap(),
        f["bindingDeploy"].as_str().unwrap()
    );
    assert_ne!(check.binding(), deploy.binding());
}

#[test]
fn bound_grant_signed_by_core_verifies() {
    let f = fixture();
    let g: Grant = serde_json::from_value(f["grant"].clone()).unwrap();
    assert_eq!(
        String::from_utf8(g.canonical_bytes()).unwrap(),
        f["grantCanonical"].as_str().unwrap()
    );
    let seed: [u8; 32] = [7; 32];
    let key = SigningKey::from_bytes(&seed);
    let now = time::OffsetDateTime::parse(
        "2026-11-14T09:31:00Z",
        &time::format_description::well_known::Rfc3339,
    )
    .unwrap();
    g.verify(&key.verifying_key(), now).unwrap();
    // Changing the binding breaks the signature.
    let mut other = g.clone();
    other.binding = Some("0".repeat(64));
    assert!(other.verify(&key.verifying_key(), now).is_err());
    // So does dropping it.
    let mut none = g;
    none.binding = None;
    assert!(none.verify(&key.verifying_key(), now).is_err());
}
