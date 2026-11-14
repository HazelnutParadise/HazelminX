package projects

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/template"
	"github.com/compose-spec/compose-go/v2/tree"
	"github.com/compose-spec/compose-go/v2/types"
	yaml "go.yaml.in/yaml/v4"
)

// The Compose files of a unit are read by compose-go, the parser Docker
// Compose itself uses (pinned in go.mod), so what is accepted and how files
// merge is what `docker compose -p <name> -f … --profile …` would do.
//
// compose-go reads files from disk on its own for include, extends.file,
// env_file and label_file. Core must never read its own host's files on a
// user's behalf, so the files are handed over as content under a root that
// does not exist on disk, include and extends.file are refused before the
// loader runs, and environment and label files are not resolved (their
// paths are only checked). Interpolation uses an empty environment: nothing
// from Core's own environment leaks in (spec 15.2) and `.env` is not read.

// root is where the unit's files appear to live while they are parsed. It
// never exists on disk; every path in an answer is made relative to it.
const root = "/hazelminx-unit"

// Finding severities.
const (
	SeverityError    = "error"     // the unit cannot be deployed as it is
	SeverityHighRisk = "high-risk" // deployable only through an approved change plan (044.2)
	SeverityWarning  = "warning"   // shown, not blocking
	SeverityKept     = "kept"      // kept as written; HazelminX does not act on it
)

// Finding is one thing the check says about the unit, located as precisely
// as compose-go allows.
type Finding struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	// File is the unit file it comes from, when known.
	File string `json:"file,omitempty"`
	// Attribute is the model path, e.g. "services.web.ports[0]".
	Attribute string `json:"attribute,omitempty"`
	Message   string `json:"message"`
}

// Variable states.
const (
	VariableMissing    = "missing"     // required (${X:?}) and no value: blocks
	VariableDefault    = "default"     // no value; the default in the file is used
	VariableEmpty      = "empty"       // no value and no default: Compose uses ""
	VariableProvided   = "provided"    // a value is set (ticket 045)
)

// Variable is one ${NAME} used by the files.
type Variable struct {
	Name     string   `json:"name"`
	State    string   `json:"state"`
	Required bool     `json:"required"`
	Default  *string  `json:"default"`
	Files    []string `json:"files"`
}

// Mount kinds.
const (
	MountSourceFile = "source-file" // a file or directory of the unit
	MountDataDir    = "data-dir"    // a relative directory that is not a unit file: created on the host
	MountHostPath   = "host-path"   // an absolute path on the host
	MountVolume     = "volume"      // a named volume
	MountAnonymous  = "anonymous"   // an anonymous volume
	MountTmpfs      = "tmpfs"
	MountOther      = "other"
)

// Mount is one volume entry of a service.
type Mount struct {
	Type string `json:"type"`
	Kind string `json:"kind"`
	// Source is relative to the unit's files for source-file and data-dir,
	// the host path for host-path, the actual volume name for volume.
	Source   string `json:"source,omitempty"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"readOnly"`
}

// Port is one published port.
type Port struct {
	HostIP    string `json:"hostIp,omitempty"`
	Published string `json:"published,omitempty"`
	Target    uint32 `json:"target"`
	Protocol  string `json:"protocol"`
}

// Build is where a service's image is built from.
type Build struct {
	// Context is relative to the unit's files, or the URL of a remote
	// context.
	Context    string `json:"context"`
	Remote     bool   `json:"remote"`
	Dockerfile string `json:"dockerfile,omitempty"`
	Inline     bool   `json:"inline"`
}

// Service is what one service would become.
type Service struct {
	Name        string   `json:"name"`
	Enabled     bool     `json:"enabled"`
	Profiles    []string `json:"profiles"`
	Image       string   `json:"image,omitempty"`
	Build       *Build   `json:"build"`
	Ports       []Port   `json:"ports"`
	Mounts      []Mount  `json:"mounts"`
	Networks    []string `json:"networks"`
	NetworkMode string   `json:"networkMode,omitempty"`
	DependsOn   []string `json:"dependsOn"`
	Healthcheck bool     `json:"healthcheck"`
	EnvFiles    []string `json:"envFiles"`
}

