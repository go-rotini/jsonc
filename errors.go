package jsonc

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Position identifies a location within JSONC source text. An error's
// position is where it was detected, which can be the start of the
// offending token rather than the bad character inside it.
type Position struct {
	// Line is the 1-indexed line number; 0 means "no position".
	Line int
	// Column is the 1-indexed column on Line, counted in bytes.
	Column int
	// Offset is the 0-indexed byte offset from the start of the input.
	// Column and Offset do not count a leading UTF-8 byte-order mark.
	Offset int
}

// String returns the position formatted as "line:column".
func (p Position) String() string {
	return fmt.Sprintf("%d:%d", p.Line, p.Column)
}

// SyntaxError is returned when the JSONC input is malformed. It also
// reports exceeded parse limits ([WithMaxDepth], [WithMaxKeys],
// [WithMaxNodes]), a missing required key, and a map key that cannot be
// converted to the map's key type.
type SyntaxError struct {
	// Message is a short human-readable description of the syntactic problem.
	Message string
	// Pos is the source position where the error was detected; Line == 0
	// when the error has no specific location.
	Pos Position
	// Token is the offending token text, when available.
	Token string
}

// Error implements the error interface.
func (e *SyntaxError) Error() string {
	if e.Pos.Line > 0 {
		return fmt.Sprintf("jsonc: line %d, column %d: %s", e.Pos.Line, e.Pos.Column, e.Message)
	}
	return fmt.Sprintf("jsonc: %s", e.Message)
}

// Is reports whether target is a *SyntaxError, so that
// errors.Is(err, [ErrSyntax]) matches any syntax error.
func (e *SyntaxError) Is(target error) bool {
	_, ok := target.(*SyntaxError)
	return ok
}

// TypeError is returned when one or more JSONC values cannot be assigned to
// the target Go types. The decoder accumulates conversion failures rather
// than failing fast and returns them all at once via this type.
type TypeError struct {
	// Errors holds one message per failed value-to-target conversion, each
	// already prefixed with "line N: " when a position is known.
	Errors []string
}

// Error implements the error interface.
func (e *TypeError) Error() string {
	return fmt.Sprintf("jsonc: unmarshal errors:\n  %s", strings.Join(e.Errors, "\n  "))
}

// Is reports whether target is a *TypeError, so that
// errors.Is(err, [ErrType]) matches any type error.
func (e *TypeError) Is(target error) bool {
	_, ok := target.(*TypeError)
	return ok
}

// UnknownFieldError is returned when decoding with [WithStrict] and a JSONC
// key has no corresponding struct field.
type UnknownFieldError struct {
	// Field is the JSONC key that did not match any struct field.
	Field string
	// Pos is the source position of the unrecognized key.
	Pos Position
}

// Error implements the error interface.
func (e *UnknownFieldError) Error() string {
	return fmt.Sprintf("jsonc: line %d: unknown field %q", e.Pos.Line, e.Field)
}

// Is reports whether target is an *UnknownFieldError, so that
// errors.Is(err, [ErrUnknownField]) matches any unknown-field error.
func (e *UnknownFieldError) Is(target error) bool {
	_, ok := target.(*UnknownFieldError)
	return ok
}

// DuplicateKeyError is returned when an object key is defined more than once.
// By default duplicates are rejected; pass [WithAllowDuplicateKeys] to opt
// into stdlib-style last-wins behavior.
type DuplicateKeyError struct {
	// Key is the duplicated object member name.
	Key string
	// Pos is the source position of the second (offending) occurrence.
	Pos Position
}

// Error implements the error interface.
func (e *DuplicateKeyError) Error() string {
	return fmt.Sprintf("jsonc: line %d: duplicate key %q", e.Pos.Line, e.Key)
}

// Is reports whether target is a *DuplicateKeyError, so that
// errors.Is(err, [ErrDuplicateKey]) matches any duplicate-key error.
func (e *DuplicateKeyError) Is(target error) bool {
	_, ok := target.(*DuplicateKeyError)
	return ok
}

// ValidationError wraps an error returned by a [StructValidator] with the
// [Position] of the JSONC node that was decoded into the struct. Err is
// available via [errors.Unwrap] for callers that want the underlying value.
type ValidationError struct {
	// Err is the error returned by the validator.
	Err error
	// Pos is the source position of the JSONC object that produced the
	// validated struct value.
	Pos Position
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("jsonc: line %d: validation: %s", e.Pos.Line, e.Err.Error())
}

// Unwrap returns e.Err.
func (e *ValidationError) Unwrap() error {
	return e.Err
}

// Is reports whether target is a *ValidationError, so that
// errors.Is(err, [ErrValidation]) matches any validation error.
func (e *ValidationError) Is(target error) bool {
	_, ok := target.(*ValidationError)
	return ok
}

