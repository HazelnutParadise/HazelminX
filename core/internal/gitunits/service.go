// Package gitunits makes Compose units whose files come from one commit of
// a Git repository (ticket 049.1, decision 0036). Core resolves the ref,
// fetches that commit into memory, takes the files Compose needs and saves
// them as an ordinary unit, so 044.1's checks and 044.2's deployment run
// unchanged. The unit's Git source is kept next to it.
package gitunits

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/github"
	"github.com/HazelnutParadise/hazelminx/core/internal/gitsource"
	"github.com/HazelnutParadise/hazelminx/core/internal/projects"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
)

// GitHub is what this package uses of the GitHub source store.
type GitHub interface {
	FetchAccess(ctx context.Context, connID string, repoID int64, ref string, actor github.Actor) (github.FetchAccess, error)
	SourceUsable(ctx context.Context, connID string, repoID int64) error
}

// Vault keeps a URL source's own credential.
type Vault interface {
	Create(ctx context.Context, in secrets.CreateInput) (secrets.Secret, error)
	Reveal(ctx context.Context, req secrets.RevealRequest) ([]byte, int, error)
	Delete(ctx context.Context, id, actor string) error
}

// Service imports and checks Git units.
type Service struct {
	DB      *sql.DB
	Units   *projects.Service
	GitHub  GitHub
	Vault   Vault
	Fetcher gitsource.Fetcher
	Now     func() time.Time
}

// Source kinds and credential kinds.
const (
	KindGitHub = "github"
	KindURL    = "url"

	AuthConnection = "connection"
	AuthNone       = "none"
	AuthToken      = "token"
	AuthSSHKey     = "ssh-key"
)

// Errors a caller maps to answers.
var (
	ErrNotGit  = errors.New("gitunits: the unit's files do not come from Git")
	ErrChanged = errors.New("gitunits: the unit's Git source changed after it was checked")
)

// Credential is a URL source's own credential, sent once; later writes
// send Unchanged to keep the stored one (only for the same URL).
type Credential struct {
	Kind       string `json:"kind"` // none | token | ssh-key
	Username   string `json:"username,omitempty"`
	Token      string `json:"token,omitempty"`
	PrivateKey string `json:"privateKey,omitempty"`
	HostKey    string `json:"hostKey,omitempty"`
	Unchanged  bool   `json:"unchanged,omitempty"`
}

// GitHubRepo names a repository through a connection.
type GitHubRepo struct {
	ConnectionID string `json:"connectionId"`
	RepoID       int64  `json:"repoId"`
}

// Input imports a unit from Git or moves it to another ref.
type Input struct {
	Name       string           `json:"name,omitempty"`
	GitHub     *GitHubRepo      `json:"github,omitempty"`
	URL        string           `json:"url,omitempty"`
	Credential *Credential      `json:"credential,omitempty"`
	Ref        string           `json:"ref"`
	MainFile   string           `json:"mainFile"`
	ExtraFiles []string         `json:"extraFiles"`
	WorkingDir string           `json:"workingDir,omitempty"`
	Profiles   []string         `json:"profiles"`
	Target     *projects.Target `json:"target"`
}

// Source is where a Git unit's files came from.
type Source struct {
	UnitID       string    `json:"unitId"`
	Kind         string    `json:"kind"`
	ConnectionID string    `json:"connectionId,omitempty"`
	RepoID       int64     `json:"repoId,omitempty"`
	RepoName     string    `json:"repoName,omitempty"`
	URL          string    `json:"url,omitempty"`
	Host         string    `json:"host"`
	Auth         string    `json:"auth"`
	Username     string    `json:"username,omitempty"`
	HostKey      string    `json:"hostKey,omitempty"`
	Ref          string    `json:"ref"`
	Commit       string    `json:"commit"`
	Files        []string  `json:"files"`
	Digest       string    `json:"digest"`
	FetchedBy    string    `json:"fetchedBy"`
	FetchedAt    time.Time `json:"fetchedAt"`
	// UnitVersion is the unit version saved with these files.
	UnitVersion int64 `json:"unitVersion"`
	secretID    string
}

// Repo is the repository as a deployment records it.
func (s *Source) Repo() string {
	if s.Kind == KindGitHub {
		return s.RepoName
	}
	return s.URL
}

