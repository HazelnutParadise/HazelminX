package github

import (
	"context"
	"errors"
	"path"
	"sort"
)

// Compose scan results.
const (
	ComposeEmptyRepository = "empty-repository" // no commits yet
	ComposeNone            = "none"             // no Compose file
	ComposeOne             = "one"              // one project directory
	ComposeSeveral         = "several"          // more than one; the user picks
)

// composeBase lists the names Docker Compose looks for, in the order it
// prefers them; composeOverride the override files it adds after the base.
var (
	composeBase     = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}
	composeOverride = []string{"compose.override.yaml", "compose.override.yml", "docker-compose.override.yaml", "docker-compose.override.yml"}
)

// ComposeProject is one directory that holds a Compose project.
type ComposeProject struct {
	// Directory is the working directory, relative to the repository root
	// ("." for the root).
	Directory string `json:"directory"`
	// Files is what `docker compose` in Directory reads by default, in
	// order: the preferred base file, then its override file if any.
	Files []string `json:"files"`
	// Ignored are other base files in the same directory that Compose does
	// not read when the preferred one exists; shown so the user is not
	// surprised.
	Ignored []string `json:"ignored,omitempty"`
}

// ComposeScan is the result of looking for Compose files at one commit.
type ComposeScan struct {
	Resolved
	State    string           `json:"state"`
	Projects []ComposeProject `json:"projects"`
	// Truncated: GitHub returned only part of the file tree, so files may
	// be missing from Projects.
	Truncated bool `json:"truncated"`
}

// FindComposeFiles fixes ref to a commit and lists every directory with a
// Compose file there.
func (s *Service) FindComposeFiles(ctx context.Context, connID string, repoID int64, ref string, actor Actor) (ComposeScan, error) {
	t, err := s.target(ctx, connID, repoID, actor)
	if err != nil {
		return ComposeScan{}, err
	}
	res, err := s.resolve(ctx, &t, ref)
	if errors.Is(err, ErrEmptyRepository) {
		return ComposeScan{Resolved: Resolved{Ref: ref, DefaultBranch: t.repo.DefaultBranch}, State: ComposeEmptyRepository, Projects: []ComposeProject{}}, nil
	}
	if err != nil {
		return ComposeScan{}, err
	}
	var tree Tree
	err = s.gh(ctx, &t, func(name string) error {
		var e error
		tree, e = s.client.GetTree(ctx, t.token, name, res.Commit.SHA)
		return e
	})
	if err != nil {
		return ComposeScan{}, err
	}
	scan := ComposeScan{Resolved: res, Truncated: tree.Truncated, Projects: composeProjects(tree.Entries)}
	switch len(scan.Projects) {
	case 0:
		scan.State = ComposeNone
	case 1:
		scan.State = ComposeOne
	default:
		scan.State = ComposeSeveral
	}
	return scan, nil
}

// composeProjects groups the Compose files of a tree by directory, the
// root first, then by path.
func composeProjects(entries []Entry) []ComposeProject {
	byDir := map[string]map[string]bool{}
	for _, e := range entries {
		if e.Type != "file" && e.Type != "symlink" {
			continue
		}
		dir := path.Dir(e.Path)
		if byDir[dir] == nil {
			byDir[dir] = map[string]bool{}
		}
		byDir[dir][e.Name] = true
	}
	var out []ComposeProject
	for dir, names := range byDir {
		var p ComposeProject
		for _, n := range composeBase {
			if !names[n] {
				continue
			}
			if p.Files == nil {
				p.Files = []string{join(dir, n)}
			} else {
				p.Ignored = append(p.Ignored, join(dir, n))
			}
		}
		if p.Files == nil {
			continue
		}
		for _, n := range composeOverride {
			if names[n] {
				p.Files = append(p.Files, join(dir, n))
				break
			}
		}
		p.Directory = dir
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Directory == ".") != (out[j].Directory == ".") {
			return out[i].Directory == "."
		}
		return out[i].Directory < out[j].Directory
	})
	if out == nil {
		out = []ComposeProject{}
	}
	return out
}

func join(dir, name string) string {
	if dir == "." {
		return name
	}
	return dir + "/" + name
}
