package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HazelnutParadise/hazelminx/core/internal/confirm"
)

type connInfoKey struct{}

// connInfo is what Core knows about the TCP connection carrying a request.
type connInfo struct {
	OpenedAt time.Time
}

// ConnContext is installed as http.Server.ConnContext: it records when each
// connection was accepted, so a ROB-010 confirmation can prove it came over
// a connection opened after the change. HTTP/2 streams share their
// connection's context.
func ConnContext(ctx context.Context, _ net.Conn) context.Context {
	return context.WithValue(ctx, connInfoKey{}, connInfo{OpenedAt: time.Now()})
}

func connOpenedAt(r *http.Request) time.Time {
	if ci, ok := r.Context().Value(connInfoKey{}).(connInfo); ok {
		return ci.OpenedAt
	}
	return time.Time{} // unknown: never counts as a new connection
}

func localAddr(r *http.Request) string {
	if a, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		return a.String()
	}
	return ""
}

type confirmationResponse struct {
	Confirmation *confirm.Confirmation `json:"confirmation"`
}

// MountConfirmations adds the ROB-010 confirmation endpoints and makes
// writes on resources with a pending confirmation answer
// CONFIRMATION_REQUIRED. Ticket 024 creates the confirmations.
func (s *Server) MountConfirmations(svc *confirm.Service) {
	if s.protected == nil {
		panic("api: MountConfirmations called before MountIAM")
	}
	s.confirm = svc
	const base = "/management/v1"
	s.handle(s.protected, base, Operation{Method: "GET", Path: base + "/confirmations/{id}", ID: "getConfirmation", Tag: "confirmations", Auth: true,
		Summary:    "a pending or finished confirmation of a change that may cut the management path (ROB-010)",
		Permission: "only the operator who applied the change; everyone else gets NOT_FOUND",
		Responses:  []Response{{Status: 200, Body: confirmationResponse{}}}}, s.getConfirmation)
	s.handle(s.protected, base, Operation{Method: "POST", Path: base + "/confirmations/{id}", ID: "confirmChange", Tag: "confirmations", Auth: true,
		Summary: "keep a change by confirming it from a connection opened after the change (through the new entry when the entry moved); " +
			"without it the change is reverted when the window ends. Behind a reverse proxy Core cannot tell connections apart, so this assumes direct access to Core.",
		Permission: "only the operator who applied the change (a session or an API token of the same account)",
		Responses:  []Response{{Status: 200, Body: confirmationResponse{}}},
		Errors:     []string{CodeConfirmExpired, CodeConfirmNewConn, CodePlatformDegraded}}, s.confirmChange)
}

func confirmError(err error) apiError {
	switch {
	case errors.Is(err, confirm.ErrNotFound):
		return newError(CodeNotFound, "no such resource")
	case errors.Is(err, confirm.ErrExpired):
		return newError(CodeConfirmExpired, "the confirmation window is over; the change is or will be reverted")
	case errors.Is(err, confirm.ErrNewConnectionRequired):
		return newError(CodeConfirmNewConn, strings.TrimPrefix(err.Error(), confirm.ErrNewConnectionRequired.Error()+": "))
	}
	return newError(CodeInternal, "internal error")
}

func (s *Server) getConfirmation(c *gin.Context) {
	cf, err := s.confirm.Get(c.Request.Context(), c.Param("id"), Principal(c).User.ID)
	if err != nil {
		respond(c, confirmError(err))
		return
	}
	c.JSON(http.StatusOK, confirmationResponse{Confirmation: cf})
}

func (s *Server) confirmChange(c *gin.Context) {
	ctx := c.Request.Context()
	uid := Principal(c).User.ID
	// Visibility first (51.6). Confirming is not new work: only a database
	// that takes no writes stops it; any other degradation must not turn a
	// working management path into an automatic revert.
	if _, err := s.confirm.Get(ctx, c.Param("id"), uid); err != nil {
		respond(c, confirmError(err))
		return
	}
	if s.platform != nil {
		if d := s.platform(); d != nil && d.Mode == PlatformDBReadOnly {
			e := newError(CodePlatformDegraded, "the platform database is read-only; the confirmation cannot be recorded")
			e.Details = map[string]any{"mode": d.Mode}
			respond(c, e)
			return
		}
	}
	cf, err := s.confirm.Confirm(ctx, c.Param("id"), confirm.Attempt{
		UserID: uid, ConnOpenedAt: connOpenedAt(c.Request), Host: c.Request.Host, LocalAddr: localAddr(c.Request),
	})
	if err != nil {
		respond(c, confirmError(err))
		return
	}
	c.JSON(http.StatusOK, confirmationResponse{Confirmation: cf})
}
