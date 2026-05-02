package jsonc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Scalars
// ---------------------------------------------------------------------------

func TestEncodeNil(t *testing.T) {
	out, err := Marshal(nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "null" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeBool(t *testing.T) {
	out, err := Marshal(true)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "true" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeInt(t *testing.T) {
	out, err := Marshal(42)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "42" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeUint(t *testing.T) {
	out, err := Marshal(uint64(1 << 63))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "9223372036854775808" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeFloat(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{1.5, "1.5"},
		{-3.14, "-3.14"},
		{1e-7, "1e-7"},
		{1e21, "1e+21"},
	}
	for _, c := range cases {
		out, err := Marshal(c.in)
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != c.want {
			t.Errorf("for %v got %q want %q", c.in, out, c.want)
		}
	}
}

func TestEncodeFloatInfRejected(t *testing.T) {
	_, err := Marshal(math.Inf(1))
	if !errors.Is(err, ErrUnsupportedValue) {
		t.Errorf("expected ErrUnsupportedValue, got %v", err)
	}
}

func TestEncodeFloatNaNRejected(t *testing.T) {
	_, err := Marshal(math.NaN())
	if !errors.Is(err, ErrUnsupportedValue) {
		t.Errorf("expected ErrUnsupportedValue, got %v", err)
	}
}

func TestEncodeString(t *testing.T) {
	out, err := Marshal("hello")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"hello"` {
		t.Errorf("got %q", out)
	}
}

func TestEncodeStringEscapes(t *testing.T) {
	out, err := Marshal("a\nb\tc\"d\\e")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"a\nb\tc\"d\\e"` {
		t.Errorf("got %q", out)
	}
}

func TestEncodeStringHTMLDefault(t *testing.T) {
	// Default: no HTML escaping (differs from stdlib).
	out, err := Marshal("<script>")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"<script>"` {
		t.Errorf("got %q", out)
	}
}

func TestEncodeStringHTMLOptIn(t *testing.T) {
	out, err := MarshalWithOptions("<&>", WithEscapeHTML(true))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.ContainsAny(out, "<&>") {
		t.Errorf("expected escaped output, got %q", out)
	}
	if !strings.Contains(string(out), "\\u003c") {
		t.Errorf("expected \\u003c in output, got %q", out)
	}
}

// ---------------------------------------------------------------------------
// Slices and arrays
// ---------------------------------------------------------------------------

func TestEncodeSlice(t *testing.T) {
	out, err := Marshal([]int{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "[1, 2, 3]" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeSliceNil(t *testing.T) {
	var s []int
	out, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "null" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeSliceEmpty(t *testing.T) {
	out, err := Marshal([]int{})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "[]" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeArray(t *testing.T) {
	out, err := Marshal([3]int{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "[1, 2, 3]" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeBytesAsBase64(t *testing.T) {
	out, err := Marshal([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"aGVsbG8="` {
		t.Errorf("got %q", out)
	}
}

// ---------------------------------------------------------------------------
// Maps
// ---------------------------------------------------------------------------

func TestEncodeMapStringKey(t *testing.T) {
	out, err := Marshal(map[string]int{"b": 2, "a": 1, "c": 3})
	if err != nil {
		t.Fatal(err)
	}
	// Default = lexicographic.
	if string(out) != `{"a": 1, "b": 2, "c": 3}` {
		t.Errorf("got %q", out)
	}
}

func TestEncodeMapNil(t *testing.T) {
	var m map[string]int
	out, err := Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "null" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeMapIntKey(t *testing.T) {
	out, err := Marshal(map[int]string{2: "b", 1: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"1": "a", "2": "b"}` {
		t.Errorf("got %q", out)
	}
}

type colorKey int

func (c colorKey) MarshalText() ([]byte, error) {
	return []byte(map[colorKey]string{1: "red", 2: "blue"}[c]), nil
}

func TestEncodeMapTextMarshalerKey(t *testing.T) {
	out, err := Marshal(map[colorKey]int{1: 100, 2: 200})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"blue": 200, "red": 100}` {
		t.Errorf("got %q", out)
	}
}

func TestEncodeMapSlice(t *testing.T) {
	ms := MapSlice{
		{Key: "first", Value: 1},
		{Key: "second", Value: "two"},
		{Key: "third", Value: true},
	}
	out, err := Marshal(ms)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"first": 1, "second": "two", "third": true}` {
		t.Errorf("got %q", out)
	}
}

// ---------------------------------------------------------------------------
// Structs
// ---------------------------------------------------------------------------

type basicStruct struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestEncodeStruct(t *testing.T) {
	// Struct fields are emitted in declaration order, matching encoding/json.
	out, err := Marshal(basicStruct{Name: "alice", Age: 30})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"name": "alice", "age": 30}` {
		t.Errorf("got %q", out)
	}
}

type omitStruct struct {
	A string `json:"a,omitempty"`
	B int    `json:"b,omitempty"`
	C bool   `json:"c,omitempty"`
}

func TestEncodeStructOmitEmpty(t *testing.T) {
	out, err := Marshal(omitStruct{A: "set"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"a": "set"}` {
		t.Errorf("got %q", out)
	}
}

type omitZeroStruct struct {
	A int `jsonc:"a,omitzero"`
	B int `jsonc:"b,omitzero"`
}

func TestEncodeStructOmitZero(t *testing.T) {
	out, err := Marshal(omitZeroStruct{A: 5})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"a": 5}` {
		t.Errorf("got %q", out)
	}
}

type stringTagStruct struct {
	N int  `json:"n,string"`
	B bool `json:"b,string"`
}

func TestEncodeStructAsString(t *testing.T) {
	out, err := Marshal(stringTagStruct{N: 42, B: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"n": "42", "b": "true"}` {
		t.Errorf("got %q", out)
	}
}

type embeddedInner struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type embeddedOuter struct {
	embeddedInner
	Z int `json:"z"`
}

func TestEncodeStructEmbedded(t *testing.T) {
	out, err := Marshal(embeddedOuter{embeddedInner: embeddedInner{X: 1, Y: 2}, Z: 3})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"x": 1, "y": 2, "z": 3}` {
		t.Errorf("got %q", out)
	}
}

type skippedStruct struct {
	Public  string `json:"public"`
	Skipped string `json:"-"`
}

func TestEncodeStructSkipDash(t *testing.T) {
	out, err := Marshal(skippedStruct{Public: "yes", Skipped: "no"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"public": "yes"}` {
		t.Errorf("got %q", out)
	}
}

// ---------------------------------------------------------------------------
// Indent and trailing comma
// ---------------------------------------------------------------------------

func TestEncodeIndent(t *testing.T) {
	out, err := MarshalIndent(map[string]int{"a": 1, "b": 2}, "  ")
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": 1,\n  \"b\": 2\n}"
	if string(out) != want {
		t.Errorf("got %q\nwant %q", out, want)
	}
}

func TestEncodeIndentTrailingComma(t *testing.T) {
	out, err := MarshalWithOptions(
		map[string]int{"a": 1},
		WithIndent("  "),
		WithTrailingComma(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": 1,\n}"
	if string(out) != want {
		t.Errorf("got %q\nwant %q", out, want)
	}
}

func TestEncodeStrictJSONOutputDropsTrailingComma(t *testing.T) {
	out, err := MarshalWithOptions(
		map[string]int{"a": 1},
		WithIndent("  "),
		WithTrailingComma(true),
		WithStrictJSONOutput(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), ",\n}") {
		t.Errorf("strict-JSON output should drop trailing comma: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Special types
// ---------------------------------------------------------------------------

func TestEncodeTime(t *testing.T) {
	ts := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	out, err := Marshal(ts)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"2024-01-15T10:30:00Z"` {
		t.Errorf("got %q", out)
	}
}

func TestEncodeDurationDefaultIsInt64(t *testing.T) {
	d := 1500 * time.Millisecond
	out, err := Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "1500000000" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeDurationAsString(t *testing.T) {
	d := 1500 * time.Millisecond
	out, err := MarshalWithOptions(d, WithDurationAsString(true))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"1.5s"` {
		t.Errorf("got %q", out)
	}
}

func TestEncodeBigInt(t *testing.T) {
	bi := new(big.Int)
	bi.SetString("12345678901234567890123456789012345", 10)
	out, err := Marshal(bi)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "12345678901234567890123456789012345" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeBigFloat(t *testing.T) {
	bf, _, err := big.ParseFloat("3.14", 10, 53, big.ToNearestEven)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Marshal(bf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "3.14") {
		t.Errorf("got %q", out)
	}
}

func TestEncodeJSONNumber(t *testing.T) {
	n := json.Number("3.14159")
	out, err := Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "3.14159" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeRawValue(t *testing.T) {
	r := RawValue(`{"a": 1, /* preserved */ "b": 2}`)
	out, err := Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(r) {
		t.Errorf("RawValue should pass through verbatim. got %q", out)
	}
}

func TestEncodeRawValueStrictDropsComments(t *testing.T) {
	r := RawValue(`{"a": 1, /* dropped */ "b": 2,}`)
	out, err := MarshalWithOptions(r, WithStrictJSONOutput(true))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "/*") {
		t.Errorf("strict output should drop comments: %s", out)
	}
}

func TestEncodeRawMessage(t *testing.T) {
	r := json.RawMessage(`{"x":1}`)
	out, err := Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"x":1}` {
		t.Errorf("got %q", out)
	}
}

// ---------------------------------------------------------------------------
// Marshaler interfaces
// ---------------------------------------------------------------------------

type customMarshaler struct {
	Tag string
}

func (c customMarshaler) MarshalJSONC() ([]byte, error) {
	return []byte(`"custom:` + c.Tag + `"`), nil
}

func TestEncodeMarshaler(t *testing.T) {
	out, err := Marshal(customMarshaler{Tag: "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"custom:foo"` {
		t.Errorf("got %q", out)
	}
}

type ctxMarshaler struct {
	Tag string
}

func (c ctxMarshaler) MarshalJSONC(_ context.Context) ([]byte, error) {
	return []byte(`"ctx:` + c.Tag + `"`), nil
}

func TestEncodeMarshalerContext(t *testing.T) {
	var buf strings.Builder
	enc := NewEncoder(&buf)
	if err := enc.Encode(ctxMarshaler{Tag: "x"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"ctx:x"`) {
		t.Errorf("got %q", buf.String())
	}
}

type stdMarshaler struct {
	Tag string
}

func (s stdMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`"std:` + s.Tag + `"`), nil
}

func TestEncodeStdlibMarshaler(t *testing.T) {
	out, err := Marshal(stdMarshaler{Tag: "y"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"std:y"` {
		t.Errorf("got %q", out)
	}
}

type textMarshalerType struct {
	Value string
}

func (tm textMarshalerType) MarshalText() ([]byte, error) {
	return []byte("txt:" + tm.Value), nil
}

func TestEncodeTextMarshaler(t *testing.T) {
	out, err := Marshal(textMarshalerType{Value: "z"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"txt:z"` {
		t.Errorf("got %q", out)
	}
}

// ---------------------------------------------------------------------------
// Custom marshalers via WithCustomMarshaler
// ---------------------------------------------------------------------------

type plainStruct struct {
	A int
}

func TestEncodeCustomMarshaler(t *testing.T) {
	opt := WithCustomMarshaler(func(p plainStruct) ([]byte, error) {
		return []byte(`"plain"`), nil
	})
	out, err := MarshalWithOptions(plainStruct{A: 1}, opt)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"plain"` {
		t.Errorf("got %q", out)
	}
}

// ---------------------------------------------------------------------------
// Pointers and cycles
// ---------------------------------------------------------------------------

func TestEncodePointerNil(t *testing.T) {
	var p *int
	out, err := Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "null" {
		t.Errorf("got %q", out)
	}
}

func TestEncodePointerValue(t *testing.T) {
	x := 5
	out, err := Marshal(&x)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "5" {
		t.Errorf("got %q", out)
	}
}

type cyclic struct {
	Self *cyclic
}

func TestEncodeCycleDetected(t *testing.T) {
	c := &cyclic{}
	c.Self = c
	_, err := Marshal(c)
	if !errors.Is(err, ErrUnsupportedValue) {
		t.Errorf("expected ErrUnsupportedValue, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Encoder streaming
// ---------------------------------------------------------------------------

func TestEncoderEncode(t *testing.T) {
	var buf strings.Builder
	enc := NewEncoder(&buf)
	if err := enc.Encode(map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != `{"a": 1}`+"\n" {
		t.Errorf("got %q", buf.String())
	}
}

func TestEncoderMultiple(t *testing.T) {
	var buf strings.Builder
	enc := NewEncoder(&buf)
	for i := range 3 {
		if err := enc.Encode(i); err != nil {
			t.Fatal(err)
		}
	}
	if buf.String() != "0\n1\n2\n" {
		t.Errorf("got %q", buf.String())
	}
}

func TestEncoderSetIndent(t *testing.T) {
	var buf strings.Builder
	enc := NewEncoder(&buf)
	enc.SetIndent("\t")
	if err := enc.Encode([]int{1, 2}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "\t1") {
		t.Errorf("expected tab indent: %q", buf.String())
	}
}

func TestEncoderSetEscapeHTML(t *testing.T) {
	var buf strings.Builder
	enc := NewEncoder(&buf)
	enc.SetEscapeHTML(true)
	if err := enc.Encode("<x>"); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(buf.String(), "<>") {
		t.Errorf("expected escaped output, got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "\\u003c") {
		t.Errorf("expected \\u003c, got %q", buf.String())
	}
}

func TestEncoderSetContext(t *testing.T) {
	var buf strings.Builder
	enc := NewEncoder(&buf)
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "v")
	enc.SetContext(ctx)
	if err := enc.Encode("x"); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Round trip through Unmarshal
// ---------------------------------------------------------------------------

func TestEncodeRoundTrip(t *testing.T) {
	type Inner struct {
		Tags []string `json:"tags"`
	}
	type Outer struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
		Inner Inner  `json:"inner"`
	}
	in := Outer{Name: "x", Count: 7, Inner: Inner{Tags: []string{"a", "b"}}}
	out, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var got Outer
	if err := Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != in.Name || got.Count != in.Count || len(got.Inner.Tags) != 2 {
		t.Errorf("round-trip mismatch: %+v vs %+v", got, in)
	}
}

// ---------------------------------------------------------------------------
// Unsupported types
// ---------------------------------------------------------------------------

func TestEncodeUnsupportedChan(t *testing.T) {
	ch := make(chan int)
	_, err := Marshal(ch)
	if !errors.Is(err, ErrUnsupportedValue) {
		t.Errorf("expected ErrUnsupportedValue, got %v", err)
	}
}

func TestEncodeUnsupportedFunc(t *testing.T) {
	_, err := Marshal(func() {})
	if !errors.Is(err, ErrUnsupportedValue) {
		t.Errorf("expected ErrUnsupportedValue, got %v", err)
	}
}
