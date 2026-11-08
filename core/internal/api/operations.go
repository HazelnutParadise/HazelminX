package api

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

// Operation documents one public operation. Every route is registered
// through Server.handle with its Operation, so the overview, the OpenAPI
// document and the router cannot drift apart (TestRoutesMatchOperations).
type Operation struct {
	Method string
	// Path in OpenAPI syntax: "/management/v1/jobs/{id}".
	Path    string
	ID      string // operationId
	Summary string
	Tag     string
	// Auth: the caller must be signed in (session cookie or bearer token).
	Auth bool
	// Permission describes the check, e.g. "platform:read on the global
	// scope". Empty for public operations.
	Permission string
	Params     []Param
	// Request is a value of the JSON body type; nil when there is no body.
	Request any
	// Responses are the success answers.
	Responses []Response
	// Errors are the codes this operation can answer besides the ones every
	// operation of its kind can (see commonErrors).
	Errors []string
	// DryRun: the operation accepts dryRun=true and answers a DryRunResult
	// without changing anything (ROB-003). Writes without it refuse the
	// parameter with DRY_RUN_UNSUPPORTED.
	DryRun bool
	// Versioned: the write requires If-Match (VERSION_REQUIRED,
	// VERSION_MISMATCH).
	Versioned bool
	// List: the operation is a list endpoint with these bounds.
	List *ListSpec
	// LongRunning: the operation answers 202 with a job (JobAccepted).
	LongRunning bool
}

// Param is a path, query or header parameter beyond the ones derived from
// List, DryRun and Versioned.
type Param struct {
	Name        string
	In          string // "path", "query", "header"
	Required    bool
	Description string
	Schema      jsonSchema
}

// Response is one documented success answer.
type Response struct {
	Status      int
	Description string
	// Body is a value of the JSON body type; nil for an empty body.
	Body any
	// Headers names response headers (ETag, Location).
	Headers []string
}

const dryRunKey = "hazelminx.dryRun"

var pathParam = regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`)

// ginPath converts "/a/{id}" to "/a/:id".
func ginPath(p string) string { return pathParam.ReplaceAllString(p, ":$1") }

// handle registers op on g (whose base path must prefix op.Path) and
// records it. A dryRun guard runs right before the final handler, after the
// group's authentication and the route's permission checks, so a refusal
// never says more than those checks allow.
func (s *Server) handle(g gin.IRoutes, base string, op Operation, handlers ...gin.HandlerFunc) {
	if !strings.HasPrefix(op.Path, base) {
		panic("api: operation " + op.Path + " is outside group " + base)
	}
	rel := ginPath(strings.TrimPrefix(op.Path, base))
	chain := append([]gin.HandlerFunc{}, handlers[:len(handlers)-1]...)
	chain = append(chain, dryRunGuard(op), handlers[len(handlers)-1])
	g.Handle(op.Method, rel, chain...)
	s.ops = append(s.ops, op)
}

// dryRunGuard enforces the dryRun contract: a write that did not declare
// dry-run support refuses the parameter instead of ignoring it and acting
// for real; a declared one accepts only "true" or "false".
func dryRunGuard(op Operation) gin.HandlerFunc {
	mutating := op.Method != http.MethodGet && op.Method != http.MethodHead
	return func(c *gin.Context) {
		v, present := c.GetQuery("dryRun")
		if !present {
			c.Next()
			return
		}
		if !op.DryRun {
			if mutating {
				respond(c, newError(CodeDryRunUnsupported, "this operation has no dryRun preview; nothing was changed"))
				return
			}
			c.Next() // read operations: lists refuse it as an undeclared parameter
			return
		}
		switch v {
		case "true":
			c.Set(dryRunKey, true)
		case "false":
		default:
			e := newError(CodeValidationFailed, `dryRun must be "true" or "false"`)
			e.Fields = []FieldError{{Field: "dryRun", Message: `expected "true" or "false"`}}
			respond(c, e)
			return
		}
		c.Next()
	}
}

// isDryRun reports whether the request asked for a preview.
func isDryRun(c *gin.Context) bool { return c.GetBool(dryRunKey) }

// DryRunResult is the answer of a write called with dryRun=true. Blockers
// are what the real call would refuse with, most important first (the real
// call answers Blockers[0]); Impact lists who would be affected (机制二).
// Identity, permission and input errors are not previews: they answer with
// their own status as the real call would.
type DryRunResult struct {
	DryRun   bool       `json:"dryRun"`
	OK       bool       `json:"ok"`
	Blockers []apiError `json:"blockers"`
	Impact   Impact     `json:"impact"`
	// Preview is operation-specific detail (for jobs: whether the request
	// would replay an existing job, which jobs it would queue behind).
	Preview any `json:"preview,omitempty"`
}

// Impact is the affected-items preview. Ticket 107 computes it from the
// dependency graph; until then operations list what they know directly.
type Impact struct {
	Direct   []ImpactItem `json:"direct"`
	Indirect []ImpactItem `json:"indirect"`
}

// ImpactItem is one affected item of the dependency graph.
type ImpactItem struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Scope string `json:"scope,omitempty"`
	// Relation says how it is affected ("locked", "queued-behind", …).
	Relation string `json:"relation,omitempty"`
}

// respondDryRun sends a preview. blockers are ordered with pickBlocking's
// precedence.
func respondDryRun(c *gin.Context, blockers []apiError, impact Impact, preview any) {
	out := DryRunResult{DryRun: true, Blockers: []apiError{}, Impact: impact, Preview: preview}
	if out.Impact.Direct == nil {
		out.Impact.Direct = []ImpactItem{}
	}
	if out.Impact.Indirect == nil {
		out.Impact.Indirect = []ImpactItem{}
	}
	if top := pickBlocking(blockers); top != nil {
		out.Blockers = append(out.Blockers, *top)
		for _, code := range top.AlsoBlockedBy {
			for _, b := range blockers {
				if b.Code == code {
					out.Blockers = append(out.Blockers, b)
					break
				}
			}
		}
		out.Blockers[0].AlsoBlockedBy = nil
	}
	out.OK = len(out.Blockers) == 0
	c.JSON(http.StatusOK, out)
}