// Resource is a top-level volume, network, secret or config.
type Resource struct {
	Key string `json:"key"`
	// Name is the actual engine name (`<project>_<key>` unless named).
	Name     string `json:"name"`
	External bool   `json:"external"`
	// File is the unit file holding a secret or config's content.
	File string `json:"file,omitempty"`
}

// Analysis is the result of checking a unit's Compose files.
type Analysis struct {
	// Valid: compose-go loaded the files and no finding is an error.
	Valid          bool       `json:"valid"`
	ComposeProject string     `json:"composeProject"`
	ProjectDir     string     `json:"projectDir"`
	Files          []string   `json:"files"`
	Profiles       []string   `json:"profiles"`
	Findings       []Finding  `json:"findings"`
	Variables      []Variable `json:"variables"`
	Services       []Service  `json:"services"`
	Volumes        []Resource `json:"volumes"`
	Networks       []Resource `json:"networks"`
	Secrets        []Resource `json:"secrets"`
	Configs        []Resource `json:"configs"`
}

// SourceFile is one file of a unit as the analysis sees it. Content is nil
// for a secret file: its content is never parsed.
type SourceFile struct {
	Path    string
	Content []byte
	Secret  bool
}

// SourceSpec is what is analysed.
type SourceSpec struct {
	ComposeProject string
	Files          []SourceFile
	MainFile       string
	ExtraFiles     []string
	WorkingDir     string
	Profiles       []string
}

// unhandled lists attributes Compose accepts that `docker compose up` on a
// single engine does not apply (they belong to Swarm or to `compose watch`).
// They are kept as written and listed (CMP-015).
var unhandled = []struct {
	path, why string
}{
	{"services.*.deploy.placement", "applies to Swarm only; docker compose up ignores it"},
	{"services.*.deploy.update_config", "applies to Swarm only; docker compose up ignores it"},
	{"services.*.deploy.rollback_config", "applies to Swarm only; docker compose up ignores it"},
	{"services.*.deploy.endpoint_mode", "applies to Swarm only; docker compose up ignores it"},
	{"services.*.deploy.mode", "applies to Swarm only; docker compose up ignores it"},
	{"services.*.develop", "used by docker compose watch only; HazelminX deploys do not watch files"},
}

// analyzeTimeout bounds one parse; compose-go also caps YAML node visits.
const analyzeTimeout = 10 * time.Second

