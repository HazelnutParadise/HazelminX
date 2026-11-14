package github

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// FetchAccess fixes the ref and narrows the installation token to the one
// repository; SourceUsable follows the connection without an actor (049.1).
func TestFetchAccessIsNarrowedAndSourceUsableFollowsTheConnection(t *testing.T) {
	e := newEnv(t, origin)
	inst := installationConn(t, e) // covers 3 and 9
	e.gh.addRepos(SourceInstallation, repo(3, "acme", "shop", true), repo(9, "acme", "docs", false))
	for i := 0; i < 10; i++ {
		e.gh.instTokens = append(e.gh.instTokens, Token{Value: fmt.Sprintf("ghs_%d", i), ExpiresAt: e.clock.Add(time.Hour)})
	}
	syncUntilDone(t, e, inst.ID, e.alice)
	r := e.gh.repos()
	e.gh.mu.Lock()
	r.commits["acme/shop"] = map[string]Commit{"main": {SHA: strings.Repeat("a", 40)}}
	e.gh.mu.Unlock()

	acc, err := e.svc.FetchAccess(ctx, inst.ID, 3, "main", e.alice)
	if err != nil {
		t.Fatalf("owner FetchAccess: %v", err)
	}
	t.Logf("url=%s host=%s commit=%s ref=%s tokenSet=%v", acc.URL, acc.Host, acc.Commit, acc.Ref, acc.Token != "")
	if acc.URL != "https://github.com/acme/shop.git" || acc.Commit != strings.Repeat("a", 40) {
		t.Fatalf("access: %+v", acc)
	}
	e.gh.mu.Lock()
	scope := e.gh.lastScope
	e.gh.mu.Unlock()
	t.Logf("installation token scope: %+v", scope)
	if fmt.Sprint(scope.RepositoryIDs) != "[3]" || scope.Permissions["contents"] != "read" {
		t.Fatalf("token not narrowed: %+v", scope)
	}
	// Another user cannot use alice's connection (GIT-011 ownership).
	if _, err := e.svc.FetchAccess(ctx, inst.ID, 3, "main", e.bob); err == nil {
		t.Fatal("bob used alice's connection")
	} else {
		t.Logf("bob: %v", err)
	}
	// A repo the installation does not cover.
	if _, err := e.svc.FetchAccess(ctx, inst.ID, 77, "main", e.alice); !errors.Is(err, ErrNotFound) {
		t.Fatalf("uncovered repo: %v", err)
	}
	if err := e.svc.SourceUsable(ctx, inst.ID, 3); err != nil {
		t.Fatalf("usable: %v", err)
	}
	if err := e.svc.SourceUsable(ctx, inst.ID, 77); !errors.Is(err, ErrSourceUnusable) {
		t.Fatalf("uncovered usable: %v", err)
	}
	// Installation loses repo 3.
	if _, err := e.st.DB.Exec(`UPDATE github_connections SET repository_ids='[9]' WHERE id=?`, inst.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SourceUsable(ctx, inst.ID, 3); !errors.Is(err, ErrSourceUnusable) {
		t.Fatalf("lost repo: %v", err)
	} else {
		t.Logf("lost repo: %v", err)
	}
	if _, err := e.st.DB.Exec(`UPDATE github_connections SET repository_ids='[3,9]' WHERE id=?`, inst.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetEnabled(ctx, inst.ID, false, e.alice); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SourceUsable(ctx, inst.ID, 3); !errors.Is(err, ErrSourceUnusable) {
		t.Fatalf("disabled: %v", err)
	} else {
		t.Logf("disabled: %v", err)
	}
	if _, err := e.svc.FetchAccess(ctx, inst.ID, 3, "main", e.alice); err == nil {
		t.Fatal("disabled connection still hands out fetch access")
	} else {
		t.Logf("disabled FetchAccess: %v", err)
	}
	if err := e.svc.SourceUsable(ctx, "ghc_nope", 3); !errors.Is(err, ErrSourceUnusable) {
		t.Fatalf("missing conn: %v", err)
	}
}
