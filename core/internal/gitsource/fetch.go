// Package gitsource fetches one commit of a Git repository into memory and
// reads its tree as data (decision 0036). Nothing is checked out: no
// filter, hook, repository configuration, submodule or symbolic link is
// ever acted on. Credentials go only into the request's authentication;
// errors are cleaned of them before they leave the package.
package gitsource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/proxy"
)

// Errors a caller tells apart.
var (
	ErrInvalidURL    = errors.New("gitsource: invalid repository URL")
	ErrAddress       = errors.New("gitsource: address not allowed")
	ErrAuth          = errors.New("gitsource: the repository refused the credential")
	ErrNotFound      = errors.New("gitsource: no such repository, ref or commit")
	ErrTooLarge      = errors.New("gitsource: the commit is larger than the fetch limit")
	ErrHostKey       = errors.New("gitsource: the SSH host key does not match the one configured")
	ErrUnavailable   = errors.New("gitsource: the repository could not be reached")
	ErrCommitChanged = errors.New("gitsource: the fetched commit is not the one asked for")
	ErrInvalidTree   = errors.New("gitsource: the commit's tree holds a path Git does not allow")
)

// Limits of one fetch.
const (
	DefaultTimeout  = 60 * time.Second
	DefaultMaxBytes = 64 << 20 // inflated Git objects held in memory
)

// Auth is how Core authenticates to the repository. At most one of Token
// and PrivateKey is set.
type Auth struct {
	// Username goes with Token over HTTPS ("x-access-token" for GitHub);
	// for SSH it is the user name ("git" when empty).
	Username string
	Token    string
	// PrivateKey is a PEM or OpenSSH private key; HostKey is the one host
	// key accepted, in authorized_keys form. SSH needs both.
	PrivateKey []byte
	HostKey    string
}

// Remote is one repository.
type Remote struct {
	URL  string
	Auth Auth
}

// Fetcher fetches commits. The zero value uses the default limits and
// refuses loopback, link-local and other internal addresses.
type Fetcher struct {
	Timeout  time.Duration
	MaxBytes int64
	// AllowLocal lets tests reach a server on a loopback address over
	// http://. Never set in production.
	AllowLocal bool
}

// Endpoint is a checked repository URL.
type Endpoint struct {
	// URL is normalised and carries no user information.
	URL string
	// Host is the lowercased host name.
	Host string
	// SSH is true for ssh:// and user@host:path URLs; User is their user.
	SSH  bool
	User string
	port string
}

var scpLike = regexp.MustCompile(`^([A-Za-z0-9._-]+)@([A-Za-z0-9.-]+):([^/][^\s]*)$`)

// ParseURL checks a repository URL: https:// or ssh:// (or user@host:path),
// no user information besides an SSH user, no query or fragment.
func (f Fetcher) ParseURL(raw string) (Endpoint, error) {
	if len(raw) > 2048 || strings.ContainsAny(raw, "\x00\r\n\t ") {
		return Endpoint{}, fmt.Errorf("%w: control characters or spaces", ErrInvalidURL)
	}
	if m := scpLike.FindStringSubmatch(raw); m != nil {
		raw = "ssh://" + m[1] + "@" + m[2] + "/" + m[3]
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return Endpoint{}, fmt.Errorf("%w: not an absolute URL", ErrInvalidURL)
	}
	if u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return Endpoint{}, fmt.Errorf("%w: a query or fragment is not allowed", ErrInvalidURL)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.Trim(u.Path, "/") == "" {
		return Endpoint{}, fmt.Errorf("%w: host and repository path are required", ErrInvalidURL)
	}
	e := Endpoint{Host: host, port: u.Port()}
	switch u.Scheme {
	case "https":
	case "http":
		if !f.AllowLocal {
			return Endpoint{}, fmt.Errorf("%w: use https:// so the server's certificate is checked", ErrInvalidURL)
		}
	case "ssh":
		e.SSH = true
		e.User = u.User.Username()
		if _, has := u.User.Password(); has {
			return Endpoint{}, fmt.Errorf("%w: a password in the URL is not allowed", ErrInvalidURL)
		}
		if e.User == "" {
			e.User = "git"
		}
	default:
		return Endpoint{}, fmt.Errorf("%w: only https:// and ssh:// repositories", ErrInvalidURL)
	}
	if !e.SSH && u.User != nil {
		return Endpoint{}, fmt.Errorf("%w: credentials in the URL are not allowed; give the token separately", ErrInvalidURL)
	}
	if e.port != "" {
		if p, err := strconv.Atoi(e.port); err != nil || p < 1 || p > 65535 {
			return Endpoint{}, fmt.Errorf("%w: bad port", ErrInvalidURL)
		}
	}
	clean := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}
	if e.SSH {
		clean.User = url.User(e.User)
	}
	e.URL = clean.String()
	return e, nil
}

