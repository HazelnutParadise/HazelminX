package jobs

import (
	"path"
	"sort"
	"strings"
	"sync"
)

// lockKey is the identity a resource is locked under. Two refs conflict when
// their keys conflict (see conflicts), not when their strings are equal.
type lockKey struct {
	node   string
	kind   string // "device", "mount", "path", or the ref's own kind
	device string
	mount  string
	p      string // normalized path for kind=path; id for other kinds
}

// keysFor derives the lock keys of a resource. A path on a known device also
// locks that device "softly": it conflicts with a disk-level operation on the
// same device but not with another path on the device (unless parent/child).
func keysFor(r ResourceRef) []lockKey {
	switch r.Kind {
	case "disk":
		return []lockKey{{node: r.Node, kind: "device", device: r.Device}}
	case "mount":
		return []lockKey{{node: r.Node, kind: "mount", device: r.Device, mount: r.Mount}}
	case "path":
		p := r.ResolvedPath
		if p == "" {
			p = r.Path
		}
		return []lockKey{{node: r.Node, kind: "path", device: r.Device, mount: r.Mount, p: normalizePath(p)}}
	default:
		return []lockKey{{node: r.Node, kind: r.Kind, p: r.ID}}
	}
}

func normalizePath(p string) string {
	if p == "" {
		return ""
	}
	p = path.Clean(strings.ReplaceAll(p, "\\", "/"))
	if p != "/" {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

// pathWithin reports whether child == parent or child is under parent at a
// separator boundary ("/data/mysql" is within "/data", not within "/dat").
func pathWithin(child, parent string) bool {
	if parent == "/" {
		return true
	}
	return child == parent || strings.HasPrefix(child, parent+"/")
}

// conflicts implements spec 33.2: locks recognise the same device, the same
// mount and parent/child paths, not just equal strings.
func conflicts(a, b lockKey) bool {
	if a.node != b.node {
		return false
	}
	sameDevice := a.device != "" && a.device == b.device
	switch {
	case a.kind == "device" || b.kind == "device":
		// A disk-level operation conflicts with anything on that device.
		return sameDevice
	case a.kind == "mount" && b.kind == "mount":
		return sameDevice || (a.mount != "" && a.mount == b.mount)
	case a.kind == "mount" || b.kind == "mount":
		// mount vs path: same mount point, or the path lives on the mount.
		m, p := a, b
		if b.kind == "mount" {
			m, p = b, a
		}
		if sameDevice {
			return true
		}
		return m.mount != "" && (p.mount == m.mount || pathWithin(p.p, m.mount))
	case a.kind == "path" && b.kind == "path":
		if a.device != "" && b.device != "" && a.device != b.device {
			return false
		}
		return pathWithin(a.p, b.p) || pathWithin(b.p, a.p)
	default:
		return a.kind == b.kind && a.p == b.p
	}
}

// lockTable holds the exclusive reservations of active jobs. It is rebuilt
// from the store on start, so a restart never forgets who owns what.
type lockTable struct {
	mu   sync.Mutex
	held map[string][]lockKey // jobID → keys
}

func newLockTable() *lockTable { return &lockTable{held: map[string][]lockKey{}} }

// tryAcquire reserves every resource of the job or none. It returns the id of
// a conflicting job when it cannot. Keys are taken in a fixed order so two
// jobs that need the same set never wait on each other forever.
func (t *lockTable) tryAcquire(jobID string, res []ResourceRef) (blockedBy string, ok bool) {
	var keys []lockKey
	for _, r := range res {
		keys = append(keys, keysFor(r)...)
	}
	sort.Slice(keys, func(i, j int) bool { return keyString(keys[i]) < keyString(keys[j]) })
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, already := t.held[jobID]; already {
		return "", true
	}
	for other, held := range t.held {
		for _, h := range held {
			for _, k := range keys {
				if conflicts(h, k) {
					return other, false
				}
			}
		}
	}
	t.held[jobID] = keys
	return "", true
}

func (t *lockTable) release(jobID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.held, jobID)
}

func keyString(k lockKey) string {
	return strings.Join([]string{k.node, k.kind, k.device, k.mount, k.p}, "\x00")
}