// Analyze checks the files of a unit. It never reads the disk and never
// returns an error for bad input: problems are findings.
func Analyze(ctx context.Context, spec SourceSpec) Analysis {
	a := Analysis{ComposeProject: spec.ComposeProject, Profiles: nonNil(spec.Profiles),
		Findings: []Finding{}, Variables: []Variable{}, Services: []Service{},
		Volumes: []Resource{}, Networks: []Resource{}, Secrets: []Resource{}, Configs: []Resource{}}
	files := map[string]SourceFile{}
	for _, f := range spec.Files {
		files[f.Path] = f
	}
	order := append([]string{spec.MainFile}, spec.ExtraFiles...)
	a.Files = order
	a.ProjectDir = spec.WorkingDir
	if a.ProjectDir == "" {
		a.ProjectDir = path.Dir(spec.MainFile)
	}
	a.ProjectDir = path.Clean(a.ProjectDir)

	// Raw pass: refuse what would make compose-go read the disk, and
	// collect the variables each file uses.
	vars := map[string]*Variable{}
	var configs []types.ConfigFile
	for _, name := range order {
		f := files[name]
		var dict map[string]any
		if err := yaml.Unmarshal(f.Content, &dict); err != nil {
			a.add(SeverityError, "yaml-invalid", name, "", "not valid YAML: "+err.Error())
			continue
		}
		if dict == nil {
			a.add(SeverityError, "compose-empty", name, "", "the file is empty")
			continue
		}
		a.refuseDiskReads(name, dict)
		for vn, v := range template.ExtractVariables(dict, template.DefaultPattern) {
			cur, ok := vars[vn]
			if !ok {
				cur = &Variable{Name: vn, Files: []string{}}
				vars[vn] = cur
			}
			cur.Required = cur.Required || v.Required
			if v.DefaultValue != "" && cur.Default == nil {
				d := v.DefaultValue
				cur.Default = &d
			}
			if !slices.Contains(cur.Files, name) {
				cur.Files = append(cur.Files, name)
			}
		}
		if n, ok := dict["name"].(string); ok && n != spec.ComposeProject {
			a.add(SeverityWarning, "name-not-used", name, "name",
				fmt.Sprintf("the top-level name %q is not used: HazelminX always runs this unit as %q", n, spec.ComposeProject))
		}
		configs = append(configs, types.ConfigFile{Filename: root + "/" + name, Content: f.Content})
	}
	for _, v := range vars {
		switch {
		case v.Required:
			v.State = VariableMissing
			a.add(SeverityError, "variable-missing", strings.Join(v.Files, ", "), "",
				"${"+v.Name+"} must be set and has no value; variables are set per environment (ticket 045)")
		case v.Default != nil:
			v.State = VariableDefault
		default:
			v.State = VariableEmpty
			a.add(SeverityWarning, "variable-empty", strings.Join(v.Files, ", "), "",
				"${"+v.Name+"} has no value and no default; Compose substitutes an empty string")
		}
		a.Variables = append(a.Variables, *v)
	}
	sort.Slice(a.Variables, func(i, j int) bool { return a.Variables[i].Name < a.Variables[j].Name })
	if a.hasErrors() {
		a.sort()
		return a
	}

	ctx, cancel := context.WithTimeout(ctx, analyzeTimeout)
	defer cancel()
	var unsupported []loader.UnsupportedAttribute
	patterns := make([]loader.UnsupportedAttributePattern, 0, len(unhandled))
	for _, u := range unhandled {
		patterns = append(patterns, loader.UnsupportedAttributePattern{Path: tree.NewPath(strings.Split(u.path, ".")...)})
	}
	project, err := loader.LoadWithContext(ctx, types.ConfigDetails{
		WorkingDir:  root + "/" + strings.TrimPrefix(path.Clean("/"+a.ProjectDir), "/"),
		ConfigFiles: configs,
		Environment: types.Mapping{},
	}, func(o *loader.Options) {
		o.SetProjectName(spec.ComposeProject, true)
		o.Profiles = spec.Profiles
		o.ResolvePaths = false // paths are resolved by resolve, which can tell a path that leaves the unit
		o.SkipInclude = true
		o.SkipResolveEnvironment = true
		o.SkipResolveLabels = true
		o.ResourceLoaders = nil
	}, loader.WithUnsupportedAttributesCheck(patterns, func(found []loader.UnsupportedAttribute) { unsupported = found }))
	if err != nil {
		a.add(SeverityError, "compose-invalid", "", "", "Docker Compose refuses these files: "+loadError(err))
		a.sort()
		return a
	}
	for _, u := range unsupported {
		why := ""
		for _, h := range unhandled {
			if u.Path.Matches(tree.NewPath(strings.Split(h.path, ".")...)) {
				why = h.why
			}
		}
		a.add(SeverityKept, "not-applied", "", u.Path.String(), "kept as written: "+why)
	}
	a.describe(project, files)
	a.sort()
	return a
}

// refuseDiskReads refuses include and extends.file: compose-go would read
// them from Core's disk.
func (a *Analysis) refuseDiskReads(file string, dict map[string]any) {
	if _, ok := dict["include"]; ok {
		a.add(SeverityError, "include-unsupported", file, "include",
			"include is not supported yet: list every file of the unit in the merge order instead (ticket 110 adds include)")
	}
	services, _ := dict["services"].(map[string]any)
	for name, s := range services {
		svc, _ := s.(map[string]any)
		ext, ok := svc["extends"].(map[string]any)
		if !ok {
			continue
		}
		if _, ok := ext["file"]; ok {
			a.add(SeverityError, "extends-file-unsupported", file, "services."+name+".extends.file",
				"extends from another file is not supported yet; extend a service of the same files instead")
		}
	}
}

// loadError strips the parse root from compose-go's message.
func loadError(err error) string {
	msg := err.Error()
	if errors.Is(err, context.DeadlineExceeded) {
		return "parsing took longer than " + analyzeTimeout.String()
	}
	return strings.ReplaceAll(strings.ReplaceAll(msg, root+"/", ""), root, ".")
}

// Path kinds of a path written in a Compose file.
const (
	pathUnit    = iota // inside the unit's files
	pathOutside        // relative, but leaves the unit's files
	pathHost           // absolute, or ~ (expanded on the host)
)

