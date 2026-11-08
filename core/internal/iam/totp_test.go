package iam

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// totpRFCSecret is the ASCII seed used by RFC 4226 appendix D and RFC 6238
// appendix B.
var totpRFCSecret = []byte("12345678901234567890")

// totpRequireDistinct fails the test when two of the labelled codes are
// equal. The verification tests need the accepted and rejected codes to differ,
// otherwise a wrong implementation could pass by accident.
func totpRequireDistinct(t *testing.T, codes map[string]string) {
	t.Helper()
	seen := map[string]string{}
	for label, code := range codes {
		if other, dup := seen[code]; dup {
			t.Fatalf("test setup invalid: code %q is shared by %s and %s, pick another time or step", code, label, other)
		}
		seen[code] = label
	}
}

// TestTOTPHOTPRFC4226Vectors checks TOTPCode against RFC 4226 appendix D.
func TestTOTPHOTPRFC4226Vectors(t *testing.T) {
	want := []string{
		"755224", "287082", "359152", "969429", "338314",
		"254676", "287922", "162583", "399871", "520489",
	}
	for counter, w := range want {
		got := TOTPCode(totpRFCSecret, int64(counter))
		if got != w {
			t.Errorf("TOTPCode(secret %q, counter %d) = %q, want %q (RFC 4226 appendix D)", totpRFCSecret, counter, got, w)
		}
	}
}

// TestTOTPRFC6238SHA1Vectors checks the last six digits of the RFC 6238
// appendix B SHA1 vectors, with the step derived through TOTPStep.
func TestTOTPRFC6238SHA1Vectors(t *testing.T) {
	cases := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	}
	for _, c := range cases {
		step := TOTPStep(time.Unix(c.unix, 0))
		got := TOTPCode(totpRFCSecret, step)
		if got != c.want {
			t.Errorf("TOTPCode(secret, TOTPStep(unix %d)=%d) = %q, want %q (RFC 6238 appendix B, last 6 digits)", c.unix, step, got, c.want)
		}
	}
}

// TestTOTPStepBoundaries checks floor(unix seconds / 30) around step edges.
func TestTOTPStepBoundaries(t *testing.T) {
	cases := []struct {
		unix int64
		want int64
	}{
		{0, 0},
		{29, 0},
		{30, 1},
		{59, 1},
		{60, 2},
	}
	for _, c := range cases {
		got := TOTPStep(time.Unix(c.unix, 0))
		if got != c.want {
			t.Errorf("TOTPStep(unix %d) = %d, want %d (floor(seconds/30))", c.unix, got, c.want)
		}
	}
}

// totpWindowStep is the step that contains unix 1111111111 (1 second in).
const totpWindowStep = int64(37037037)

func totpWindowNow() time.Time { return time.Unix(1111111111, 0) }

// TestTOTPVerifyAcceptsCurrentAndAdjacentSteps checks the skew window of one
// step either way, and that the returned step is the step the code belongs to.
func TestTOTPVerifyAcceptsCurrentAndAdjacentSteps(t *testing.T) {
	now := totpWindowNow()
	s := totpWindowStep
	codes := map[string]string{
		"step-1": TOTPCode(totpRFCSecret, s-1),
		"step":   TOTPCode(totpRFCSecret, s),
		"step+1": TOTPCode(totpRFCSecret, s+1),
	}
	totpRequireDistinct(t, codes)

	for _, c := range []struct {
		label string
		step  int64
	}{
		{"step-1", s - 1},
		{"step", s},
		{"step+1", s + 1},
	} {
		got, ok := VerifyTOTP(totpRFCSecret, codes[c.label], now, 0)
		if !ok {
			t.Errorf("VerifyTOTP(code %q for %s = step %d, now %d, lastStep 0) ok = false, want true", codes[c.label], c.label, c.step, now.Unix())
			continue
		}
		if got != c.step {
			t.Errorf("VerifyTOTP(code %q for %s, now %d, lastStep 0) step = %d, want %d", codes[c.label], c.label, now.Unix(), got, c.step)
		}
	}
}

// TestTOTPVerifyRejectsOutsideSkewWindow checks that codes two steps away
// from now are refused, with ok=false and step 0.
func TestTOTPVerifyRejectsOutsideSkewWindow(t *testing.T) {
	now := totpWindowNow()
	s := totpWindowStep
	codes := map[string]string{
		"step-2": TOTPCode(totpRFCSecret, s-2),
		"step+2": TOTPCode(totpRFCSecret, s+2),
		"step-1": TOTPCode(totpRFCSecret, s-1),
		"step":   TOTPCode(totpRFCSecret, s),
		"step+1": TOTPCode(totpRFCSecret, s+1),
	}
	totpRequireDistinct(t, codes)

	for _, label := range []string{"step-2", "step+2"} {
		got, ok := VerifyTOTP(totpRFCSecret, codes[label], now, 0)
		if ok || got != 0 {
			t.Errorf("VerifyTOTP(code %q for %s, now %d, lastStep 0) = (step %d, ok %v), want (step 0, ok false)", codes[label], label, now.Unix(), got, ok)
		}
	}
}

