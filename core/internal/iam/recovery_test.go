package iam

import (
	"regexp"
	"strings"
	"testing"
)

// recoveryCodeFormat is the display format from the Setup design board:
// two groups of four characters from the alphabet, separated by '-'.
var recoveryCodeFormat = regexp.MustCompile(`^[23456789ABCDEFGHJKLMNPQRSTUVWXYZ]{4}-[23456789ABCDEFGHJKLMNPQRSTUVWXYZ]{4}$`)

// TestRecoveryGenerateReturnsDistinctFormattedCodes checks the count, the
// format and that the codes within one set are all different.
func TestRecoveryGenerateReturnsDistinctFormattedCodes(t *testing.T) {
	codes, err := GenerateRecoveryCodes(RecoveryCodeCount)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes(%d) error = %v, want nil", RecoveryCodeCount, err)
	}
	if len(codes) != 10 {
		t.Fatalf("GenerateRecoveryCodes(%d) returned %d codes, want 10", RecoveryCodeCount, len(codes))
	}

	seen := map[string]int{}
	for i, c := range codes {
		if !recoveryCodeFormat.MatchString(c) {
			t.Errorf("GenerateRecoveryCodes(%d)[%d] = %q, want format XXXX-XXXX over the recovery alphabet", RecoveryCodeCount, i, c)
		}
		if prev, dup := seen[c]; dup {
			t.Errorf("GenerateRecoveryCodes(%d) returned %q at index %d and %d, want all codes distinct", RecoveryCodeCount, c, prev, i)
		}
		seen[c] = i
	}
}

// TestRecoveryGenerateNonPositiveCountReturnsEmpty checks that n <= 0 gives
// an empty result without an error.
func TestRecoveryGenerateNonPositiveCountReturnsEmpty(t *testing.T) {
	for _, n := range []int{0, -1} {
		codes, err := GenerateRecoveryCodes(n)
		if err != nil {
			t.Errorf("GenerateRecoveryCodes(%d) error = %v, want nil", n, err)
		}
		if len(codes) != 0 {
			t.Errorf("GenerateRecoveryCodes(%d) returned %d codes %v, want 0", n, len(codes), codes)
		}
	}
}

// TestRecoveryGenerateReturnsRequestedCount checks that a positive n gives
// exactly n codes, including n = 1 where an off-by-one would be easy to miss.
func TestRecoveryGenerateReturnsRequestedCount(t *testing.T) {
	for _, n := range []int{1, 3, 25} {
		codes, err := GenerateRecoveryCodes(n)
		if err != nil {
			t.Errorf("GenerateRecoveryCodes(%d) error = %v, want nil", n, err)
		}
		if len(codes) != n {
			t.Errorf("GenerateRecoveryCodes(%d) returned %d codes, want %d", n, len(codes), n)
		}
	}
}

// TestRecoveryGenerateCoversAlphabetWithoutAmbiguousSymbols generates 3000
// codes and checks that every alphabet symbol shows up and that 0, O, 1 and I
// never do.
func TestRecoveryGenerateCoversAlphabetWithoutAmbiguousSymbols(t *testing.T) {
	const rounds = 300
	counts := map[rune]int{}
	total := 0
	for r := 0; r < rounds; r++ {
		codes, err := GenerateRecoveryCodes(RecoveryCodeCount)
		if err != nil {
			t.Fatalf("round %d: GenerateRecoveryCodes(%d) error = %v, want nil", r, RecoveryCodeCount, err)
		}
		for _, c := range codes {
			total++
			for _, ch := range strings.ReplaceAll(c, "-", "") {
				counts[ch]++
			}
		}
	}
	if total != rounds*RecoveryCodeCount {
		t.Fatalf("generated %d codes, want %d", total, rounds*RecoveryCodeCount)
	}

	for _, ch := range "0O1I" {
		if n := counts[ch]; n != 0 {
			t.Errorf("symbol %q appeared %d times across %d codes, want 0 (ambiguous symbols must never be generated)", ch, n, total)
		}
	}

	for _, ch := range RecoveryAlphabet {
		if counts[ch] == 0 {
			t.Errorf("alphabet symbol %q never appeared across %d codes (%d characters), want at least once", ch, total, total*recoveryCodeLength)
		}
	}
}

// TestRecoveryNormalizeAcceptsInputVariants checks the canonical form for
// lower case, surrounding and inner spaces, and a stray dash.
func TestRecoveryNormalizeAcceptsInputVariants(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"7kq2-9mxd", "7KQ29MXD"},
		{" 7KQ2 9MXD ", "7KQ29MXD"},
		{"7KQ29MXD", "7KQ29MXD"},
		{"7KQ-2-9MXD", "7KQ29MXD"},
	}
	for _, c := range cases {
		got, ok := NormalizeRecoveryCode(c.in)
		if !ok {
			t.Errorf("NormalizeRecoveryCode(%q) ok = false, want true (canonical %q)", c.in, c.want)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeRecoveryCode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestRecoveryNormalizeRejectsInvalidInput checks that wrong lengths, the
// ambiguous symbols 0 O 1 I (also in lower case), and non-ASCII input are
// all refused.
func TestRecoveryNormalizeRejectsInvalidInput(t *testing.T) {
	inputs := []string{
		"7KQ29MX",    // too short
		"7KQ2-9MX0",  // zero
		"7KQ2-9MXO",  // letter O
		"7KQ2-9MX1",  // one
		"7KQ2-9MXI",  // letter I
		"7kq2-9mxo",  // letter O in lower case
		"",           // empty
		"7KQ2-9MXDD", // too long
		"7KQ2-9MXＤ",  // full-width Ｄ in place of D
	}
	for _, in := range inputs {
		got, ok := NormalizeRecoveryCode(in)
		if ok {
			t.Errorf("NormalizeRecoveryCode(%q) = (%q, true), want ok=false", in, got)
		}
	}
}

// TestRecoveryNormalizeRoundTripsGeneratedCodes checks that every generated
// code normalizes to itself without the dash, in upper and lower case.
func TestRecoveryNormalizeRoundTripsGeneratedCodes(t *testing.T) {
	codes, err := GenerateRecoveryCodes(RecoveryCodeCount)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes(%d) error = %v, want nil", RecoveryCodeCount, err)
	}
	for _, c := range codes {
		want := strings.ReplaceAll(c, "-", "")
		for _, in := range []string{c, strings.ToLower(c)} {
			got, ok := NormalizeRecoveryCode(in)
			if !ok {
				t.Errorf("NormalizeRecoveryCode(%q) ok = false, want true for a generated code", in)
				continue
			}
			if got != want {
				t.Errorf("NormalizeRecoveryCode(%q) = %q, want %q", in, got, want)
			}
		}
	}
}
