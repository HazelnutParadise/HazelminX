package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// rawNameFailOnPanic runs as a deferred call in each subtest and turns a
// panic (the TODO stubs) into a failure of that one case.
func rawNameFailOnPanic(t *testing.T) {
	t.Helper()
	if r := recover(); r != nil {
		t.Fatalf("panic: %v", r)
	}
}

// rawNameOracleText is the Text that NewRawName must produce for raw: each
// invalid sequence becomes one U+FFFD, as utf8.DecodeRune reports it.
func rawNameOracleText(raw []byte) string {
	var sb strings.Builder
	for i := 0; i < len(raw); {
		r, size := utf8.DecodeRune(raw[i:])
		if r == utf8.RuneError && size == 1 {
			sb.WriteRune(utf8.RuneError)
		} else {
			sb.Write(raw[i : i+size])
		}
		i += size
	}
	return sb.String()
}

type rawNameSample struct {
	name string
	raw  []byte
}

// rawNameSamples covers the cases named in the ticket. Valid samples have no
// bytes field; invalid ones do.
func rawNameSamples() []rawNameSample {
	return []rawNameSample{
		{"empty", []byte{}},
		{"nil", nil},
		{"ascii", []byte("report.txt")},
		{"emoji", []byte("🗂️ 報告 😀")},
		{"U+202E right-to-left override", []byte("‮gnp.exe")},
		{"NUL", []byte("a\x00b")},
		{"newline", []byte("a\nb")},
		{"0xff", []byte{0xff}},
		{"invalid between ascii", []byte{'a', 0xff, 'b'}},
		{"truncated two-byte sequence", []byte("é")[:1]},
		{"truncated three-byte sequence", []byte("中")[:2]},
		{"two invalid bytes", []byte{0xff, 0xfe}},
		{"doc example report with invalid byte", []byte("report\xff.txt")},
	}
}

func TestNewRawNameText(t *testing.T) {
	tests := []struct {
		name      string
		raw       []byte
		wantText  string
		wantBytes string
	}{
		{name: "empty input", raw: []byte{}, wantText: "", wantBytes: ""},
		{name: "ascii is kept", raw: []byte("report.txt"), wantText: "report.txt", wantBytes: ""},
		{name: "emoji is kept", raw: []byte("😀"), wantText: "😀", wantBytes: ""},
		{name: "U+202E is kept", raw: []byte("‮gnp"), wantText: "‮gnp", wantBytes: ""},
		{name: "NUL is kept", raw: []byte("a\x00b"), wantText: "a\x00b", wantBytes: ""},
		{name: "invalid byte becomes U+FFFD and bytes set", raw: []byte{'a', 0xff, 'b'}, wantText: "a�b", wantBytes: "Yf9i"},
		{name: "truncated two-byte sequence at end", raw: []byte("é")[:1], wantText: "�", wantBytes: "ww=="},
		{name: "one replacement per invalid sequence", raw: []byte{0xff, 0xfe}, wantText: "��", wantBytes: "//4="},
		{name: "truncated three-byte sequence gives two replacements", raw: []byte("中")[:2], wantText: "��", wantBytes: "5Lg="},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer rawNameFailOnPanic(t)
			got := NewRawName(tc.raw)
			if got.Text != tc.wantText {
				t.Errorf("Text = %q, want %q", got.Text, tc.wantText)
			}
			if got.Bytes != tc.wantBytes {
				t.Errorf("Bytes = %q, want %q", got.Bytes, tc.wantBytes)
			}
		})
	}
}

