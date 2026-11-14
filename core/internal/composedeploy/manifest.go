// Package composedeploy deploys a Compose unit through the privileged
// executor (ticket 044.2, decision 0035). A deployment is a change plan of
// kind compose.deploy that runs as a job of the same kind: the job checks
// the host (compose.check on the agent), then writes the revision and runs
// docker compose up (compose.deploy). Each step's grant is bound to the
// revision's manifest, so the executor runs exactly the content Core signed.
package composedeploy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// DigestPrefix is the first line of the manifest's digest text; the Rust
// executor (agent/crates/privileged/src/compose_manifest.rs) builds the
// same text.
const DigestPrefix = "hazelminx-compose-manifest-v1"

// ManifestFile is one file of a revision.
type ManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest is the executor's ComposeManifest; field names are its own
// (snake_case).
type Manifest struct {
	Project     string         `json:"project"`
	Revision    string         `json:"revision"`
	Environment string         `json:"environment"`
	MainFile    string         `json:"main_file"`
	ExtraFiles  []string       `json:"extra_files"`
	Profiles    []string       `json:"profiles"`
	WorkingDir  *string        `json:"working_dir,omitempty"`
	Files       []ManifestFile `json:"files"`
	// HostRiskApproved: the owner approved this content's high-risk
	// settings in a change plan; without it the executor refuses them.
	HostRiskApproved bool `json:"host_risk_approved"`
}

// StagedFile is a file's content as the agent's step receives it.
type StagedFile struct {
	SHA256  string `json:"sha256"`
	Content string `json:"content"`
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// NewManifest builds the manifest of files (path → content) and the files
// to stage. Paths are 044.1-validated; files are sorted by path.
func NewManifest(project, revision, environment, mainFile string, extra, profiles []string, workingDir string, files map[string][]byte) (Manifest, []StagedFile) {
	m := Manifest{Project: project, Revision: revision, Environment: environment, MainFile: mainFile,
		ExtraFiles: append([]string{}, extra...), Profiles: append([]string{}, profiles...)}
	if workingDir != "" {
		w := workingDir
		m.WorkingDir = &w
	}
	seen := map[string]bool{}
	var staged []StagedFile
	for p, c := range files {
		h := sum(c)
		m.Files = append(m.Files, ManifestFile{Path: p, Size: int64(len(c)), SHA256: h})
		if !seen[h] {
			seen[h] = true
			staged = append(staged, StagedFile{SHA256: h, Content: string(c)})
		}
	}
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	sort.Slice(staged, func(i, j int) bool { return staged[i].SHA256 < staged[j].SHA256 })
	return m, staged
}

// DigestText is the line-oriented text the grant binding hashes. Files are
// in byte order of their paths, as the executor sorts them.
func (m Manifest) DigestText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\nproject:%s\nrevision:%s\nenvironment:%s\nmain:%s\n", DigestPrefix, m.Project, m.Revision, m.Environment, m.MainFile)
	for _, f := range m.ExtraFiles {
		fmt.Fprintf(&b, "extra:%s\n", f)
	}
	for _, p := range m.Profiles {
		fmt.Fprintf(&b, "profile:%s\n", p)
	}
	w := ""
	if m.WorkingDir != nil {
		w = *m.WorkingDir
	}
	fmt.Fprintf(&b, "workdir:%s\n", w)
	if m.HostRiskApproved {
		b.WriteString("hostrisk:approved\n")
	} else {
		b.WriteString("hostrisk:none\n")
	}
	files := append([]ManifestFile{}, m.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	for _, f := range files {
		fmt.Fprintf(&b, "file:%s %d %s\n", f.SHA256, f.Size, f.Path)
	}
	return b.String()
}

// Digest is the hex SHA-256 of DigestText.
func (m Manifest) Digest() string { return sum([]byte(m.DigestText())) }

// Binding is the grant binding for an executor action on m: the SHA-256 of
// "<action>:<digest>", so a grant for compose_check cannot run
// compose_deploy.
func (m Manifest) Binding(action string) string { return sum([]byte(action + ":" + m.Digest())) }
