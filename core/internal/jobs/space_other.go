//go:build !unix

package jobs

// FreeBytesOf is unavailable on this platform; the space reserve check is
// skipped (Core is only supported on Linux, see engineering/architecture.md).
func FreeBytesOf(string) func() (int64, error) { return nil }
