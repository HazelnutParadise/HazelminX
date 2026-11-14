package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ghRepo is GitHub's repository JSON.
type ghRepo struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	FullName      string     `json:"full_name"`
	Owner         Account    `json:"owner"`
	Private       bool       `json:"private"`
	Visibility    string     `json:"visibility"`
	Archived      bool       `json:"archived"`
	Disabled      bool       `json:"disabled"`
	Fork          bool       `json:"fork"`
	DefaultBranch string     `json:"default_branch"`
	HTMLURL       string     `json:"html_url"`
	PushedAt      *time.Time `json:"pushed_at"`
	Size          int64      `json:"size"`
	Permissions   struct {
		Admin bool `json:"admin"`
		Push  bool `json:"push"`
	} `json:"permissions"`
}

func (r ghRepo) repository() Repository {
	vis := r.Visibility
	if vis == "" {
		vis = "public"
		if r.Private {
			vis = "private"
		}
	}
	return Repository{ID: r.ID, FullName: r.FullName, Name: r.Name, Owner: r.Owner, Private: r.Private, Visibility: vis,
		Archived: r.Archived, Disabled: r.Disabled, Fork: r.Fork, DefaultBranch: r.DefaultBranch, HTMLURL: r.HTMLURL,
		PushedAt: r.PushedAt, SizeKB: r.Size, Push: r.Permissions.Push, Admin: r.Permissions.Admin}
}

// hasNext reports whether a Link header announces rel="next".
func hasNext(h http.Header) bool {
	for _, part := range strings.Split(h.Get("Link"), ",") {
		if strings.Contains(part, `rel="next"`) {
			return true
		}
	}
	return false
}

// repoPath escapes each part of owner/name.
func repoPath(fullName string) string {
	owner, name, _ := strings.Cut(fullName, "/")
	return url.PathEscape(owner) + "/" + url.PathEscape(name)
}

