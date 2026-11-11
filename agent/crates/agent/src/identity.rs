//! Node identity: an Ed25519 key pair that is generated once per node and
//! kept in `state_dir/identity/`. The public-key fingerprint is the stable
//! node ID (NOD-005: IP and hostname are not identity). The private key never
//! leaves this host; the Core only ever sees the public key and, after
//! enrollment (ticket 019), the certificate it issues for it.
//!
//! Cloning a VM copies this directory; the Core detects two live sessions
//! with the same fingerprint and refuses the second (T-017). Nothing here
//! tries to be clever about that locally.

use anyhow::{Context, Result};
use ed25519_dalek::pkcs8::{DecodePrivateKey, EncodePrivateKey, EncodePublicKey};
use ed25519_dalek::{SigningKey, VerifyingKey};
use sha2::{Digest, Sha256};
use std::path::{Path, PathBuf};

pub const PRIVATE_KEY_FILE: &str = "node.key";
pub const PUBLIC_KEY_FILE: &str = "node.pub";

pub struct Identity {
    signing: SigningKey,
    dir: PathBuf,
}

/// Debug never prints key material, only the public fingerprint and location.
impl std::fmt::Debug for Identity {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Identity")
            .field("fingerprint", &self.fingerprint())
            .field("dir", &self.dir)
            .finish()
    }
}

impl Identity {
    /// Loads the identity from `state_dir/identity`, creating it on first run.
    /// Directory is 0700 and the private key 0600; an existing key is never
    /// overwritten.
    pub fn load_or_create(state_dir: &Path) -> Result<Identity> {
        let dir = state_dir.join("identity");
        create_private_dir(&dir)?;
        let key_path = dir.join(PRIVATE_KEY_FILE);
        if key_path.exists() {
            let pem = std::fs::read_to_string(&key_path)
                .with_context(|| format!("read {}", key_path.display()))?;
            let signing = SigningKey::from_pkcs8_pem(&pem).with_context(|| {
                format!("parse {} (not a PKCS#8 Ed25519 key)", key_path.display())
            })?;
            return Ok(Identity { signing, dir });
        }
        let signing = SigningKey::generate(&mut rand::rngs::OsRng);
        let pem = signing
            .to_pkcs8_pem(ed25519_dalek::pkcs8::spki::der::pem::LineEnding::LF)
            .context("encode private key")?;
        write_private_file(&key_path, pem.as_bytes())?;
        let pub_pem = signing
            .verifying_key()
            .to_public_key_pem(ed25519_dalek::pkcs8::spki::der::pem::LineEnding::LF)
            .context("encode public key")?;
        std::fs::write(dir.join(PUBLIC_KEY_FILE), pub_pem)
            .with_context(|| format!("write {}", dir.join(PUBLIC_KEY_FILE).display()))?;
        Ok(Identity { signing, dir })
    }

    /// Moves the current identity aside (to `identity.replaced-<unix time>`)
    /// and creates a new key, for re-enrolling a cloned host or one whose key
    /// was revoked. Returns where the old identity went, if there was one.
    pub fn replace(state_dir: &Path) -> Result<(Identity, Option<PathBuf>)> {
        let dir = state_dir.join("identity");
        let mut moved = None;
        if dir.exists() {
            let secs = std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .map(|d| d.as_secs())
                .unwrap_or(0);
            let to = state_dir.join(format!("identity.replaced-{secs}"));
            anyhow::ensure!(
                !to.exists(),
                "{} already exists; try again in a second",
                to.display()
            );
            std::fs::rename(&dir, &to)
                .with_context(|| format!("move {} to {}", dir.display(), to.display()))?;
            moved = Some(to);
        }
        Ok((Identity::load_or_create(state_dir)?, moved))
    }

    /// Signs `msg` with the node key (handshake proof).
    pub fn sign(&self, msg: &[u8]) -> [u8; 64] {
        use ed25519_dalek::Signer;
        self.signing.sign(msg).to_bytes()
    }

    pub fn verifying_key(&self) -> VerifyingKey {
        self.signing.verifying_key()
    }

    /// Stable node ID: lowercase hex SHA-256 of the raw 32-byte public key.
    pub fn fingerprint(&self) -> String {
        hex::encode(Sha256::digest(self.verifying_key().as_bytes()))
    }