// resolve resolves p the way Compose does: relative to the project
// directory, whichever merged file it was written in (52.5.1).
func resolve(projectDir, p string) (string, int) {
	if path.IsAbs(p) {
		return path.Clean(p), pathHost
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		return p, pathHost
	}
	j := path.Join(projectDir, p)
	if j == ".." || strings.HasPrefix(j, "../") {
		return j, pathOutside
	}
	return j, pathUnit
}

func (a *Analysis) describe(p *types.Project, files map[string]SourceFile) {
	exists := func(rp string) bool { _, ok := files[rp]; return ok }
	dirExists := func(rp string) bool {
		if rp == "." {
			return true
		}
		for f := range files {
			if strings.HasPrefix(f, rp+"/") {
				return true
			}
		}
		return false
	}
	secretFile := func(rp string) bool { f, ok := files[rp]; return ok && f.Secret }

	add := func(name string, s types.ServiceConfig, enabled bool) {
		at := "services." + name
		sv := Service{Name: name, Enabled: enabled, Profiles: nonNil(s.Profiles), Image: s.Image,
			Ports: []Port{}, Mounts: []Mount{}, Networks: []string{}, DependsOn: []string{}, EnvFiles: []string{},
			NetworkMode: s.NetworkMode, Healthcheck: s.HealthCheck != nil && !s.HealthCheck.Disable}
		if !enabled {
			a.add(SeverityWarning, "profile-disabled", "", at, "not started: none of its profiles ("+strings.Join(s.Profiles, ", ")+") is enabled")
		}
		if s.Build != nil {
			b := &Build{Context: s.Build.Context, Dockerfile: s.Build.Dockerfile, Inline: s.Build.DockerfileInline != ""}
			ctxPath := s.Build.Context
			if ctxPath == "" {
				ctxPath = "."
			}
			remote := strings.Contains(ctxPath, "://") || strings.HasPrefix(ctxPath, "git@")
			r, kind := resolve(a.ProjectDir, ctxPath)
			if !remote && kind == pathUnit {
				b.Context = r
				if !dirExists(r) {
					a.add(SeverityError, "build-context-missing", "", at+".build.context", "the build context "+r+" is not among the unit's files")
				} else if !b.Inline {
					df := s.Build.Dockerfile
					if df == "" {
						df = "Dockerfile"
					}
					dfp, dk := resolve(r, df)
					b.Dockerfile = dfp
					if dk != pathUnit || !exists(dfp) {
						a.add(SeverityError, "dockerfile-missing", "", at+".build.dockerfile", "the Dockerfile "+df+" is not among the unit's files")
					}
				}
			} else if remote {
				b.Remote = true
				a.add(SeverityWarning, "build-context-remote", "", at+".build.context", "the build context is fetched by the engine from "+s.Build.Context)
			} else {
				a.add(SeverityError, "path-outside-unit", "", at+".build.context", "the build context "+ctxPath+" is not inside the unit's files")
			}
			sv.Build = b
		}
		for i, port := range s.Ports {
			sv.Ports = append(sv.Ports, Port{HostIP: port.HostIP, Published: port.Published, Target: port.Target, Protocol: port.Protocol})
			if port.Published != "" && (port.HostIP == "" || port.HostIP == "0.0.0.0" || port.HostIP == "::") {
				a.add(SeverityWarning, "port-all-addresses", "", fmt.Sprintf("%s.ports[%d]", at, i),
					"host port "+port.Published+" is published on every address of the host")
			}
		}
		for i, v := range s.Volumes {
			m := Mount{Type: v.Type, Target: v.Target, ReadOnly: v.ReadOnly}
			va := fmt.Sprintf("%s.volumes[%d]", at, i)
			switch v.Type {
			case types.VolumeTypeBind:
				r, kind := resolve(a.ProjectDir, v.Source)
				switch kind {
				case pathUnit:
					m.Source = r
					if exists(r) || dirExists(r) {
						m.Kind = MountSourceFile
					} else {
						m.Kind = MountDataDir
						a.add(SeverityWarning, "data-dir", "", va, r+" is not a unit file: it is a data directory created on the host")
					}
				case pathHost:
					m.Kind, m.Source = MountHostPath, r
					a.hostPathRisk(va, r, v.ReadOnly)
				default:
					m.Kind, m.Source = MountHostPath, v.Source
					a.add(SeverityError, "path-outside-unit", "", va, "the bind mount source "+v.Source+" leaves the unit's files")
				}
			case types.VolumeTypeVolume:
				if v.Source == "" {
					m.Kind = MountAnonymous
				} else {
					m.Kind, m.Source = MountVolume, v.Source
					if vol, ok := p.Volumes[v.Source]; ok && vol.Name != "" {
						m.Source = vol.Name
					}
				}
			case types.VolumeTypeTmpfs:
				m.Kind = MountTmpfs
			default:
				m.Kind, m.Source = MountOther, v.Source
				if v.Type == types.VolumeTypeNamedPipe || v.Type == "npipe" {
					a.add(SeverityError, "mount-unsupported", "", va, "named pipes exist only on Windows engines")
				}
			}
			sv.Mounts = append(sv.Mounts, m)
		}
		for n := range s.Networks {
			sv.Networks = append(sv.Networks, n)
		}
		sort.Strings(sv.Networks)
		for n := range s.DependsOn {
			sv.DependsOn = append(sv.DependsOn, n)
		}
		sort.Strings(sv.DependsOn)
		for i, ef := range s.EnvFiles {
			ea := fmt.Sprintf("%s.env_file[%d]", at, i)
			r, kind := resolve(a.ProjectDir, ef.Path)
			if kind != pathUnit {
				a.add(SeverityError, "path-outside-unit", "", ea, "the env file "+ef.Path+" is not inside the unit's files")
				continue
			}
			sv.EnvFiles = append(sv.EnvFiles, r)
			switch {
			case exists(r) && !secretFile(r):
				a.add(SeverityError, "env-file-not-secret", r, ea, r+" is used as an env file: mark it secret so its values are kept in the secret store")
			case !exists(r) && bool(ef.Required):
				a.add(SeverityError, "env-file-missing", "", ea, "the env file "+r+" is not among the unit's files (variable groups arrive in ticket 109)")
			case !exists(r):
				a.add(SeverityWarning, "env-file-optional-missing", "", ea, "the optional env file "+r+" is not among the unit's files")
			}
		}
		a.serviceRisks(at, s)
		a.Services = append(a.Services, sv)
	}
	for name, s := range p.Services {
		add(name, s, true)
	}
	for name, s := range p.DisabledServices {
		add(name, s, false)
	}
	sort.Slice(a.Services, func(i, j int) bool { return a.Services[i].Name < a.Services[j].Name })

	for k, v := range p.Volumes {
		a.Volumes = append(a.Volumes, Resource{Key: k, Name: v.Name, External: bool(v.External)})
		if v.External {
			a.add(SeverityWarning, "external-volume", "", "volumes."+k, "the volume "+v.Name+" must already exist on the engine")
		}
	}
	for k, v := range p.Networks {
		a.Networks = append(a.Networks, Resource{Key: k, Name: v.Name, External: bool(v.External)})
		if v.External {
			a.add(SeverityWarning, "external-network", "", "networks."+k, "the network "+v.Name+" must already exist on the engine")
		}
	}
	fileObj := func(kind string, k string, v types.FileObjectConfig, mustBeSecret bool) Resource {
		r := Resource{Key: k, Name: v.Name, External: bool(v.External)}
		at := kind + "." + k
		switch {
		case v.File != "":
			rp, kind := resolve(a.ProjectDir, v.File)
			if kind != pathUnit {
				a.add(SeverityError, "path-outside-unit", "", at+".file", "the file "+v.File+" is not inside the unit's files")
				break
			}
			r.File = rp
			if !exists(rp) {
				a.add(SeverityError, "file-missing", "", at+".file", rp+" is not among the unit's files")
			} else if mustBeSecret && !secretFile(rp) {
				a.add(SeverityError, "secret-file-not-secret", rp, at+".file", rp+" holds a Compose secret: mark it secret so it is kept in the secret store")
			}
		case v.Environment != "":
			a.add(SeverityWarning, "object-from-environment", "", at+".environment", "the content comes from the variable "+v.Environment+"; variables are set per environment (ticket 045)")
		}
		return r
	}
	for k, v := range p.Secrets {
		a.Secrets = append(a.Secrets, fileObj("secrets", k, types.FileObjectConfig(v), true))
	}
	for k, v := range p.Configs {
		a.Configs = append(a.Configs, fileObj("configs", k, types.FileObjectConfig(v), false))
	}
	for _, list := range [][]Resource{a.Volumes, a.Networks, a.Secrets, a.Configs} {
		sort.Slice(list, func(i, j int) bool { return list[i].Key < list[j].Key })
	}
	for k := range p.Extensions {
		a.add(SeverityKept, "extension", "", k, "extension kept as written; HazelminX does not read it")
	}
	if dirFile(files, a.ProjectDir, ".env") {
		a.add(SeverityWarning, "dotenv-not-read", path.Join(a.ProjectDir, ".env"), "",
			".env is not read: substitution values come from the environment's variables (ticket 045)")
	}
}

