//! Enrollment (engineering/contracts/README.md, 註冊; decision 0031): the
//! one-time code, the key derived from it, the MAC that authenticates the
//! Core CA before the agent can verify TLS, and the HTTP bodies.

use hmac::{Hmac, Mac};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

pub const DISCOVERY_PATH: &str = "/agent/v1/enroll/discovery";
pub const ENROLL_PATH: &str = "/agent/v1/enroll";

const PREFIX: &str = "HZX";
const ALPHABET: &str = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
const CHARS: usize = 16;
const GROUP: usize = 4;
const KEY_DOMAIN: &str = "hazelminx-enroll-v1\n";
const MAC_DOMAIN: &str = "hazelminx-enroll-discovery-v1\n";

/// A parsed enrollment code.
#[derive(Clone, PartialEq, Eq)]
pub struct EnrollmentCode {
    chars: String,
}

/// Debug never prints the secret part.
impl std::fmt::Debug for EnrollmentCode {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "EnrollmentCode({}-…)", self.id())
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InvalidCode(pub String);

impl std::fmt::Display for InvalidCode {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "not an enrollment code: {}", self.0)
    }
}

impl std::error::Error for InvalidCode {}

impl EnrollmentCode {
    /// Drops '-' and spaces, uppercases, maps O→0 and I, L→1, then requires
    /// "HZX" and 16 characters of the Crockford alphabet.
    pub fn parse(text: &str) -> Result<EnrollmentCode, InvalidCode> {
        let mut s = String::with_capacity(text.len());
        for c in text.chars() {
            if c == '-' || c == ' ' || c == '\t' {
                continue;
            }
            let c = c.to_ascii_uppercase();
            s.push(match c {
                'O' => '0',
                'I' | 'L' => '1',
                c => c,
            });
        }
        let Some(body) = s.strip_prefix(PREFIX) else {
            return Err(InvalidCode(format!(
                "want {PREFIX} followed by {CHARS} characters"
            )));
        };
        if body.chars().count() != CHARS {
            return Err(InvalidCode(format!(
                "want {PREFIX} followed by {CHARS} characters"
            )));
        }
        if let Some(c) = body.chars().find(|c| !ALPHABET.contains(*c)) {
            return Err(InvalidCode(format!("{c:?} is not a code character")));
        }
        Ok(EnrollmentCode {
            chars: body.to_string(),
        })
    }

    /// "HZX" and the 16 characters, without separators.
    pub fn normalized(&self) -> String {
        format!("{PREFIX}{}", self.chars)
    }

    /// The public lookup part (first group).
    pub fn id(&self) -> &str {
        &self.chars[..GROUP]
    }

    /// SHA-256("hazelminx-enroll-v1\n" + normalized).
    pub fn key(&self) -> [u8; 32] {
        let mut h = Sha256::new();
        h.update(KEY_DOMAIN.as_bytes());
        h.update(self.normalized().as_bytes());
        h.finalize().into()
    }
}

/// Display form HZX-XXXX-XXXX-XXXX-XXXX (this is the secret; do not log it).
impl std::fmt::Display for EnrollmentCode {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(PREFIX)?;
        for i in (0..CHARS).step_by(GROUP) {
            write!(f, "-{}", &self.chars[i..i + GROUP])?;
        }
        Ok(())
    }
}

/// Standard base64 of HMAC-SHA256(key, domain + ca_pem).
pub fn discovery_mac(key: &[u8; 32], ca_pem: &[u8]) -> String {
    use base64::Engine;
    let mut m = <Hmac<Sha256> as Mac>::new_from_slice(key).expect("any key length");
    m.update(MAC_DOMAIN.as_bytes());
    m.update(ca_pem);
    base64::engine::general_purpose::STANDARD.encode(m.finalize().into_bytes())
}

/// Constant-time check of a discovery MAC.
pub fn verify_discovery_mac(key: &[u8; 32], ca_pem: &[u8], mac: &str) -> bool {
    use base64::Engine;
    let Ok(got) = base64::engine::general_purpose::STANDARD.decode(mac) else {
        return false;
    };
    let mut m = <Hmac<Sha256> as Mac>::new_from_slice(key).expect("any key length");
    m.update(MAC_DOMAIN.as_bytes());
    m.update(ca_pem);
    m.verify_slice(&got).is_ok()
}

/// Answer of GET [`DISCOVERY_PATH`].
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Discovery {
    pub ca_certificate: String,
    pub mac: String,
}

/// Body of POST [`ENROLL_PATH`].
#[derive(Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct EnrollRequest {
    pub code: String,
    pub node_public_key: String,
    pub hostname: String,
    pub machine_id: String,
    pub os: String,
    pub arch: String,
    pub agent_version: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub group: String,
}

/// Every answer of POST [`ENROLL_PATH`]. Unknown fields are ignored so a
/// newer Core can add information.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
pub struct EnrollResponse {
    pub status: String,
    #[serde(default)]
    pub enrollment_id: String,
    #[serde(default)]
    pub retry_after_seconds: u64,
    #[serde(default)]
    pub node_id: String,
    #[serde(default)]
    pub node_certificate: String,
    #[serde(default)]
    pub ca_certificate: String,
    #[serde(default)]
    pub privileged_issuer_key: String,
    #[serde(default)]
    pub code: String,
    #[serde(default)]
    pub message: String,
}

pub const STATUS_PENDING: &str = "pending";
pub const STATUS_APPROVED: &str = "approved";
pub const STATUS_REFUSED: &str = "refused";
