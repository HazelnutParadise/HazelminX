package gitsource_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// redirector answers every request with a redirect to target.
func redirector(t *testing.T, target string) string {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target+"/info/refs?service=git-upload-pack", http.StatusFound)
	}))
	t.Cleanup(s.Close)
	return s.URL
}