// ---- address policy ------------------------------------------------------

type policyKey struct{}

func allowLocal(ctx context.Context) bool { v, _ := ctx.Value(policyKey{}).(bool); return v }

// checkAddr refuses addresses a repository URL must never reach: loopback
// (unless allowed for tests), link-local (cloud metadata), unspecified,
// multicast and IPv4-mapped forms of them.
func checkAddr(ip netip.Addr, local bool) error {
	ip = ip.Unmap()
	switch {
	case ip.IsLoopback() && local:
		return nil
	case ip.IsLoopback(), ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(), ip.IsUnspecified(), ip.IsMulticast(), ip.IsInterfaceLocalMulticast():
		return fmt.Errorf("%w: %s", ErrAddress, ip)
	}
	if ip.Is4() && ip.As4()[0] == 0 { // 0.0.0.0/8 reaches this host on Linux
		return fmt.Errorf("%w: %s", ErrAddress, ip)
	}
	return nil
}

// resolve returns the first allowed address of host, or the reason none is.
func resolve(ctx context.Context, host string, local bool) (netip.Addr, error) {
	if ip, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return ip, checkAddr(ip, local)
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return netip.Addr{}, fmt.Errorf("%w: cannot resolve %s", ErrUnavailable, host)
	}
	var first error
	for _, ip := range ips {
		if err := checkAddr(ip, local); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		return ip, nil
	}
	return netip.Addr{}, first
}

func dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ip, err := resolve(ctx, host, allowLocal(ctx))
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	return d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
}

var errRedirect = errors.New("gitsource: the server redirected; redirects are not followed")

var install sync.Once

// installTransport replaces go-git's HTTP transports with one that checks
// every address it dials and never follows a redirect. Core uses go-git
// nowhere else.
func installTransport() {
	install.Do(func() {
		hc := &http.Client{
			Transport: &http.Transport{DialContext: dial, DisableKeepAlives: true, ForceAttemptHTTP2: true,
				TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 30 * time.Second},
			CheckRedirect: func(*http.Request, []*http.Request) error { return errRedirect },
		}
		hc.Transport = limitBodies{hc.Transport}
		t := githttp.NewClient(hc)
		client.InstallProtocol("https", t)
		client.InstallProtocol("http", t)
		for _, s := range []string{"git", "file"} {
			client.InstallProtocol(s, nil)
		}
		// Never let the Core host's ~/.ssh/config or /etc/ssh/ssh_config
		// rename the host the address check approved.
		gitssh.DefaultSSHConfig = nil
		proxy.RegisterDialerType(sshDialScheme, func(u *url.URL, _ proxy.Dialer) (proxy.Dialer, error) {
			ns, err := strconv.ParseInt(u.Query().Get("deadline"), 10, 64)
			if err != nil {
				return nil, err
			}
			return sshDialer{deadline: time.Unix(0, ns), local: u.Query().Get("local") == "1"}, nil
		})
	})
}

// maxResponse bounds any one HTTP response body (ref advertisements
// included); packs are bounded again by the object store.
const maxResponse = 2 * DefaultMaxBytes

type limitBodies struct{ rt http.RoundTripper }

func (l limitBodies) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := l.rt.RoundTrip(r)
	if err == nil {
		res.Body = &limitedBody{rc: res.Body, left: maxResponse}
	}
	return res, err
}

type limitedBody struct {
	rc   io.ReadCloser
	left int64
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		return 0, ErrTooLarge
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.rc.Read(p)
	b.left -= int64(n)
	return n, err
}

