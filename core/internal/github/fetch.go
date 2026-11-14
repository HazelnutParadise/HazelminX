package github

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// FetchAccess is what fetching one commit of a repository needs (ticket
// 049.1, decision 0036). Token is short-lived for an installation and is
// never stored by the caller.
type FetchAccess struct {
	URL      string
	Host     string
	Token    string
	RepoID   int64
	FullName string
	Ref      string
	Commit   string
}

var fullNameRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})/[A-Za-z0-9._-]{1,100}$`)

// FetchAccess fixes ref to a commit for a repository the actor's
// connection may read and returns the clone URL with a token narrowed to
// that repository's contents (installations) or the connection's token.
func (s *Service) FetchAccess(ctx context.Context, connID string, repoID int64, ref string, actor Actor) (FetchAccess, error) {
	t, err := s.target(ctx, connID, repoID, actor)
	if err != nil {
		return FetchAccess{}, err
	}
	res, err := s.resolve(ctx, &t, ref)
	if err != nil {
		return FetchAccess{}, err
	}
	if !fullNameRE.MatchString(t.repo.FullName) || strings.Contains(t.repo.FullName, "..") {
		return FetchAccess{}, fmt.Errorf("github: unexpected repository name %q", t.repo.FullName)
	}
	u, err := url.Parse(s.clone)
	if err != nil {
		return FetchAccess{}, err
	}
	return FetchAccess{URL: s.clone + "/" + t.repo.FullName + ".git", Host: strings.ToLower(u.Hostname()), Token: t.token,
		RepoID: t.repo.ID, FullName: t.repo.FullName, Ref: res.Ref, Commit: res.Commit.SHA}, nil
}

// ErrSourceUnusable says a stored Git source's connection can no longer
// read its repository.
var ErrSourceUnusable = errors.New("github: the connection can no longer read this repository")

// SourceUsable checks, without an actor, that a connection still exists,
// is usable and still lists the repository. Deployments recheck their
// source with it (GIT-038).
func (s *Service) SourceUsable(ctx context.Context, connID string, repoID int64) error {
	c, err := s.getConn(ctx, s.db, connID)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrNotFound) {
		return fmt.Errorf("%w: the connection was removed", ErrSourceUnusable)
	}
	if err != nil {
		return err
	}
	switch c.State {
	case StateDisabled:
		return fmt.Errorf("%w: the connection is disabled", ErrSourceUnusable)
	case StateNeedsReauth:
		return fmt.Errorf("%w: the connection needs to be authorized again", ErrSourceUnusable)
	}
	if !c.coversRepository(repoID) {
		return fmt.Errorf("%w: the installation no longer includes the repository", ErrSourceUnusable)
	}
	var one int
	err = s.db.QueryRowContext(ctx, `SELECT 1 FROM github_repositories WHERE connection_id=? AND repo_id=? LIMIT 1`, c.ID, repoID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: the repository is no longer listed for the connection", ErrSourceUnusable)
	}
	return err
}
