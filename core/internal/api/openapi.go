package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const openAPIPath = "/management/v1/openapi.json"

// openAPIRef links an operation to its entry in the OpenAPI document (a JSON
// pointer: "/" in the path is escaped as "~1").
func openAPIRef(op Operation) string {
	return openAPIPath + "#/paths/" + strings.ReplaceAll(op.Path, "/", "~1") + "/" + strings.ToLower(op.Method)
}

func (s *Server) serveOpenAPI(c *gin.Context) {
	s.openapiOnce.Do(func() {
		doc, err := json.Marshal(s.openAPI())
		if err != nil {
			panic("api: OpenAPI document does not marshal: " + err.Error())
		}
		s.openapiDoc = doc
	})
	c.Data(http.StatusOK, "application/json; charset=utf-8", s.openapiDoc)
}

// commonErrors are the codes every operation of a kind may answer, in
// addition to Operation.Errors.
func commonErrors(op Operation) []string {
	codes := []string{CodeValidationFailed, CodeInternal}
	if op.Auth {
		codes = append(codes, CodeUnauthenticated, CodeForbidden, CodeNotFound)
		if op.Method != http.MethodGet {
			codes = append(codes, CodeCSRFMismatch, CodeOriginMismatch)
		}
	}
	if op.Request != nil {
		codes = append(codes, CodePayloadTooLarge)
	}
	if op.Method != http.MethodGet && !op.DryRun {
		codes = append(codes, CodeDryRunUnsupported)
	}
	if op.Versioned {
		codes = append(codes, CodeVersionRequired, CodeVersionMismatch)
	}
	if op.List != nil {
		codes = append(codes, CodeQueryLimitExceeded)
	}
	return codes
}