func (b *limitedBody) Close() error { return b.rc.Close() }

// sshDialScheme is a dialer go-git's SSH transport reaches through its
// proxy option: go-git dials and handshakes without the caller's context,
// so this dialer checks the address and puts the fetch's deadline on the
// connection, which bounds the SSH handshake too.
const sshDialScheme = "hazelminx-ssh"

type sshDialer struct {
	deadline time.Time
	local    bool
}

func (d sshDialer) Dial(network, addr string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, addr)
}

func (d sshDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	ctx, cancel := context.WithDeadline(ctx, d.deadline)
	defer cancel()
	c, err := dial(context.WithValue(ctx, policyKey{}, d.local), network, addr)
	if err != nil {
		return nil, err
	}
	c.SetDeadline(d.deadline)
	return c, nil
}

// proxyFor routes an SSH connection through sshDialer; HTTPS needs none.
func proxyFor(ctx context.Context, ssh bool, local bool) transport.ProxyOptions {
	if !ssh {
		return transport.ProxyOptions{}
	}
	dl, ok := ctx.Deadline()
	if !ok {
		dl = time.Now().Add(DefaultTimeout)
	}
	l := "0"
	if local {
		l = "1"
	}
	return transport.ProxyOptions{URL: sshDialScheme + "://dial?deadline=" + strconv.FormatInt(dl.UnixNano(), 10) + "&local=" + l}
}

// ---- fetching --------------------------------------------------------------

func (f Fetcher) limits() (time.Duration, int64) {
	t, m := f.Timeout, f.MaxBytes
	if t <= 0 {
		t = DefaultTimeout
	}
	if m <= 0 {
		m = DefaultMaxBytes
	}
	return t, m
}

// prepare checks the URL and turns Auth into go-git's form. For SSH the
// host is resolved and checked here and the URL rewritten to the address,
// so the check cannot be undone by a second lookup.
func (f Fetcher) prepare(ctx context.Context, r Remote) (string, transport.AuthMethod, Endpoint, error) {
	installTransport()
	e, err := f.ParseURL(r.URL)
	if err != nil {
		return "", nil, e, err
	}
	if !e.SSH {
		if r.Auth.PrivateKey != nil {
			return "", nil, e, fmt.Errorf("%w: an SSH key needs an ssh:// URL", ErrInvalidURL)
		}
		if r.Auth.Token == "" {
			return e.URL, nil, e, nil
		}
		user := r.Auth.Username
		if user == "" {
			user = "x-access-token"
		}
		return e.URL, &githttp.BasicAuth{Username: user, Password: r.Auth.Token}, e, nil
	}
	if r.Auth.Token != "" || r.Auth.PrivateKey == nil || r.Auth.HostKey == "" {
		return "", nil, e, fmt.Errorf("%w: an ssh:// repository needs a private key and the server's host key", ErrInvalidURL)
	}
	signer, err := ssh.ParsePrivateKey(r.Auth.PrivateKey)
	if err != nil {
		return "", nil, e, fmt.Errorf("%w: the private key cannot be read (a key with a passphrase is not supported)", ErrInvalidURL)
	}
	want, _, _, _, err := ssh.ParseAuthorizedKey([]byte(r.Auth.HostKey))
	if err != nil {
		return "", nil, e, fmt.Errorf("%w: the host key is not in authorized_keys form", ErrInvalidURL)
	}
	ip, err := resolve(ctx, e.Host, f.AllowLocal)
	if err != nil {
		return "", nil, e, err
	}
	port := e.port
	if port == "" {
		port = "22"
	}
	u, _ := url.Parse(e.URL)
	u.Host = net.JoinHostPort(ip.String(), port)
	auth := &gitssh.PublicKeys{User: e.User, Signer: signer}
	auth.HostKeyCallback = func(_ string, _ net.Addr, got ssh.PublicKey) error {
		if got.Type() == want.Type() && string(got.Marshal()) == string(want.Marshal()) {
			return nil
		}
		return ErrHostKey
	}
	auth.HostKeyAlgorithms = []string{want.Type()}
	if want.Type() == ssh.KeyAlgoRSA {
		// The key type of an RSA key names SHA-1 signatures, which
		// current OpenSSH servers refuse; the key is the same.
		auth.HostKeyAlgorithms = []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	}
	return u.String(), auth, e, nil
}

