package gitsource_test

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/HazelnutParadise/hazelminx/core/internal/gitsource"
	"github.com/HazelnutParadise/hazelminx/core/internal/gitsource/gittest"
)

var local = gitsource.Fetcher{AllowLocal: true}

func TestFetchesExactlyTheResolvedCommitOverHTTPWithTokenInHeaderOnly(t *testing.T) {
	ctx := context.Background()
	r := gittest.NewRepo(t, "app")
	first := r.Commit("main", "one", map[string]string{"compose.yaml": "services: {a: {image: busybox}}\n", "conf/app.conf": "v1\n"})
	r.Tag("v1")
	second := r.Commit("main", "two", map[string]string{"conf/app.conf": "v2\n"})
	const token = "ghp_secretvalue123456"
	srv := r.ServeHTTP(token)
	remote := gitsource.Remote{URL: srv.URL(r), Auth: gitsource.Auth{Token: token}}

	got, err := local.Resolve(ctx, remote, "main")
	if err != nil || got != second {
		t.Fatalf("main resolved to %q, %v; want %s", got, err, second)
	}
	if got, err := local.Resolve(ctx, remote, "v1"); err != nil || got != first {
		t.Fatalf("annotated tag v1 resolved to %q, %v; want the commit %s", got, err, first)
	}
	if got, err := local.Resolve(ctx, remote, ""); err != nil || got != second {
		t.Fatalf("HEAD resolved to %q, %v", got, err)
	}

	// The branch moved on; fetching the older SHA still reads the older tree.
	snap, err := local.Fetch(ctx, remote, first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := snap.Read(snap.Entries["conf/app.conf"], 1<<20)
	if err != nil || string(b) != "v1\n" {
		t.Fatalf("conf/app.conf = %q, %v", b, err)
	}
	uris, auth := srv.Requests()
	for i, u := range uris {
		if strings.Contains(u, token) {
			t.Fatalf("token in request URI %s", u)
		}
		if auth[i] != "" && !strings.HasPrefix(auth[i], "Basic ") {
			t.Fatalf("unexpected auth header %q", auth[i])
		}
	}

	if _, err := local.Fetch(ctx, gitsource.Remote{URL: srv.URL(r), Auth: gitsource.Auth{Token: "wrong"}}, first); !errors.Is(err, gitsource.ErrAuth) {
		t.Fatalf("wrong token: %v", err)
	}
	if _, err := local.Fetch(ctx, remote, strings.Repeat("ab", 20)); !errors.Is(err, gitsource.ErrNotFound) {
		t.Fatalf("unknown commit: %v", err)
	} else if strings.Contains(err.Error(), token) {
		t.Fatalf("token in error: %v", err)
	}
	if _, err := local.Resolve(ctx, remote, "nope"); !errors.Is(err, gitsource.ErrNotFound) {
		t.Fatalf("unknown branch: %v", err)
	}
}

func TestFetchReportsTreeEntryKinds(t *testing.T) {
	ctx := context.Background()
	r := gittest.NewRepo(t, "kinds")
	base := r.Commit("main", "one", map[string]string{"a.yml": "x: 1\n", "link": "->a.yml"})
	sha := r.Gitlink("main", "vendor/lib", base)
	srv := r.ServeHTTP("")
	snap, err := local.Fetch(ctx, gitsource.Remote{URL: srv.URL(r)}, sha)
	if err != nil {
		t.Fatal(err)
	}
	if e := snap.Entries["a.yml"]; !e.Regular() {
		t.Fatalf("a.yml: %v", e.Mode)
	}
	if e := snap.Entries["link"]; !e.Symlink() {
		t.Fatalf("link: %v", e.Mode)
	}
	if e := snap.Entries["vendor/lib"]; !e.Gitlink() {
		t.Fatalf("vendor/lib: %v", e.Mode)
	}
}

func TestFetchStopsAtTheMemoryLimit(t *testing.T) {
	r := gittest.NewRepo(t, "big")
	sha := r.Commit("main", "big", map[string]string{"zeros": strings.Repeat("\x00", 4<<20)})
	srv := r.ServeHTTP("")
	f := gitsource.Fetcher{AllowLocal: true, MaxBytes: 1 << 20}
	if _, err := f.Fetch(context.Background(), gitsource.Remote{URL: srv.URL(r)}, sha); !errors.Is(err, gitsource.ErrTooLarge) {
		t.Fatalf("got %v, want ErrTooLarge", err)
	}
}

func TestSSHNeedsTheConfiguredHostKey(t *testing.T) {
	ctx := context.Background()
	r := gittest.NewRepo(t, "ssh")
	sha := r.Commit("main", "one", map[string]string{"compose.yaml": "services: {}\n"})
	s := r.ServeSSH()
	remote := gitsource.Remote{URL: s.URL(r), Auth: gitsource.Auth{PrivateKey: s.ClientKey, HostKey: s.HostKey}}
	if got, err := local.Resolve(ctx, remote, "main"); err != nil || got != sha {
		t.Fatalf("resolve over ssh: %q %v", got, err)
	}
	if _, err := local.Fetch(ctx, remote, sha); err != nil {
		t.Fatalf("fetch over ssh: %v", err)
	}
	_, other, _ := gittest.NewKey(t)
	bad := remote
	bad.Auth.HostKey = other
	if _, err := local.Fetch(ctx, bad, sha); !errors.Is(err, gitsource.ErrHostKey) {
		t.Fatalf("wrong host key: %v", err)
	}
	wrongClient, _, _ := gittest.NewKey(t)
	bad = remote
	bad.Auth.PrivateKey = wrongClient
	if _, err := local.Fetch(ctx, bad, sha); !errors.Is(err, gitsource.ErrAuth) {
		t.Fatalf("wrong client key: %v", err)
	} else if strings.Contains(err.Error(), "PRIVATE KEY") {
		t.Fatalf("key in error: %v", err)
	}
	noHostKey := remote
	noHostKey.Auth.HostKey = ""
	if _, err := local.Fetch(ctx, noHostKey, sha); !errors.Is(err, gitsource.ErrInvalidURL) {
		t.Fatalf("ssh without host key: %v", err)
	}
}

func TestURLAndAddressRules(t *testing.T) {
	strict := gitsource.Fetcher{}
	for _, u := range []string{
		"http://example.com/a.git",          // plain HTTP
		"https://user:pw@example.com/a.git", // credentials in URL
		"https://tok@example.com/a.git",     // token as user
		"file:///etc/passwd",                // local files
		"git://example.com/a.git",           // unauthenticated, unencrypted
		"ext::sh -c id",                     // transport helper
		"https://example.com/a.git?x=1",     // query
		"https://example.com",               // no path
		"ssh://git:pw@example.com/a.git",    // password
		"--upload-pack=touch /tmp/x",        // option-looking
	} {
		if _, err := strict.ParseURL(u); !errors.Is(err, gitsource.ErrInvalidURL) {
			t.Errorf("%q accepted: %v", u, err)
		}
	}
	for u, want := range map[string]string{
		"https://GitHub.com/o/r.git":           "github.com",
		"git@github.com:o/r.git":               "github.com",
		"ssh://deploy@10.0.0.5:2222/srv/r.git": "10.0.0.5",
	} {
		e, err := strict.ParseURL(u)
		if err != nil || e.Host != want || strings.Contains(e.URL, "@") && !e.SSH {
			t.Errorf("%q: %+v %v", u, e, err)
		}
	}
	ctx := context.Background()
	for _, host := range []string{"127.0.0.1", "169.254.169.254", "[::1]", "0.0.0.0", "[::ffff:127.0.0.1]", "[fe80::1]"} {
		_, err := strict.Resolve(ctx, gitsource.Remote{URL: "https://" + host + "/a.git"}, "main")
		if !errors.Is(err, gitsource.ErrAddress) {
			t.Errorf("%s reachable: %v", host, err)
		}
	}
	_ = netip.Addr{}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	r := gittest.NewRepo(t, "moved")
	sha := r.Commit("main", "one", map[string]string{"a": "b"})
	srv := r.ServeHTTP("")
	hop := redirector(t, srv.URL(r))
	_, err := local.Fetch(context.Background(), gitsource.Remote{URL: hop + "/moved.git", Auth: gitsource.Auth{Token: "tok-xyz"}}, sha)
	if err == nil {
		t.Fatal("a redirect was followed")
	}
	_, auth := srv.Requests()
	for _, a := range auth {
		if a != "" {
			t.Fatalf("credential reached the redirect target: %q", a)
		}
	}
}
