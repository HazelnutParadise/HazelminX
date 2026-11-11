package protocol

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
)

type enrollmentFixture struct {
	Code          string   `json:"code"`
	Normalized    string   `json:"normalized"`
	CodeID        string   `json:"codeId"`
	Key           string   `json:"key"`
	CACertificate string   `json:"caCertificate"`
	MAC           string   `json:"mac"`
	Equivalent    []string `json:"equivalent"`
	Aliases       struct {
		Input      string `json:"input"`
		Normalized string `json:"normalized"`
	} `json:"aliases"`
	Invalid []string `json:"invalid"`
}

func loadEnrollmentFixture(t *testing.T) enrollmentFixture {
	t.Helper()
	var f enrollmentFixture
	if err := json.Unmarshal(readFixture(t, "enrollment.json"), &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// The code, its normalized form, key and discovery MAC match the shared
// fixture (the Rust side checks the same file).
func TestEnrollmentFixture(t *testing.T) {
	f := loadEnrollmentFixture(t)
	code, err := ParseEnrollmentCode(f.Code)
	if err != nil {
		t.Fatal(err)
	}
	if code.Normalized() != f.Normalized || code.ID() != f.CodeID {
		t.Fatalf("normalized %q id %q", code.Normalized(), code.ID())
	}
	if hex.EncodeToString(code.Key()) != f.Key {
		t.Fatalf("key %x", code.Key())
	}
	if got := DiscoveryMAC(code.Key(), []byte(f.CACertificate)); got != f.MAC {
		t.Fatalf("mac %s want %s", got, f.MAC)
	}
	if !VerifyDiscoveryMAC(code.Key(), []byte(f.CACertificate), f.MAC) {
		t.Fatal("fixture mac does not verify")
	}
	if VerifyDiscoveryMAC(code.Key(), []byte(f.CACertificate+"\n"), f.MAC) {
		t.Fatal("mac verifies over a changed CA")
	}
	other, _ := ParseEnrollmentCode(f.Aliases.Input)
	if VerifyDiscoveryMAC(other.Key(), []byte(f.CACertificate), f.MAC) {
		t.Fatal("mac verifies under another code")
	}
	if code.String() != f.Code {
		t.Fatalf("display %q", code.String())
	}
}

func TestEnrollmentCodeNormalization(t *testing.T) {
	f := loadEnrollmentFixture(t)
	for _, in := range f.Equivalent {
		c, err := ParseEnrollmentCode(in)
		if err != nil || c.Normalized() != f.Normalized {
			t.Errorf("%q → %q, %v", in, c.Normalized(), err)
		}
	}
	c, err := ParseEnrollmentCode(f.Aliases.Input)
	if err != nil || c.Normalized() != f.Aliases.Normalized {
		t.Errorf("aliases %q → %q, %v", f.Aliases.Input, c.Normalized(), err)
	}
	for _, in := range f.Invalid {
		if _, err := ParseEnrollmentCode(in); !errors.Is(err, ErrEnrollmentCode) {
			t.Errorf("%q accepted (%v)", in, err)
		}
	}
}

func TestNewEnrollmentCodeIsWellFormedAndRandom(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		c, err := NewEnrollmentCode()
		if err != nil {
			t.Fatal(err)
		}
		back, err := ParseEnrollmentCode(c.String())
		if err != nil || back.Normalized() != c.Normalized() {
			t.Fatalf("%s does not parse back: %v", c, err)
		}
		if seen[c.Normalized()] {
			t.Fatalf("repeated code %s", c)
		}
		seen[c.Normalized()] = true
	}
}

func TestDecodeEnrollRequestIsStrict(t *testing.T) {
	good := `{"code":"HZX-7Q4M-2KD9-PX81-C3VB","nodePublicKey":"x","hostname":"h","machineId":"m","os":"o","arch":"a","agentVersion":"v"}`
	r, err := DecodeEnrollRequest([]byte(good))
	if err != nil || r.Hostname != "h" || r.Group != "" {
		t.Fatalf("good: %+v %v", r, err)
	}
	for name, body := range map[string]string{
		"unknown field":   `{"code":"c","nodePublicKey":"x","hostname":"h","machineId":"m","os":"o","arch":"a","agentVersion":"v","extra":1}`,
		"duplicate key":   `{"code":"c","code":"d","nodePublicKey":"x","hostname":"h","machineId":"m","os":"o","arch":"a","agentVersion":"v"}`,
		"case variant":    `{"Code":"c","nodePublicKey":"x","hostname":"h","machineId":"m","os":"o","arch":"a","agentVersion":"v"}`,
		"missing field":   `{"code":"c","nodePublicKey":"x","hostname":"h","os":"o","arch":"a","agentVersion":"v"}`,
		"not an object":   `[1]`,
		"wrong type":      `{"code":1,"nodePublicKey":"x","hostname":"h","machineId":"m","os":"o","arch":"a","agentVersion":"v"}`,
		"trailing object": good + `{}`,
	} {
		if _, err := DecodeEnrollRequest([]byte(body)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