// operationErrors is the sorted, de-duplicated error list of op.
func operationErrors(op Operation) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range append(commonErrors(op), op.Errors...) {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

// openAPI builds the OpenAPI 3.1 document from the operation registry and
// the Go types the handlers send.
func (s *Server) openAPI() map[string]any {
	comps := map[string]jsonSchema{}
	ref := func(v any) jsonSchema {
		if v == nil {
			return nil
		}
		return schemaOf(reflect.TypeOf(v), comps)
	}
	errSchema := ref(apiError{})
	codes := ErrorCodes()
	var codeNames []string
	table := []map[string]any{}
	for _, e := range codes {
		codeNames = append(codeNames, e.Code)
		table = append(table, map[string]any{"code": e.Code, "status": e.Status, "retryable": e.Retryable, "meaning": e.Meaning})
	}
	if name := strings.TrimPrefix(errSchema["$ref"].(string), "#/components/schemas/"); comps[name] != nil {
		props := comps[name]["properties"].(map[string]any)
		props["code"] = jsonSchema{"type": "string", "enum": codeNames}
	}

	paths := map[string]map[string]any{}
	for _, op := range s.ops {
		o := map[string]any{"operationId": op.ID, "summary": op.Summary}
		if op.Tag != "" {
			o["tags"] = []string{op.Tag}
		}
		if op.Auth {
			o["security"] = []map[string][]string{{"session": {}}, {"bearer": {}}}
		} else {
			o["security"] = []map[string][]string{}
		}
		if op.Permission != "" {
			o["x-hazelminx-permission"] = op.Permission
		}
		var params []map[string]any
		for _, m := range pathParam.FindAllStringSubmatch(op.Path, -1) {
			params = append(params, map[string]any{"name": m[1], "in": "path", "required": true, "schema": jsonSchema{"type": "string"}})
		}
		for _, p := range op.Params {
			sch := p.Schema
			if sch == nil {
				sch = jsonSchema{"type": "string"}
			}
			params = append(params, map[string]any{"name": p.Name, "in": p.In, "required": p.Required, "description": p.Description, "schema": sch})
		}
		if op.List != nil {
			params = append(params, listParams(*op.List)...)
			sp := withListDefaults(*op.List)
			o["x-hazelminx-list"] = map[string]any{"name": sp.Name, "defaultLimit": sp.DefaultLimit, "maxLimit": sp.MaxLimit,
				"queryCap": sp.QueryCap, "timeoutSeconds": int(sp.Timeout.Seconds())}
		}
		if op.DryRun {
			params = append(params, map[string]any{"name": "dryRun", "in": "query", "required": false,
				"description": "true answers a DryRunResult preview and changes nothing (ROB-003)", "schema": jsonSchema{"type": "boolean"}})
			o["x-hazelminx-dry-run"] = true
		}
		if op.Versioned {
			params = append(params, map[string]any{"name": "If-Match", "in": "header", "required": true,
				"description": `"v<version>" from the ETag of the resource as you read it`, "schema": jsonSchema{"type": "string", "pattern": `^"v[0-9]+"$`}})
			o["x-hazelminx-versioned"] = true
		}
		if op.Auth && op.Method != http.MethodGet {
			params = append(params, map[string]any{"name": CSRFHeader, "in": "header", "required": false,
				"description": "required with cookie sessions: the csrfToken from login or /auth/session", "schema": jsonSchema{"type": "string"}})
		}
		if op.LongRunning {
			o["x-hazelminx-long-running"] = true
		}
		if params != nil {
			o["parameters"] = params
		}
		if op.Request != nil {
			o["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": ref(op.Request)}}}
		}
		responses := map[string]any{}
		for _, r := range op.Responses {
			desc := r.Description
			if desc == "" {
				desc = http.StatusText(r.Status)
			}
			resp := map[string]any{"description": desc}
			if r.Body != nil {
				resp["content"] = map[string]any{"application/json": map[string]any{"schema": ref(r.Body)}}
			}
			if len(r.Headers) > 0 {
				h := map[string]any{}
				for _, name := range r.Headers {
					h[name] = map[string]any{"schema": jsonSchema{"type": "string"}}
				}
				resp["headers"] = h
			}
			responses[strconv.Itoa(r.Status)] = resp
		}
		byStatus := map[int][]string{}
		for _, code := range operationErrors(op) {
			st := lookupCode(code).Status
			byStatus[st] = append(byStatus[st], code)
		}
		for st, list := range byStatus {
			responses[strconv.Itoa(st)] = map[string]any{
				"description": http.StatusText(st) + ": " + strings.Join(list, ", "),
				"content":     map[string]any{"application/json": map[string]any{"schema": errSchema}},
			}
		}
		o["responses"] = responses
		o["x-hazelminx-error-codes"] = operationErrors(op)
		if paths[op.Path] == nil {
			paths[op.Path] = map[string]any{}
		}
		paths[op.Path][strings.ToLower(op.Method)] = o
	}

	schemas := map[string]any{}
	for k, v := range comps {
		schemas[k] = v
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "HazelminX management API",
			"version": Version,
			"description": "Generated from the route registry of hazelminx-core. Errors share one body (Error) whose code comes from " +
				"x-hazelminx-error-codes; when several refusals apply, the one earliest in x-hazelminx-error-precedence is sent and the " +
				"others are listed in alsoBlockedBy. Lists answer {items, page} and page with nextCursor. Writes on versioned resources " +
				"need If-Match. Long operations answer 202 with a job.",
		},
		"paths": paths,
		"components": map[string]any{
			"schemas": schemas,
			"securitySchemes": map[string]any{
				"session": map[string]any{"type": "apiKey", "in": "cookie", "name": SessionCookie,
					"description": "browser session from POST /management/v1/auth/login; state changes also need the " + CSRFHeader + " header"},
				"bearer": map[string]any{"type": "http", "scheme": "bearer", "description": "API token (ticket 015)"},
			},
		},
		"x-hazelminx-error-codes":      table,
		"x-hazelminx-error-precedence": BlockingOrder(),
	}
}

// listParams documents the query parameters of a list.
func listParams(spec ListSpec) []map[string]any {
	sp := withListDefaults(spec)
	var sorts []string
	for _, f := range append(append([]string{}, sp.Sorts...), sp.TieBreaker) {
		sorts = append(sorts, f, "-"+f)
	}
	out := []map[string]any{
		{"name": "limit", "in": "query", "description": "rows per page", "schema": jsonSchema{"type": "integer", "minimum": 1, "maximum": sp.MaxLimit, "default": sp.DefaultLimit}},
		{"name": "cursor", "in": "query", "description": "page.nextCursor of the previous page; continues the same query", "schema": jsonSchema{"type": "string"}},
		{"name": "sort", "in": "query", "description": "comma-separated fields, '-' for descending; default " + renderSort(sp.DefaultSort),
			"schema": jsonSchema{"type": "string"}, "x-hazelminx-sort-fields": sorts},
	}
	for _, f := range sp.Filters {
		sch := jsonSchema{"type": "string"}
		switch {
		case f.Int:
			sch = jsonSchema{"type": "integer", "minimum": 0}
		case len(f.Enum) > 0 && f.Multi:
			sch = jsonSchema{"type": "string", "description": "comma-separated values of: " + strings.Join(f.Enum, ", ")}
		case len(f.Enum) > 0:
			sch = jsonSchema{"type": "string", "enum": f.Enum}
		}
		out = append(out, map[string]any{"name": f.Name, "in": "query", "description": f.Summary, "schema": sch})
	}
	return out
}