// TestTOTPVerifyRejectsReplayedCode checks that a code accepted at step s is
// refused when verified again with lastStep = s.
func TestTOTPVerifyRejectsReplayedCode(t *testing.T) {
	now := totpWindowNow()
	s := totpWindowStep
	code := TOTPCode(totpRFCSecret, s)

	got, ok := VerifyTOTP(totpRFCSecret, code, now, 0)
	if !ok || got != s {
		t.Fatalf("first VerifyTOTP(code %q, now %d, lastStep 0) = (step %d, ok %v), want (step %d, ok true)", code, now.Unix(), got, ok, s)
	}

	got, ok = VerifyTOTP(totpRFCSecret, code, now, s)
	if ok || got != 0 {
		t.Errorf("replay VerifyTOTP(code %q, now %d, lastStep %d) = (step %d, ok %v), want (step 0, ok false)", code, now.Unix(), s, got, ok)
	}
}

// TestTOTPVerifyRejectsStepsAtOrBeforeLastStep checks that with lastStep = s,
// the code of step s-1 is refused too, not only the exact replay.
func TestTOTPVerifyRejectsStepsAtOrBeforeLastStep(t *testing.T) {
	now := totpWindowNow()
	s := totpWindowStep
	codes := map[string]string{
		"step-1": TOTPCode(totpRFCSecret, s-1),
		"step":   TOTPCode(totpRFCSecret, s),
	}
	totpRequireDistinct(t, codes)

	for _, label := range []string{"step-1", "step"} {
		got, ok := VerifyTOTP(totpRFCSecret, codes[label], now, s)
		if ok || got != 0 {
			t.Errorf("VerifyTOTP(code %q for %s, now %d, lastStep %d) = (step %d, ok %v), want (step 0, ok false)", codes[label], label, now.Unix(), s, got, ok)
		}
	}
}

// TestTOTPVerifyAcceptsNextStepAfterLastStep checks that a step after the
// last accepted one is still accepted, and reports that step.
func TestTOTPVerifyAcceptsNextStepAfterLastStep(t *testing.T) {
	now := totpWindowNow()
	s := totpWindowStep
	code := TOTPCode(totpRFCSecret, s+1)

	got, ok := VerifyTOTP(totpRFCSecret, code, now, s)
	if !ok || got != s+1 {
		t.Errorf("VerifyTOTP(code %q for step %d, now %d, lastStep %d) = (step %d, ok %v), want (step %d, ok true)", code, s+1, now.Unix(), s, got, ok, s+1)
	}
}

// TestTOTPVerifyRejectsMalformedCodes checks that anything that is not
// exactly TOTPDigits ASCII digits after removing spaces is refused.
func TestTOTPVerifyRejectsMalformedCodes(t *testing.T) {
	now := time.Unix(59, 0)
	inputs := []string{
		"",
		"12345",
		"1234567",
		"12a456",
		"１２３４５６", // full-width digits
	}
	for _, in := range inputs {
		got, ok := VerifyTOTP(totpRFCSecret, in, now, 0)
		if ok || got != 0 {
			t.Errorf("VerifyTOTP(code %q, now %d, lastStep 0) = (step %d, ok %v), want (step 0, ok false)", in, now.Unix(), got, ok)
		}
	}
}

// TestTOTPVerifyAcceptsSpacedCode checks that a code typed with a space in
// the middle, as authenticator apps display it, is accepted.
func TestTOTPVerifyAcceptsSpacedCode(t *testing.T) {
	now := time.Unix(59, 0)
	code := TOTPCode(totpRFCSecret, 1)
	if code != "287082" {
		t.Fatalf("test setup invalid: TOTPCode(secret, 1) = %q, want %q", code, "287082")
	}
	in := code[:3] + " " + code[3:]
	if in != "287 082" {
		t.Fatalf("test setup invalid: spaced input = %q, want %q", in, "287 082")
	}

	got, ok := VerifyTOTP(totpRFCSecret, in, now, 0)
	if !ok || got != 1 {
		t.Errorf("VerifyTOTP(code %q, now %d, lastStep 0) = (step %d, ok %v), want (step 1, ok true)", in, now.Unix(), got, ok)
	}
}

