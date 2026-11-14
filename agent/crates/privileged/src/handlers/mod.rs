//! One module per action. Handlers receive typed parameters and a
//! [`crate::command::CommandRunner`]; they build [`crate::command::CommandSpec`]s
//! and parse outputs. None of them formats a shell string.

pub mod engines;
pub mod packages;
pub mod storage_usage;
pub mod system_info;
pub mod systemd;
