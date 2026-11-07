//! Executor configuration: `/etc/hazelminx/privileged.toml`, overridable by
//! flags and `HAZELMINX_PRIVILEGED_*` variables. `allowed_uid` has no
//! default on purpose: the installer (ticket 013) writes the agent account's
//! uid, and a missing value refuses to start rather than guessing.

use anyhow::{Context, Result};
use serde::Deserialize;
use std::path::{Path, PathBuf};

pub const DEFAULT_CONFIG_PATH: &str = "/etc/hazelminx/privileged.toml";
pub const DEFAULT_SOCKET_PATH: &str = "/run/hazelminx/privileged.sock";
pub const DEFAULT_ISSUER_KEY_PATH: &str = "/etc/hazelminx/privileged/issuer.pub";

#[derive(Debug, Clone, Deserialize, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct Config {
    #[serde(default = "default_socket_path")]
    pub socket_path: PathBuf,
    /// PEM (SPKI) Ed25519 public key that signs grants.
    #[serde(default = "default_issuer_key_path")]
    pub issuer_public_key: PathBuf,
    /// uid of the `hazelminx-agent` account; the only accepted caller.
    pub allowed_uid: Option<u32>,
    #[serde(default = "default_max_actions")]
    pub max_concurrent_actions: usize,
    /// `json` (default) or `text`.
    #[serde(default = "default_log_format")]
    pub log_format: String,
}

fn default_socket_path() -> PathBuf {
    PathBuf::from(DEFAULT_SOCKET_PATH)
}
fn default_issuer_key_path() -> PathBuf {
    PathBuf::from(DEFAULT_ISSUER_KEY_PATH)
}
fn default_max_actions() -> usize {
    crate::server::DEFAULT_MAX_CONCURRENT_ACTIONS
}
fn default_log_format() -> String {
    "json".into()
}

impl Default for Config {
    fn default() -> Self {
        Self {
            socket_path: default_socket_path(),
            issuer_public_key: default_issuer_key_path(),
            allowed_uid: None,
            max_concurrent_actions: default_max_actions(),
            log_format: default_log_format(),
        }
    }
}

#[derive(Debug, Default, Clone)]
pub struct Overrides {
    pub socket_path: Option<PathBuf>,
    pub issuer_public_key: Option<PathBuf>,
    pub allowed_uid: Option<u32>,
    pub log_format: Option<String>,
}

impl Config {
    pub fn load(path: &Path, overrides: Overrides) -> Result<Config> {
        let mut cfg = match std::fs::read_to_string(path) {
            Ok(text) => toml::from_str::<Config>(&text)
                .with_context(|| format!("parse config {}", path.display()))?,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Config::default(),
            Err(e) => return Err(e).with_context(|| format!("read config {}", path.display())),
        };
        if let Some(v) = overrides.socket_path {
            cfg.socket_path = v;
        }
        if let Some(v) = overrides.issuer_public_key {
            cfg.issuer_public_key = v;
        }
        if let Some(v) = overrides.allowed_uid {
            cfg.allowed_uid = Some(v);
        }
        if let Some(v) = overrides.log_format {
            cfg.log_format = v;
        }
        cfg.validate()?;
        Ok(cfg)
    }

    fn validate(&self) -> Result<()> {
        anyhow::ensure!(
            self.socket_path.is_absolute(),
            "socket_path must be absolute"
        );
        anyhow::ensure!(
            self.issuer_public_key.is_absolute(),
            "issuer_public_key must be absolute"
        );
        anyhow::ensure!(
            self.max_concurrent_actions >= 1,
            "max_concurrent_actions must be at least 1"
        );
        anyhow::ensure!(
            self.log_format == "json" || self.log_format == "text",
            "log_format {:?} must be json or text",
            self.log_format
        );
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn defaults_and_overrides() {
        let cfg = Config::load(
            Path::new("/nonexistent/privileged.toml"),
            Overrides::default(),
        )
        .unwrap();
        assert_eq!(cfg, Config::default());
        assert_eq!(cfg.allowed_uid, None);
        let dir = tempfile::tempdir().unwrap();
        let p = dir.path().join("privileged.toml");
        std::fs::write(&p, "allowed_uid = 998\nsocket_path = \"/run/x.sock\"\n").unwrap();
        let cfg = Config::load(
            &p,
            Overrides {
                allowed_uid: Some(1001),
                ..Default::default()
            },
        )
        .unwrap();
        assert_eq!(cfg.allowed_uid, Some(1001));
        assert_eq!(cfg.socket_path, PathBuf::from("/run/x.sock"));
    }

    #[test]
    fn unknown_field_and_bad_values_are_rejected() {
        let dir = tempfile::tempdir().unwrap();
        let p = dir.path().join("privileged.toml");
        std::fs::write(&p, "allowed_user = \"x\"\n").unwrap();
        assert!(Config::load(&p, Overrides::default()).is_err());
        std::fs::write(&p, "max_concurrent_actions = 0\n").unwrap();
        assert!(Config::load(&p, Overrides::default()).is_err());
    }
}