// DigestText is the line-per-field text the source digest is taken of.
// The unit's files are its commit's content, so the commit and the list of
// paths pin them.
func (s *Source) DigestText() string {
	var b strings.Builder
	for _, kv := range [][2]string{{"kind", s.Kind}, {"connection", s.ConnectionID}, {"repo-id", strconv.FormatInt(s.RepoID, 10)},
		{"url", s.URL}, {"host", s.Host}, {"auth", s.Auth}, {"username", s.Username}, {"host-key", s.HostKey},
		{"ref", s.Ref}, {"commit", s.Commit}} {
		b.WriteString(kv[0] + ":" + kv[1] + "\n")
	}
	for _, f := range s.Files {
		b.WriteString("file:" + f + "\n")
	}
	return b.String()
}

func digest(s *Source) string {
	h := sha256.Sum256([]byte(s.DigestText()))
	return hex.EncodeToString(h[:])
}

// Result is what a check or an import produced.
type Result struct {
	Source   Source            `json:"source"`
	Analysis projects.Analysis `json:"analysis"`
	Unit     *projects.Unit    `json:"unit,omitempty"`
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func ghActor(a projects.Actor) github.Actor {
	return github.Actor{UserID: a.UserID, Username: a.Username, TraceID: a.TraceID, SourceIP: a.SourceIP}
}

func invalid(field, msg string) error { return &projects.ValidationError{Field: field, Message: msg} }

// fetched is one resolved and fetched source before it is saved.
type fetched struct {
	src     Source
	files   []projects.FileInput
	newCred *Credential // to store; nil keeps cur's or needs none
}

// fetch resolves, fetches and extracts. cur is the stored source when the
// unit already comes from Git.
func (s *Service) fetch(ctx context.Context, a projects.Actor, composeProject string, in Input, cur *Source) (*fetched, error) {
	if (in.GitHub == nil) == (in.URL == "") {
		return nil, invalid("github", "give either a GitHub repository (github) or a URL (url)")
	}
	if strings.ContainsAny(in.Ref, "\x00\n\r ") || len(in.Ref) > 255 || strings.HasPrefix(in.Ref, "-") {
		return nil, invalid("ref", "a branch, tag or commit SHA")
	}
	f := &fetched{src: Source{Ref: in.Ref, FetchedBy: a.UserID, FetchedAt: s.now().UTC().Truncate(time.Second)}}
	var remote gitsource.Remote
	if g := in.GitHub; g != nil {
		if in.Credential != nil {
			return nil, invalid("credential", "a GitHub repository is read with its connection's credential")
		}
		acc, err := s.GitHub.FetchAccess(ctx, g.ConnectionID, g.RepoID, in.Ref, ghActor(a))
		if err != nil {
			return nil, err
		}
		f.src.Kind, f.src.Auth, f.src.ConnectionID, f.src.RepoID, f.src.RepoName, f.src.Host, f.src.Ref, f.src.Commit =
			KindGitHub, AuthConnection, g.ConnectionID, acc.RepoID, acc.FullName, acc.Host, acc.Ref, acc.Commit
		remote = gitsource.Remote{URL: acc.URL, Auth: gitsource.Auth{Username: "x-access-token", Token: acc.Token}}
	} else {
		ep, err := s.Fetcher.ParseURL(in.URL)
		if err != nil {
			return nil, invalid("url", err.Error())
		}
		f.src.Kind, f.src.URL, f.src.Host = KindURL, ep.URL, ep.Host
		auth, err := s.credential(ctx, in, ep, cur, f)
		if err != nil {
			return nil, err
		}
		remote = gitsource.Remote{URL: ep.URL, Auth: auth}
		sha, err := s.Fetcher.Resolve(ctx, remote, in.Ref)
		if err != nil {
			return nil, err
		}
		f.src.Commit = sha
		if in.Ref == "" {
			f.src.Ref = "HEAD"
		}
	}
	snap, err := s.Fetcher.Fetch(ctx, remote, f.src.Commit)
	if err != nil {
		return nil, err
	}
	ex, err := extract(ctx, snap, layout{composeProject: composeProject, mainFile: in.MainFile, extraFiles: in.ExtraFiles,
		workingDir: in.WorkingDir, profiles: in.Profiles})
	if err != nil {
		return nil, err
	}
	f.files, f.src.Files = ex.files, ex.paths
	f.src.Digest = digest(&f.src)
	return f, nil
}

// credential checks a URL source's credential and returns it for the fetch.
func (s *Service) credential(ctx context.Context, in Input, ep gitsource.Endpoint, cur *Source, f *fetched) (gitsource.Auth, error) {
	c := in.Credential
	if c == nil {
		c = &Credential{Kind: AuthNone}
	}
	if c.Unchanged {
		if cur == nil || cur.Kind != KindURL || cur.URL != ep.URL {
			return gitsource.Auth{}, invalid("credential.unchanged", "the stored credential is kept only for the same URL; send it again")
		}
		f.src.Auth, f.src.Username, f.src.HostKey, f.src.secretID = cur.Auth, cur.Username, cur.HostKey, cur.secretID
		if c.HostKey != "" && cur.Auth == AuthSSHKey {
			f.src.HostKey = strings.TrimSpace(c.HostKey)
		}
		if cur.secretID == "" {
			return gitsource.Auth{}, nil
		}
		v, _, err := s.Vault.Reveal(ctx, secrets.RevealRequest{ID: cur.secretID, Consumer: projects.Consumer(cur.UnitID)})
		if err != nil {
			return gitsource.Auth{}, fmt.Errorf("gitunits: read the stored credential: %w", err)
		}
		return authOf(f.src.Auth, f.src.Username, v, f.src.HostKey), nil
	}
	switch c.Kind {
	case AuthNone:
		if c.Token != "" || c.PrivateKey != "" {
			return gitsource.Auth{}, invalid("credential", "kind none carries no token or key")
		}
		if ep.SSH {
			return gitsource.Auth{}, invalid("credential", "an ssh:// repository needs kind ssh-key")
		}
		f.src.Auth = AuthNone
		return gitsource.Auth{}, nil
	case AuthToken:
		if ep.SSH || c.Token == "" || c.PrivateKey != "" || len(c.Token) > 4096 || strings.ContainsAny(c.Token, "\r\n") {
			return gitsource.Auth{}, invalid("credential", "a token is sent over https:// only, as one line")
		}
		if strings.ContainsAny(c.Username, ":\r\n") || len(c.Username) > 100 {
			return gitsource.Auth{}, invalid("credential.username", "one line without ':'")
		}
		f.src.Auth, f.src.Username, f.newCred = AuthToken, c.Username, c
		return authOf(AuthToken, c.Username, []byte(c.Token), ""), nil
	case AuthSSHKey:
		if !ep.SSH || c.PrivateKey == "" || c.Token != "" || len(c.PrivateKey) > 16<<10 {
			return gitsource.Auth{}, invalid("credential", "an SSH key is used with an ssh:// URL only")
		}
		if c.HostKey == "" {
			return gitsource.Auth{}, invalid("credential.hostKey", "the server's host key is required (for example from ssh-keyscan, checked against what the server's owner publishes)")
		}
		f.src.Auth, f.src.Username, f.src.HostKey, f.newCred = AuthSSHKey, ep.User, strings.TrimSpace(c.HostKey), c
		return authOf(AuthSSHKey, ep.User, []byte(c.PrivateKey), f.src.HostKey), nil
	}
	return gitsource.Auth{}, invalid("credential.kind", "one of none, token, ssh-key")
}

func authOf(kind, user string, value []byte, hostKey string) gitsource.Auth {
	switch kind {
	case AuthToken:
		return gitsource.Auth{Username: user, Token: string(value)}
	case AuthSSHKey:
		return gitsource.Auth{Username: user, PrivateKey: value, HostKey: hostKey}
	}
	return gitsource.Auth{}
}

func unitInput(in Input, f *fetched) projects.UnitInput {
	return projects.UnitInput{Name: in.Name, Source: projects.SourceGit, MainFile: in.MainFile, ExtraFiles: in.ExtraFiles,
		WorkingDir: in.WorkingDir, Profiles: in.Profiles, Target: in.Target, Files: f.files}
}

// Check resolves and fetches the source and analyses the unit it would
// make, saving nothing.
func (s *Service) Check(ctx context.Context, a projects.Actor, projectID, envID string, in Input) (Result, error) {
	cp, cur, _, err := s.current(ctx, projectID, envID, in.Name)
	if err != nil {
		return Result{}, err
	}
	f, err := s.fetch(ctx, a, cp, in, cur)
	if err != nil {
		return Result{}, err
	}
	an, err := s.Units.CheckInput(ctx, projectID, envID, unitInput(in, f))
	if err != nil {
		return Result{}, err
	}
	return Result{Source: f.src, Analysis: an}, nil
}

// current returns the Compose project name the unit has or would get, and
// its stored Git source and version when it exists.
func (s *Service) current(ctx context.Context, projectID, envID, name string) (string, *Source, *projects.Unit, error) {
	u, err := s.Units.GetUnit(ctx, projectID, envID, false)
	if errors.Is(err, projects.ErrNotFound) {
		env, err := s.Units.GetEnvironment(ctx, projectID, envID)
		if err != nil {
			return "", nil, nil, err
		}
		p, err := s.Units.GetProject(ctx, projectID)
		if err != nil {
			return "", nil, nil, err
		}
		return p.Name + "-" + env.Name + "-" + name, nil, nil, nil
	}
	if err != nil {
		return "", nil, nil, err
	}
	if u.Source != projects.SourceGit {
		return u.ComposeProject, nil, u, nil
	}
	src, err := s.Get(ctx, u.ID)
	if err != nil {
		return "", nil, nil, err
	}
	return u.ComposeProject, src, u, nil
}

// Import creates the unit from Git (ifVersion 0) or moves an existing Git
// unit to the commit in.Ref names now (ifVersion its current version).
func (s *Service) Import(ctx context.Context, a projects.Actor, projectID, envID string, ifVersion int64, in Input) (Result, error) {
	cp, cur, u, err := s.current(ctx, projectID, envID, in.Name)
	if err != nil {
		return Result{}, err
	}
	switch {
	case ifVersion == 0 && u != nil:
		return Result{}, projects.ErrExists
	case ifVersion != 0 && u == nil:
		return Result{}, projects.ErrNotFound
	case u != nil && u.Version != ifVersion:
		return Result{}, projects.ErrVersionMismatch
	}
	unitID := projects.NewUnitID()
	if u != nil {
		unitID = u.ID
		if in.Name == "" {
			in.Name = u.Name
		}
	}
	f, err := s.fetch(ctx, a, cp, in, cur)
	if err != nil {
		return Result{}, err
	}
	f.src.UnitID = unitID
	var created string
	if c := f.newCred; c != nil {
		v := c.Token
		if c.Kind == AuthSSHKey {
			v = c.PrivateKey
		}
		sec, err := s.Vault.Create(ctx, secrets.CreateInput{Kind: secrets.KindGeneric, Name: "projects/" + unitID + "/git-credential#" + strconv.FormatInt(s.now().UnixNano(), 36),
			Value: []byte(v), Scope: []string{projects.Consumer(unitID)}, Actor: a.UserID})
		if err != nil {
			return Result{}, fmt.Errorf("gitunits: store the credential: %w", err)
		}
		created, f.src.secretID = sec.ID, sec.ID
	}
	hook := func(ctx context.Context, tx *sql.Tx, id string, version int64) error {
		if id != unitID {
			return fmt.Errorf("gitunits: unit id %s, expected %s", id, unitID)
		}
		f.src.UnitVersion = version
		return writeSource(ctx, tx, &f.src)
	}
	unit, err := s.Units.SaveGitUnit(ctx, a, projectID, envID, unitID, ifVersion, unitInput(in, f), hook)
	if err != nil {
		if created != "" {
			s.Vault.Delete(ctx, created, a.UserID)
		}
		return Result{}, err
	}
	if cur != nil && cur.secretID != "" && cur.secretID != f.src.secretID {
		s.Vault.Delete(ctx, cur.secretID, a.UserID)
	}
	an, err := s.Units.AnalyzeUnit(ctx, unit)
	if err != nil {
		return Result{}, err
	}
	return Result{Source: f.src, Analysis: an, Unit: unit}, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func writeSource(ctx context.Context, tx *sql.Tx, s *Source) error {
	files, _ := json.Marshal(s.Files)
	_, err := tx.ExecContext(ctx, `INSERT INTO compose_unit_git_sources (unit_id, kind, connection_id, repo_id, repo_name, url, host, auth,
		username, secret_id, host_key, ref, commit_sha, files, unit_version, digest, fetched_by, fetched_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT (unit_id) DO UPDATE SET kind=excluded.kind, connection_id=excluded.connection_id, repo_id=excluded.repo_id,
		repo_name=excluded.repo_name, url=excluded.url, host=excluded.host, auth=excluded.auth, username=excluded.username,
		secret_id=excluded.secret_id, host_key=excluded.host_key, ref=excluded.ref, commit_sha=excluded.commit_sha, files=excluded.files, unit_version=excluded.unit_version,
		digest=excluded.digest, fetched_by=excluded.fetched_by, fetched_at=excluded.fetched_at`,
		s.UnitID, s.Kind, s.ConnectionID, s.RepoID, s.RepoName, s.URL, s.Host, s.Auth, s.Username, nullable(s.secretID), s.HostKey,
		s.Ref, s.Commit, string(files), s.UnitVersion, s.Digest, s.FetchedBy, s.FetchedAt.UTC().Format(time.RFC3339))
	return err
}

// Get returns a unit's Git source; projects.ErrNotFound when it has none.
func (s *Service) Get(ctx context.Context, unitID string) (*Source, error) {
	var src Source
	var secret sql.NullString
	var files, at string
	err := s.DB.QueryRowContext(ctx, `SELECT unit_id, kind, connection_id, repo_id, repo_name, url, host, auth, username, secret_id,
		host_key, ref, commit_sha, files, unit_version, digest, fetched_by, fetched_at FROM compose_unit_git_sources WHERE unit_id = ?`, unitID).Scan(
		&src.UnitID, &src.Kind, &src.ConnectionID, &src.RepoID, &src.RepoName, &src.URL, &src.Host, &src.Auth, &src.Username, &secret,
		&src.HostKey, &src.Ref, &src.Commit, &files, &src.UnitVersion, &src.Digest, &src.FetchedBy, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, projects.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	src.secretID = secret.String
	if err := json.Unmarshal([]byte(files), &src.Files); err != nil {
		return nil, err
	}
	src.FetchedAt, _ = time.Parse(time.RFC3339, at)
	return &src, nil
}

// Recheck is run when a deployment of a unit version starts
// (GIT-038): a Git unit's source must still be the one saved with that
// version, and a GitHub connection must still read the repository. It
// returns nil, nil for a unit that does not come from Git.
func (s *Service) Recheck(ctx context.Context, u *projects.Unit) (*Source, error) {
	if u.Source != projects.SourceGit {
		return nil, nil
	}
	src, err := s.Get(ctx, u.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: the Git source is missing", ErrChanged)
	}
	if src.UnitVersion != u.Version {
		return nil, fmt.Errorf("%w: the unit moved to another commit", ErrChanged)
	}
	if digest(src) != src.Digest {
		return nil, fmt.Errorf("%w: the stored source does not match its digest", ErrChanged)
	}
	switch src.Kind {
	case KindGitHub:
		if err := s.GitHub.SourceUsable(ctx, src.ConnectionID, src.RepoID); err != nil {
			return nil, err
		}
	case KindURL:
		// The unit's own credential may have been revoked since: list the
		// remote's refs with it. A branch that moved does not matter; the
		// files are the saved commit's.
		var auth gitsource.Auth
		if src.secretID != "" {
			v, _, err := s.Vault.Reveal(ctx, secrets.RevealRequest{ID: src.secretID, Consumer: projects.Consumer(src.UnitID)})
			if err != nil {
				return nil, fmt.Errorf("gitunits: read the stored credential: %w", err)
			}
			auth = authOf(src.Auth, src.Username, v, src.HostKey)
		}
		ref := src.Ref
		if IsSHA(ref) {
			ref = "HEAD"
		}
		if _, err := s.Fetcher.Resolve(ctx, gitsource.Remote{URL: src.URL, Auth: auth}, ref); err != nil && !errors.Is(err, gitsource.ErrNotFound) {
			return nil, fmt.Errorf("the repository can no longer be read with the stored credential: %w", err)
		}
	}
	return src, nil
}

// IsSHA says whether ref is a full commit SHA.
func IsSHA(ref string) bool { return gitsource.IsCommit(ref) }