func dirFile(files map[string]SourceFile, dir, name string) bool {
	_, ok := files[path.Join(dir, name)]
	return ok
}

// sensitiveHostPaths are host paths whose mount gives a container control
// of the host.
var sensitiveHostPaths = []string{"/", "/etc", "/root", "/proc", "/sys", "/dev", "/boot", "/var/lib/docker", "/var/lib/containers", "/run", "/var/run"}

var engineSockets = []string{"docker.sock", "podman.sock", "containerd.sock"}

func (a *Analysis) hostPathRisk(at, p string, ro bool) {
	clean := path.Clean(p)
	for _, s := range engineSockets {
		if strings.HasSuffix(clean, "/"+s) {
			a.add(SeverityHighRisk, "engine-socket", "", at, "mounts the engine socket "+clean+": the container controls the engine and the host")
			return
		}
	}
	for _, s := range sensitiveHostPaths {
		if clean == s || (s != "/" && strings.HasPrefix(clean, s+"/")) {
			mode := "read-write"
			if ro {
				mode = "read-only"
			}
			a.add(SeverityHighRisk, "host-system-path", "", at, "mounts the host system path "+clean+" ("+mode+")")
			return
		}
	}
	a.add(SeverityWarning, "host-path", "", at, "mounts the host path "+clean+"; whether it exists is checked on the host before deploying")
}

