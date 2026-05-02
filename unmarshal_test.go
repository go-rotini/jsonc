package jsonc

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Unmarshal / UnmarshalWithOptions / UnmarshalTo / DecodeFile
// ---------------------------------------------------------------------------

func TestUnmarshalScalar(t *testing.T) {
	var s string
	if err := Unmarshal([]byte(`"hello"`), &s); err != nil {
		t.Fatal(err)
	}
	if s != "hello" {
		t.Errorf("got %q", s)
	}
}

func TestUnmarshalNilPointerError(t *testing.T) {
	err := Unmarshal([]byte(`"x"`), nil)
	if !errors.Is(err, ErrNilPointer) {
		t.Errorf("expected ErrNilPointer, got %v", err)
	}
}

func TestUnmarshalNonPointerError(t *testing.T) {
	var s string
	err := Unmarshal([]byte(`"x"`), s) // value, not pointer
	if !errors.Is(err, ErrNilPointer) {
		t.Errorf("expected ErrNilPointer for non-pointer arg, got %v", err)
	}
}

func TestUnmarshalNilTypedPointer(t *testing.T) {
	var sp *string
	err := Unmarshal([]byte(`"x"`), sp) // typed but nil pointer
	if !errors.Is(err, ErrNilPointer) {
		t.Errorf("expected ErrNilPointer for nil typed pointer, got %v", err)
	}
}

func TestUnmarshalSyntaxError(t *testing.T) {
	var s string
	err := Unmarshal([]byte(`"unterminated`), &s)
	if err == nil {
		t.Fatal("expected error")
	}
	var se *SyntaxError
	if !errors.As(err, &se) {
		t.Errorf("expected *SyntaxError, got %T: %v", err, err)
	}
}

func TestUnmarshalWithOptionsStrictRejectsUnknown(t *testing.T) {
	type S struct {
		Known string `json:"known"`
	}
	var s S
	err := UnmarshalWithOptions(
		[]byte(`{"known": "yes", "extra": 1}`),
		&s,
		WithStrict(),
	)
	if err == nil {
		t.Fatal("expected unknown field error")
	}
	var ufe *UnknownFieldError
	if !errors.As(err, &ufe) {
		t.Errorf("expected *UnknownFieldError, got %T: %v", err, err)
	}
}

func TestUnmarshalWithOptionsStrictJSONRejectsComments(t *testing.T) {
	var v any
	err := UnmarshalWithOptions(
		[]byte(`{ /* nope */ }`),
		&v,
		WithStrictJSON(),
	)
	if err == nil {
		t.Fatal("expected strict JSON error")
	}
	var sje *StrictJSONError
	if !errors.As(err, &sje) {
		t.Errorf("expected *StrictJSONError, got %T: %v", err, err)
	}
}

func TestUnmarshalMaxDocumentSize(t *testing.T) {
	data := []byte(`"hello world from a longer string"`)
	var s string
	err := UnmarshalWithOptions(data, &s, WithMaxDocumentSize(5))
	if !errors.Is(err, ErrDocumentSize) {
		t.Errorf("expected ErrDocumentSize, got %v", err)
	}
}

