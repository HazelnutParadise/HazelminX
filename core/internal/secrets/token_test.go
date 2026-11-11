package secrets

import (
	"strings"
	"testing"
)

func TestHashTokenIsVerifyOnly(t *testing.T) {
	d := HashToken("tok_abc123")
	if !strings.HasPrefix(d, "sha256:") || strings.Contains(d, "tok_abc123") {
		t.Fatalf("digest %q", d)
	}
	if !VerifyToken("tok_abc123", d) {
		t.Fatal("valid token rejected")
	}
	if VerifyToken("tok_abc124", d) || VerifyToken("", d) || VerifyToken("tok_abc123", "md5:x") {
		t.Fatal("invalid token accepted")
	}
	if HashToken("a") == HashToken("b") {
		t.Fatal("collision")
	}
}
