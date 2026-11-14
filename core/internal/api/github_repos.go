package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/HazelnutParadise/hazelminx/core/internal/github"
)

// Repository picker (ticket 048.1, spec 7.4 and 15). Repositories are read
// through one connection the caller owns. The merged list names the
// connection of every entry; a repository reached through two connections
// is two entries, and browsing always uses the connection named in the
// path, never another one.

var githubRepositoriesList = ListSpec{Name: "github-repositories", Sorts: []string{"fullName", "pushedAt"},
	DefaultSort: []SortKey{{Field: "fullName"}}, TieBreaker: "id",
	Filters: []FilterSpec{
		{Name: "connectionId", Summary: "only repositories read through this connection"},
		{Name: "q", Summary: "owner/name contains this text, ignoring case"},
		{Name: "owner", Summary: "only repositories of this owner login"},
		{Name: "visibility", Summary: "only repositories with this visibility", Enum: []string{"public", "private", "internal"}},
		{Name: "archived", Summary: "only archived (true) or not archived (false) repositories", Enum: []string{"true", "false"}},
	}}

type githubSyncResponse struct {
	Sync github.SyncStatus `json:"sync"`
}

type githubRepositoryResponse struct {
	Repository github.RepoItem `json:"repository"`
}

type githubResolvedResponse struct {
	Resolved github.Resolved `json:"resolved"`
}

type githubDirectoryResponse struct {
	Directory github.Directory `json:"directory"`
}

type githubComposeResponse struct {
	Compose github.ComposeScan `json:"compose"`
}

// githubPagedResponse is one GitHub page of branches, tags or commits.
// page.nextCursor is the cursor of the next page, absent on the last.
type githubPagedResponse[T any] struct {
	Items []T                 `json:"items"`
	Page  githubPagedPageMeta `json:"page"`
}

type githubPagedPageMeta struct {
	Limit      int    `json:"limit"`
	NextCursor string `json:"nextCursor,omitempty"`
}