    pub fn dir(&self) -> &Path {
        &self.dir
    }
}

#[cfg(unix)]
fn create_private_dir(dir: &Path) -> Result<()> {
    use std::os::unix::fs::{DirBuilderExt, PermissionsExt};
    if !dir.exists() {
        std::fs::DirBuilder::new()
            .recursive(true)
            .mode(0o700)
            .create(dir)
            .with_context(|| format!("create {}", dir.display()))?;
    }
    std::fs::set_permissions(dir, std::fs::Permissions::from_mode(0o700))
        .with_context(|| format!("chmod 0700 {}", dir.display()))?;
    Ok(())
}

#[cfg(unix)]
fn write_private_file(path: &Path, bytes: &[u8]) -> Result<()> {
    use std::io::Write;
    use std::os::unix::fs::OpenOptionsExt;
    let mut f = std::fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(path)
        .with_context(|| format!("create {}", path.display()))?;
    f.write_all(bytes)?;
    f.sync_all()?;
    Ok(())
}

#[cfg(not(unix))]
fn create_private_dir(dir: &Path) -> Result<()> {
    // Windows ACL handling is part of the pending Windows scope (WIN-001).
    std::fs::create_dir_all(dir).with_context(|| format!("create {}", dir.display()))
}

#[cfg(not(unix))]
fn write_private_file(path: &Path, bytes: &[u8]) -> Result<()> {
    anyhow::ensure!(!path.exists(), "{} already exists", path.display());
    std::fs::write(path, bytes).with_context(|| format!("write {}", path.display()))
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::PermissionsExt;

    #[test]
    fn creates_once_and_reloads_same_identity() {
        let dir = tempfile::tempdir().unwrap();
        let a = Identity::load_or_create(dir.path()).unwrap();
        let b = Identity::load_or_create(dir.path()).unwrap();
        assert_eq!(a.fingerprint(), b.fingerprint());
        assert_eq!(a.fingerprint().len(), 64);
        assert!(a.fingerprint().chars().all(|c| c.is_ascii_hexdigit()));
    }

    #[test]
    fn private_key_is_0600_in_0700_dir() {
        let dir = tempfile::tempdir().unwrap();
        let id = Identity::load_or_create(dir.path()).unwrap();
        let key = std::fs::metadata(id.dir().join(PRIVATE_KEY_FILE)).unwrap();
        assert_eq!(key.permissions().mode() & 0o777, 0o600);
        let d = std::fs::metadata(id.dir()).unwrap();
        assert_eq!(d.permissions().mode() & 0o777, 0o700);
        assert!(id.dir().join(PUBLIC_KEY_FILE).exists());
    }

    #[test]
    fn corrupt_key_is_an_error_not_a_regeneration() {
        let dir = tempfile::tempdir().unwrap();
        let id_dir = dir.path().join("identity");
        std::fs::create_dir_all(&id_dir).unwrap();
        std::fs::write(id_dir.join(PRIVATE_KEY_FILE), "garbage").unwrap();
        let err = Identity::load_or_create(dir.path()).unwrap_err();
        assert!(err.to_string().contains("parse"), "{err:#}");
        assert_eq!(
            std::fs::read_to_string(id_dir.join(PRIVATE_KEY_FILE)).unwrap(),
            "garbage"
        );
    }

    #[test]
    fn replace_keeps_the_old_identity_aside_and_makes_a_new_key() {
        let dir = tempfile::tempdir().unwrap();
        let a = Identity::load_or_create(dir.path()).unwrap();
        std::fs::write(a.dir().join("node.crt"), "old cert").unwrap();
        let (b, moved) = Identity::replace(dir.path()).unwrap();
        assert_ne!(a.fingerprint(), b.fingerprint());
        let moved = moved.unwrap();
        assert!(moved.join(PRIVATE_KEY_FILE).exists() && moved.join("node.crt").exists());
        assert!(!b.dir().join("node.crt").exists());
    }

    #[test]
    fn different_dirs_are_different_nodes() {
        let a = Identity::load_or_create(tempfile::tempdir().unwrap().path()).unwrap();
        let b = Identity::load_or_create(tempfile::tempdir().unwrap().path()).unwrap();
        assert_ne!(a.fingerprint(), b.fingerprint());
    }
}
