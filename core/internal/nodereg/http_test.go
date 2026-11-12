package nodereg

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

func TestAgentHandlerServesEnrollmentAndPassesTheRestOn(t *testing.T) {
	e := newEnv(t)
	_, code := e.newCode(CreateInput{})
	c, _ := protocol.ParseEnrollmentCode(code)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(299) })
	h := e.reg.AgentHandler(next)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.RemoteAddr = "198.51.100.7:5555"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := do(http.MethodGet, protocol.EnrollDiscoveryPath+"?codeId="+c.ID(), "")
	var d protocol.EnrollDiscovery
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &d) != nil || !protocol.VerifyDiscoveryMAC(c.Key(), []byte(d.CACertificate), d.MAC) {
		t.Fatalf("discovery %d %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Content-Length") != strconv.Itoa(rec.Body.Len()) {
		t.Fatal("no Content-Length")
	}
	if rec := do(http.MethodGet, protocol.EnrollDiscoveryPath+"?codeId=ZZZZ", ""); rec.Code != 404 || !strings.Contains(rec.Body.String(), protocol.EnrollCodeInvalid) {
		t.Fatalf("unknown code id %d %s", rec.Code, rec.Body)
	}

	hst := newHost("h", "")
	body, _ := json.Marshal(hst.request(code))
	rec = do(http.MethodPost, protocol.EnrollPath, string(body))
	var r protocol.EnrollResponse
	if rec.Code != 202 || json.Unmarshal(rec.Body.Bytes(), &r) != nil || r.Status != protocol.EnrollPending {
		t.Fatalf("claim %d %s", rec.Code, rec.Body)
	}
	en, _ := e.reg.GetEnrollment(t.Context(), r.EnrollmentID)
	if en.Host.SourceAddr != "198.51.100.7" {
		t.Fatalf("source %q", en.Host.SourceAddr)
	}
	if rec := do(http.MethodPost, protocol.EnrollPath, `{"code":"x","extra":1}`); rec.Code != 400 {
		t.Fatalf("bad body %d", rec.Code)
	}
	if rec := do(http.MethodGet, protocol.EnrollPath, ""); rec.Code != 405 {
		t.Fatalf("GET enroll %d", rec.Code)
	}
	if rec := do(http.MethodGet, protocol.ConnectPath, ""); rec.Code != 299 {
		t.Fatalf("channel path not passed on: %d", rec.Code)
	}
}