// escapeSegments escapes each /-separated part, keeping the slashes
// (branch names and paths may contain them).
func escapeSegments(p string) string {
	parts := strings.Split(p, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func (c *HTTPClient) ListUserOrgs(ctx context.Context, token string) ([]Account, error) {
	var all []Account
	for page := 1; page <= 20; page++ {
		var out []Account
		h, err := c.call(ctx, http.MethodGet, fmt.Sprintf("%s/user/orgs?per_page=100&page=%d", c.APIBase, page), bearer(token), nil, nil, &out)
		if err != nil {
			return nil, err
		}
		all = append(all, out...)
		if !hasNext(h) {
			return all, nil
		}
	}
	return nil, &APIError{Kind: ErrUnavailable, Reason: "more than 2000 organizations; list not complete"}
}

func (c *HTTPClient) ListRepositories(ctx context.Context, token string, src RepoSource, page int, etag string) (RepoPage, error) {
	var u string
	switch src.Kind {
	case SourceUser:
		u = c.APIBase + "/user/repos?affiliation=owner,collaborator&sort=full_name"
	case SourceOrg:
		u = c.APIBase + "/orgs/" + url.PathEscape(src.Login) + "/repos?type=all&sort=full_name"
	case SourceInstallation:
		u = c.APIBase + "/installation/repositories?"
	default:
		return RepoPage{}, fmt.Errorf("github: unknown repository source %q", src.Kind)
	}
	u += fmt.Sprintf("&per_page=100&page=%d", page)
	var extra http.Header
	if etag != "" {
		extra = http.Header{"If-None-Match": {etag}}
	}
	var list []ghRepo
	var inst struct {
		Repositories []ghRepo `json:"repositories"`
	}
	var out any = &list
	if src.Kind == SourceInstallation {
		out = &inst
	}
	h, status, err := c.callH(ctx, http.MethodGet, u, bearer(token), nil, nil, extra, out)
	if err != nil {
		return RepoPage{}, err
	}
	p := RepoPage{Next: hasNext(h), ETag: h.Get("ETag"), NotModified: status == http.StatusNotModified, RateRemaining: -1}
	if v, err := strconv.Atoi(h.Get("X-RateLimit-Remaining")); err == nil {
		p.RateRemaining = v
	}
	if v, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		p.RateReset = time.Unix(v, 0).UTC()
	}
	if src.Kind == SourceInstallation {
		list = inst.Repositories
	}
	for _, r := range list {
		p.Repositories = append(p.Repositories, r.repository())
	}
	return p, nil
}

func (c *HTTPClient) GetRepositoryByID(ctx context.Context, token string, id int64) (Repository, error) {
	var out ghRepo
	_, err := c.call(ctx, http.MethodGet, fmt.Sprintf("%s/repositories/%d", c.APIBase, id), bearer(token), nil, nil, &out)
	return out.repository(), err
}

func (c *HTTPClient) ListRefs(ctx context.Context, token, fullName, kind string, page int) ([]Ref, bool, error) {
	if kind != "branches" && kind != "tags" {
		return nil, false, fmt.Errorf("github: unknown ref kind %q", kind)
	}
	var out []struct {
		Name   string `json:"name"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
		Protected bool `json:"protected"`
	}
	h, err := c.call(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s?per_page=100&page=%d", c.APIBase, repoPath(fullName), kind, page), bearer(token), nil, nil, &out)
	if err != nil {
		return nil, false, err
	}
	refs := make([]Ref, 0, len(out))
	for _, r := range out {
		refs = append(refs, Ref{Name: r.Name, SHA: r.Commit.SHA, Protected: r.Protected})
	}
	return refs, hasNext(h), nil
}

type ghCommit struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Commit  struct {
		Message string `json:"message"`
		Author  struct {
			Name string    `json:"name"`
			Date time.Time `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

func (g ghCommit) commit() Commit {
	msg, _, _ := strings.Cut(g.Commit.Message, "\n")
	return Commit{SHA: g.SHA, Message: msg, Author: g.Commit.Author.Name, Date: g.Commit.Author.Date.UTC(), HTMLURL: g.HTMLURL}
}

func (c *HTTPClient) ListCommits(ctx context.Context, token, fullName, ref string, page int) ([]Commit, bool, error) {
	u := fmt.Sprintf("%s/repos/%s/commits?per_page=100&page=%d", c.APIBase, repoPath(fullName), page)
	if ref != "" {
		u += "&sha=" + url.QueryEscape(ref)
	}
	var out []ghCommit
	h, err := c.call(ctx, http.MethodGet, u, bearer(token), nil, nil, &out)
	if err != nil {
		return nil, false, err
	}
	commits := make([]Commit, 0, len(out))
	for _, g := range out {
		commits = append(commits, g.commit())
	}
	return commits, hasNext(h), nil
}

func (c *HTTPClient) GetCommit(ctx context.Context, token, fullName, ref string) (Commit, error) {
	var out ghCommit
	_, err := c.call(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s/commits/%s", c.APIBase, repoPath(fullName), escapeSegments(ref)), bearer(token), nil, nil, &out)
	return out.commit(), err
}

func (c *HTTPClient) GetTree(ctx context.Context, token, fullName, sha string) (Tree, error) {
	var out struct {
		SHA       string `json:"sha"`
		Truncated bool   `json:"truncated"`
		Tree      []struct {
			Path string `json:"path"`
			Type string `json:"type"` // blob | tree | commit
			Mode string `json:"mode"`
			Size int64  `json:"size"`
		} `json:"tree"`
	}
	_, err := c.call(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s/git/trees/%s?recursive=1", c.APIBase, repoPath(fullName), url.PathEscape(sha)), bearer(token), nil, nil, &out)
	if err != nil {
		return Tree{}, err
	}
	t := Tree{SHA: out.SHA, Truncated: out.Truncated}
	for _, e := range out.Tree {
		typ := "file"
		switch {
		case e.Type == "tree":
			typ = "dir"
		case e.Type == "commit":
			typ = "submodule"
		case e.Mode == "120000":
			typ = "symlink"
		}
		name := e.Path[strings.LastIndex(e.Path, "/")+1:]
		t.Entries = append(t.Entries, Entry{Name: name, Path: e.Path, Type: typ, Size: e.Size})
	}
	return t, nil
}

func (c *HTTPClient) ListDirectory(ctx context.Context, token, fullName, path, sha string) ([]Entry, error) {
	var out []Entry
	u := fmt.Sprintf("%s/repos/%s/contents/%s?ref=%s", c.APIBase, repoPath(fullName), escapeSegments(strings.Trim(path, "/")), url.QueryEscape(sha))
	if _, err := c.call(ctx, http.MethodGet, u, bearer(token), nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
