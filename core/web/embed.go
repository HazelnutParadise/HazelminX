// Package web serves the built React interface that CI embeds into the core
// binary (spec 8.4: the UI ships inside hazelminx-core; no Node.js on hosts).
//
// Without the "embedweb" build tag (local development, tests) the embedded
// file system is empty and the HTTP layer answers UI requests with 503 so the
// absence is explicit instead of a blank page.
package web

import "io/fs"

// FS returns the built interface, or nil when this binary has no UI embedded.
func FS() fs.FS { return assets() }
