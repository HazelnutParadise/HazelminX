//go:build unix

package jobs

import "syscall"

// FreeBytesOf returns a probe of the space available to unprivileged writers
// on the filesystem holding path (what the space reserve of ENG-011 is
// measured against).
func FreeBytesOf(path string) func() (int64, error) {
	return func() (int64, error) {
		var st syscall.Statfs_t
		if err := syscall.Statfs(path, &st); err != nil {
			return 0, err
		}
		return int64(st.Bavail) * int64(st.Bsize), nil
	}
}
