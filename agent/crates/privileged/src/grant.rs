//! Signed authorization grants. The Core (later: via the agent's enrollment,
//! ticket 019) issues a grant per job with the permissions the acting user
//! holds; the executor only trusts grants signed by the configured issuer
//! key. A grant is a capability: it is valid between `not_before` and
//! `expires_at` unless its id is revoked. Lifetime is capped so a leaked
//! grant cannot outlive [`MAX_GRANT_LIFETIME`].

use ed25519_dalek::{Signature, Signer, SigningKey, Verifier, VerifyingKey};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::Mutex;
use time::{Duration, OffsetDateTime};

pub const CANONICAL_PREFIX: &str = "hazelminx-grant-v1";
pub const MAX_GRANT_LIFETIME: Duration = Duration::hours(24);
/// Allowed clock skew between issuer and executor.
pub const CLOCK_SKEW: Duration = Duration::seconds(60);
pub const REVOCATION_RETENTION: Duration =
    Duration::hours(24).saturating_add(Duration::seconds(120));

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Grant {
    /// Unique id; revocation refers to it.
    pub id: String,
    /// Who the grant was issued for (platform user id), for audit only.
    pub subject: String,
    /// Permission names such as `host.services.manage`.
    pub permissions: Vec<String>,
    #[serde(with = "time::serde::rfc3339")]
    pub not_before: OffsetDateTime,
    #[serde(with = "time::serde::rfc3339")]
    pub expires_at: OffsetDateTime,
    /// Hex Ed25519 signature over [`Grant::canonical_bytes`]. Never logged.
    #[serde(default)]
    pub signature: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum GrantRejection {
    /// id, subject or a permission contains characters outside the token charset.
    MalformedField,
    BadSignature,
    NotYetValid,
    Expired,
    LifetimeTooLong,
    Revoked,
}

impl std::fmt::Display for GrantRejection {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let s = match self {
            GrantRejection::MalformedField => "grant field contains invalid characters",
            GrantRejection::BadSignature => "grant signature does not verify",
            GrantRejection::NotYetValid => "grant is not valid yet",
            GrantRejection::Expired => "grant has expired",
            GrantRejection::LifetimeTooLong => "grant lifetime exceeds the maximum",
            GrantRejection::Revoked => "grant was revoked",
        };
        f.write_str(s)
    }
}

/// Characters allowed in id, subject and permission names.
pub fn is_token(s: &str) -> bool {
    !s.is_empty()
        && s.len() <= 256
        && s.bytes()
            .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'.' | b'_' | b':' | b'@' | b'-'))
}

impl Grant {
    /// Unsigned grant valid from `now` for `lifetime`.
    pub fn new(
        id: impl Into<String>,
        subject: impl Into<String>,
        permissions: Vec<String>,
        now: OffsetDateTime,
        lifetime: Duration,
    ) -> Self {
        Self {
            id: id.into(),
            subject: subject.into(),
            permissions,
            not_before: now,
            expires_at: now + lifetime,
            signature: String::new(),
        }
    }

    /// Line-oriented canonical form; reproducible from Go without JSON
    /// canonicalization. Requires [`is_token`] fields (checked in `verify`).
    pub fn canonical_bytes(&self) -> Vec<u8> {
        let fmt = &time::format_description::well_known::Rfc3339;
        let nb = self
            .not_before
            .to_offset(time::UtcOffset::UTC)
            .format(fmt)
            .expect("rfc3339");
        let exp = self
            .expires_at
            .to_offset(time::UtcOffset::UTC)
            .format(fmt)
            .expect("rfc3339");
        format!(
            "{CANONICAL_PREFIX}\n{}\n{}\n{nb}\n{exp}\n{}\n",
            self.id,
            self.subject,
            self.permissions.join(",")
        )
        .into_bytes()
    }

    pub fn sign(&mut self, key: &SigningKey) {
        let sig: Signature = key.sign(&self.canonical_bytes());
        self.signature = hex::encode(sig.to_bytes());
    }

    fn fields_are_tokens(&self) -> bool {
        is_token(&self.id)
            && is_token(&self.subject)
            && self.permissions.iter().all(|p| is_token(p))
    }

    /// Checks field charset, signature, validity window and lifetime cap.
    /// Revocation is checked separately by [`Revocations`].
    pub fn verify(&self, issuer: &VerifyingKey, now: OffsetDateTime) -> Result<(), GrantRejection> {
        if !self.fields_are_tokens() {
            return Err(GrantRejection::MalformedField);
        }
        let raw = hex::decode(&self.signature).map_err(|_| GrantRejection::BadSignature)?;
        let sig = Signature::from_slice(&raw).map_err(|_| GrantRejection::BadSignature)?;
        issuer
            .verify(&self.canonical_bytes(), &sig)
            .map_err(|_| GrantRejection::BadSignature)?;
        if self.expires_at - self.not_before > MAX_GRANT_LIFETIME {
            return Err(GrantRejection::LifetimeTooLong);
        }
        if now + CLOCK_SKEW < self.not_before {
            return Err(GrantRejection::NotYetValid);
        }
        if now - CLOCK_SKEW >= self.expires_at {
            return Err(GrantRejection::Expired);
        }
        Ok(())
    }

    pub fn has_permission(&self, perm: &str) -> bool {
        self.permissions.iter().any(|p| p == perm)
    }
}

/// In-memory revocation list. An entry is kept for [`REVOCATION_RETENTION`]:
/// a grant revoked at time R may have `not_before` up to R + skew, live for
/// at most [`MAX_GRANT_LIFETIME`], and be accepted until skew after expiry,
/// so nothing revoked can be accepted after that.
#[derive(Default)]
pub struct Revocations {
    inner: Mutex<HashMap<String, OffsetDateTime>>,
}

