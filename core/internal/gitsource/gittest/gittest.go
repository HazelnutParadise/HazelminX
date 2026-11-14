// Package gittest serves real Git repositories for tests: over HTTP with
// git http-backend, and over SSH with an in-process server that runs
// git-upload-pack. Both need the git program; tests skip without it.
package gittest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// Repo is a bare repository under a served root.
type Repo struct {
	t    *testing.T
	Root string // directory holding <name>.git
	Name string
	work string
}

// Git runs git in dir with a clean configuration.
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// NewRepo makes an empty bare repository name.git under a new root. The
// server allows fetching any commit by SHA, as GitHub does.
func NewRepo(t *testing.T, name string) *Repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	work := t.TempDir()
	Git(t, root, "init", "-q", "--bare", "-b", "main", name+".git")
	Git(t, filepath.Join(root, name+".git"), "config", "uploadpack.allowAnySHA1InWant", "true")
	Git(t, filepath.Join(root, name+".git"), "config", "http.receivepack", "false")
	Git(t, work, "init", "-q", "-b", "main")
	Git(t, work, "remote", "add", "origin", filepath.Join(root, name+".git"))
	return &Repo{t: t, Root: root, Name: name, work: work}
}

// Commit writes files (path → content; a value "->target" makes a
// symbolic link), commits them on branch and pushes. It returns the SHA.
// A branch that does not exist yet starts from an empty tree.
func (r *Repo) Commit(branch, msg string, files map[string]string) string {
	r.t.Helper()
	if exec.Command("git", "-C", r.work, "rev-parse", "--verify", "-q", "refs/heads/"+branch).Run() == nil {
		Git(r.t, r.work, "checkout", "-q", branch)
	} else {
		Git(r.t, r.work, "checkout", "-q", "--orphan", branch)
		Git(r.t, r.work, "rm", "-rqf", "--ignore-unmatch", ".")
		Git(r.t, r.work, "clean", "-qfdx")
	}
	for p, c := range files {
		full := filepath.Join(r.work, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			r.t.Fatal(err)
		}
		os.Remove(full)
		if strings.HasPrefix(c, "->") {
			if err := os.Symlink(c[2:], full); err != nil {
				r.t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			r.t.Fatal(err)
		}
	}
	Git(r.t, r.work, "add", "-A")
	Git(r.t, r.work, "commit", "-q", "--allow-empty", "-m", msg)
	Git(r.t, r.work, "push", "-q", "-f", "origin", branch)
	return Git(r.t, r.work, "rev-parse", "HEAD")
}

// Gitlink adds a submodule entry pointing at sha without fetching it.
func (r *Repo) Gitlink(branch, path, sha string) string {
	r.t.Helper()
	Git(r.t, r.work, "checkout", "-q", branch)
	Git(r.t, r.work, "update-index", "--add", "--cacheinfo", "160000,"+sha+","+path)
	Git(r.t, r.work, "commit", "-q", "-m", "gitlink")
	Git(r.t, r.work, "push", "-q", "-f", "origin", branch)
	return Git(r.t, r.work, "rev-parse", "HEAD")
}

// Tag makes an annotated tag at HEAD and pushes it.
func (r *Repo) Tag(name string) {
	r.t.Helper()
	Git(r.t, r.work, "tag", "-a", "-m", name, name)
	Git(r.t, r.work, "push", "-q", "origin", name)
}

// HTTPServer serves the root with git http-backend. When token is set,
// requests must send Basic auth with that password (any user name); the
// handler records the Authorization header of every request and the
// request URLs.
type HTTPServer struct {
	*httptest.Server
	mu      sync.Mutex
	Seen    []string // request URIs
	Headers []string // Authorization headers
}

// ServeHTTP starts a server for r.
func (r *Repo) ServeHTTP(token string) *HTTPServer {
	r.t.Helper()
	backend, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		r.t.Fatal(err)
	}
	h := &cgi.Handler{Path: filepath.Join(strings.TrimSpace(string(backend)), "git-http-backend"),
		Env: []string{"GIT_PROJECT_ROOT=" + r.Root, "GIT_HTTP_EXPORT_ALL=1", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}}
	s := &HTTPServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		s.mu.Lock()
		s.Seen = append(s.Seen, req.RequestURI)
		s.Headers = append(s.Headers, req.Header.Get("Authorization"))
		s.mu.Unlock()
		if token != "" {
			if _, pw, ok := req.BasicAuth(); !ok || pw != token {
				w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
				http.Error(w, "auth", http.StatusUnauthorized)
				return
			}
		}
		h.ServeHTTP(w, req)
	}))
	r.t.Cleanup(s.Close)
	return s
}

