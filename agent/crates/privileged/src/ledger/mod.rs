//! Host change ledger (spec 55.1, ticket 113).
//!
//! Every change the platform makes to the host is written here before and
//! after it happens, so complete removal (037) can tell what the platform
//! installed, what it modified and what was already there, and can compare
//! the host with the pre-install snapshot.
//!
//! - [`entry`]    what a line records: target, change, before/after, owner
//! - [`store`]    the root-only, hash-chained, append-only file
//! - [`probe`]    how each target's state is read (and files backed up)
//! - [`recorder`] intent → change → result around one action
//! - [`snapshot`] pre-install snapshot: capture and compare
//! - [`drift`]    files the user changed after the platform wrote them
//! - [`sync`]     pages of raw lines for Core's copy

pub mod drift;
pub mod entry;
pub mod probe;
pub mod recorder;
pub mod snapshot;
pub mod store;
pub mod sync;
