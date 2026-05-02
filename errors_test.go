package jsonc

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSentinelErrorsIs(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		sentinel error
	}{
		{"SyntaxError", &SyntaxError{Message: "bad"}, ErrSyntax},
		{"TypeError", &TypeError{Errors: []string{"x"}}, ErrType},
		{"UnknownFieldError", &UnknownFieldError{Field: "f"}, ErrUnknownField},
		{"DuplicateKeyError", &DuplicateKeyError{Key: "k"}, ErrDuplicateKey},
		{"ValidationError", &ValidationError{Err: fmt.Errorf("v")}, ErrValidation},
		{"DefaultError", &DefaultError{Field: "f", Message: "m"}, ErrDefault},
		{"OverflowError", &OverflowError{Value: "999", Type: "int8"}, ErrOverflow},
		{"StrictJSONError", &StrictJSONError{Feature: "// line comment"}, ErrStrictJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.err, tt.sentinel) {
				t.Errorf("errors.Is(%T, sentinel) = false, want true", tt.err)
			}
		})
	}
}

func TestPlainSentinelErrors(t *testing.T) {
	// These sentinels are plain errors (errors.New), so wrapping and Is
	// follow normal Go semantics.
	tests := []struct {
		name     string
		sentinel error
		msg      string
	}{
		{"ErrPathSyntax", ErrPathSyntax, "invalid path syntax"},
		{"ErrPathNotFound", ErrPathNotFound, "path not found"},
		{"ErrPointerSyntax", ErrPointerSyntax, "invalid JSON Pointer syntax"},
		{"ErrPatchSyntax", ErrPatchSyntax, "invalid JSON Patch syntax"},
		{"ErrNilPointer", ErrNilPointer, "non-nil pointer required"},
		{"ErrDocumentSize", ErrDocumentSize, "document size exceeds limit"},
		{"ErrUnsupportedValue", ErrUnsupportedValue, "unsupported value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := fmt.Errorf("wrap: %w", tt.sentinel)
			if !errors.Is(wrapped, tt.sentinel) {
				t.Errorf("errors.Is(wrapped, %s) = false, want true", tt.name)
			}
			if !strings.Contains(tt.sentinel.Error(), tt.msg) {
				t.Errorf("%s.Error() = %q, want to contain %q", tt.name, tt.sentinel.Error(), tt.msg)
			}
		})
	}
}

func TestErrorsAs(t *testing.T) {
	t.Run("SyntaxError", func(t *testing.T) {
		err := error(&SyntaxError{Message: "unexpected", Pos: Position{Line: 5, Column: 3}})
		var target *SyntaxError
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if target.Pos.Line != 5 || target.Pos.Column != 3 {
			t.Errorf("Pos = %v, want 5:3", target.Pos)
		}
		if target.Message != "unexpected" {
			t.Errorf("Message = %q, want %q", target.Message, "unexpected")
		}
	})

	t.Run("TypeError", func(t *testing.T) {
		err := error(&TypeError{Errors: []string{"a", "b"}})
		var target *TypeError
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if len(target.Errors) != 2 {
			t.Errorf("len(Errors) = %d, want 2", len(target.Errors))
		}
	})

	t.Run("UnknownFieldError", func(t *testing.T) {
		err := error(&UnknownFieldError{Field: "xyz", Pos: Position{Line: 10}})
		var target *UnknownFieldError
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if target.Field != "xyz" {
			t.Errorf("Field = %q, want %q", target.Field, "xyz")
		}
	})

	t.Run("DuplicateKeyError", func(t *testing.T) {
		err := error(&DuplicateKeyError{Key: "dup", Pos: Position{Line: 3}})
		var target *DuplicateKeyError
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if target.Key != "dup" {
			t.Errorf("Key = %q, want %q", target.Key, "dup")
		}
	})

	t.Run("OverflowError", func(t *testing.T) {
		err := error(&OverflowError{Value: "999", Type: "int8", Pos: Position{Line: 7}})
		var target *OverflowError
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if target.Value != "999" || target.Type != "int8" {
			t.Errorf("got Value=%q Type=%q, want 999/int8", target.Value, target.Type)
		}
	})

	t.Run("StrictJSONError", func(t *testing.T) {
		err := error(&StrictJSONError{Feature: "trailing comma", Pos: Position{Line: 4}})
		var target *StrictJSONError
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if target.Feature != "trailing comma" {
			t.Errorf("Feature = %q", target.Feature)
		}
	})

	t.Run("DefaultError", func(t *testing.T) {
		err := error(&DefaultError{Field: "port", Message: "bad default"})
		var target *DefaultError
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if target.Field != "port" {
			t.Errorf("Field = %q, want %q", target.Field, "port")
		}
	})

	t.Run("ValidationError", func(t *testing.T) {
		inner := fmt.Errorf("must be positive")
		err := error(&ValidationError{Err: inner, Pos: Position{Line: 1}})
		var target *ValidationError
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if target.Err != inner {
			t.Errorf("Err = %v, want %v", target.Err, inner)
		}
	})
}

