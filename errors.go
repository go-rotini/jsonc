package jsonc

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

// Position identifies a location within JSONC source text.
type Position struct {
	// Line is the 1-indexed line number; 0 means "no position".
	Line int
	// Column is the 1-indexed column number on Line.
	Column int
	// Offset is the 0-indexed byte offset from the start of the input.
	Offset int
}

// String returns the position formatted as "line:column".
func (p Position) String() string {
	return fmt.Sprintf("%d:%d", p.Line, p.Column)
}

// SyntaxError is returned when the JSONC input is malformed.
// Use [errors.Is](err, [ErrSyntax]) to test for syntax errors generically.
type SyntaxError struct {
	// Message is a short human-readable description of the syntactic problem.
	Message string
	// Pos is the source position where the error was detected; Line == 0
	// when the error has no specific location.
	Pos Position
	// Token is the offending token text, when available.
	Token string
}

func (e *SyntaxError) Error() string {
	if e.Pos.Line > 0 {
		return fmt.Sprintf("jsonc: line %d, column %d: %s", e.Pos.Line, e.Pos.Column, e.Message)
	}
	return fmt.Sprintf("jsonc: %s", e.Message)
}

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

func (e *TypeError) Error() string {
	return fmt.Sprintf("jsonc: unmarshal errors:\n  %s", strings.Join(e.Errors, "\n  "))
}

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

func (e *UnknownFieldError) Error() string {
	return fmt.Sprintf("jsonc: line %d: unknown field %q", e.Pos.Line, e.Field)
}

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

func (e *DuplicateKeyError) Error() string {
	return fmt.Sprintf("jsonc: line %d: duplicate key %q", e.Pos.Line, e.Key)
}

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

func (e *ValidationError) Error() string {
	return fmt.Sprintf("jsonc: line %d: validation: %s", e.Pos.Line, e.Err.Error())
}

func (e *ValidationError) Unwrap() error {
	return e.Err
}

func (e *ValidationError) Is(target error) bool {
	_, ok := target.(*ValidationError)
	return ok
}

// DefaultError is returned when a default value from a struct tag cannot be
// applied (parse failure, overflow, or unsupported field type).
type DefaultError struct {
	// Field is the struct field name (or jsonc-tag name, when set).
	Field string
	// Message describes why the default could not be applied.
	Message string
	// Pos is the source position of the surrounding object, if known.
	Pos Position
}

func (e *DefaultError) Error() string {
	return fmt.Sprintf("jsonc: field %q: %s", e.Field, e.Message)
}

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

func (e *OverflowError) Error() string {
	return fmt.Sprintf("jsonc: line %d: value %s overflows %s", e.Pos.Line, e.Value, e.Type)
}

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

func (e *StrictJSONError) Error() string {
	return fmt.Sprintf("jsonc: line %d: strict JSON mode rejects %s", e.Pos.Line, e.Feature)
}

func (e *StrictJSONError) Is(target error) bool {
	_, ok := target.(*StrictJSONError)
	return ok
}

// Sentinel errors for use with [errors.Is].
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
	// ErrNilPointer indicates a nil pointer was passed where a non-nil pointer is required.
	ErrNilPointer = errors.New("jsonc: non-nil pointer required")
	// ErrDocumentSize indicates the input exceeds the configured [WithMaxDocumentSize] limit.
	ErrDocumentSize = errors.New("jsonc: document size exceeds limit")
	// ErrUnsupportedValue indicates a value cannot be encoded (Inf/NaN, channels,
	// functions, complex numbers, or cyclic data).
	ErrUnsupportedValue = errors.New("jsonc: unsupported value")
)

// FormatError returns a human-readable string for errors that carry a
// [Position] (such as [SyntaxError], [DuplicateKeyError], [OverflowError],
// [StrictJSONError], or [ValidationError]). The output includes the offending
// source line and a column pointer. For other error types it returns
// err.Error(). Set color to true to include ANSI color escape sequences.
func FormatError(data []byte, err error, color ...bool) string {
	pos := extractPosition(err)
	if pos.Line == 0 {
		return err.Error()
	}

	lines := bytes.Split(data, []byte("\n"))
	lineIdx := pos.Line - 1
	if lineIdx < 0 || lineIdx >= len(lines) {
		return err.Error()
	}

	useColor := len(color) > 0 && color[0]

	var buf bytes.Buffer
	if useColor {
		fmt.Fprintf(&buf, "\x1b[1;31m%s\x1b[0m\n", err.Error())
		fmt.Fprintf(&buf, "  %s\n", string(lines[lineIdx]))
		fmt.Fprintf(&buf, "  %s\x1b[1;31m^\x1b[0m\n", repeatByte(' ', pos.Column-1))
	} else {
		fmt.Fprintf(&buf, "%s\n", err.Error())
		fmt.Fprintf(&buf, "  %s\n", string(lines[lineIdx]))
		fmt.Fprintf(&buf, "  %s^\n", repeatByte(' ', pos.Column-1))
	}
	return buf.String()
}

func extractPosition(err error) Position {
	var synErr *SyntaxError
	var valErr *ValidationError
	var dupErr *DuplicateKeyError
	var ovfErr *OverflowError
	var unkErr *UnknownFieldError
	var strErr *StrictJSONError
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
	default:
		return Position{}
	}
}

func repeatByte(b byte, n int) string {
	if n <= 0 {
		return ""
	}
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = b
	}
	return string(buf)
}
