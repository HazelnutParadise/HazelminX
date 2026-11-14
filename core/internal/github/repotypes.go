package github

import "time"

// Repository source kinds. A connection's repositories come from one or
// more sources, each synced with its own cursor (spec 7.4):
//   - user: repositories the token's user owns or collaborates on;
//   - org: one organization's repositories, so one organization failing
//     does not hide the others;
//   - installation: the repositories an App installation was given.
const (
	SourceUser         = "user"
	SourceOrg          = "org"
	SourceInstallation = "installation"
)

// RepoSource names one source of repositories.
type RepoSource struct {
	Kind  string `json:"kind"`
	Login string `json:"login,omitempty"` // organization login for kind org
}

func (s RepoSource) key() string {
	if s.Kind == SourceOrg {
		return SourceOrg + ":" + s.Login
	}
	return s.Kind
}

// RepoPage is one page of a repository source.
type RepoPage struct {
	Repositories []Repository
	Next         bool
	ETag         string
	NotModified  bool
	// RateRemaining is X-RateLimit-Remaining (-1 when absent) and
	// RateReset X-RateLimit-Reset, so a sync can stop before GitHub
	// refuses.
	RateRemaining int
	RateReset     time.Time
}

// Repository is what the picker shows about a repository.
type Repository struct {
	ID            int64      `json:"id"`
	FullName      string     `json:"fullName"`
	Name          string     `json:"name"`
	Owner         Account    `json:"owner"`
	Private       bool       `json:"private"`
	Visibility    string     `json:"visibility"` // public | private | internal
	Archived      bool       `json:"archived"`
	Disabled      bool       `json:"disabled"`
	Fork          bool       `json:"fork"`
	DefaultBranch string     `json:"defaultBranch"`
	HTMLURL       string     `json:"htmlUrl"`
	PushedAt      *time.Time `json:"pushedAt,omitempty"`
	// SizeKB is GitHub's size estimate; 0 often means an empty repository,
	// which only reading its commits confirms.
	SizeKB int64 `json:"sizeKb"`
	// Push and Admin are what the connection may do there, as GitHub says.
	Push  bool `json:"push"`
	Admin bool `json:"admin"`
}

// Ref is a branch or a tag and the commit it points to now.
type Ref struct {
	Name      string `json:"name"`
	SHA       string `json:"sha"`
	Protected bool   `json:"protected,omitempty"`
}

// Commit is one commit, with the first line of its message.
type Commit struct {
	SHA     string    `json:"sha"`
	Message string    `json:"message"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date"`
	HTMLURL string    `json:"htmlUrl,omitempty"`
}

// Tree is the file list of a commit. Truncated: GitHub returned only part
// of a very large tree.
type Tree struct {
	SHA       string
	Truncated bool
	Entries   []Entry
}

// Entry is one file or directory.
type Entry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"` // file | dir | symlink | submodule
	Size int64  `json:"size"`
}
