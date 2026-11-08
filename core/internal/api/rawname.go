package api

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// RawName carries a name that came from the host (file name, container label
// value, host name) and may not be valid UTF-8 (addendum 03 機制五, ROB-007,
// 51.6 item decided in ticket 010).
//
// JSON shape:
//
//		{"text": "report�.txt", "bytes": "cmVwb3J0/y50eHQ="}
//
//	  - text is always valid UTF-8: invalid byte sequences are replaced by
//	    U+FFFD, one replacement per invalid sequence as utf8.DecodeRune
//	    reports it. Control and invisible characters are kept as they are;
//	    making them visible is the interface's job (ROB-007).
//	  - bytes is the standard base64 (with padding) of the original bytes. It
//	    is present only when the original is not valid UTF-8, so two names
//	    that display the same text are told apart by bytes.
//	  - Operations that address a name (rename, download) send the RawName
//	    back; when bytes is present it wins over text.
type RawName struct {
	Text  string `json:"text"`
	Bytes string `json:"bytes,omitempty"`
}

// NewRawName wraps raw bytes from the host.
//
// Expected results:
//   - valid UTF-8 (including empty, emoji, U+202E, NUL, newline) → Text is
//     the same string, Bytes is "".
//   - invalid UTF-8, e.g. []byte{'a', 0xff, 'b'} → Text "a�b", Bytes
//     "Yf9i".
//   - a truncated multi-byte sequence at the end, e.g. "é"[:1] → Text
//     "�", Bytes "ww==".
func NewRawName(raw []byte) RawName {
	if utf8.Valid(raw) {
		return RawName{Text: string(raw)}
	}
	var b strings.Builder
	for i := 0; i < len(raw); {
		r, size := utf8.DecodeRune(raw[i:])
		b.WriteRune(r) // RuneError for an invalid sequence
		i += size
	}
	return RawName{Text: b.String(), Bytes: base64.StdEncoding.EncodeToString(raw)}
}

// Raw returns the original bytes.
//
// Expected results:
//   - Bytes present → its base64 decoding.
//   - Bytes empty → []byte(Text).
//   - Bytes present but not valid base64 → error (wrapping ErrInvalidRawName)
//     so a handler answers VALIDATION_FAILED instead of acting on a wrong
//     name.
//   - Round trip: NewRawName(b).Raw() equals b for every b.
func (n RawName) Raw() ([]byte, error) {
	if n.Bytes == "" {
		return []byte(n.Text), nil
	}
	b, err := base64.StdEncoding.DecodeString(n.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRawName, err)
	}
	return b, nil
}

// ErrInvalidRawName is returned by Raw for a malformed bytes field.
var ErrInvalidRawName = errors.New("api: rawName bytes is not valid base64")
