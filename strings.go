package jsonc

import (
	"errors"
	"fmt"
	"unicode/utf16"
	"unicode/utf8"
)

// errInvalidQuotedString is returned by [unquoteString] when the input is
// not a well-formed JSON-quoted string. Specific errors wrap this sentinel
// with %w so callers can match via [errors.Is].
var errInvalidQuotedString = errors.New("jsonc: invalid quoted string")

// unquoteString unescapes a JSON-quoted string token. The input includes
// the surrounding double quotes and the body with escape sequences as
// written. Returns the unescaped Go string value.
//
// Escape rules per RFC 8259 §7:
//   - \" \\ \/ \b \f \n \r \t — single-character escapes.
//   - \uXXXX — exactly 4 hex digits. Surrogate pairs are combined into
//     a single rune. Lone surrogates become U+FFFD (matches stdlib).
//
// The scanner has already validated that the input is well-formed at the
// scan level (well-formed escapes, no unescaped control characters); this
// function performs the byte-level decode.
func unquoteString(raw string) (string, error) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return "", fmt.Errorf("%w: not surrounded by double quotes", errInvalidQuotedString)
	}
	body := raw[1 : len(raw)-1]

	// Fast path: no escapes — return as-is.
	if !containsByte(body, '\\') {
		return body, nil
	}

	out := make([]byte, 0, len(body))
	i := 0
	for i < len(body) {
		c := body[i]
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		// Escape sequence.
		if i+1 >= len(body) {
			return "", fmt.Errorf("%w: dangling backslash", errInvalidQuotedString)
		}
		newOut, newI, err := decodeEscape(out, body, i)
		if err != nil {
			return "", err
		}
		out = newOut
		i = newI
	}
	return string(out), nil
}

// decodeEscape decodes a single escape sequence at body[i]. It appends the
// decoded rune to out and returns the updated slice and new index. body[i]
// is known to be '\\' and i+1 is known to be in range. Errors propagate
// from invalid \u escapes; otherwise the function does not error.
func decodeEscape(out []byte, body string, i int) ([]byte, int, error) {
	switch next := body[i+1]; next {
	case '"':
		return append(out, '"'), i + 2, nil
	case '\\':
		return append(out, '\\'), i + 2, nil
	case '/':
		return append(out, '/'), i + 2, nil
	case 'b':
		return append(out, '\b'), i + 2, nil
	case 'f':
		return append(out, '\f'), i + 2, nil
	case 'n':
		return append(out, '\n'), i + 2, nil
	case 'r':
		return append(out, '\r'), i + 2, nil
	case 't':
		return append(out, '\t'), i + 2, nil
	case 'u':
		return decodeUnicodeEscape(out, body, i)
	default:
		return nil, 0, fmt.Errorf("%w: unknown escape sequence", errInvalidQuotedString)
	}
}

// decodeUnicodeEscape decodes a \uXXXX escape (and its optional surrogate
// pair partner) at body[i]. It appends the decoded rune to out and returns
// the updated slice and new index.
func decodeUnicodeEscape(out []byte, body string, i int) ([]byte, int, error) {
	if i+6 > len(body) {
		return nil, 0, fmt.Errorf("%w: incomplete \\u escape", errInvalidQuotedString)
	}
	r1, err := parseHex4(body[i+2 : i+6])
	if err != nil {
		return nil, 0, err
	}
	i += 6
	if !utf16.IsSurrogate(rune(r1)) {
		return utf8.AppendRune(out, rune(r1)), i, nil
	}
	// Try to pair with a following \uXXXX.
	if i+6 <= len(body) && body[i] == '\\' && body[i+1] == 'u' {
		if r2, err2 := parseHex4(body[i+2 : i+6]); err2 == nil {
			if combined := utf16.DecodeRune(rune(r1), rune(r2)); combined != utf8.RuneError {
				return utf8.AppendRune(out, combined), i + 6, nil
			}
		}
	}
	// Lone surrogate — replace with U+FFFD (matches stdlib).
	return utf8.AppendRune(out, utf8.RuneError), i, nil
}

// parseHex4 parses exactly 4 hexadecimal digits into a uint16. It assumes
// the input has length 4; the caller must ensure that.
func parseHex4(s string) (uint16, error) {
	var v uint16
	for i := range 4 {
		c := s[i]
		var d uint16
		switch {
		case c >= '0' && c <= '9':
			d = uint16(c - '0')
		case c >= 'a' && c <= 'f':
			d = uint16(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = uint16(c-'A') + 10
		default:
			return 0, fmt.Errorf("%w: invalid hex digit in \\u escape", errInvalidQuotedString)
		}
		v = v<<4 | d
	}
	return v, nil
}

// containsByte returns true if s contains b.
func containsByte(s string, b byte) bool {
	for i := range len(s) {
		if s[i] == b {
			return true
		}
	}
	return false
}
