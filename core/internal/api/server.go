// Package api is the HTTP surface of hazelminx-core.
//
// The management contract (ticket 010, spec 36) lives here as shared pieces
// every resource route uses: the error code table (errcodes.go), lists
// (listing.go), versioned writes (version.go), dryRun previews and the
// operation registry (operations.go), 202 job answers (jobresp.go) and the
// OpenAPI document generated from the registry (openapi.go).
package api

import (
	"context"

	"github.com/HazelnutParadise/hazelminx/core/internal/composedeploy"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/confirm"
	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/plans"
	"github.com/gin-gonic/gin"
)

// Version is stamped by the build (-ldflags "-X .../api.Version=1.2.3").
var Version = "dev"

// Health is answered by HealthChecker implementations (store, later agents).
type HealthChecker interface {
	Ping(ctx context.Context) error
}

// Server owns the router.
type Server struct {
	engine *gin.Engine
	health HealthChecker
	ui     fs.FS
	start  time.Time
	// ops is the operation registry behind the overview and OpenAPI; every
	// route is added through handle.
	ops []Operation
	// extras let features add overview fields (job kinds…).
	extras []func(*overviewResponse)

	iam       *iam.Service     // set by MountIAM
	protected *gin.RouterGroup // authenticated /management/v1 group
	origin    string           // configured public origin for passkeys; "" derives it per request
	jobs      *jobs.Engine     // set by RegisterJobs
	confirm   *confirm.Service // set by MountConfirmations
	plans     *plans.Service   // set by MountChangePlans
	// reservedKinds are job kinds POST /jobs refuses: kind → the endpoint
	// that submits them after checking the resource's permissions.
	reservedKinds map[string]string
	// nodeServices lists the Compose projects deployed to a node (set by
	// MountComposeDeploy).
	nodeServices func(ctx context.Context, nodeID string) ([]composedeploy.OnNode, error)
	// platform reports platform-level degradation (addendum 03 機制四);
	// ticket 017 wires the detectors. Nil means not degraded.
	platform func() *PlatformDegradation

	openapiOnce sync.Once
	openapiDoc  []byte
}

// New builds the router. ui may be nil when no interface is embedded.
func New(health HealthChecker, ui fs.FS) *Server {
	gin.SetMode(gin.ReleaseMode)
	e := gin.New()
	// Every answer carries X-Trace-ID, including unknown paths and panics.
	e.Use(traceID(), gin.CustomRecovery(func(c *gin.Context, _ any) {
		respond(c, newError(CodeInternal, "internal error"))
	}))
	s := &Server{engine: e, health: health, ui: ui, start: time.Now()}

	s.handle(e, "", Operation{Method: "GET", Path: "/healthz", ID: "health", Tag: "platform",
		Summary:   "liveness and store health",
		Responses: []Response{{Status: 200, Body: healthResponse{}, Description: "healthy"}, {Status: 503, Body: healthResponse{}, Description: "the store does not answer"}}},
		s.healthz)

	m := e.Group("/management/v1")
	s.handle(m, "/management/v1", Operation{Method: "GET", Path: "/management/v1", ID: "overview", Tag: "platform",
		Summary:   "this overview: every operation with a link to its OpenAPI entry",
		Responses: []Response{{Status: 200, Body: overviewResponse{}}}},
		s.overview)
	m.GET("/", s.overview) // same operation with a trailing slash
	s.handle(m, "/management/v1", Operation{Method: "GET", Path: "/management/v1/openapi.json", ID: "openapi", Tag: "platform",
		Summary:   "OpenAPI 3.1 document of this API, generated from the operation registry",
		Responses: []Response{{Status: 200, Description: "OpenAPI document"}}},
		s.serveOpenAPI)

	e.NoRoute(s.serveUI)
	return s
}

// Handler returns the http.Handler.
func (s *Server) Handler() http.Handler { return s.engine }

type healthResponse struct {
	Status  string            `json:"status"`
	Version string            `json:"version"`
	Checks  map[string]string `json:"checks"`
	Time    string            `json:"time"`
}

func (s *Server) healthz(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	resp := healthResponse{Status: "ok", Version: Version, Checks: map[string]string{}, Time: time.Now().UTC().Format(time.RFC3339)}
	code := http.StatusOK
	if err := s.health.Ping(ctx); err != nil {
		resp.Status = "degraded"
		resp.Checks["store"] = err.Error()
		code = http.StatusServiceUnavailable
	} else {
		resp.Checks["store"] = "ok"
	}
	c.JSON(code, resp)
}

type overviewResponse struct {
	Name       string              `json:"name"`
	Version    string              `json:"version"`
	Build      string              `json:"build"`
	OpenAPI    string              `json:"openapi"`
	Operations []overviewOperation `json:"operations"`
	// JobKinds lists the registered job kinds (when the job engine is
	// mounted).
	JobKinds []string `json:"jobKinds,omitempty"`
	// ChangePlanKinds lists the registered change-plan kinds.
	ChangePlanKinds []string `json:"changePlanKinds,omitempty"`
}

type overviewOperation struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Summary string `json:"summary"`
	// Docs links to the operation in the OpenAPI document.
	Docs string `json:"docs"`
}

// overview is the API discovery document required by the engineering
// guidelines: every public operation is reachable from here.
func (s *Server) overview(c *gin.Context) {
	body := overviewResponse{Name: "HazelminX management API", Version: "v1", Build: Version, OpenAPI: openAPIPath}
	for _, op := range s.ops {
		body.Operations = append(body.Operations, overviewOperation{Method: op.Method, Path: op.Path, Summary: op.Summary, Docs: openAPIRef(op)})
	}
	for _, f := range s.extras {
		f(&body)
	}
	c.JSON(http.StatusOK, body)
}

func (s *Server) serveUI(c *gin.Context) {
	p := c.Request.URL.Path
	if strings.HasPrefix(p, "/management/") || strings.HasPrefix(p, "/integrations/") || c.Request.Method != http.MethodGet {
		respond(c, newError(CodeNotFound, "no such operation: "+c.Request.Method+" "+p))
		return
	}
	if s.ui == nil {
		respond(c, newError(CodeUINotEmbedded, "this build has no web interface embedded; use the management API"))
		return
	}
	name := strings.TrimPrefix(p, "/")
	if name != "" && name != "index.html" {
		if f, err := s.ui.Open(name); err == nil {
			f.Close()
			// Vite fingerprints assets/; everything else (boot.js, favicon) keeps its name across releases.
			if strings.HasPrefix(name, "assets/") {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				c.Header("Cache-Control", "no-cache")
			}
			c.FileFromFS(name, http.FS(s.ui))
			return
		}
		if strings.HasPrefix(name, "assets/") {
			// A build artifact from another release: answering with index.html would make the browser run HTML as a script.
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "no such interface asset", "path": p})
			return
		}
	}
	// index.html is served by hand: http.FS redirects "/index.html" to "/",
	// and unknown paths fall back to it so client-side routing works.
	data, err := fs.ReadFile(s.ui, "index.html")
	if err != nil {
		respond(c, newError(CodeUIIndexMissing, "embedded interface has no index.html"))
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/html; charset=utf-8", data)
}