// clean maps go-git's errors to this package's and removes the credential
// from the text.
func clean(err error, r Remote) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	for _, v := range []string{r.Auth.Token, string(r.Auth.PrivateKey)} {
		if v != "" {
			msg = strings.ReplaceAll(msg, v, "[redacted]")
		}
	}
	kind := classify(err, msg)
	switch kind {
	case ErrUnavailable, ErrAuth:
		// What the server or network said stays out of the answer: it would
		// tell a caller which internal ports are open (and could echo
		// something sensitive). The kind is enough to act on.
		msg = ""
	}
	return &cleanErr{kind: kind, msg: msg}
}

func classify(err error, msg string) error {
	for _, k := range []error{ErrHostKey, ErrAddress, ErrTooLarge, ErrInvalidURL, ErrAuth, ErrNotFound, ErrUnavailable} {
		if errors.Is(err, k) || strings.Contains(msg, k.Error()) {
			return k
		}
	}
	switch {
	case errors.Is(err, transport.ErrAuthenticationRequired), errors.Is(err, transport.ErrAuthorizationFailed),
		strings.Contains(msg, "unable to authenticate"):
		return ErrAuth
	case errors.Is(err, transport.ErrRepositoryNotFound), errors.Is(err, transport.ErrEmptyRemoteRepository),
		errors.As(err, new(git.NoMatchingRefSpecError)), errors.Is(err, plumbing.ErrObjectNotFound), errors.Is(err, plumbing.ErrReferenceNotFound),
		strings.Contains(msg, "not our ref"), strings.Contains(msg, "couldn't find remote ref"):
		return ErrNotFound
	}
	return ErrUnavailable
}

type cleanErr struct {
	kind error
	msg  string
}

func (e *cleanErr) Error() string {
	if e.msg == "" {
		return e.kind.Error()
	}
	if strings.HasPrefix(e.msg, "gitsource:") {
		return e.msg
	}
	return e.kind.Error() + ": " + e.msg
}
func (e *cleanErr) Unwrap() error { return e.kind }

var shaRE = regexp.MustCompile(`^[0-9a-f]{40}$`)

// IsCommit says whether s is a full commit SHA.
func IsCommit(s string) bool { return shaRE.MatchString(s) }

// Resolve fixes a branch, tag or full commit SHA to a commit SHA by
// listing the remote's refs. A full SHA is returned as it is; whether the
// commit exists is known only when it is fetched. Empty ref is the
// remote's HEAD.
func (f Fetcher) Resolve(ctx context.Context, r Remote, ref string) (string, error) {
	if IsCommit(ref) {
		return ref, nil
	}
	timeout, _ := f.limits()
	ctx, cancel := context.WithTimeout(context.WithValue(ctx, policyKey{}, f.AllowLocal), timeout)
	defer cancel()
	u, auth, ep, err := f.prepare(ctx, r)
	if err != nil {
		return "", err
	}
	rem := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{Name: "origin", URLs: []string{u}})
	refs, err := rem.ListContext(ctx, &git.ListOptions{Auth: auth, PeelingOption: git.AppendPeeled, ProxyOptions: proxyFor(ctx, ep.SSH, f.AllowLocal)})
	if err != nil {
		return "", clean(err, r)
	}
	byName := map[plumbing.ReferenceName]*plumbing.Reference{}
	for _, x := range refs {
		byName[x.Name()] = x
	}
	var names []plumbing.ReferenceName
	switch {
	case ref == "" || ref == "HEAD":
		names = []plumbing.ReferenceName{plumbing.HEAD}
	case strings.HasPrefix(ref, "refs/"):
		names = []plumbing.ReferenceName{plumbing.ReferenceName(ref)}
	default:
		names = []plumbing.ReferenceName{plumbing.NewBranchReferenceName(ref), plumbing.NewTagReferenceName(ref)}
	}
	for _, n := range names {
		x, ok := byName[n]
		for i := 0; ok && x.Type() == plumbing.SymbolicReference && i < 5; i++ {
			x, ok = byName[x.Target()]
		}
		if !ok {
			continue
		}
		// An annotated tag is listed twice; the peeled entry is the commit.
		if p, ok := byName[plumbing.ReferenceName(string(x.Name())+"^{}")]; ok {
			x = p
		}
		return x.Hash().String(), nil
	}
	return "", fmt.Errorf("%w: %s", ErrNotFound, ref)
}