func (h *githubRoutes) mountRepos(g gin.IRoutes, base string, op func(Operation) Operation, use gin.HandlerFunc, usePerm string) {
	s := h.s
	gh := []string{CodeGitHubRejected, CodeGitHubUnavailable, CodeGitHubReauthRequired}
	repo := base + "/github/connections/{id}/repositories/{repoId}"
	cursor := Param{Name: "cursor", In: "query", Description: "page.nextCursor of the previous answer; GitHub pages hold 100 items", Schema: jsonSchema{"type": "string"}}
	ref := Param{Name: "ref", In: "query", Description: "branch, tag or commit SHA; empty means the repository's current default branch", Schema: jsonSchema{"type": "string"}}

	s.handle(g, base, op(Operation{Method: "GET", Path: base + "/github/repositories", ID: "listGitHubRepositories",
		Summary:    "repositories of the caller's connections as last synced: each entry names its connection, visibility, archive state and what the connection may do there; the same repository through two connections is two entries; read each connection's sync to know whether the list is complete",
		Permission: usePerm, List: &githubRepositoriesList,
		Responses: []Response{{Status: 200, Body: ListResponse[github.RepoItem]{}}}}), use, h.listRepositories)
	s.handle(g, base, op(Operation{Method: "GET", Path: base + "/github/connections/{id}/repositories/sync", ID: "getGitHubRepositorySync",
		Summary:    "inventory state of one connection: complete only when every source (own and collaborator repositories, each organization, or the installation) was read to its end; partial, rate-limited and syncing are told apart, per source, with retryAt",
		Permission: usePerm, Responses: []Response{{Status: 200, Body: githubSyncResponse{}}}}), use, h.repositorySync)
	s.handle(g, base, op(Operation{Method: "POST", Path: base + "/github/connections/{id}/repositories/sync", ID: "syncGitHubRepositories",
		Summary:    "run one sync step: read at most 10 GitHub pages from the sources not finished, each from its own cursor, reusing ETags; when all are finished, start a new round; repeat while state is syncing; while rate-limited no call is made before retryAt and no other connection is used",
		Permission: usePerm, Responses: []Response{{Status: 200, Body: githubSyncResponse{}}}, Errors: gh}), use, h.syncRepositories)
	s.handle(g, base, op(Operation{Method: "GET", Path: repo, ID: "getGitHubRepository",
		Summary:    "read the repository through this connection now (current name, default branch, archive state) and update the inventory; only repositories in this connection's inventory are reachable",
		Permission: usePerm, Responses: []Response{{Status: 200, Body: githubRepositoryResponse{}}}, Errors: gh}), use, h.getRepository)
	s.handle(g, base, op(Operation{Method: "GET", Path: repo + "/branches", ID: "listGitHubBranches",
		Summary: "branches with the commit each points to now; an empty repository has none", Permission: usePerm, Params: []Param{cursor},
		Responses: []Response{{Status: 200, Body: githubPagedResponse[github.Ref]{}}}, Errors: append([]string{CodeValidationFailed}, gh...)}), use, h.refs("branches"))
	s.handle(g, base, op(Operation{Method: "GET", Path: repo + "/tags", ID: "listGitHubTags",
		Summary: "tags with their commits", Permission: usePerm, Params: []Param{cursor},
		Responses: []Response{{Status: 200, Body: githubPagedResponse[github.Ref]{}}}, Errors: append([]string{CodeValidationFailed}, gh...)}), use, h.refs("tags"))
	s.handle(g, base, op(Operation{Method: "GET", Path: repo + "/commits", ID: "listGitHubCommits",
		Summary: "commits reachable from ref, newest first", Permission: usePerm, Params: []Param{ref, cursor},
		Responses: []Response{{Status: 200, Body: githubPagedResponse[github.Commit]{}}}, Errors: append([]string{CodeValidationFailed, CodeGitHubRepoEmpty}, gh...)}), use, h.commits)
	s.handle(g, base, op(Operation{Method: "GET", Path: repo + "/resolve", ID: "resolveGitHubRef",
		Summary:    "fix ref to the commit it points to now; a deployment keeps this SHA, so a branch that moves later does not change it",
		Permission: usePerm, Params: []Param{ref},
		Responses: []Response{{Status: 200, Body: githubResolvedResponse{}}}, Errors: append([]string{CodeValidationFailed, CodeGitHubRepoEmpty}, gh...)}), use, h.resolve)
	s.handle(g, base, op(Operation{Method: "GET", Path: repo + "/contents", ID: "listGitHubDirectory",
		Summary:    "one directory at ref (fixed to a commit first), directories first",
		Permission: usePerm, Params: []Param{ref, {Name: "path", In: "query", Description: "directory relative to the repository root; empty for the root", Schema: jsonSchema{"type": "string"}}},
		Responses: []Response{{Status: 200, Body: githubDirectoryResponse{}}}, Errors: append([]string{CodeValidationFailed, CodeGitHubRepoEmpty}, gh...)}), use, h.directory)
	s.handle(g, base, op(Operation{Method: "GET", Path: repo + "/compose-files", ID: "findGitHubComposeFiles",
		Summary:    "Compose projects at ref (fixed to a commit): state empty-repository, none, one or several; each project gives its working directory and the files docker compose reads there by default, in order (base, then override), and the base files it would ignore",
		Permission: usePerm, Params: []Param{ref},
		Responses: []Response{{Status: 200, Body: githubComposeResponse{}}}, Errors: append([]string{CodeValidationFailed}, gh...)}), use, h.compose)
}

