package api

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// Versioned writes (spec 36.1, 34.2). A modifiable resource carries an
// integer version that grows by one on every write. Reads send it as
// `ETag: "v<version>"`; a write must send it back in If-Match. A missing
// condition is 428 VERSION_REQUIRED and a stale one 412 VERSION_MISMATCH
// with details.currentVersion, so nothing is overwritten silently.

// ETag formats a version as a strong entity tag.
func ETag(version int64) string { return `"v` + strconv.FormatInt(version, 10) + `"` }

// setETag sends the version of the resource being returned.
func setETag(c *gin.Context, version int64) { c.Header("ETag", ETag(version)) }

// parseETag reads one strong tag written by ETag. Weak tags, lists and "*"
// are refused: a versioned write names exactly the version it read.
func parseETag(v string) (int64, bool) {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, `"v`) || !strings.HasSuffix(v, `"`) || len(v) < 4 {
		return 0, false
	}
	n, err := strconv.ParseInt(v[2:len(v)-1], 10, 64)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// requireIfMatch returns the version the client read. It answers the
// request itself and returns ok=false when the header is missing or "*"
// (428 VERSION_REQUIRED) or malformed (400).
func requireIfMatch(c *gin.Context) (version int64, ok bool) {
	raw := strings.TrimSpace(c.GetHeader("If-Match"))
	// "*" only asserts that the resource exists (RFC 9110 13.1.1); it would
	// bypass the lost-update check, so it counts as no condition.
	if raw == "" || raw == "*" {
		respond(c, newError(CodeVersionRequired, `send If-Match: "v<version>" with the version you read (ETag of the GET)`))
		return 0, false
	}
	v, ok := parseETag(raw)
	if !ok {
		e := newError(CodeValidationFailed, `If-Match must be one strong entity tag "v<version>" as sent in ETag; weak tags and lists are not accepted`)
		e.Fields = []FieldError{{Field: "If-Match", Message: "expected \"v<version>\""}}
		respond(c, e)
		return 0, false
	}
	return v, true
}

// versionMismatch is the 412 for a stale If-Match.
func versionMismatch(resourceID string, current int64) apiError {
	e := newError(CodeVersionMismatch, "the resource changed after you read it; reload it and decide again")
	e.ResourceID = resourceID
	e.Details = map[string]any{"currentVersion": current, "currentETag": ETag(current)}
	return e
}
