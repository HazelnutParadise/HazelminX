package projects

import (
	"context"
	"strings"
	"testing"
)

func analyze(t *testing.T, spec SourceSpec) Analysis {
	t.Helper()
	if spec.ComposeProject == "" {
		spec.ComposeProject = "shop-production-web"
	}
	return Analyze(context.Background(), spec)
}

func files(kv ...string) []SourceFile {
	var out []SourceFile
	for i := 0; i < len(kv); i += 2 {
		out = append(out, SourceFile{Path: kv[i], Content: []byte(kv[i+1])})
	}
	return out
}

func finding(a Analysis, code string) *Finding {
	for i := range a.Findings {
		if a.Findings[i].Code == code {
			return &a.Findings[i]
		}
	}
	return nil
}

func service(a Analysis, name string) *ServicePreview {
	for i := range a.Services {
		if a.Services[i].Name == name {
			return &a.Services[i]
		}
	}
	return nil
}

const baseCompose = `
services:
  web:
    build: ./web
    ports: ["127.0.0.1:8080:80"]
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf:ro
      - ./data:/data
      - uploads:/uploads
    healthcheck:
      test: ["CMD", "true"]
    x-note: keep me
  debug:
    image: busybox:1.37
    profiles: [debug]
volumes:
  uploads: {}
x-shared: {a: 1}
`

const overrideCompose = `
services:
  web:
    environment:
      LOG_LEVEL: warn
    ports: ["127.0.0.1:9090:90"]
`

func TestMergesFilesInOrderWithProfilesBuildAndRelativePaths(t *testing.T) {
	a := analyze(t, SourceSpec{
		Files: files("deploy/compose.yaml", baseCompose, "deploy/compose.prod.yaml", overrideCompose,
			"deploy/web/Dockerfile", "FROM scratch\n", "deploy/nginx.conf", "events {}\n"),
		MainFile: "deploy/compose.yaml", ExtraFiles: []string{"deploy/compose.prod.yaml"},
	})
	if !a.Valid {
		t.Fatalf("not valid: %+v", a.Findings)
	}
	if a.ProjectDir != "deploy" {
		t.Fatalf("project dir %q", a.ProjectDir)
	}
	web := service(a, "web")
	if web == nil || !web.Enabled || web.Build == nil || web.Build.Context != "deploy/web" || web.Build.Dockerfile != "deploy/web/Dockerfile" {
		t.Fatalf("web build: %+v", web)
	}
	if len(web.Ports) != 2 {
		t.Fatalf("ports should merge like Compose (append): %+v", web.Ports)
	}
	kinds := map[string]Mount{}
	for _, m := range web.Mounts {
		kinds[m.Target] = m
	}
	if m := kinds["/etc/nginx/nginx.conf"]; m.Kind != MountSourceFile || m.Source != "deploy/nginx.conf" || !m.ReadOnly {
		t.Fatalf("source file mount: %+v", m)
	}
	if m := kinds["/data"]; m.Kind != MountDataDir || m.Source != "deploy/data" {
		t.Fatalf("data dir mount: %+v", m)
	}
	if m := kinds["/uploads"]; m.Kind != MountVolume || m.Source != "shop-production-web_uploads" {
		t.Fatalf("named volume takes the fixed project name: %+v", m)
	}
	if d := service(a, "debug"); d == nil || d.Enabled {
		t.Fatalf("debug should be listed and disabled: %+v", d)
	}
	if f := finding(a, "profile-disabled"); f == nil || f.Attribute != "services.debug" {
		t.Fatalf("profile finding: %+v", f)
	}
	if finding(a, "extension") == nil {
		t.Fatal("x-shared should be listed as kept")
	}

	b := analyze(t, SourceSpec{
		Files:    files("deploy/compose.yaml", baseCompose, "deploy/web/Dockerfile", "FROM scratch\n", "deploy/nginx.conf", "x"),
		MainFile: "deploy/compose.yaml", Profiles: []string{"debug"},
	})
	if d := service(b, "debug"); d == nil || !d.Enabled {
		t.Fatalf("debug enabled with its profile: %+v", d)
	}
}

func TestRefusesWhatComposeRefusesAndSaysWhy(t *testing.T) {
	a := analyze(t, SourceSpec{Files: files("compose.yaml", "services:\n  web:\n    image: nginx\n    restartt: always\n"), MainFile: "compose.yaml"})
	f := finding(a, "compose-invalid")
	if a.Valid || f == nil || !strings.Contains(f.Message, "restartt") {
		t.Fatalf("unknown key must be refused with its name: %+v", a.Findings)
	}
	if strings.Contains(f.Message, root) {
		t.Fatalf("message leaks the parse root: %s", f.Message)
	}

	bad := analyze(t, SourceSpec{Files: files("compose.yaml", "services: [\n"), MainFile: "compose.yaml"})
	if bad.Valid || finding(bad, "yaml-invalid") == nil {
		t.Fatalf("broken YAML: %+v", bad.Findings)
	}
}