func TestUnmarshalTo(t *testing.T) {
	type S struct {
		N int `json:"n"`
	}
	got, err := UnmarshalTo[S]([]byte(`{"n": 42}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.N != 42 {
		t.Errorf("got %+v", got)
	}
}

func TestUnmarshalToError(t *testing.T) {
	_, err := UnmarshalTo[int]([]byte(`"not a number"`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeFileSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.jsonc")
	contents := []byte(`{
  // Server config
  "port": 8080,
  "host": "localhost",
}`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	type Config struct {
		Port int    `json:"port"`
		Host string `json:"host"`
	}
	var c Config
	if err := DecodeFile(path, &c); err != nil {
		t.Fatal(err)
	}
	if c.Port != 8080 || c.Host != "localhost" {
		t.Errorf("got %+v", c)
	}
}

func TestDecodeFileMissing(t *testing.T) {
	var v any
	err := DecodeFile("/nonexistent/path/that/does/not/exist.jsonc", &v)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "jsonc:") {
		t.Errorf("expected jsonc-prefixed error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// RawValue
// ---------------------------------------------------------------------------

func TestRawValueUnmarshalRoundTrip(t *testing.T) {
	type S struct {
		Meta RawValue `json:"meta"`
		Name string   `json:"name"`
	}
	src := `{"meta": {"k": 1, "v": [1,2,3]}, "name": "x"}`
	var s S
	if err := Unmarshal([]byte(src), &s); err != nil {
		t.Fatal(err)
	}
	if s.Name != "x" {
		t.Errorf("name = %q", s.Name)
	}
	if len(s.Meta) == 0 {
		t.Fatal("meta should not be empty")
	}

	// Decode the RawValue further.
	var meta map[string]any
	if err := s.Meta.Unmarshal(&meta); err != nil {
		t.Fatal(err)
	}
	if _, ok := meta["k"]; !ok {
		t.Errorf("meta missing key k: %+v", meta)
	}
}

func TestRawValueMarshalJSONCNil(t *testing.T) {
	var r RawValue
	out, err := r.MarshalJSONC()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "null" {
		t.Errorf("got %q", out)
	}
}

func TestRawValueMarshalJSONCNonEmpty(t *testing.T) {
	r := RawValue(`{"a":1}`)
	out, err := r.MarshalJSONC()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"a":1}` {
		t.Errorf("got %q", out)
	}
}

func TestRawValueUnmarshalJSONC(t *testing.T) {
	var r RawValue
	if err := r.UnmarshalJSONC([]byte(`[1,2,3]`)); err != nil {
		t.Fatal(err)
	}
	if string(r) != `[1,2,3]` {
		t.Errorf("got %q", r)
	}
}

func TestRawValueStandardize(t *testing.T) {
	r := RawValue(`{
  // comment
  "a": 1,
  "b": 2,
}`)
	out, err := r.Standardize()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "//") {
		t.Errorf("standardized output should not contain comments: %s", out)
	}
	if strings.Contains(string(out), ",}") || strings.Contains(string(out), ",\n}") {
		t.Errorf("standardized output should not contain trailing commas: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Decoder streaming
// ---------------------------------------------------------------------------

func TestDecoderSingleValue(t *testing.T) {
	dec := NewDecoder(strings.NewReader(`42`))
	var n int
	if err := dec.Decode(&n); err != nil {
		t.Fatal(err)
	}
	if n != 42 {
		t.Errorf("got %d", n)
	}
}

func TestDecoderMultipleValues(t *testing.T) {
	src := `42 "hello" {"x": 1} [1,2,3] true`
	dec := NewDecoder(strings.NewReader(src))

	var n int
	if err := dec.Decode(&n); err != nil {
		t.Fatal(err)
	}
	if n != 42 {
		t.Errorf("first value: got %d", n)
	}

	var s string
	if err := dec.Decode(&s); err != nil {
		t.Fatal(err)
	}
	if s != "hello" {
		t.Errorf("second value: got %q", s)
	}

	var m map[string]int
	if err := dec.Decode(&m); err != nil {
		t.Fatal(err)
	}
	if m["x"] != 1 {
		t.Errorf("third value: got %+v", m)
	}

	var arr []int
	if err := dec.Decode(&arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != 3 || arr[0] != 1 || arr[2] != 3 {
		t.Errorf("fourth value: got %+v", arr)
	}

	var b bool
	if err := dec.Decode(&b); err != nil {
		t.Fatal(err)
	}
	if !b {
		t.Errorf("fifth value: got %v", b)
	}

	// EOF on next call.
	var dummy any
	if err := dec.Decode(&dummy); !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestDecoderMore(t *testing.T) {
	dec := NewDecoder(strings.NewReader(`1 2`))
	if !dec.More() {
		t.Error("expected More() before any decode")
	}
	var n int
	if err := dec.Decode(&n); err != nil {
		t.Fatal(err)
	}
	if !dec.More() {
		t.Error("expected More() after first decode")
	}
	if err := dec.Decode(&n); err != nil {
		t.Fatal(err)
	}
	if dec.More() {
		t.Error("expected !More() after consuming all values")
	}
}

func TestDecoderInputOffset(t *testing.T) {
	dec := NewDecoder(strings.NewReader(`1 2 3`))
	if dec.InputOffset() != 0 {
		t.Errorf("initial offset should be 0, got %d", dec.InputOffset())
	}
	var n int
	if err := dec.Decode(&n); err != nil {
		t.Fatal(err)
	}
	if dec.InputOffset() == 0 {
		t.Error("expected offset to advance after first decode")
	}
}

func TestDecoderEmptyInput(t *testing.T) {
	dec := NewDecoder(strings.NewReader(``))
	if dec.More() {
		t.Error("More() should be false for empty input")
	}
	var v any
	if err := dec.Decode(&v); !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestDecoderCommentsBetweenValues(t *testing.T) {
	src := `
		// leading
		1
		/* between */
		2
		// trailing
	`
	dec := NewDecoder(strings.NewReader(src))
	var a, b int
	if err := dec.Decode(&a); err != nil {
		t.Fatal(err)
	}
	if err := dec.Decode(&b); err != nil {
		t.Fatal(err)
	}
	if a != 1 || b != 2 {
		t.Errorf("got %d, %d", a, b)
	}
}

func TestDecoderSetContext(t *testing.T) {
	type ctxKey struct{}
	type ctxRecv struct {
		Got string
	}

	src := `"hello"`
	dec := NewDecoder(strings.NewReader(src))
	ctx := context.WithValue(context.Background(), ctxKey{}, "value")
	dec.SetContext(ctx)

	// SetContext is best-tested via DecodeContext / interface dispatch
	// elsewhere. Here we just verify SetContext does not error and the
	// subsequent Decode still works.
	var s string
	if err := dec.Decode(&s); err != nil {
		t.Fatal(err)
	}
	if s != "hello" {
		t.Errorf("got %q", s)
	}
	_ = ctxRecv{}
}

func TestDecoderDecodeContext(t *testing.T) {
	dec := NewDecoder(strings.NewReader(`"hello"`))
	var s string
	if err := dec.DecodeContext(context.Background(), &s); err != nil {
		t.Fatal(err)
	}
	if s != "hello" {
		t.Errorf("got %q", s)
	}
}

func TestDecoderReadError(t *testing.T) {
	dec := NewDecoder(&errReader{err: errors.New("boom")})
	var s string
	err := dec.Decode(&s)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected boom, got %v", err)
	}
}

func TestDecoderWithOptions(t *testing.T) {
	type S struct {
		Known string `json:"known"`
	}
	dec := NewDecoder(
		strings.NewReader(`{"known": "yes", "extra": 1}`),
		WithStrict(),
	)
	var s S
	err := dec.Decode(&s)
	if err == nil {
		t.Fatal("expected unknown field error")
	}
	var ufe *UnknownFieldError
	if !errors.As(err, &ufe) {
		t.Errorf("expected *UnknownFieldError, got %T", err)
	}
}

// ---------------------------------------------------------------------------
// errReader for testing reader errors
// ---------------------------------------------------------------------------

type errReader struct {
	err error
}

func (r *errReader) Read(_ []byte) (int, error) {
	return 0, r.err
}