// Entry is one tree entry of a fetched commit.
type Entry struct {
	Path string
	Mode filemode.FileMode
	hash plumbing.Hash
}

// Snapshot is one fetched commit.
type Snapshot struct {
	Commit  string
	Entries map[string]Entry
	store   *capped
}

// Kind of an entry, as the extraction sees it.
func (e Entry) Regular() bool { return e.Mode == filemode.Regular || e.Mode == filemode.Executable }
func (e Entry) Symlink() bool { return e.Mode == filemode.Symlink }
func (e Entry) Gitlink() bool { return e.Mode == filemode.Submodule }

// Read returns a blob's content when it is at most max bytes.
func (s *Snapshot) Read(e Entry, max int64) ([]byte, error) {
	b, err := object.GetBlob(s.store, e.hash)
	if err != nil {
		return nil, err
	}
	if b.Size > max {
		return nil, fmt.Errorf("%w: %s is %d bytes, at most %d", ErrTooLarge, e.Path, b.Size, max)
	}
	rd, err := b.Reader()
	if err != nil {
		return nil, err
	}
	defer rd.Close()
	return io.ReadAll(io.LimitReader(rd, max+1))
}

// Fetch fetches exactly commit sha (depth 1, no tags) into memory and
// lists its tree. The commit object's hash must be sha.
func (f Fetcher) Fetch(ctx context.Context, r Remote, sha string) (*Snapshot, error) {
	if !IsCommit(sha) {
		return nil, fmt.Errorf("%w: a full 40-character commit SHA is required", ErrNotFound)
	}
	timeout, max := f.limits()
	ctx, cancel := context.WithTimeout(context.WithValue(ctx, policyKey{}, f.AllowLocal), timeout)
	defer cancel()
	u, auth, ep, err := f.prepare(ctx, r)
	if err != nil {
		return nil, err
	}
	st := &capped{Storage: memory.NewStorage(), max: max}
	rem := git.NewRemote(st, &config.RemoteConfig{Name: "origin", URLs: []string{u}})
	err = rem.FetchContext(ctx, &git.FetchOptions{RemoteName: "origin", Auth: auth, Depth: 1, Tags: git.NoTags,
		ProxyOptions: proxyFor(ctx, ep.SSH, f.AllowLocal),
		RefSpecs:     []config.RefSpec{config.RefSpec(sha + ":refs/hazelminx/pin")}})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		if st.over() {
			return nil, fmt.Errorf("%w (%d bytes)", ErrTooLarge, max)
		}
		return nil, clean(err, r)
	}
	h := plumbing.NewHash(sha)
	cm, err := object.GetCommit(st, h)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, sha)
	}
	if cm.Hash != h {
		return nil, ErrCommitChanged
	}
	tree, err := cm.Tree()
	if err != nil {
		return nil, fmt.Errorf("%w: the commit's tree was not sent", ErrUnavailable)
	}
	snap := &Snapshot{Commit: sha, Entries: map[string]Entry{}, store: st}
	w := object.NewTreeWalker(tree, true, nil)
	defer w.Close()
	for {
		name, te, err := w.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// go-git refuses entries such as ".." or ".git": the commit is
			// unusable as it is, not unreachable.
			return nil, fmt.Errorf("%w: %v", ErrInvalidTree, err)
		}
		if te.Mode == filemode.Dir {
			continue
		}
		snap.Entries[name] = Entry{Path: name, Mode: te.Mode, hash: te.Hash}
		if len(snap.Entries) > 200000 {
			return nil, fmt.Errorf("%w: more than 200000 tree entries", ErrTooLarge)
		}
	}
	return snap, nil
}
