package nodereg

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

const maxEnrollBody = 16 << 10

// AgentHandler serves the agent listener: enrollment on its two paths and
// everything else (the channel) by next.
func (r *Registry) AgentHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case protocol.EnrollDiscoveryPath:
			if req.Method != http.MethodGet {
				writeJSON(w, http.StatusMethodNotAllowed, refusedBody(protocol.EnrollRequestInvalid, "use GET"))
				return
			}
			d, err := r.Discovery(req.Context(), req.URL.Query().Get("codeId"))
			switch {
			case errors.Is(err, ErrCodeNotFound):
				writeJSON(w, http.StatusNotFound, refusedBody(protocol.EnrollCodeInvalid, "no usable enrollment code with this id"))
			case err != nil:
				writeJSON(w, http.StatusInternalServerError, refusedBody("INTERNAL", "internal error"))
			default:
				writeJSON(w, http.StatusOK, d)
			}
		case protocol.EnrollPath:
			if req.Method != http.MethodPost {
				writeJSON(w, http.StatusMethodNotAllowed, refusedBody(protocol.EnrollRequestInvalid, "use POST"))
				return
			}
			body, err := io.ReadAll(io.LimitReader(req.Body, maxEnrollBody+1))
			if err != nil || len(body) > maxEnrollBody {
				writeJSON(w, http.StatusBadRequest, refusedBody(protocol.EnrollRequestInvalid, "request body missing or larger than 16 KiB"))
				return
			}
			er, err := protocol.DecodeEnrollRequest(body)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, refusedBody(protocol.EnrollRequestInvalid, err.Error()))
				return
			}
			status, resp := r.Claim(req.Context(), er, remoteHost(req.RemoteAddr))
			writeJSON(w, status, resp)
		default:
			next.ServeHTTP(w, req)
		}
	})
}

func refusedBody(code, msg string) protocol.EnrollResponse {
	return protocol.EnrollResponse{Status: protocol.EnrollRefused, Code: code, Message: msg}
}

// writeJSON always sets Content-Length: the agent's HTTP client reads
// exactly that many bytes.
func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		status, b = http.StatusInternalServerError, []byte(`{"status":"refused","code":"INTERNAL","message":"internal error"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func remoteHost(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}
