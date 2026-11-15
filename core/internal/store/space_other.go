//go:build !unix

package store

import "errors"

// freeBytes is unavailable on this platform (Core is only supported on Linux).
var freeBytes = func(string) (int64, error) { return 0, errors.New("free space unknown on this platform") }