// DefaultError is returned when a default value from a struct tag cannot be
// applied (parse failure, overflow, or unsupported field type).
type DefaultError struct {
	// Field is the field's key name (from its "jsonc" or "json" tag, else
	// the Go field name).
	Field string
	// Message describes why the default could not be applied.
	Message string
	// Pos is the source position of the JSONC object that lacks the field.
	Pos Position
}

// Error implements the error interface.
func (e *DefaultError) Error() string {
	return fmt.Sprintf("jsonc: field %q: %s", e.Field, e.Message)
}

// Is reports whether target is a *DefaultError, so that
// errors.Is(err, [ErrDefault]) matches any default-value error.
func (e *DefaultError) Is(target error) bool {
	_, ok := target.(*DefaultError)
	return ok
}

// OverflowError is returned when a JSONC number overflows the target Go type.
type OverflowError struct {
	// Value is the offending number's source text.
	Value string
	// Type is the Go target type that could not hold Value.
	Type string
	// Pos is the source position of the number.
	Pos Position
}

// Error implements the error interface.
func (e *OverflowError) Error() string {
	return fmt.Sprintf("jsonc: line %d: value %s overflows %s", e.Pos.Line, e.Value, e.Type)
}

// Is reports whether target is an *OverflowError, so that
// errors.Is(err, [ErrOverflow]) matches any overflow error.
func (e *OverflowError) Is(target error) bool {
	_, ok := target.(*OverflowError)
	return ok
}

// StrictJSONError is returned when the input contains a JSONC extension
// (a comment or a trailing comma) while [WithStrictJSON] is enabled.
type StrictJSONError struct {
	// Feature names the rejected extension, e.g. "// line comment" or
	// "trailing comma".
	Feature string
	// Pos is the source position where the extension appeared.
	Pos Position
}

// Error implements the error interface.
func (e *StrictJSONError) Error() string {
	return fmt.Sprintf("jsonc: line %d: strict JSON mode rejects %s", e.Pos.Line, e.Feature)
}

// Is reports whether target is a *StrictJSONError, so that
// errors.Is(err, [ErrStrictJSON]) matches any strict-JSON error.
func (e *StrictJSONError) Is(target error) bool {
	_, ok := target.(*StrictJSONError)
	return ok
}

// Sentinel errors for use with [errors.Is]. Never pass one's address to
// [errors.As] (as in &ErrSyntax): that overwrites the shared sentinel.
// Declare a variable of the error type for errors.As instead.
var (
	// ErrSyntax indicates malformed JSONC input.
	ErrSyntax = &SyntaxError{}
	// ErrType indicates one or more JSONC values could not be assigned to the target Go types.
	ErrType = &TypeError{}
	// ErrUnknownField indicates a JSONC key has no corresponding struct field (with [WithStrict]).
	ErrUnknownField = &UnknownFieldError{}
	// ErrDuplicateKey indicates an object key was defined more than once.
	ErrDuplicateKey = &DuplicateKeyError{}
	// ErrValidation indicates a [StructValidator] rejected a decoded struct.
	ErrValidation = &ValidationError{}
	// ErrDefault indicates a default struct tag value could not be applied.
	ErrDefault = &DefaultError{}
	// ErrOverflow indicates a JSONC number overflows the target Go type.
	ErrOverflow = &OverflowError{}
	// ErrStrictJSON indicates a JSONC extension (comment or trailing comma)
	// was encountered while [WithStrictJSON] was enabled.
	ErrStrictJSON = &StrictJSONError{}

	// ErrPathSyntax indicates an invalid [Path] expression.
	ErrPathSyntax = errors.New("jsonc: invalid path syntax")
	// ErrPathNotFound indicates no node matched a [Path] query.
	ErrPathNotFound = errors.New("jsonc: path not found")
	// ErrPointerSyntax indicates an invalid RFC 6901 JSON Pointer expression.
	ErrPointerSyntax = errors.New("jsonc: invalid JSON Pointer syntax")
	// ErrPatchSyntax indicates an invalid RFC 6902 JSON Patch document.
	ErrPatchSyntax = errors.New("jsonc: invalid JSON Patch syntax")
	// ErrPatchTestFailed indicates a JSON Patch "test" operation found a
	// value different from the expected one. It also matches
	// [ErrPatchSyntax], so checks for that error still catch a failed test.
	ErrPatchTestFailed error = patchTestError{}
	// ErrNilPointer indicates a nil pointer was passed where a non-nil pointer is required.
	ErrNilPointer = errors.New("jsonc: non-nil pointer required")
	// ErrDocumentSize indicates the input exceeds the configured [WithMaxDocumentSize] limit.
	ErrDocumentSize = errors.New("jsonc: document size exceeds limit")
	// ErrUnsupportedValue indicates a value cannot be encoded (Inf/NaN, channels,
	// functions, complex numbers, cyclic data, or an unsupported map key type).
	ErrUnsupportedValue = errors.New("jsonc: unsupported value")
)

