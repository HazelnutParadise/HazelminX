package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// fakeRepos is the repository side of fakeGitHub. Sources are keyed by
// RepoSource.key(); pages hold pageSize repositories.
type fakeRepos struct {
	pageSize   int
	orgs       map[string][]Account         // token → organizations
	orgsErr    error                        // ListUserOrgs answers this
	sources    map[string][]Repository      // source key → every repository
	pageErr    map[string]error             // "key#page" → error for that page
	rate       map[string]int               // "key#page" → X-RateLimit-Remaining sent
	refused    map[string]bool              // tokens GitHub no longer accepts
	byID       map[int64]Repository         // current state, by id
	moved      map[string]bool              // old full names answering 301
	empty      map[string]bool              // full names with no commits
	refs       map[string]map[string][]Ref  // full name → kind → refs
	commits    map[string]map[string]Commit // full name → ref or sha → commit
	history    map[string][]Commit          // full name → commits newest first
	trees      map[string]Tree              // sha → tree
	dirs       map[string][]Entry           // sha + ":" + path → entries
	lastTokens []string                     // tokens the repository calls used
	lastPages  []string                     // "key#page etag" asked
}

func (f *fakeGitHub) repos() *fakeRepos {
	if f.r == nil {
		f.r = &fakeRepos{pageSize: 100, orgs: map[string][]Account{}, sources: map[string][]Repository{}, pageErr: map[string]error{},
			rate: map[string]int{}, refused: map[string]bool{}, byID: map[int64]Repository{}, moved: map[string]bool{}, empty: map[string]bool{},
			refs: map[string]map[string][]Ref{}, commits: map[string]map[string]Commit{}, history: map[string][]Commit{},
			trees: map[string]Tree{}, dirs: map[string][]Entry{}}
	}
	return f.r
}

// addRepos puts repositories into a source and makes them readable by id.
func (f *fakeGitHub) addRepos(key string, rs ...Repository) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.repos()
	r.sources[key] = append(r.sources[key], rs...)
	for _, x := range rs {
		r.byID[x.ID] = x
	}
}

func (f *fakeGitHub) repoCall(name, token string) (*fakeRepos, error) {
	if err := f.enter(name); err != nil {
		return nil, err
	}
	r := f.repos()
	r.lastTokens = append(r.lastTokens, token)
	if r.refused[token] {
		return nil, refused(401, "Bad credentials")
	}
	return r, nil
}

func (f *fakeGitHub) ListUserOrgs(_ context.Context, token string) ([]Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, err := f.repoCall("ListUserOrgs", token)
	if err != nil {
		return nil, err
	}
	if r.orgsErr != nil {
		return nil, r.orgsErr
	}
	return r.orgs[token], nil
}

func etagOf(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return `W/"` + hex.EncodeToString(sum[:8]) + `"`
}

func (f *fakeGitHub) ListRepositories(_ context.Context, token string, src RepoSource, page int, etag string) (RepoPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, err := f.repoCall("ListRepositories", token)
	if err != nil {
		return RepoPage{}, err
	}
	k := fmt.Sprintf("%s#%d", src.key(), page)
	r.lastPages = append(r.lastPages, k+" "+etag)
	if e := r.pageErr[k]; e != nil {
		return RepoPage{}, e
	}
	all := r.sources[src.key()]
	from, to := (page-1)*r.pageSize, page*r.pageSize
	from, to = min(from, len(all)), min(to, len(all))
	items := append([]Repository(nil), all[from:to]...)
	p := RepoPage{Next: to < len(all), ETag: etagOf(items), RateRemaining: 4000}
	if v, ok := r.rate[k]; ok {
		p.RateRemaining, p.RateReset = v, f.clockNow().Add(20*60e9)
	}
	if etag != "" && etag == p.ETag {
		// GitHub answers 304 with no body; whether more pages follow is
		// not reliably there either.
		return RepoPage{NotModified: true, ETag: etag, RateRemaining: p.RateRemaining, RateReset: p.RateReset}, nil
	}
	f.calls["ListRepositories:200"]++
	p.Repositories = items
	return p, nil
}

func (f *fakeGitHub) GetRepositoryByID(_ context.Context, token string, id int64) (Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, err := f.repoCall("GetRepositoryByID", token)
	if err != nil {
		return Repository{}, err
	}
	x, ok := r.byID[id]
	if !ok {
		return Repository{}, refused(404, "Not Found")
	}
	return x, nil
}

func (r *fakeRepos) at(name string) error {
	if r.moved[name] {
		return &APIError{Kind: ErrRejected, Status: 301, Reason: "Moved Permanently"}
	}
	if r.empty[name] {
		return refused(409, "Git Repository is empty.")
	}
	return nil
}

func (f *fakeGitHub) ListRefs(_ context.Context, token, fullName, kind string, page int) ([]Ref, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, err := f.repoCall("ListRefs", token)
	if err != nil {
		return nil, false, err
	}
	if r.moved[fullName] {
		return nil, false, r.at(fullName)
	}
	all := r.refs[fullName][kind]
	from, to := min((page-1)*r.pageSize, len(all)), min(page*r.pageSize, len(all))
	return append([]Ref(nil), all[from:to]...), to < len(all), nil
}

func (f *fakeGitHub) ListCommits(_ context.Context, token, fullName, ref string, page int) ([]Commit, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, err := f.repoCall("ListCommits", token)
	if err != nil {
		return nil, false, err
	}
	if err := r.at(fullName); err != nil {
		return nil, false, err
	}
	if _, ok := r.commits[fullName][ref]; ref != "" && !ok {
		return nil, false, refused(404, "No commit found for SHA: "+ref)
	}
	all := r.history[fullName]
	from, to := min((page-1)*r.pageSize, len(all)), min(page*r.pageSize, len(all))
	return append([]Commit(nil), all[from:to]...), to < len(all), nil
}

func (f *fakeGitHub) GetCommit(_ context.Context, token, fullName, ref string) (Commit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, err := f.repoCall("GetCommit", token)
	if err != nil {
		return Commit{}, err
	}
	if err := r.at(fullName); err != nil {
		return Commit{}, err
	}
	c, ok := r.commits[fullName][ref]
	if !ok {
		return Commit{}, refused(422, "No commit found for SHA: "+ref)
	}
	return c, nil
}

func (f *fakeGitHub) GetTree(_ context.Context, token, fullName, sha string) (Tree, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, err := f.repoCall("GetTree", token)
	if err != nil {
		return Tree{}, err
	}
	if err := r.at(fullName); err != nil {
		return Tree{}, err
	}
	t, ok := r.trees[sha]
	if !ok {
		return Tree{}, refused(404, "Not Found")
	}
	return t, nil
}

func (f *fakeGitHub) ListDirectory(_ context.Context, token, fullName, path, sha string) ([]Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, err := f.repoCall("ListDirectory", token)
	if err != nil {
		return nil, err
	}
	if err := r.at(fullName); err != nil {
		return nil, err
	}
	es, ok := r.dirs[sha+":"+path]
	if !ok {
		return nil, refused(404, "Not Found")
	}
	return es, nil
}

// files builds a tree from paths.
func files(sha string, paths ...string) Tree {
	t := Tree{SHA: sha}
	for _, p := range paths {
		t.Entries = append(t.Entries, Entry{Name: p[strings.LastIndex(p, "/")+1:], Path: p, Type: "file"})
	}
	return t
}
