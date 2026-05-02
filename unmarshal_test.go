package jsonc

import (
	"bytes"
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

// ---------------------------------------------------------------------------
// Decoder limits: max depth / keys / nodes.
// ---------------------------------------------------------------------------

func TestDecoderMaxDepthExceeded(t *testing.T) {
	// A deeply-nested array hits maxDepth.
	deep := strings.Repeat("[", 200) + strings.Repeat("]", 200)
	var v any
	if err := UnmarshalWithOptions([]byte(deep), &v, WithMaxDepth(10)); err == nil {
		t.Error("expected depth-limit error")
	}
}

func TestDecoderMaxKeysExceeded(t *testing.T) {
	src := `{"a":1,"b":2,"c":3,"d":4,"e":5}`
	var v any
	if err := UnmarshalWithOptions([]byte(src), &v, WithMaxKeys(3)); err == nil {
		t.Error("expected max-keys error")
	}
}

func TestDecoderMaxNodesExceeded(t *testing.T) {
	src := `[1,2,3,4,5,6,7,8,9,10]`
	var v any
	if err := UnmarshalWithOptions([]byte(src), &v, WithMaxNodes(3)); err == nil {
		t.Error("expected max-nodes error")
	}
}

// ---------------------------------------------------------------------------
// Decoder.InputOffset returns consumed bytes after a Decode.
// ---------------------------------------------------------------------------

func TestDecoderInputOffsetReturnsConsumedBytes(t *testing.T) {
	src := `42 99`
	dec := NewDecoder(bytes.NewReader([]byte(src)))
	var n int
	if err := dec.Decode(&n); err != nil {
		t.Fatal(err)
	}
	off := dec.InputOffset()
	if off == 0 {
		t.Error("expected nonzero input offset after first decode")
	}
}

// ---------------------------------------------------------------------------
// Ordered maps with comment children.
// ---------------------------------------------------------------------------

func TestUnmarshalOrderedMapWithCommentChildren(t *testing.T) {
	src := `{
		// before a
		"a": 1,
		/* between */
		"b": 2
	}`
	var v any
	if err := UnmarshalWithOptions([]byte(src), &v, WithOrderedMap()); err != nil {
		t.Fatal(err)
	}
	ms, ok := v.(MapSlice)
	if !ok {
		t.Fatalf("expected MapSlice, got %T", v)
	}
	if len(ms) != 2 || ms[0].Key != "a" || ms[1].Key != "b" {
		t.Errorf("got %+v", ms)
	}
}

// ---------------------------------------------------------------------------
// Strict mode propagation through nested struct fields.
// ---------------------------------------------------------------------------

func TestStrictModeWithUnknownNestedField(t *testing.T) {
	type Inner struct {
		Known string `json:"known"`
	}
	type Outer struct {
		Inner Inner `json:"inner"`
	}
	src := `{"inner": {"known": "x", "unexpected": 1}}`
	var v Outer
	err := UnmarshalWithOptions([]byte(src), &v, WithStrict())
	var ufe *UnknownFieldError
	if !errors.As(err, &ufe) {
		t.Errorf("expected *UnknownFieldError, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Decoder.More / EOF handling.
// ---------------------------------------------------------------------------

func TestDecoderMoreAfterEOF(t *testing.T) {
	dec := NewDecoder(strings.NewReader(`1`))
	var n int
	if err := dec.Decode(&n); err != nil {
		t.Fatal(err)
	}
	if dec.More() {
		t.Error("More should be false after consuming all values")
	}
	// Second decode returns EOF.
	if err := dec.Decode(&n); err == nil {
		t.Error("expected EOF on second decode")
	}
}

func TestDecoderMoreSkipsLeadingComments(t *testing.T) {
	src := "// header\n42\n// footer\n"
	dec := NewDecoder(strings.NewReader(src))
	if !dec.More() {
		t.Error("More should be true with a value present after leading comment")
	}
	var n int
	if err := dec.Decode(&n); err != nil {
		t.Fatal(err)
	}
	if n != 42 {
		t.Errorf("got %d", n)
	}
}

func TestDecoderMoreFalseAfterLeadingCommentOnly(t *testing.T) {
	dec := NewDecoder(strings.NewReader("// just a comment\n"))
	if dec.More() {
		t.Error("More should be false when only comments remain")
	}
}

func TestDecoderMultipleValuesWithCommentsBetween(t *testing.T) {
	src := "// header\n1\n// between\n2\n// after\n"
	dec := NewDecoder(strings.NewReader(src))
	var got []int
	for dec.More() {
		var n int
		if err := dec.Decode(&n); err != nil {
			t.Fatal(err)
		}
		got = append(got, n)
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("got %+v", got)
	}
}

func TestDecoderEmptyReaderEOF(t *testing.T) {
	dec := NewDecoder(bytes.NewReader(nil))
	var v any
	if err := dec.Decode(&v); err == nil {
		t.Error("expected EOF on empty reader")
	}
}

func TestDecoderEmptyBuffer(t *testing.T) {
	dec := NewDecoder(strings.NewReader(""))
	if dec.More() {
		t.Error("More on empty stream should be false")
	}
}

func TestDecoderMoreOnReaderError(t *testing.T) {
	dec := NewDecoder(&errReader{err: errors.New("io boom")})
	if dec.More() {
		t.Error("More on a failing reader should be false")
	}
}

func TestDecoderInlineCommentBeforeValue(t *testing.T) {
	src := `/* head */ 42`
	dec := NewDecoder(strings.NewReader(src))
	var n int
	if err := dec.Decode(&n); err != nil {
		t.Fatal(err)
	}
	if n != 42 {
		t.Errorf("got %d", n)
	}
}

func TestDecoderMoreWithOnlyWhitespace(t *testing.T) {
	dec := NewDecoder(strings.NewReader("   \n\t  "))
	if dec.More() {
		t.Error("More on whitespace-only stream should be false")
	}
}

// ---------------------------------------------------------------------------
// Decoder.SetContext propagates value to UnmarshalerContext.
// ---------------------------------------------------------------------------

type ctxDecodingHolder struct {
	Got string
}

func (c *ctxDecodingHolder) UnmarshalJSONC(ctx context.Context, unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	if v := ctx.Value(ctxKey{}); v != nil {
		c.Got = v.(string) + "+" + s
	} else {
		c.Got = s
	}
	return nil
}

func TestDecoderSetContextPropagates(t *testing.T) {
	dec := NewDecoder(strings.NewReader(`"hello"`))
	dec.SetContext(context.WithValue(context.Background(), ctxKey{}, "ctx"))
	var c ctxDecodingHolder
	if err := dec.Decode(&c); err != nil {
		t.Fatal(err)
	}
	if c.Got != "ctx+hello" {
		t.Errorf("got %q", c.Got)
	}
}

// ---------------------------------------------------------------------------
// Top-level Unmarshal dispatches to UnmarshalerContext.
// ---------------------------------------------------------------------------

type ctxRecv struct {
	S string
}

func (c *ctxRecv) UnmarshalJSONC(_ context.Context, unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	c.S = "ctx:" + s
	return nil
}

func TestUnmarshalViaContextInterface(t *testing.T) {
	var c ctxRecv
	if err := Unmarshal([]byte(`"v"`), &c); err != nil {
		t.Fatal(err)
	}
	if c.S != "ctx:v" {
		t.Errorf("got %q", c.S)
	}
}

// ---------------------------------------------------------------------------
// WithCustomUnmarshaler option.
// ---------------------------------------------------------------------------

type CustomU struct {
	Tag string
}

func TestCustomUnmarshalerOption(t *testing.T) {
	opt := WithCustomUnmarshaler(func(c *CustomU, data []byte) error {
		c.Tag = "custom:" + string(data)
		return nil
	})
	var c CustomU
	if err := UnmarshalWithOptions([]byte(`"hello"`), &c, opt); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.Tag, "hello") {
		t.Errorf("got %+v", c)
	}
}

// ---------------------------------------------------------------------------
// StructValidator integration.
// ---------------------------------------------------------------------------

type rejectingValidator struct{}

func (rejectingValidator) Struct(_ any) error {
	return errors.New("validator says no")
}

func TestStructValidatorRejects(t *testing.T) {
	type S struct {
		V int `json:"v"`
	}
	var s S
	err := UnmarshalWithOptions([]byte(`{"v": 1}`), &s, WithValidator(rejectingValidator{}))
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("expected ValidationError, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Required tag presence semantics.
// ---------------------------------------------------------------------------

func TestRequiredTagMissingKey(t *testing.T) {
	type S struct {
		V int `json:"v" jsonc:",required"`
	}
	var s S
	err := Unmarshal([]byte(`{}`), &s)
	if err == nil {
		t.Error("expected required error")
	}
}

func TestRequiredTagNullSatisfies(t *testing.T) {
	type S struct {
		V *int `json:"v" jsonc:",required"`
	}
	var s S
	if err := Unmarshal([]byte(`{"v": null}`), &s); err != nil {
		t.Errorf("null should satisfy required: %v", err)
	}
}

// ---------------------------------------------------------------------------
// RawValue lifecycle through Unmarshal.
// ---------------------------------------------------------------------------

func TestRawValueScalar(t *testing.T) {
	type S struct {
		N RawValue `json:"n"`
	}
	src := `{"n": 42}`
	var s S
	if err := Unmarshal([]byte(src), &s); err != nil {
		t.Fatal(err)
	}
	if string(s.N) != "42" {
		t.Errorf("got %q", s.N)
	}
}

func TestRawValueBoolean(t *testing.T) {
	type S struct {
		B RawValue `json:"b"`
	}
	src := `{"b": true}`
	var s S
	if err := Unmarshal([]byte(src), &s); err != nil {
		t.Fatal(err)
	}
	if string(s.B) != "true" {
		t.Errorf("got %q", s.B)
	}
}

func TestRawValueNull(t *testing.T) {
	type S struct {
		Z RawValue `json:"z"`
	}
	src := `{"z": null}`
	var s S
	if err := Unmarshal([]byte(src), &s); err != nil {
		t.Fatal(err)
	}
	if string(s.Z) != "null" {
		t.Errorf("got %q", s.Z)
	}
}

type rawScalar struct {
	N RawValue `json:"n"`
	S RawValue `json:"s"`
	B RawValue `json:"b"`
	Z RawValue `json:"z"`
}

func TestRawValueAllScalarKinds(t *testing.T) {
	src := `{"n": 42, "s": "hello", "b": true, "z": null}`
	var v rawScalar
	if err := Unmarshal([]byte(src), &v); err != nil {
		t.Fatal(err)
	}
	if string(v.N) != "42" || string(v.S) != `"hello"` || string(v.B) != "true" || string(v.Z) != "null" {
		t.Errorf("got %+v", v)
	}
}

type embeddedRaw struct {
	V RawValue `json:"v"`
}

func TestRawValueOnNumberWithFreshAST(t *testing.T) {
	// Decode then encode RawValue forces nodeRawBytes paths.
	src := `{"v": 1.5e10}`
	var s embeddedRaw
	if err := Unmarshal([]byte(src), &s); err != nil {
		t.Fatal(err)
	}
	if string(s.V) != "1.5e10" {
		t.Errorf("got %q", s.V)
	}
}

// ---------------------------------------------------------------------------
// Generic Unmarshal of nested objects into any.
// ---------------------------------------------------------------------------

func TestUnmarshalNestedObjectsIntoAny(t *testing.T) {
	src := `{"a": {"b": {"c": [1, 2, [3, 4]]}}}`
	var v any
	if err := Unmarshal([]byte(src), &v); err != nil {
		t.Fatal(err)
	}
	// Drill into nested map[string]any.
	m := v.(map[string]any)
	a := m["a"].(map[string]any)
	b := a["b"].(map[string]any)
	c := b["c"].([]any)
	if len(c) != 3 {
		t.Errorf("expected 3 elements, got %d", len(c))
	}
}

func TestUnmarshalEmptyObjectWithCommentsOnly(t *testing.T) {
	src := `{
		// only a comment, no members
	}`
	var v map[string]any
	if err := Unmarshal([]byte(src), &v); err != nil {
		t.Fatal(err)
	}
	if len(v) != 0 {
		t.Errorf("expected empty map, got %+v", v)
	}
}
