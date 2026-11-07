// Package api is the HTTP surface of hazelminx-core.
//
// Only the health endpoint and the /management/v1 overview exist here; the
// contract (pagination, If-Match, 202 + jobId, error format) lands in ticket
// 010 and authentication in ticket 009. Everything under /management/v1 will
// sit behind those middlewares; nothing here may be mistaken for an authorized
// write path.
package api

import (
	"context"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
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
	// operations is the discovery list served by overview; every registered
	// feature appends its public operations here.
	operations []gin.H
	// extras are extra overview fields contributed by features (job kinds…).
	extras []func() gin.H

	iam       *iam.Service     // set by MountIAM
	protected *gin.RouterGroup // authenticated /management/v1 group
	origin    string           // configured public origin for passkeys; "" derives it per request
}

// New builds the router. ui may be nil when no interface is embedded.
func New(health HealthChecker, ui fs.FS) *Server {
	gin.SetMode(gin.ReleaseMode)
	e := gin.New()
	e.Use(gin.Recovery())
	s := &Server{engine: e, health: health, ui: ui, start: time.Now()}

	e.GET("/healthz", s.healthz)
	s.addOperation("GET", "/healthz", "liveness and store health")

	m := e.Group("/management/v1")
	m.GET("", s.overview)
	m.GET("/", s.overview)
	s.addOperation("GET", "/management/v1", "this overview")

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

// overview is the API discovery document required by the engineering
// guidelines: every public operation must be reachable from here.
func (s *Server) overview(c *gin.Context) {
	body := gin.H{
		"name":       "HazelminX management API",
		"version":    "v1",
		"build":      Version,
		"operations": s.operations,
		"openapi":    nil, // ticket 010 publishes /management/v1/openapi.json
	}
	for _, f := range s.extras {
		for k, v := range f() {
			body[k] = v
		}
	}
	c.JSON(http.StatusOK, body)
}

func (s *Server) addOperation(method, path, summary string) {
	s.operations = append(s.operations, gin.H{"method": method, "path": path, "summary": summary})
}

func (s *Server) serveUI(c *gin.Context) {
	p := c.Request.URL.Path
	if strings.HasPrefix(p, "/management/") || strings.HasPrefix(p, "/integrations/") || c.Request.Method != http.MethodGet {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "no such operation", "path": p})
		return
	}
	if s.ui == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "UI_NOT_EMBEDDED", "message": "this build has no web interface embedded; use the management API"})
		return
	}
	name := strings.TrimPrefix(p, "/")
	if name != "" && name != "index.html" {
		if f, err := s.ui.Open(name); err == nil {
			f.Close()
			c.FileFromFS(name, http.FS(s.ui))
			return
		}
	}
	// index.html is served by hand: http.FS redirects "/index.html" to "/",
	// and unknown paths fall back to it so client-side routing works.
	data, err := fs.ReadFile(s.ui, "index.html")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "UI_INDEX_MISSING", "message": "embedded interface has no index.html"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/html; charset=utf-8", data)
}
