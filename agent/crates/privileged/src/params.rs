//! Validated parameter types. Every value that reaches a command line is one
//! of these; the command layer never sees raw strings. Validation rejects only
//! what the target syntax cannot hold (unit names, user names) or what the OS
//! cannot represent (NUL bytes). Everything else, including spaces, quotes,
//! newlines and shell metacharacters, is kept verbatim and passed as a single
//! argv entry (SEC-006, T-039).

use serde::{Deserialize, Serialize};
use std::fmt;
use std::path::{Path, PathBuf};

/// Why a parameter was rejected. The message names the rule, never the value,
/// so it is safe to log and return to the caller (SEC-010).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ParamError {
    pub reason: String,
}

impl ParamError {
    pub fn new(reason: impl Into<String>) -> Self {
        Self {
            reason: reason.into(),
        }
    }
}

impl fmt::Display for ParamError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.reason)
    }
}

impl std::error::Error for ParamError {}

pub const UNIT_NAME_MAX: usize = 256;
pub const UNIT_SUFFIXES: &[&str] = &[
    ".service",
    ".socket",
    ".timer",
    ".target",
    ".mount",
    ".automount",
    ".path",
    ".slice",
    ".scope",
    ".swap",
    ".device",
];

/// A systemd unit name such as `nginx.service` or `getty@tty1.service`.
#[derive(Debug, Clone, PartialEq, Eq, Hash, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(try_from = "String", into = "String")]
pub struct UnitName(String);