// URL of the repository on s.
func (s *HTTPServer) URL(r *Repo) string { return s.Server.URL + "/" + r.Name + ".git" }

// Requests returns what the server saw.
func (s *HTTPServer) Requests() (uris, auth []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.Seen...), append([]string(nil), s.Headers...)
}

// SSHServer serves the root over SSH to one client key.
type SSHServer struct {
	Addr string
	// HostKey is the server's key in authorized_keys form.
	HostKey string
	// ClientKey is the private key the server accepts, PEM.
	ClientKey []byte
}

// URL of the repository on s.
func (s *SSHServer) URL(r *Repo) string { return "ssh://git@" + s.Addr + "/" + r.Name + ".git" }

// NewKey returns an Ed25519 key in OpenSSH PEM form and its public key in
// authorized_keys form.
func NewKey(t *testing.T) ([]byte, string, ssh.Signer) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	blk, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pub := signer.PublicKey()
	return pem.EncodeToMemory(blk), pub.Type() + " " + base64.StdEncoding.EncodeToString(pub.Marshal()), signer
}

// ServeSSH starts an SSH server on 127.0.0.1 that runs git-upload-pack
// for the repositories under r.Root.
func (r *Repo) ServeSSH() *SSHServer {
	t := r.t
	t.Helper()
	_, hostPub, hostSigner := NewKey(t)
	clientPEM, _, clientSigner := NewKey(t)
	want := clientSigner.PublicKey().Marshal()
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if string(k.Marshal()) == string(want) {
			return nil, nil
		}
		return nil, fmt.Errorf("unknown key")
	}}
	cfg.AddHostKey(hostSigner)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serveConn(c, cfg, r.Root)
		}
	}()
	return &SSHServer{Addr: ln.Addr().String(), HostKey: hostPub, ClientKey: clientPEM}
}

func serveConn(c net.Conn, cfg *ssh.ServerConfig, root string) {
	defer c.Close()
	_, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			nc.Reject(ssh.UnknownChannelType, "")
			continue
		}
		ch, in, err := nc.Accept()
		if err != nil {
			return
		}
		go func() {
			defer ch.Close()
			for req := range in {
				if req.Type != "exec" {
					req.Reply(false, nil)
					continue
				}
				var p struct{ Cmd string }
				ssh.Unmarshal(req.Payload, &p)
				fields := strings.SplitN(p.Cmd, " ", 2)
				if len(fields) != 2 || fields[0] != "git-upload-pack" {
					req.Reply(false, nil)
					return
				}
				repo := filepath.Join(root, filepath.Base(strings.Trim(fields[1], "'\"/")))
				req.Reply(true, nil)
				cmd := exec.Command("git-upload-pack", repo)
				cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
				cmd.Stdout, cmd.Stderr = ch, ch.Stderr()
				stdin, err := cmd.StdinPipe()
				if err != nil || cmd.Start() != nil {
					return
				}
				go func() { io.Copy(stdin, ch); stdin.Close() }()
				status := 0
				if err := cmd.Wait(); err != nil {
					status = 1
				}
				ch.SendRequest("exit-status", false, ssh.Marshal(struct{ S uint32 }{uint32(status)}))
				return
			}
		}()
	}
}
