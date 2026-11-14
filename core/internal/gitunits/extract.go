package gitunits

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/HazelnutParadise/hazelminx/core/internal/gitsource"
	"github.com/HazelnutParadise/hazelminx/core/internal/projects"
)

// Refusal is why a commit cannot become a unit yet; Path is the file or
// attribute it is about.
type Refusal struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// RefusedError lists every refusal found.
type RefusedError struct{ Refusals []Refusal }

func (e *RefusedError) Error() string {
	var b strings.Builder
	b.WriteString("gitunits: the commit cannot be deployed yet:")
	for _, r := range e.Refusals {
		b.WriteString(" " + r.Path + ": " + r.Message + ";")
	}
	return b.String()
}

// LFS pointer files start with one of these (the second is the format's
// original name, still read by git-lfs).
var lfsPointers = [][]byte{[]byte("version https://git-lfs.github.com/spec/v1"), []byte("version https://hawser.github.com/spec/v1")}

// layout is what the extraction needs from the input.
type layout struct {
	composeProject string
	mainFile       string
	extraFiles     []string
	workingDir     string
	profiles       []string
}

type extracted struct {
	files []projects.FileInput
	paths []string
}

// extract takes the Compose files and the files they reference from the
// commit (decision 0036). It never reads a symbolic link, submodule or LFS
// object, and refuses a local build, so nothing deployed is missing files.
func extract(ctx context.Context, snap *gitsource.Snapshot, l layout) (*extracted, error) {
	var refused []Refusal
	refuse := func(code, p, msg string) { refused = append(refused, Refusal{Code: code, Path: p, Message: msg}) }
	got := map[string][]byte{}
	secret := map[string]bool{}
	var total int

	// inside reports a tree entry that would leave content out: a link or
	// submodule at p or at a directory above it.
	blocked := func(p string) (gitsource.Entry, bool) {
		for q := p; q != "." && q != "/" && q != ""; q = path.Dir(q) {
			if e, ok := snap.Entries[q]; ok && !e.Regular() {
				return e, true
			}
		}
		return gitsource.Entry{}, false
	}
	add := func(p string, asSecret bool) {
		if _, ok := got[p]; ok {
			secret[p] = secret[p] || asSecret
			return
		}
		if e, bad := blocked(p); bad {
			switch {
			case e.Symlink():
				refuse("symlink", e.Path, "is a symbolic link; files are taken from the commit only as regular files")
			case e.Gitlink():
				refuse("submodule", e.Path, "is a Git submodule; submodules are fetched in ticket 049.2")
			default:
				refuse("unsupported-entry", e.Path, "is not a regular file")
			}
			return
		}
		e, ok := snap.Entries[p]
		if !ok {
			refuse("file-missing", p, "is not in the commit")
			return
		}
		b, err := snap.Read(e, projects.MaxFileBytes)
		if err != nil {
			refuse("file-too-large", p, fmt.Sprintf("is larger than %d bytes", projects.MaxFileBytes))
			return
		}
		if slices.ContainsFunc(lfsPointers, func(p []byte) bool { return bytes.HasPrefix(b, p) }) {
			refuse("lfs", p, "is a Git LFS pointer; LFS objects are fetched in ticket 049.2")
			return
		}
		if !utf8.Valid(b) {
			refuse("binary", p, "is not UTF-8 text; binary files arrive with builds from Git in ticket 049.2")
			return
		}
		total += len(b)
		got[p], secret[p] = b, asSecret
	}
	// addTree adds every file under dir (a mounted directory of the repo).
	addTree := func(dir string) {
		var under []string
		for p, e := range snap.Entries {
			if dir == "." || strings.HasPrefix(p, dir+"/") {
				if !e.Regular() {
					add(p, false) // records why
					continue
				}
				under = append(under, p)
			}
		}
		sort.Strings(under)
		if len(under) > projects.MaxFiles {
			refuse("too-many-files", dir, fmt.Sprintf("holds %d files; a unit holds at most %d", len(under), projects.MaxFiles))
			return
		}
		for _, p := range under {
			add(p, false)
		}
	}
	isDir := func(p string) bool {
		if p == "." {
			return len(snap.Entries) > 0
		}
		for q := range snap.Entries {
			if strings.HasPrefix(q, p+"/") {
				return true
			}
		}
		return false
	}

	for _, f := range append([]string{l.mainFile}, l.extraFiles...) {
		add(f, false)
	}
	if len(refused) > 0 {
		return nil, &RefusedError{Refusals: refused}
	}
	spec := projects.SourceSpec{ComposeProject: l.composeProject, MainFile: l.mainFile, ExtraFiles: l.extraFiles,
		WorkingDir: l.workingDir, Profiles: l.profiles}
	for p, b := range got {
		spec.Files = append(spec.Files, projects.SourceFile{Path: p, Content: b})
	}
	an := projects.Analyze(ctx, spec)
	// inTree: p is a file or directory of the commit, or sits under a link
	// or submodule there (add then records the refusal: deploying it as an
	// empty data directory would leave the commit's content out).
	inTree := func(p string) bool {
		if _, bad := blocked(p); bad {
			return true
		}
		_, ok := snap.Entries[p]
		return ok || isDir(p)
	}
	for _, s := range an.Services {
		if s.Build != nil && !s.Build.Remote {
			refuse("build", "services."+s.Name+".build", "builds from Git come in ticket 049.2; use an image for now")
		}
		for _, m := range s.Mounts {
			// A source-file mount found only the Compose files themselves
			// (they are all the analysis was given): a directory holding
			// them is taken whole, as a checkout would have it.
			if (m.Kind != projects.MountDataDir && m.Kind != projects.MountSourceFile) || !inTree(m.Source) {
				continue
			}
			if _, ok := snap.Entries[m.Source]; ok || !isDir(m.Source) {
				add(m.Source, false)
			} else {
				addTree(m.Source)
			}
		}
		for _, ef := range s.EnvFiles {
			if inTree(ef) {
				add(ef, true)
			}
		}
	}
	for _, r := range an.Secrets {
		if r.File != "" && inTree(r.File) {
			add(r.File, true)
		}
	}
	for _, r := range an.Configs {
		if r.File != "" && inTree(r.File) {
			add(r.File, false)
		}
	}
	if len(got) > projects.MaxFiles {
		refuse("too-many-files", ".", fmt.Sprintf("%d files are needed; a unit holds at most %d", len(got), projects.MaxFiles))
	}
	if total > projects.MaxUnitBytes {
		refuse("too-large", ".", fmt.Sprintf("the needed files hold %d bytes; a unit holds at most %d", total, projects.MaxUnitBytes))
	}
	if len(refused) > 0 {
		return nil, &RefusedError{Refusals: refused}
	}
	out := &extracted{}
	for p := range got {
		out.paths = append(out.paths, p)
	}
	sort.Strings(out.paths)
	for _, p := range out.paths {
		c := string(got[p])
		out.files = append(out.files, projects.FileInput{Path: p, Content: &c, Secret: secret[p]})
	}
	return out, nil
}