impl UnitName {
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl TryFrom<String> for UnitName {
    type Error = ParamError;
    fn try_from(value: String) -> Result<Self, Self::Error> {
        if value.is_empty() {
            return Err(ParamError::new("unit name is empty"));
        }
        if value.len() > UNIT_NAME_MAX {
            return Err(ParamError::new("unit name is longer than 256 bytes"));
        }
        if !value.bytes().all(|b| {
            b.is_ascii_alphanumeric() || matches!(b, b':' | b'_' | b'.' | b'@' | b'-' | b'\\')
        }) {
            return Err(ParamError::new(
                "unit name contains characters outside [A-Za-z0-9:_.@-\\]",
            ));
        }
        if value.starts_with('.') || value.starts_with('-') {
            return Err(ParamError::new("unit name starts with '.' or '-'"));
        }
        let Some(dot) = value.rfind('.') else {
            return Err(ParamError::new("unit name has no type suffix"));
        };
        if dot == 0 || !UNIT_SUFFIXES.contains(&&value[dot..]) {
            return Err(ParamError::new(
                "unit name suffix is not a systemd unit type",
            ));
        }
        Ok(UnitName(value))
    }
}

impl From<UnitName> for String {
    fn from(v: UnitName) -> String {
        v.0
    }
}

pub const USER_NAME_MAX: usize = 32;

/// A Linux account name (`useradd` portable rules plus an optional trailing `$`).
#[derive(Debug, Clone, PartialEq, Eq, Hash, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(try_from = "String", into = "String")]
pub struct UserName(String);

impl UserName {
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl TryFrom<String> for UserName {
    type Error = ParamError;
    fn try_from(value: String) -> Result<Self, Self::Error> {
        if value.is_empty() {
            return Err(ParamError::new("user name is empty"));
        }
        if value.len() > USER_NAME_MAX {
            return Err(ParamError::new("user name is longer than 32 bytes"));
        }
        let body = value.strip_suffix('$').unwrap_or(&value);
        let mut bytes = body.bytes();
        let first_ok = bytes
            .next()
            .is_some_and(|b| b.is_ascii_alphabetic() || b == b'_');
        if !first_ok {
            return Err(ParamError::new("user name must start with a letter or '_'"));
        }
        if !bytes.all(|b| b.is_ascii_alphanumeric() || matches!(b, b'_' | b'.' | b'-')) {
            return Err(ParamError::new(
                "user name contains characters outside [A-Za-z0-9_.-] (optional trailing '$')",
            ));
        }
        Ok(UserName(value))
    }
}

impl From<UserName> for String {
    fn from(v: UserName) -> String {
        v.0
    }
}

/// An absolute host path with no `.` or `..` components. Any other bytes,
/// including whitespace, quotes and newlines, are kept as-is.
#[derive(Debug, Clone, PartialEq, Eq, Hash, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(try_from = "String", into = "String")]
pub struct HostPath(PathBuf);

impl HostPath {
    pub fn as_path(&self) -> &Path {
        &self.0
    }
}

impl TryFrom<String> for HostPath {
    type Error = ParamError;
    fn try_from(value: String) -> Result<Self, Self::Error> {
        if value.is_empty() {
            return Err(ParamError::new("path is empty"));
        }
        if value.contains('\0') {
            return Err(ParamError::new("path contains a NUL byte"));
        }
        if !value.starts_with('/') {
            return Err(ParamError::new("path is not absolute"));
        }
        if has_dot_components(&value) {
            return Err(ParamError::new("path contains '.' or '..' components"));
        }
        Ok(HostPath(PathBuf::from(value)))
    }
}

impl From<HostPath> for String {
    fn from(v: HostPath) -> String {
        v.0.to_string_lossy().into_owned()
    }
}

pub const PACKAGE_NAME_MAX: usize = 128;

/// A distribution package name (dpkg or rpm), e.g. `docker-ce`,
/// `libstdc++6`, `python3.12`. Starts with an alphanumeric byte; the rest is
/// `[A-Za-z0-9+._-]`, which covers both naming policies.
#[derive(Debug, Clone, PartialEq, Eq, Hash, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(try_from = "String", into = "String")]
pub struct PackageName(String);

impl PackageName {
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl TryFrom<String> for PackageName {
    type Error = ParamError;
    fn try_from(value: String) -> Result<Self, Self::Error> {
        if value.is_empty() {
            return Err(ParamError::new("package name is empty"));
        }
        if value.len() > PACKAGE_NAME_MAX {
            return Err(ParamError::new("package name is longer than 128 bytes"));
        }
        if !value.as_bytes()[0].is_ascii_alphanumeric() {
            return Err(ParamError::new(
                "package name must start with a letter or digit",
            ));
        }
        if !value
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'+' | b'.' | b'_' | b'-'))
        {
            return Err(ParamError::new(
                "package name contains characters outside [A-Za-z0-9+._-]",
            ));
        }
        Ok(PackageName(value))
    }
}

impl From<PackageName> for String {
    fn from(v: PackageName) -> String {
        v.0
    }
}

/// An environment variable: a shell-identifier name and a NUL-free value.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(try_from = "RawEnvVar", into = "RawEnvVar")]
pub struct EnvVar {
    name: String,
    value: String,
}

#[derive(Serialize, Deserialize)]
struct RawEnvVar {
    name: String,
    value: String,
}

impl EnvVar {
    pub fn new(name: impl Into<String>, value: impl Into<String>) -> Result<Self, ParamError> {
        Self::try_from(RawEnvVar {
            name: name.into(),
            value: value.into(),
        })
    }
    pub fn name(&self) -> &str {
        &self.name
    }
    pub fn value(&self) -> &str {
        &self.value
    }
}

impl TryFrom<RawEnvVar> for EnvVar {
    type Error = ParamError;
    fn try_from(raw: RawEnvVar) -> Result<Self, Self::Error> {
        let mut bytes = raw.name.bytes();
        let first_ok = bytes
            .next()
            .is_some_and(|b| b.is_ascii_alphabetic() || b == b'_');
        if !first_ok || !bytes.all(|b| b.is_ascii_alphanumeric() || b == b'_') {
            return Err(ParamError::new(
                "environment variable name must match [A-Za-z_][A-Za-z0-9_]*",
            ));
        }
        if raw.value.contains('\0') {
            return Err(ParamError::new(
                "environment variable value contains a NUL byte",
            ));
        }
        Ok(EnvVar {
            name: raw.name,
            value: raw.value,
        })
    }
}

impl From<EnvVar> for RawEnvVar {
    fn from(v: EnvVar) -> RawEnvVar {
        RawEnvVar {
            name: v.name,
            value: v.value,
        }
    }
}

fn has_dot_components(p: &str) -> bool {
    p.split('/').any(|seg| seg == "." || seg == "..")
}

#[cfg(test)]
mod tests {
    use super::*;