func (h *githubRoutes) listRepositories(c *gin.Context) {
	q, perr := parseListQuery(c, withListDefaults(githubRepositoriesList))
	if perr != nil {
		respond(c, *perr)
		return
	}
	first := func(k string) string {
		if v := q.Filters[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	f := github.RepoFilter{ConnectionID: first("connectionId"), Query: first("q"), Owner: first("owner"), Visibility: first("visibility")}
	if v := first("archived"); v != "" {
		b := v == "true"
		f.Archived = &b
	}
	rows, err := h.svc.ListRepositories(c.Request.Context(), githubActor(c), f)
	if err != nil {
		h.fail(c, err)
		return
	}
	runList[github.RepoItem](c, githubRepositoriesList, memorySource[github.RepoItem]{rows: rows, value: func(r github.RepoItem, field string) string {
		switch field {
		case "fullName":
			return r.FullName
		case "pushedAt":
			if r.PushedAt != nil {
				return r.PushedAt.UTC().Format(sortTime)
			}
			return ""
		}
		return r.ConnectionID + "#" + strconv.FormatInt(r.ID, 10)
	}})
}

func (h *githubRoutes) repositorySync(c *gin.Context) {
	st, err := h.svc.RepositorySync(c.Request.Context(), c.Param("id"), githubActor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, githubSyncResponse{Sync: st})
}

func (h *githubRoutes) syncRepositories(c *gin.Context) {
	ctx, cancel := githubCtx(c)
	defer cancel()
	st, err := h.svc.SyncRepositories(ctx, c.Param("id"), githubActor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, githubSyncResponse{Sync: st})
}

// repoID reads {repoId}; anything but a positive integer is not found.
func (h *githubRoutes) repoID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("repoId"), 10, 64)
	if err != nil || id <= 0 {
		notFound(c)
		return 0, false
	}
	return id, true
}

// page reads the cursor of a GitHub-paged list.
func pageOf(c *gin.Context) (int, bool) {
	v := c.Query("cursor")
	if v == "" {
		return 1, true
	}
	p, err := strconv.Atoi(v)
	if err != nil || p < 2 || p > 10000 {
		e := newError(CodeValidationFailed, "cursor must be the page.nextCursor of a previous answer")
		e.Fields = []FieldError{{Field: "cursor", Message: "not a cursor of this list"}}
		respond(c, e)
		return 0, false
	}
	return p, true
}

func paged[T any](c *gin.Context, page int, p github.RefPage[T]) {
	meta := githubPagedPageMeta{Limit: 100}
	if p.Next {
		meta.NextCursor = strconv.Itoa(page + 1)
	}
	c.JSON(http.StatusOK, githubPagedResponse[T]{Items: p.Items, Page: meta})
}

func (h *githubRoutes) getRepository(c *gin.Context) {
	id, ok := h.repoID(c)
	if !ok {
		return
	}
	ctx, cancel := githubCtx(c)
	defer cancel()
	r, err := h.svc.GetRepository(ctx, c.Param("id"), id, githubActor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, githubRepositoryResponse{Repository: r})
}

func (h *githubRoutes) refs(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := h.repoID(c)
		if !ok {
			return
		}
		page, ok := pageOf(c)
		if !ok {
			return
		}
		ctx, cancel := githubCtx(c)
		defer cancel()
		p, err := h.svc.ListRefs(ctx, c.Param("id"), id, kind, page, githubActor(c))
		if err != nil {
			h.fail(c, err)
			return
		}
		paged(c, page, p)
	}
}

func (h *githubRoutes) commits(c *gin.Context) {
	id, ok := h.repoID(c)
	if !ok {
		return
	}
	page, ok := pageOf(c)
	if !ok {
		return
	}
	ctx, cancel := githubCtx(c)
	defer cancel()
	p, err := h.svc.ListCommits(ctx, c.Param("id"), id, c.Query("ref"), page, githubActor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	paged(c, page, p)
}

func (h *githubRoutes) resolve(c *gin.Context) {
	id, ok := h.repoID(c)
	if !ok {
		return
	}
	ctx, cancel := githubCtx(c)
	defer cancel()
	r, err := h.svc.ResolveRef(ctx, c.Param("id"), id, c.Query("ref"), githubActor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, githubResolvedResponse{Resolved: r})
}

func (h *githubRoutes) directory(c *gin.Context) {
	id, ok := h.repoID(c)
	if !ok {
		return
	}
	ctx, cancel := githubCtx(c)
	defer cancel()
	d, err := h.svc.ListDirectory(ctx, c.Param("id"), id, c.Query("ref"), c.Query("path"), githubActor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, githubDirectoryResponse{Directory: d})
}

func (h *githubRoutes) compose(c *gin.Context) {
	id, ok := h.repoID(c)
	if !ok {
		return
	}
	ctx, cancel := githubCtx(c)
	defer cancel()
	scan, err := h.svc.FindComposeFiles(ctx, c.Param("id"), id, c.Query("ref"), githubActor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, githubComposeResponse{Compose: scan})
}
