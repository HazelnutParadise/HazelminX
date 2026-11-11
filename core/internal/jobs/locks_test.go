package jobs

import "testing"

func TestLockConflicts(t *testing.T) {
	disk := ResourceRef{Kind: "disk", Device: "dev-a"}
	pathOnDisk := ResourceRef{Kind: "path", Device: "dev-a", Mount: "/data", Path: "/data/mysql"}
	childPath := ResourceRef{Kind: "path", Device: "dev-a", Mount: "/data", Path: "/data/mysql/db1/"}
	siblingPath := ResourceRef{Kind: "path", Device: "dev-a", Mount: "/data", Path: "/data/mysqlx"}
	otherDisk := ResourceRef{Kind: "disk", Device: "dev-b"}
	mount := ResourceRef{Kind: "mount", Device: "dev-a", Mount: "/data"}
	db := ResourceRef{Kind: "database", ID: "db-1"}
	dbOther := ResourceRef{Kind: "database", ID: "db-2"}
	otherNode := ResourceRef{Kind: "disk", Device: "dev-a", Node: "n2"}
	symlinked := ResourceRef{Kind: "path", Device: "dev-a", Path: "/srv/link", ResolvedPath: "/data/mysql"}

	cases := []struct {
		name string
		a, b ResourceRef
		want bool
	}{
		{"same disk: format vs format", disk, disk, true},
		{"disk vs path on it (ENG-004 format vs move)", disk, pathOnDisk, true},
		{"different disks", disk, otherDisk, false},
		{"parent/child path", pathOnDisk, childPath, true},
		{"sibling with common prefix is not a child", pathOnDisk, siblingPath, false},
		{"mount vs path under it", mount, pathOnDisk, true},
		{"same database upgrade vs restore", db, db, true},
		{"different databases", db, dbOther, false},
		{"same device id on another node", disk, otherNode, false},
		{"resolved path beats raw path", symlinked, childPath, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := false
			for _, ka := range keysFor(c.a) {
				for _, kb := range keysFor(c.b) {
					if conflicts(ka, kb) || conflicts(kb, ka) {
						got = true
					}
				}
			}
			if got != c.want {
				t.Fatalf("conflict=%v want %v", got, c.want)
			}
		})
	}
}

func TestLockTableAllOrNothing(t *testing.T) {
	lt := newLockTable()
	if by, ok := lt.tryAcquire("j1", []ResourceRef{{Kind: "disk", Device: "a"}, {Kind: "database", ID: "x"}}); !ok {
		t.Fatalf("first acquire blocked by %s", by)
	}
	// j2 needs an unrelated db and the locked disk: must get nothing.
	if by, ok := lt.tryAcquire("j2", []ResourceRef{{Kind: "database", ID: "y"}, {Kind: "disk", Device: "a"}}); ok || by != "j1" {
		t.Fatalf("j2 ok=%v by=%s", ok, by)
	}
	if _, held := lt.held["j2"]; held {
		t.Fatal("j2 must not hold partial locks")
	}
	// j3 only needs database y, which j2 never got.
	if _, ok := lt.tryAcquire("j3", []ResourceRef{{Kind: "database", ID: "y"}}); !ok {
		t.Fatal("j3 must run")
	}
	lt.release("j1")
	if _, ok := lt.tryAcquire("j2", []ResourceRef{{Kind: "disk", Device: "a"}}); !ok {
		t.Fatal("j2 must run after j1 released")
	}
	// Re-acquire by the same job is a no-op success (restart rebuild).
	if _, ok := lt.tryAcquire("j2", []ResourceRef{{Kind: "disk", Device: "a"}}); !ok {
		t.Fatal("same job re-acquire must succeed")
	}
}