    fn unit(s: &str) -> Result<UnitName, ParamError> {
        UnitName::try_from(s.to_string())
    }

    #[test]
    fn unit_name_accepts_real_units() {
        for ok in [
            "nginx.service",
            "getty@tty1.service",
            "apt-daily.timer",
            "sys-kernel-debug.mount",
            "dev-sda1.device",
            "docker.socket",
            "multi-user.target",
            "a\\x2db.service",
        ] {
            assert!(unit(ok).is_ok(), "{ok}");
        }
    }

    #[test]
    fn unit_name_rejects_shell_syntax_and_unknown_suffix() {
        for bad in [
            "",
            "nginx",
            "nginx.service; rm -rf /",
            "nginx .service",
            "nginx.service\nreboot",
            "\"nginx\".service",
            "$(reboot).service",
            "nginx.service|cat",
            "../x.service",
            "nginx.exe",
        ] {
            assert!(unit(bad).is_err(), "{bad:?}");
        }
        let long = format!("{}.service", "a".repeat(UNIT_NAME_MAX));
        assert!(unit(&long).is_err());
    }

    #[test]
    fn user_name_rules() {
        for ok in [
            "root",
            "hazelminx-agent",
            "www_data",
            "_apt",
            "backup$",
            "Deploy.2",
        ] {
            assert!(UserName::try_from(ok.to_string()).is_ok(), "{ok}");
        }
        for bad in [
            "",
            "-root",
            "9lives",
            "root toor",
            "root;id",
            "root\nid",
            "ro'ot",
            "$root",
            &"a".repeat(USER_NAME_MAX + 1),
        ] {
            assert!(UserName::try_from(bad.to_string()).is_err(), "{bad:?}");
        }
    }

    #[test]
    fn host_path_keeps_hostile_bytes_but_requires_absolute_normal_form() {
        let nasty = "/srv/data/it's \"here\"; rm -rf ~\n$(id) `id` | && > out";
        let p = HostPath::try_from(nasty.to_string()).unwrap();
        assert_eq!(p.as_path(), Path::new(nasty));
        for bad in ["", "relative/x", "/a/../b", "/a/./b", "/a\0b"] {
            assert!(HostPath::try_from(bad.to_string()).is_err(), "{bad:?}");
        }
    }

    #[test]
    fn package_name_rules() {
        for ok in [
            "docker-ce",
            "libstdc++6",
            "python3.12",
            "nvidia_container_toolkit",
            "0ad",
        ] {
            assert!(PackageName::try_from(ok.to_string()).is_ok(), "{ok}");
        }
        for bad in [
            "",
            "-x",
            ".x",
            "a b",
            "a;id",
            "a\nb",
            "a/b",
            "$(id)",
            &"a".repeat(PACKAGE_NAME_MAX + 1),
        ] {
            assert!(PackageName::try_from(bad.to_string()).is_err(), "{bad:?}");
        }
    }

    #[test]
    fn env_var_rules() {
        let v = EnvVar::new("DEPLOY_MSG", "it's \"quoted\"\nnext $(id)").unwrap();
        assert_eq!(v.value(), "it's \"quoted\"\nnext $(id)");
        assert!(EnvVar::new("", "x").is_err());
        assert!(EnvVar::new("1X", "x").is_err());
        assert!(EnvVar::new("A B", "x").is_err());
        assert!(EnvVar::new("A=B", "x").is_err());
        assert!(EnvVar::new("A", "x\0y").is_err());
    }

    #[test]
    fn serde_round_trip_goes_through_validation() {
        let ok: UnitName = serde_json::from_str("\"nginx.service\"").unwrap();
        assert_eq!(ok.as_str(), "nginx.service");
        assert!(serde_json::from_str::<UnitName>("\"a b.service\"").is_err());
        assert!(serde_json::from_str::<HostPath>("\"rel\"").is_err());
        assert!(serde_json::from_str::<UserName>("\"a b\"").is_err());
    }
}
