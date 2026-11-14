package projects

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// T-052: the analysis must agree with the native `docker compose config`
// of the installed Docker Compose on what the files become. Skipped where
// there is no docker CLI with the compose plugin.

type nativeConfig struct {
	Services map[string]struct {
		Image string `json:"image"`
		Build *struct {
			Context    string `json:"context"`
			Dockerfile string `json:"dockerfile"`
		} `json:"build"`
		Ports []struct {
			HostIP    string `json:"host_ip"`
			Published string `json:"published"`
			Target    uint32 `json:"target"`
			Protocol  string `json:"protocol"`
		} `json:"ports"`
		Volumes []struct {
			Type   string `json:"type"`
			Source string `json:"source"`
			Target string `json:"target"`
		} `json:"volumes"`
		Networks map[string]any `json:"networks"`
	} `json:"services"`
	Volumes  map[string]struct{ Name string } `json:"volumes"`
	Networks map[string]struct{ Name string } `json:"networks"`
}

func nativeCompose(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("no docker CLI: native comparison not run")
	}
	out, err := exec.Command(bin, "compose", "version", "--short").Output()
	if err != nil {
		t.Skip("docker has no compose plugin: native comparison not run")
	}
	t.Logf("comparing with Docker Compose %s", strings.TrimSpace(string(out)))
	return bin
}

func TestT052AgreesWithNativeCompose(t *testing.T) {
	bin := nativeCompose(t)
	unit := map[string]string{
		"deploy/compose.yaml": `
services:
  web:
    build:
      context: ./web
      dockerfile: Containerfile
    ports: ["127.0.0.1:8080:80"]
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf:ro
      - ./data:/data
      - uploads:/uploads
    networks: [front, back]
  db:
    image: postgres:16.4
    volumes: [pg:/var/lib/postgresql/data]
    networks: [back]
  debug:
    image: busybox:1.37
    profiles: [debug]
  tools:
    image: alpine:3.20
    profiles: [ops]
volumes:
  uploads: {}
  pg:
    name: shop-pg
networks:
  front: {}
  back: {}
`,
		"deploy/compose.prod.yaml": `
services:
  web:
    ports: ["127.0.0.1:9090:90"]
    volumes:
      - ../shared/ca.pem:/etc/ssl/ca.pem:ro
`,
		"deploy/web/Containerfile": "FROM scratch\n",
		"deploy/nginx.conf":        "events {}\n",
		"shared/ca.pem":            "pem\n",
	}
	main, extra, profiles, project := "deploy/compose.yaml", []string{"deploy/compose.prod.yaml"}, []string{"debug"}, "shop-production-web"

	dir := t.TempDir()
	var spec SourceSpec
	spec.ComposeProject, spec.MainFile, spec.ExtraFiles, spec.Profiles = project, main, extra, profiles
	for p, body := range unit {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		spec.Files = append(spec.Files, SourceFile{Path: p, Content: []byte(body)})
	}
	a := Analyze(context.Background(), spec)
	if !a.Valid {
		t.Fatalf("analysis refused the files: %+v", a.Findings)
	}

	args := []string{"compose", "-p", project, "-f", filepath.Join(dir, main)}
	for _, f := range extra {
		args = append(args, "-f", filepath.Join(dir, f))
	}
	for _, p := range profiles {
		args = append(args, "--profile", p)
	}
	args = append(args, "config", "--format", "json")
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")} // nothing else: no COMPOSE_* or variables leak in
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("docker compose config: %v: %s", err, ee.Stderr)
		}
		t.Fatal(err)
	}
	var n nativeConfig
	if err := json.Unmarshal(out, &n); err != nil {
		t.Fatal(err)
	}
	relTo := func(p string) string {
		r, err := filepath.Rel(dir, p)
		if err != nil || strings.HasPrefix(r, "..") {
			return p
		}
		return filepath.ToSlash(r)
	}

	var enabled, native []string
	for _, s := range a.Services {
		if s.Enabled {
			enabled = append(enabled, s.Name)
		}
	}
	for name := range n.Services {
		native = append(native, name)
	}
	sort.Strings(native)
	if strings.Join(enabled, ",") != strings.Join(native, ",") {
		t.Fatalf("enabled services: ours %v, native %v", enabled, native)
	}
	for _, s := range a.Services {
		if !s.Enabled {
			continue
		}
		ns := n.Services[s.Name]
		if s.Build != nil {
			if ns.Build == nil || relTo(ns.Build.Context) != s.Build.Context ||
				filepath.ToSlash(filepath.Join(relTo(ns.Build.Context), ns.Build.Dockerfile)) != s.Build.Dockerfile {
				t.Errorf("%s build: ours %+v, native %+v", s.Name, s.Build, ns.Build)
			}
		} else if ns.Image != s.Image {
			t.Errorf("%s image: ours %q, native %q", s.Name, s.Image, ns.Image)
		}
		if len(ns.Ports) != len(s.Ports) {
			t.Errorf("%s ports: ours %+v, native %+v", s.Name, s.Ports, ns.Ports)
		}
		for i := range ns.Ports {
			if i < len(s.Ports) && (ns.Ports[i].Published != s.Ports[i].Published || ns.Ports[i].Target != s.Ports[i].Target || ns.Ports[i].HostIP != s.Ports[i].HostIP) {
				t.Errorf("%s port %d: ours %+v, native %+v", s.Name, i, s.Ports[i], ns.Ports[i])
			}
		}
		if len(ns.Volumes) != len(s.Mounts) {
			t.Fatalf("%s mounts: ours %+v, native %+v", s.Name, s.Mounts, ns.Volumes)
		}
		for i, v := range ns.Volumes {
			m := s.Mounts[i]
			src := v.Source
			if v.Type == "bind" {
				src = relTo(v.Source)
			} else if vol, ok := n.Volumes[v.Source]; ok {
				src = vol.Name
			}
			if v.Type != m.Type || v.Target != m.Target || src != m.Source {
				t.Errorf("%s mount %d: ours %+v, native %+v (%s)", s.Name, i, m, v, src)
			}
		}
		var nets []string
		for k := range ns.Networks {
			nets = append(nets, k)
		}
		sort.Strings(nets)
		if strings.Join(nets, ",") != strings.Join(s.Networks, ",") {
			t.Errorf("%s networks: ours %v, native %v", s.Name, s.Networks, nets)
		}
	}
	for _, v := range a.Volumes {
		if n.Volumes[v.Key].Name != v.Name {
			t.Errorf("volume %s: ours %q, native %q", v.Key, v.Name, n.Volumes[v.Key].Name)
		}
	}
	for _, nw := range a.Networks {
		if n.Networks[nw.Key].Name != nw.Name {
			t.Errorf("network %s: ours %q, native %q", nw.Key, nw.Name, n.Networks[nw.Key].Name)
		}
	}
}
