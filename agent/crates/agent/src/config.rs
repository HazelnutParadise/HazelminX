//! Agent bootstrap configuration.
//!
//! Precedence: command-line flag > `HAZELMINX_AGENT_*` environment variable >
//! TOML file > default. Secrets (node private key) are never in this file;
//! they live in `state_dir` with 0600 permissions (see `identity`).

use anyhow::{Context, Result};
use serde::Deserialize;
use std::path::{Path, PathBuf};

/// Default config path on Linux (spec chapter 37: `/etc/hazelminx/`).
pub const DEFAULT_CONFIG_PATH: &str = "/etc/hazelminx/agent.toml";
/// Default state directory (spec chapter 37: `/var/lib/hazelminx/`).
pub const DEFAULT_STATE_DIR: &str = "/var/lib/hazelminx/agent";

#[derive(Debug, Clone, Deserialize, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct Config {
    /// Where the node identity, job journal and caches live.
    #[serde(default = "default_state_dir")]
    pub state_dir: PathBuf,
    /// Core management endpoint the agent dials, e.g. `https://core.example:9801`.
    /// `None` means "not enrolled yet"; the agent then only maintains its identity.
    #[serde(default)]
    pub core_url: Option<String>,
    /// Human-readable node name proposed at enrollment; the stable identity is the key fingerprint.
    #[serde(default)]
    pub node_name: Option<String>,
    /// Log format: `json` (default) or `text`.
    #[serde(default = "default_log_format")]
    pub log_format: String,
}

fn default_state_dir() -> PathBuf {
    PathBuf::from(DEFAULT_STATE_DIR)
}

fn default_log_format() -> String {
    "json".to_string()
}

impl Default for Config {
    fn default() -> Self {
        Self {
            state_dir: default_state_dir(),
            core_url: None,
            node_name: None,
            log_format: default_log_format(),
        }
    }
}

/// Overrides collected from flags and environment, applied on top of the file.
#[derive(Debug, Default, Clone)]
pub struct Overrides {
    pub state_dir: Option<PathBuf>,
    pub core_url: Option<String>,
    pub node_name: Option<String>,
    pub log_format: Option<String>,
}

impl Config {
    /// Loads the TOML file if it exists (a missing file means defaults), then
    /// applies overrides and validates.
    pub fn load(path: &Path, overrides: Overrides) -> Result<Config> {
        let mut cfg = match std::fs::read_to_string(path) {
            Ok(text) => toml::from_str::<Config>(&text)
                .with_context(|| format!("parse config {}", path.display()))?,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Config::default(),
            Err(e) => return Err(e).with_context(|| format!("read config {}", path.display())),
        };
        if let Some(v) = overrides.state_dir {
            cfg.state_dir = v;
        }
        if let Some(v) = overrides.core_url {
            cfg.core_url = Some(v);
        }
        if let Some(v) = overrides.node_name {
            cfg.node_name = Some(v);
        }
        if let Some(v) = overrides.log_format {
            cfg.log_format = v;
        }
        cfg.validate()?;
        Ok(cfg)
    }

    fn validate(&self) -> Result<()> {
        anyhow::ensure!(
            self.state_dir.is_absolute(),
            "state_dir {} must be an absolute path",
            self.state_dir.display()
        );
        anyhow::ensure!(
            self.log_format == "json" || self.log_format == "text",
            "log_format {:?} must be json or text",
            self.log_format
        );
        if let Some(url) = &self.core_url {
            anyhow::ensure!(
                url.starts_with("https://") || url.starts_with("http://"),
                "core_url {url:?} must start with https:// (http:// is only for local tests)"
            );
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn missing_file_yields_defaults() {
        let cfg = Config::load(Path::new("/nonexistent/agent.toml"), Overrides::default()).unwrap();
        assert_eq!(cfg, Config::default());
    }

    #[test]
    fn file_then_overrides() {
        let dir = tempfile::tempdir().unwrap();
        let p = dir.path().join("agent.toml");
        std::fs::write(
            &p,
            "state_dir = \"/var/lib/x\"\ncore_url = \"https://core:9801\"\nlog_format = \"text\"\n",
        )
        .unwrap();
        let cfg = Config::load(
            &p,
            Overrides {
                core_url: Some("https://other:9801".into()),
                ..Default::default()
            },
        )
        .unwrap();
        assert_eq!(cfg.state_dir, PathBuf::from("/var/lib/x"));
        assert_eq!(cfg.core_url.as_deref(), Some("https://other:9801"));
        assert_eq!(cfg.log_format, "text");
    }

    #[test]
    fn unknown_field_is_rejected() {
        let dir = tempfile::tempdir().unwrap();
        let p = dir.path().join("agent.toml");
        std::fs::write(&p, "state_dir = \"/a\"\ncore_adress = \"typo\"\n").unwrap();
        let err = Config::load(&p, Overrides::default()).unwrap_err();
        assert!(err.to_string().contains("parse config"), "{err:#}");
    }

    #[test]
    fn validation_rejects_relative_state_dir_and_bad_scheme() {
        let err = Config::load(
            Path::new("/nonexistent"),
            Overrides {
                state_dir: Some("relative".into()),
                ..Default::default()
            },
        )
        .unwrap_err();
        assert!(err.to_string().contains("absolute"));
        let err = Config::load(
            Path::new("/nonexistent"),
            Overrides {
                core_url: Some("ftp://core".into()),
                ..Default::default()
            },
        )
        .unwrap_err();
        assert!(err.to_string().contains("core_url"));
    }
}