// TestTOTPNewSecretLengthAndRandomness checks the seed size and that two
// calls do not return the same seed.
func TestTOTPNewSecretLengthAndRandomness(t *testing.T) {
	a, err := NewTOTPSecret()
	if err != nil {
		t.Fatalf("NewTOTPSecret() first call error = %v, want nil", err)
	}
	b, err := NewTOTPSecret()
	if err != nil {
		t.Fatalf("NewTOTPSecret() second call error = %v, want nil", err)
	}
	if len(a) != TOTPSecretSize {
		t.Errorf("len(NewTOTPSecret()) = %d, want TOTPSecretSize %d", len(a), TOTPSecretSize)
	}
	if len(b) != TOTPSecretSize {
		t.Errorf("len(second NewTOTPSecret()) = %d, want TOTPSecretSize %d", len(b), TOTPSecretSize)
	}
	if string(a) == string(b) {
		t.Errorf("two NewTOTPSecret() calls returned the same seed %x, want different values", a)
	}
}

// TestTOTPEncodeSecretBase32 checks RFC 4648 base32 without padding.
func TestTOTPEncodeSecretBase32(t *testing.T) {
	const wantRFC = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	got := EncodeTOTPSecret(totpRFCSecret)
	if got != wantRFC {
		t.Errorf("EncodeTOTPSecret(%q) = %q, want %q", totpRFCSecret, got, wantRFC)
	}
	if strings.Contains(got, "=") {
		t.Errorf("EncodeTOTPSecret(%q) = %q, must not contain padding '='", totpRFCSecret, got)
	}

	// One byte 0x66 ('f') encodes to two characters "MY" plus padding when
	// padded; the unpadded form must be exactly "MY".
	one := EncodeTOTPSecret([]byte("f"))
	if strings.Contains(one, "=") {
		t.Errorf("EncodeTOTPSecret(\"f\") = %q, must not contain padding '='", one)
	}
	if one != "MY" {
		t.Errorf("EncodeTOTPSecret(\"f\") = %q, want %q", one, "MY")
	}
}

// TestTOTPURIParts checks the otpauth URI fields for a plain issuer and
// account.
func TestTOTPURIParts(t *testing.T) {
	const wantSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	raw := TOTPURI("HazelminX", "tim", totpRFCSecret)

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(TOTPURI) of %q error = %v, want nil", raw, err)
	}
	if u.Scheme != "otpauth" {
		t.Errorf("TOTPURI scheme = %q, want %q (raw %q)", u.Scheme, "otpauth", raw)
	}
	if u.Host != "totp" {
		t.Errorf("TOTPURI host = %q, want %q (raw %q)", u.Host, "totp", raw)
	}
	if u.Path != "/HazelminX:tim" {
		t.Errorf("TOTPURI path = %q, want %q (raw %q)", u.Path, "/HazelminX:tim", raw)
	}

	q := u.Query()
	want := map[string]string{
		"secret":    wantSecret,
		"issuer":    "HazelminX",
		"algorithm": "SHA1",
		"digits":    "6",
		"period":    "30",
	}
	for k, w := range want {
		if got := q.Get(k); got != w {
			t.Errorf("TOTPURI query %s = %q, want %q (raw %q)", k, got, w, raw)
		}
	}
}

// TestTOTPURIEscapesLabel checks the label escaping for an account with a
// space and for an account with a colon.
func TestTOTPURIEscapesLabel(t *testing.T) {
	t.Run("space in account", func(t *testing.T) {
		raw := TOTPURI("HazelminX", "tim lai", totpRFCSecret)
		if strings.ContainsAny(raw, " \t\r\n") {
			t.Errorf("TOTPURI(\"HazelminX\", \"tim lai\") = %q, must not contain whitespace", raw)
		}
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("url.Parse(%q) error = %v, want nil", raw, err)
		}
		if u.Path != "/HazelminX:tim lai" {
			t.Errorf("TOTPURI(\"HazelminX\", \"tim lai\") parsed path = %q, want %q (raw %q)", u.Path, "/HazelminX:tim lai", raw)
		}
	})

	t.Run("colon in account", func(t *testing.T) {
		raw := TOTPURI("HazelminX", "a:b", totpRFCSecret)
		if !strings.Contains(raw, "HazelminX:a%3Ab") {
			t.Errorf("TOTPURI(\"HazelminX\", \"a:b\") = %q, want the raw label to contain %q", raw, "HazelminX:a%3Ab")
		}
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("url.Parse(%q) error = %v, want nil", raw, err)
		}
		if u.Path != "/HazelminX:a:b" {
			t.Errorf("TOTPURI(\"HazelminX\", \"a:b\") parsed path = %q, want %q (raw %q)", u.Path, "/HazelminX:a:b", raw)
		}
	})
}