func TestErrorMessages(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			"SyntaxError with position",
			&SyntaxError{Message: "unexpected token", Pos: Position{Line: 5, Column: 10}},
			"jsonc: line 5, column 10: unexpected token",
		},
		{
			"SyntaxError without position",
			&SyntaxError{Message: "unexpected EOF"},
			"jsonc: unexpected EOF",
		},
		{
			"TypeError",
			&TypeError{Errors: []string{"field a: wrong type", "field b: overflow"}},
			"jsonc: unmarshal errors:\n  field a: wrong type\n  field b: overflow",
		},
		{
			"UnknownFieldError",
			&UnknownFieldError{Field: "foo", Pos: Position{Line: 3}},
			"jsonc: line 3: unknown field \"foo\"",
		},
		{
			"DuplicateKeyError",
			&DuplicateKeyError{Key: "bar", Pos: Position{Line: 7}},
			"jsonc: line 7: duplicate key \"bar\"",
		},
		{
			"ValidationError",
			&ValidationError{Err: fmt.Errorf("too large"), Pos: Position{Line: 2}},
			"jsonc: line 2: validation: too large",
		},
		{
			"DefaultError",
			&DefaultError{Field: "count", Message: "not an integer"},
			"jsonc: field \"count\": not an integer",
		},
		{
			"OverflowError",
			&OverflowError{Value: "999", Type: "int8", Pos: Position{Line: 4}},
			"jsonc: line 4: value 999 overflows int8",
		},
		{
			"StrictJSONError",
			&StrictJSONError{Feature: "// line comment", Pos: Position{Line: 6}},
			"jsonc: line 6: strict JSON mode rejects // line comment",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.want {
				t.Errorf("Error() =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}

func TestValidationErrorUnwrap(t *testing.T) {
	inner := fmt.Errorf("port must be > 0")
	ve := &ValidationError{Err: inner, Pos: Position{Line: 1}}

	unwrapped := ve.Unwrap()
	if unwrapped != inner {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, inner)
	}

	// errors.Is should reach the inner error via Unwrap
	sentinel := fmt.Errorf("port must be > 0")
	wrapped := &ValidationError{Err: fmt.Errorf("wrap: %w", sentinel), Pos: Position{Line: 2}}
	if !errors.Is(wrapped.Unwrap(), sentinel) {
		t.Error("errors.Is through Unwrap failed")
	}
}

func TestPositionString(t *testing.T) {
	tests := []struct {
		pos  Position
		want string
	}{
		{Position{Line: 1, Column: 1}, "1:1"},
		{Position{Line: 42, Column: 7}, "42:7"},
		{Position{Line: 0, Column: 0}, "0:0"},
		{Position{Line: 100, Column: 50, Offset: 999}, "100:50"},
	}
	for _, tt := range tests {
		got := tt.pos.String()
		if got != tt.want {
			t.Errorf("Position{%d,%d}.String() = %q, want %q", tt.pos.Line, tt.pos.Column, got, tt.want)
		}
	}
}

func TestFormatErrorWithColor(t *testing.T) {
	data := []byte("\"key\": \"hello\"\n\"bad\": @invalid\n\"other\": true")
	err := &SyntaxError{
		Message: "unexpected character",
		Pos:     Position{Line: 2, Column: 8},
	}

	// Without color
	plain := FormatError(data, err)
	if !strings.Contains(plain, "unexpected character") {
		t.Error("plain FormatError missing error message")
	}
	if !strings.Contains(plain, "@invalid") {
		t.Error("plain FormatError missing source line")
	}
	if !strings.Contains(plain, "^") {
		t.Error("plain FormatError missing caret pointer")
	}
	if strings.Contains(plain, "\x1b[") {
		t.Error("plain FormatError should not contain ANSI escapes")
	}

	// With color
	colored := FormatError(data, err, true)
	if !strings.Contains(colored, "\x1b[1;31m") {
		t.Error("colored FormatError missing ANSI escape")
	}
	if !strings.Contains(colored, "@invalid") {
		t.Error("colored FormatError missing source line")
	}
	if !strings.Contains(colored, "^") {
		t.Error("colored FormatError missing caret pointer")
	}
}

func TestFormatErrorNoPosition(t *testing.T) {
	data := []byte(`{"key": "hello"}`)
	err := fmt.Errorf("some generic error")

	result := FormatError(data, err)
	if result != "some generic error" {
		t.Errorf("FormatError for non-positional error = %q, want %q", result, "some generic error")
	}
}

func TestFormatErrorLineOutOfRange(t *testing.T) {
	data := []byte("one line")
	err := &SyntaxError{
		Message: "problem",
		Pos:     Position{Line: 99, Column: 1},
	}
	result := FormatError(data, err)
	// Should fall back to err.Error() when line is out of range
	if result != err.Error() {
		t.Errorf("FormatError = %q, want %q", result, err.Error())
	}
}

func TestFormatErrorVariousTypes(t *testing.T) {
	data := []byte("line1\nline2\nline3")
	pos := Position{Line: 2, Column: 3}

	// DuplicateKeyError
	dup := &DuplicateKeyError{Key: "k", Pos: pos}
	out := FormatError(data, dup)
	if !strings.Contains(out, "line2") {
		t.Error("FormatError for DuplicateKeyError missing source line")
	}

	// OverflowError
	ovf := &OverflowError{Value: "9999", Type: "int8", Pos: pos}
	out = FormatError(data, ovf)
	if !strings.Contains(out, "line2") {
		t.Error("FormatError for OverflowError missing source line")
	}

	// StrictJSONError
	sj := &StrictJSONError{Feature: "comment", Pos: pos}
	out = FormatError(data, sj)
	if !strings.Contains(out, "line2") {
		t.Error("FormatError for StrictJSONError missing source line")
	}

	// ValidationError
	val := &ValidationError{Err: fmt.Errorf("fail"), Pos: pos}
	out = FormatError(data, val)
	if !strings.Contains(out, "line2") {
		t.Error("FormatError for ValidationError missing source line")
	}

	// UnknownFieldError
	unk := &UnknownFieldError{Field: "f", Pos: pos}
	out = FormatError(data, unk)
	if !strings.Contains(out, "line2") {
		t.Error("FormatError for UnknownFieldError missing source line")
	}
}

func TestSentinelErrorsNotCrossMatch(t *testing.T) {
	// Ensure typed sentinel errors don't match the wrong type.
	syntaxErr := &SyntaxError{Message: "test"}
	if errors.Is(syntaxErr, ErrType) {
		t.Error("SyntaxError should not match ErrType")
	}
	if errors.Is(syntaxErr, ErrDuplicateKey) {
		t.Error("SyntaxError should not match ErrDuplicateKey")
	}
	if errors.Is(syntaxErr, ErrStrictJSON) {
		t.Error("SyntaxError should not match ErrStrictJSON")
	}

	typeErr := &TypeError{Errors: []string{"x"}}
	if errors.Is(typeErr, ErrSyntax) {
		t.Error("TypeError should not match ErrSyntax")
	}

	strictErr := &StrictJSONError{Feature: "comment"}
	if errors.Is(strictErr, ErrSyntax) {
		t.Error("StrictJSONError should not match ErrSyntax")
	}
}

func TestFormatErrorColumnPointerPosition(t *testing.T) {
	data := []byte(`"key": @bad`)
	err := &SyntaxError{
		Message: "bad char",
		Pos:     Position{Line: 1, Column: 8},
	}
	result := FormatError(data, err)
	lines := strings.Split(result, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(lines))
	}
	pointerLine := lines[2]
	if !strings.Contains(pointerLine, "^") {
		t.Errorf("pointer line = %q, want caret", pointerLine)
	}
}