func TestNewRawNameRoundTrip(t *testing.T) {
	cases := rawNameSamples()
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 1000; i++ {
		b := make([]byte, rng.Intn(40))
		rng.Read(b)
		cases = append(cases, rawNameSample{name: "random", raw: b})
	}
	for i, tc := range cases {
		t.Run(fmt.Sprintf("%s-%d", tc.name, i), func(t *testing.T) {
			defer rawNameFailOnPanic(t)
			n := NewRawName(tc.raw)
			if !utf8.ValidString(n.Text) {
				t.Fatalf("Text %q is not valid UTF-8", n.Text)
			}
			if n.Text != rawNameOracleText(tc.raw) {
				t.Errorf("Text = %q, want %q", n.Text, rawNameOracleText(tc.raw))
			}
			if utf8.Valid(tc.raw) != (n.Bytes == "") {
				t.Errorf("Bytes = %q, but valid UTF-8 is %v; bytes must be set only for invalid input", n.Bytes, utf8.Valid(tc.raw))
			}
			if n.Bytes != "" && n.Bytes != base64.StdEncoding.EncodeToString(tc.raw) {
				t.Errorf("Bytes = %q, want base64 %q", n.Bytes, base64.StdEncoding.EncodeToString(tc.raw))
			}
			back, err := n.Raw()
			if err != nil {
				t.Fatalf("Raw() error: %v", err)
			}
			if !bytes.Equal(back, tc.raw) {
				t.Errorf("Raw() = %x, want %x", back, tc.raw)
			}
		})
	}
}

func TestRawNameRaw(t *testing.T) {
	tests := []struct {
		name    string
		in      RawName
		want    []byte
		wantErr bool
	}{
		{name: "bytes present decodes base64", in: RawName{Text: "a�b", Bytes: "Yf9i"}, want: []byte{'a', 0xff, 'b'}},
		{name: "bytes win over text", in: RawName{Text: "ignored", Bytes: "ww=="}, want: []byte{0xc3}},
		{name: "bytes empty uses text", in: RawName{Text: "hello"}, want: []byte("hello")},
		{name: "bytes empty and text empty", in: RawName{}, want: []byte{}},
		{name: "bytes not base64 is an error", in: RawName{Text: "x", Bytes: "!!!!"}, wantErr: true},
		{name: "bytes missing padding is an error", in: RawName{Text: "x", Bytes: "Yf9"}, wantErr: true},
		{name: "bytes with extra padding is an error", in: RawName{Text: "x", Bytes: "Yf9i="}, wantErr: true},
		{name: "bytes with a space is an error", in: RawName{Text: "x", Bytes: "Yf9 i"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer rawNameFailOnPanic(t)
			got, err := tc.in.Raw()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Raw() = %x, want error", got)
				}
				if !errors.Is(err, ErrInvalidRawName) {
					t.Errorf("error %v does not wrap ErrInvalidRawName", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Raw() error: %v", err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("Raw() = %x, want %x", got, tc.want)
			}
		})
	}
}

func TestRawNameJSON(t *testing.T) {
	for _, tc := range rawNameSamples() {
		t.Run(tc.name, func(t *testing.T) {
			defer rawNameFailOnPanic(t)
			n := NewRawName(tc.raw)
			data, err := json.Marshal(n)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatalf("Unmarshal into map: %v", err)
			}
			if _, ok := m["text"]; !ok {
				t.Errorf("JSON %s has no text field", data)
			}
			_, hasBytes := m["bytes"]
			valid := utf8.Valid(tc.raw)
			if valid && hasBytes {
				t.Errorf("valid UTF-8 must not carry bytes, got JSON %s", data)
			}
			if !valid && !hasBytes {
				t.Errorf("invalid UTF-8 must carry bytes, got JSON %s", data)
			}

			var back RawName
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("Unmarshal into RawName: %v", err)
			}
			got, err := back.Raw()
			if err != nil {
				t.Fatalf("Raw() after JSON round trip: %v", err)
			}
			if !bytes.Equal(got, tc.raw) {
				t.Errorf("JSON round trip = %x, want %x", got, tc.raw)
			}
		})
	}
}

func TestRawNameJSONShape(t *testing.T) {
	defer rawNameFailOnPanic(t)
	data, err := json.Marshal(NewRawName([]byte("report\xff.txt")))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := map[string]any{"text": "report�.txt", "bytes": "cmVwb3J0/y50eHQ="}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JSON = %s, want fields %v", data, want)
	}
}
