package jobs

import "testing"

// The agent compares a step's targets with these wire entries, so the
// mapping must never produce an empty id (which would cover nothing or,
// for a path, everything) and paths come out in the lock key's form.
func TestWireResource(t *testing.T) {
	for _, c := range []struct {
		in       ResourceRef
		kind, id string
		ok       bool
	}{
		{ResourceRef{Kind: "path", Path: "/srv/app/", ResolvedPath: "/data//app"}, "path", "/data/app", true},
		{ResourceRef{Kind: "path", Path: "/srv/./app"}, "path", "/srv/app", true},
		{ResourceRef{Kind: "path"}, "", "", false},
		{ResourceRef{Kind: "path", Path: "relative/x"}, "", "", false},
		{ResourceRef{Kind: "disk", Device: "nvme-Samsung_123"}, "disk", "nvme-Samsung_123", true},
		{ResourceRef{Kind: "disk"}, "", "", false},
		{ResourceRef{Kind: "mount", Mount: "/data/"}, "mount", "/data", true},
		{ResourceRef{Kind: "service", ID: "nginx.service"}, "service", "nginx.service", true},
		{ResourceRef{Kind: "service"}, "", "", false},
		{ResourceRef{ID: "x"}, "", "", false},
	} {
		kind, id, ok := WireResource(c.in)
		if kind != c.kind || id != c.id || ok != c.ok {
			t.Errorf("%+v → %q %q %v, want %q %q %v", c.in, kind, id, ok, c.kind, c.id, c.ok)
		}
	}
}