func TestKeepsSwarmOnlyAndWatchAttributes(t *testing.T) {
	a := analyze(t, SourceSpec{Files: files("compose.yaml", `
services:
  web:
    image: nginx
    deploy:
      placement:
        constraints: [node.role==manager]
      resources:
        limits: {memory: 64M}
    develop:
      watch: [{action: sync, path: ./src, target: /src}]
    security_opt: [label:disable]
    healthcheck: {test: ["CMD", "true"]}
`), MainFile: "compose.yaml"})
	if !a.Valid {
		t.Fatalf("valid files: %+v", a.Findings)
	}
	var kept []string
	for _, f := range a.Findings {
		if f.Severity == SeverityKept {
			kept = append(kept, f.Attribute)
		}
	}
	joined := strings.Join(kept, " ")
	if !strings.Contains(joined, "services.web.deploy.placement") || !strings.Contains(joined, "services.web.develop") {
		t.Fatalf("kept: %v", kept)
	}
	if strings.Contains(joined, "resources") {
		t.Fatalf("resource limits are applied by compose up and must not be listed as kept: %v", kept)
	}
	if web := service(a, "web"); !web.Healthcheck {
		t.Fatal("healthcheck lost")
	}
}

func TestNeverReadsTheDisk(t *testing.T) {
	cases := map[string]string{
		"include":      "include: [/etc/hostname]\nservices: {}\n",
		"extends-file": "services:\n  web:\n    extends: {file: /etc/hostname, service: x}\n",
	}
	for name, body := range cases {
		a := analyze(t, SourceSpec{Files: files("compose.yaml", body), MainFile: "compose.yaml"})
		if a.Valid {
			t.Fatalf("%s accepted: %+v", name, a.Findings)
		}
	}
	// A default is still interpolated: a path defaulting outside the unit
	// is reported, never opened.
	a := analyze(t, SourceSpec{Files: files("compose.yaml", "services:\n  web:\n    image: nginx\n    env_file: ${X:-../../etc/passwd}\n"), MainFile: "compose.yaml"})
	if a.Valid || finding(a, "path-outside-unit") == nil {
		t.Fatalf("env file outside the unit: %+v", a.Findings)
	}
}

func TestVariablesUseACleanEnvironment(t *testing.T) {
	t.Setenv("TAG", "from-core")
	a := analyze(t, SourceSpec{Files: files("compose.yaml", "services:\n  web:\n    image: shop:${TAG:?必須指定 TAG}\n    environment:\n      A: ${LEVEL:-info}\n      B: ${UNSET}\n"), MainFile: "compose.yaml"})
	states := map[string]string{}
	for _, v := range a.Variables {
		states[v.Name] = v.State
	}
	if states["TAG"] != VariableMissing || states["LEVEL"] != VariableDefault || states["UNSET"] != VariableEmpty {
		t.Fatalf("states %v", states)
	}
	if a.Valid {
		t.Fatal("a missing required variable must block, even when Core's own environment has it")
	}
}

func TestHighRiskSettingsAreListed(t *testing.T) {
	a := analyze(t, SourceSpec{Files: files("compose.yaml", `
services:
  agent:
    image: portainer/agent
    privileged: true
    network_mode: host
    cap_add: [SYS_ADMIN]
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - /:/host:ro
`), MainFile: "compose.yaml"})
	if !a.Valid {
		t.Fatalf("high risk is not invalid: %+v", a.Findings)
	}
	for _, code := range []string{"privileged", "host-namespace", "cap-add", "engine-socket", "host-system-path"} {
		if f := finding(a, code); f == nil || f.Severity != SeverityHighRisk {
			t.Fatalf("%s: %+v", code, a.Findings)
		}
	}
}

func TestSecretFilesMustBeMarkedSecret(t *testing.T) {
	body := "services:\n  db:\n    image: postgres:16\n    secrets: [pw]\n    env_file: app.env\nsecrets:\n  pw:\n    file: ./pw.txt\n"
	a := analyze(t, SourceSpec{Files: files("compose.yaml", body, "pw.txt", "x", "app.env", "A=1"), MainFile: "compose.yaml"})
	if a.Valid || finding(a, "secret-file-not-secret") == nil || finding(a, "env-file-not-secret") == nil {
		t.Fatalf("plain secret files: %+v", a.Findings)
	}
	ok := analyze(t, SourceSpec{Files: []SourceFile{{Path: "compose.yaml", Content: []byte(body)}, {Path: "pw.txt", Secret: true}, {Path: "app.env", Secret: true}}, MainFile: "compose.yaml"})
	if !ok.Valid {
		t.Fatalf("secret files: %+v", ok.Findings)
	}
	missing := analyze(t, SourceSpec{Files: files("compose.yaml", body), MainFile: "compose.yaml"})
	if missing.Valid || finding(missing, "file-missing") == nil || finding(missing, "env-file-missing") == nil {
		t.Fatalf("missing files: %+v", missing.Findings)
	}
}

func TestRelativePathsThatLeaveTheUnit(t *testing.T) {
	a := analyze(t, SourceSpec{Files: files("compose.yaml", "services:\n  web:\n    build: ../../outside\n    volumes: [\"../../x:/x\"]\n"), MainFile: "compose.yaml"})
	if a.Valid || finding(a, "path-outside-unit") == nil {
		t.Fatalf("%+v", a.Findings)
	}
}