func (a *Analysis) serviceRisks(at string, s types.ServiceConfig) {
	if s.Privileged {
		a.add(SeverityHighRisk, "privileged", "", at+".privileged", "runs privileged: the container has every capability and all host devices")
	}
	for k, v := range map[string]string{"network_mode": s.NetworkMode, "pid": s.Pid, "ipc": s.Ipc, "userns_mode": s.UserNSMode, "cgroup": s.Cgroup} {
		if v == "host" {
			a.add(SeverityHighRisk, "host-namespace", "", at+"."+k, k+": host shares the host's namespace with the container")
		}
	}
	if len(s.CapAdd) > 0 {
		caps := slices.Clone(s.CapAdd)
		sort.Strings(caps)
		a.add(SeverityHighRisk, "cap-add", "", at+".cap_add", "adds capabilities: "+strings.Join(caps, ", "))
	}
	for i, d := range s.Devices {
		a.add(SeverityHighRisk, "device", "", fmt.Sprintf("%s.devices[%d]", at, i), "gives the container the host device "+d.Source)
	}
	for i, o := range s.SecurityOpt {
		if strings.Contains(o, "unconfined") || strings.HasPrefix(o, "no-new-privileges:false") {
			a.add(SeverityHighRisk, "security-opt", "", fmt.Sprintf("%s.security_opt[%d]", at, i), "weakens confinement: "+o)
		}
	}
}

func (a *Analysis) add(sev, code, file, attr, msg string) {
	a.Findings = append(a.Findings, Finding{Severity: sev, Code: code, File: file, Attribute: attr, Message: msg})
}

func (a *Analysis) hasErrors() bool {
	return slices.ContainsFunc(a.Findings, func(f Finding) bool { return f.Severity == SeverityError })
}

var severityOrder = map[string]int{SeverityError: 0, SeverityHighRisk: 1, SeverityWarning: 2, SeverityKept: 3}

func (a *Analysis) sort() {
	sort.SliceStable(a.Findings, func(i, j int) bool {
		x, y := a.Findings[i], a.Findings[j]
		if severityOrder[x.Severity] != severityOrder[y.Severity] {
			return severityOrder[x.Severity] < severityOrder[y.Severity]
		}
		if x.Attribute != y.Attribute {
			return x.Attribute < y.Attribute
		}
		return x.Code < y.Code
	})
	a.Valid = !a.hasErrors()
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