impl Revocations {
    pub fn revoke(&self, grant_id: &str, now: OffsetDateTime) {
        let mut map = self.inner.lock().unwrap();
        map.retain(|_, until| *until > now);
        map.insert(grant_id.to_string(), now + REVOCATION_RETENTION);
    }
    pub fn is_revoked(&self, grant_id: &str, now: OffsetDateTime) -> bool {
        self.inner
            .lock()
            .unwrap()
            .get(grant_id)
            .is_some_and(|until| *until > now)
    }
    pub fn len(&self) -> usize {
        self.inner.lock().unwrap().len()
    }
    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use time::macros::datetime;

    fn keys() -> (SigningKey, VerifyingKey) {
        let sk = SigningKey::generate(&mut rand::rngs::OsRng);
        let vk = sk.verifying_key();
        (sk, vk)
    }

    fn now() -> OffsetDateTime {
        datetime!(2026-11-07 10:00:00 UTC)
    }

    fn grant(sk: &SigningKey) -> Grant {
        let mut g = Grant::new(
            "g-1",
            "user-42",
            vec!["host.info.read".into()],
            now(),
            Duration::minutes(10),
        );
        g.sign(sk);
        g
    }

    #[test]
    fn signed_grant_verifies_and_tampering_is_detected() {
        let (sk, vk) = keys();
        let g = grant(&sk);
        assert_eq!(g.verify(&vk, now()), Ok(()));
        let mut t = g.clone();
        t.permissions.push("host.services.manage".into());
        assert_eq!(t.verify(&vk, now()), Err(GrantRejection::BadSignature));
        let mut t = g.clone();
        t.expires_at += Duration::hours(1);
        assert_eq!(t.verify(&vk, now()), Err(GrantRejection::BadSignature));
        let (_, other) = keys();
        assert_eq!(g.verify(&other, now()), Err(GrantRejection::BadSignature));
        let mut t = g.clone();
        t.signature = "zz".into();
        assert_eq!(t.verify(&vk, now()), Err(GrantRejection::BadSignature));
    }

    #[test]
    fn validity_window_and_lifetime_cap() {
        let (sk, vk) = keys();
        let g = grant(&sk);
        assert_eq!(
            g.verify(&vk, now() - Duration::minutes(5)),
            Err(GrantRejection::NotYetValid)
        );
        assert_eq!(
            g.verify(&vk, now() + Duration::minutes(11)),
            Err(GrantRejection::Expired)
        );
        // Within clock skew is fine.
        assert_eq!(g.verify(&vk, now() - Duration::seconds(30)), Ok(()));
        let mut long = Grant::new(
            "g-2",
            "u",
            vec![],
            now(),
            MAX_GRANT_LIFETIME + Duration::seconds(1),
        );
        long.sign(&sk);
        assert_eq!(
            long.verify(&vk, now()),
            Err(GrantRejection::LifetimeTooLong)
        );
    }

    #[test]
    fn fields_with_newlines_or_commas_cannot_forge_canonical_form() {
        let (sk, vk) = keys();
        let mut g = Grant::new(
            "g-3",
            "u\nhost.shell",
            vec!["host.info.read".into()],
            now(),
            Duration::minutes(1),
        );
        g.sign(&sk);
        assert_eq!(g.verify(&vk, now()), Err(GrantRejection::MalformedField));
        let mut g = Grant::new(
            "g-3",
            "u",
            vec!["host.info.read,host.shell".into()],
            now(),
            Duration::minutes(1),
        );
        g.sign(&sk);
        assert_eq!(g.verify(&vk, now()), Err(GrantRejection::MalformedField));
        assert!(is_token("user:42@node.example-1_x"));
        assert!(!is_token(""));
    }

    #[test]
    fn canonical_form_is_stable_for_go_interop() {
        let g = Grant::new(
            "g-1",
            "user-42",
            vec!["a.b".into(), "c.d".into()],
            now(),
            Duration::minutes(10),
        );
        let s = String::from_utf8(g.canonical_bytes()).unwrap();
        assert_eq!(
            s,
            "hazelminx-grant-v1\ng-1\nuser-42\n2026-11-07T10:00:00Z\n2026-11-07T10:10:00Z\na.b,c.d\n"
        );
    }

    #[test]
    fn json_uses_rfc3339_and_omits_nothing_needed_by_verify() {
        let (sk, vk) = keys();
        let g = grant(&sk);
        let j = serde_json::to_string(&g).unwrap();
        assert!(j.contains("\"not_before\":\"2026-11-07T10:00:00Z\""), "{j}");
        let back: Grant = serde_json::from_str(&j).unwrap();
        assert_eq!(back.verify(&vk, now()), Ok(()));
    }

    #[test]
    fn revocations_expire_after_max_lifetime() {
        let r = Revocations::default();
        assert!(!r.is_revoked("g-1", now()));
        r.revoke("g-1", now());
        assert!(r.is_revoked("g-1", now()));
        // Still revoked at the last instant a revoked grant could be accepted
        // (not_before skew + max lifetime + expiry skew), gone after that.
        assert!(r.is_revoked(
            "g-1",
            now() + MAX_GRANT_LIFETIME + CLOCK_SKEW * 2 - Duration::seconds(1)
        ));
        assert!(!r.is_revoked("g-1", now() + MAX_GRANT_LIFETIME + CLOCK_SKEW * 2));
        r.revoke("g-2", now());
        assert!(r.is_revoked("g-2", now() + Duration::hours(1)));
        assert!(r.len() <= 2);
    }
}