// patchTestError is the type of [ErrPatchTestFailed].
type patchTestError struct{}

func (patchTestError) Error() string { return "jsonc: JSON Patch test failed" }

// Is reports whether target is [ErrPatchSyntax].
func (patchTestError) Is(target error) bool { return target == ErrPatchSyntax }

// FormatError formats err for display against the source data. For an
// error that carries a [Position] ([SyntaxError], [DuplicateKeyError],
// [OverflowError], [StrictJSONError], [UnknownFieldError], [ValidationError]
// or [DefaultError], possibly wrapped), the output adds the offending source
// line and a caret under the column. Lines end at "\n", "\r\n" or "\r", as
// for the parser; a line longer than 120 bytes is cut to the part around
// the column, with "..." marking what was cut. For any other error, or a
// position outside data, it returns err.Error(). Pass true for color to add
// ANSI color escape sequences.
func FormatError(data []byte, err error, color ...bool) string {
	pos := extractPosition(err)
	if pos.Line == 0 {
		return err.Error()
	}

	src, ok := sourceLine(bytes.TrimPrefix(data, utf8BOM), pos.Line)
	if !ok {
		return err.Error()
	}
	line, col := clipLine(src, min(max(pos.Column-1, 0), len(src)))
	pad := caretPadding(line[:col])

	useColor := len(color) > 0 && color[0]

	var buf bytes.Buffer
	if useColor {
		fmt.Fprintf(&buf, "\x1b[1;31m%s\x1b[0m\n", err.Error())
		fmt.Fprintf(&buf, "  %s\n", line)
		fmt.Fprintf(&buf, "  %s\x1b[1;31m^\x1b[0m\n", pad)
	} else {
		fmt.Fprintf(&buf, "%s\n", err.Error())
		fmt.Fprintf(&buf, "  %s\n", line)
		fmt.Fprintf(&buf, "  %s^\n", pad)
	}
	return buf.String()
}

// utf8BOM is the UTF-8 byte-order mark.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// formatErrorWidth is the longest source line [FormatError] shows whole.
const formatErrorWidth = 120

// sourceLine returns the 1-indexed line n of data, splitting on "\n",
// "\r\n" and "\r" as the scanner does.
func sourceLine(data []byte, n int) ([]byte, bool) {
	for line := 1; ; line++ {
		end := bytes.IndexAny(data, "\r\n")
		if line == n {
			if end < 0 {
				return data, true
			}
			return data[:end], true
		}
		if end < 0 {
			return nil, false
		}
		if data[end] == '\r' && end+1 < len(data) && data[end+1] == '\n' {
			end++
		}
		data = data[end+1:]
	}
}

// clipLine cuts a line longer than formatErrorWidth to a window around the
// byte offset col, on rune boundaries, marking cut ends with "...". It
// returns the line and col's offset in it.
func clipLine(line []byte, col int) (string, int) {
	if len(line) <= formatErrorWidth {
		return string(line), col
	}
	start := max(col-formatErrorWidth/2, 0)
	for start > 0 && !utf8.RuneStart(line[start]) {
		start--
	}
	end := min(start+formatErrorWidth, len(line))
	for end < len(line) && !utf8.RuneStart(line[end]) {
		end++
	}
	var sb strings.Builder
	if start > 0 {
		sb.WriteString("...")
	}
	newCol := sb.Len() + col - start
	sb.Write(line[start:end])
	if end < len(line) {
		sb.WriteString("...")
	}
	return sb.String(), newCol
}

// caretPadding returns the indentation that puts a caret under the
// character after prefix: the tabs in prefix kept, and one space for each
// other character.
func caretPadding(prefix string) string {
	var sb strings.Builder
	for _, r := range prefix {
		if r == '\t' {
			sb.WriteByte('\t')
		} else {
			sb.WriteByte(' ')
		}
	}
	return sb.String()
}

func extractPosition(err error) Position {
	var synErr *SyntaxError
	var valErr *ValidationError
	var dupErr *DuplicateKeyError
	var ovfErr *OverflowError
	var unkErr *UnknownFieldError
	var strErr *StrictJSONError
	var defErr *DefaultError
	switch {
	case errors.As(err, &synErr):
		return synErr.Pos
	case errors.As(err, &valErr):
		return valErr.Pos
	case errors.As(err, &dupErr):
		return dupErr.Pos
	case errors.As(err, &ovfErr):
		return ovfErr.Pos
	case errors.As(err, &unkErr):
		return unkErr.Pos
	case errors.As(err, &strErr):
		return strErr.Pos
	case errors.As(err, &defErr):
		return defErr.Pos
	default:
		return Position{}
	}
}
