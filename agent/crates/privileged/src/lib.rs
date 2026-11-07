//! hazelminx-privileged: the root-side half of the agent split (spec 32.2).
//!
//! The low-privilege `hazelminx-agent` talks to this process over a Unix
//! socket. Every request names one action from [`action::Action`] with
//! structured parameters; there is no "run this string" action. Each request
//! carries a signed [`grant::Grant`] that lists the permissions the caller
//! was given; the executor checks caller uid, grant signature, validity window,
//! revocation and the action's required permission before anything runs.
//!
//! Module map:
//! - [`params`]   validated parameter types (unit names, paths, users, env)
//! - [`command`]  argv-only command execution seam (`CommandRunner`)
//! - [`action`]   the closed set of named actions and their permissions
//! - [`grant`]    signed authorization grants and the revocation list
//! - [`audit`]    security events (never contain secrets)
//! - [`protocol`] socket frames
//! - [`executor`] `Executor` trait and the real host dispatcher
//! - [`server`]   socket listener, caller check, authorization pipeline
//! - [`client`]   the agent-side client (also used by the tests)

pub mod action;
pub mod audit;
pub mod client;
pub mod command;
pub mod config;
pub mod executor;
pub mod grant;
pub mod handlers;
pub mod params;
pub mod protocol;
pub mod server;

use std::future::Future;
use std::pin::Pin;

/// Boxed future used by the object-safe traits (`CommandRunner`, `Executor`).
pub type BoxFuture<'a, T> = Pin<Box<dyn Future<Output = T> + Send + 'a>>;
