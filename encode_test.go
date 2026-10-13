package jsonc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"path/filepath"
	"regexp"
	"strconv"
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

func TestEncodeStringHTMLLineSeparators(t *testing.T) {
	in := "a b c<"
	want, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := MarshalWithOptions(in, WithEscapeHTML(true))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("got %s, want %s (encoding/json)", got, want)
	}
	raw, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "\"a b c<\"" {
		t.Errorf("without WithEscapeHTML got %q, want separators unescaped", raw)
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

type alwaysZero struct{ N int }

func (alwaysZero) IsZero() bool { return true }

type jsonOmitZeroStruct struct {
	A int        `json:"a,omitzero"`
	B int        `json:"b,omitzero"`
	T time.Time  `json:"t,omitzero"`
	Z alwaysZero `json:"z,omitzero"`
	P *int       `json:"p,omitzero"`
}

func TestEncodeStructOmitZeroJSONTag(t *testing.T) {
	out, err := Marshal(jsonOmitZeroStruct{A: 5, Z: alwaysZero{N: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"a": 5}` {
		t.Errorf("got %s, want {\"a\": 5}", out)
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
	if string(out) != `{"x": 1}` {
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

type rawJSON string

func (r rawJSON) MarshalJSON() ([]byte, error) { return []byte(r), nil }

type rawJSONC string

func (r rawJSONC) MarshalJSONC() ([]byte, error) { return []byte(r), nil }

func TestMarshalerOutputValidated(t *testing.T) {
	bad := []struct {
		name string
		v    any
		opts []EncodeOption
	}{
		{"RawMessage unterminated", json.RawMessage(`{bad`), nil},
		{"RawMessage two values", json.RawMessage(`1 2`), nil},
		{"RawMessage comment", json.RawMessage(`1 // c`), nil},
		{"MarshalJSON injection", map[string]rawJSON{"x": `"a"}, "admin": true, "y": {"z"`}, nil},
		{"MarshalJSON injection strict", map[string]rawJSON{"x": `"a"}, "admin": true, "y": {"z"`}, []EncodeOption{WithStrictJSONOutput(true)}},
		{"MarshalJSON empty", rawJSON(""), nil},
		{"MarshalJSON trailing comma", rawJSON(`[1,]`), nil},
		{"MarshalJSONC two values", rawJSONC(`1 2`), nil},
		{"RawValue unterminated", RawValue(`{"a":`), nil},
		{"custom marshaler", 1, []EncodeOption{WithCustomMarshaler(func(int) ([]byte, error) { return []byte(`}`), nil })}},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			out, err := MarshalWithOptions(tt.v, tt.opts...)
			if err == nil {
				t.Fatalf("got %s, want error", out)
			}
		})
	}

	good := []struct {
		name string
		v    any
		opts []EncodeOption
		want string
	}{
		{"MarshalJSON re-indented", struct{ A rawJSON }{`{"b":[1,2]}`}, []EncodeOption{WithIndent("  ")},
			"{\n  \"A\": {\n    \"b\": [\n      1,\n      2\n    ]\n  }\n}"},
		{"MarshalJSONC comment kept", struct{ A rawJSONC }{"{\"b\": 1, // c\n}"}, []EncodeOption{WithIndent("  ")},
			"{\n  \"A\": {\n    \"b\": 1 // c\n  }\n}"},
		{"MarshalJSONC strict output", struct{ A rawJSONC }{"[1, /* c */ 2,]"}, []EncodeOption{WithStrictJSONOutput(true)},
			`{"A": [1, 2]}`},
		{"RawValue verbatim", struct{ A RawValue }{RawValue("[1, /* c */ 2,]")}, nil,
			`{"A": [1, /* c */ 2,]}`},
		{"number text kept", rawJSON(`1.50e+2`), nil, `1.50e+2`},
	}
	for _, tt := range good {
		t.Run(tt.name, func(t *testing.T) {
			out, err := MarshalWithOptions(tt.v, tt.opts...)
			if err != nil || string(out) != tt.want {
				t.Errorf("got %q, %v; want %q", out, err, tt.want)
			}
		})
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

// ---------------------------------------------------------------------------
// WithComment(map[string][]Comment) — path-keyed comment injection
// ---------------------------------------------------------------------------

type withCommentServer struct {
	Port int    `json:"port"`
	Host string `json:"host"`
}

type withCommentRoot struct {
	Server withCommentServer `json:"server"`
	Tags   []string          `json:"tags"`
}

func TestEncodeWithCommentHead(t *testing.T) {
	v := withCommentServer{Port: 8080, Host: "localhost"}
	out, err := MarshalWithOptions(v,
		WithIndent("  "),
		WithComment(map[string][]Comment{
			"port": {{Position: HeadCommentPos, Text: "the listening port"}},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  // the listening port\n  \"port\": 8080,\n  \"host\": \"localhost\"\n}"
	if string(out) != want {
		t.Errorf("got %q\nwant %q", out, want)
	}
}

func TestEncodeWithCommentLine(t *testing.T) {
	v := withCommentServer{Port: 8080, Host: "localhost"}
	out, err := MarshalWithOptions(v,
		WithIndent("  "),
		WithComment(map[string][]Comment{
			"port": {{Position: LineCommentPos, Text: "default"}},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"port": 8080, // default`) {
		t.Errorf("expected line comment after value, got %q", out)
	}
}

func TestEncodeWithCommentFootMid(t *testing.T) {
	// Foot comment on a non-last field appears between members.
	v := withCommentServer{Port: 8080, Host: "localhost"}
	out, err := MarshalWithOptions(v,
		WithIndent("  "),
		WithComment(map[string][]Comment{
			"port": {{Position: FootCommentPos, Text: "end of port section"}},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"port\": 8080,\n  // end of port section\n  \"host\": \"localhost\"\n}"
	if string(out) != want {
		t.Errorf("got %q\nwant %q", out, want)
	}
}

func TestEncodeWithCommentFootLast(t *testing.T) {
	// Foot comment on the last field appears before the closing brace,
	// without an extra blank line.
	v := withCommentServer{Port: 8080, Host: "localhost"}
	out, err := MarshalWithOptions(v,
		WithIndent("  "),
		WithComment(map[string][]Comment{
			"host": {{Position: FootCommentPos, Text: "trailing"}},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"port\": 8080,\n  \"host\": \"localhost\"\n  // trailing\n}"
	if string(out) != want {
		t.Errorf("got %q\nwant %q", out, want)
	}
}

func TestEncodeWithCommentNestedPath(t *testing.T) {
	v := withCommentRoot{
		Server: withCommentServer{Port: 8080, Host: "localhost"},
		Tags:   []string{"a"},
	}
	out, err := MarshalWithOptions(v,
		WithIndent("  "),
		WithComment(map[string][]Comment{
			"server.port": {{Position: HeadCommentPos, Text: "nested head"}},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "    // nested head\n    \"port\": 8080") {
		t.Errorf("expected nested head comment, got:\n%s", out)
	}
}

func TestEncodeWithCommentArrayIndex(t *testing.T) {
	v := withCommentRoot{
		Server: withCommentServer{Port: 1, Host: "h"},
		Tags:   []string{"first", "second", "third"},
	}
	out, err := MarshalWithOptions(v,
		WithIndent("  "),
		WithComment(map[string][]Comment{
			"tags[1]": {{Position: HeadCommentPos, Text: "the middle one"}},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "// the middle one\n    \"second\"") {
		t.Errorf("expected head comment on tags[1], got:\n%s", out)
	}
}

func TestEncodeWithCommentMultiplePerPath(t *testing.T) {
	v := withCommentServer{Port: 8080, Host: "localhost"}
	out, err := MarshalWithOptions(v,
		WithIndent("  "),
		WithComment(map[string][]Comment{
			"port": {
				{Position: HeadCommentPos, Text: "first head"},
				{Position: HeadCommentPos, Text: "second head"},
				{Position: LineCommentPos, Text: "inline"},
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "// first head") || !strings.Contains(s, "// second head") {
		t.Errorf("expected both head comments, got:\n%s", s)
	}
	if !strings.Contains(s, "// inline") {
		t.Errorf("expected inline comment, got:\n%s", s)
	}
}

func TestEncodeWithCommentDroppedInCompact(t *testing.T) {
	// No indent → compact mode → comments must be dropped.
	v := withCommentServer{Port: 8080, Host: "localhost"}
	out, err := MarshalWithOptions(v,
		WithComment(map[string][]Comment{
			"port": {{Position: HeadCommentPos, Text: "should not appear"}},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "should not appear") {
		t.Errorf("compact mode should drop comments, got: %s", out)
	}
	if strings.Contains(string(out), "//") {
		t.Errorf("compact mode should drop comments, got: %s", out)
	}
}

func TestEncodeWithCommentDroppedInStrictOutput(t *testing.T) {
	v := withCommentServer{Port: 8080, Host: "localhost"}
	out, err := MarshalWithOptions(v,
		WithIndent("  "),
		WithStrictJSONOutput(true),
		WithComment(map[string][]Comment{
			"port": {{Position: HeadCommentPos, Text: "should not appear"}},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "should not appear") {
		t.Errorf("strict-JSON output should drop comments, got: %s", out)
	}
}

func TestEncodeWithCommentMap(t *testing.T) {
	// Same path-lookup logic must work for native maps.
	v := map[string]int{"a": 1, "b": 2}
	out, err := MarshalWithOptions(v,
		WithIndent("  "),
		WithComment(map[string][]Comment{
			"a": {{Position: HeadCommentPos, Text: "first"}},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "// first\n  \"a\"") {
		t.Errorf("expected head comment on map key 'a', got:\n%s", out)
	}
}

func TestEncodeWithCommentMapSlice(t *testing.T) {
	// MapSlice should also honor path lookups by key string.
	v := MapSlice{
		{Key: "x", Value: 1},
		{Key: "y", Value: 2},
	}
	out, err := MarshalWithOptions(v,
		WithIndent("  "),
		WithComment(map[string][]Comment{
			"x": {{Position: LineCommentPos, Text: "the x"}},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"x": 1, // the x`) {
		t.Errorf("expected line comment on MapSlice key 'x', got:\n%s", out)
	}
}

func TestEncodeWithCommentMultilineHeadText(t *testing.T) {
	// Multi-line text in a head comment should produce multiple // lines.
	v := withCommentServer{Port: 8080, Host: "localhost"}
	out, err := MarshalWithOptions(v,
		WithIndent("  "),
		WithComment(map[string][]Comment{
			"port": {{Position: HeadCommentPos, Text: "line one\nline two"}},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "// line one\n  // line two\n  \"port\"") {
		t.Errorf("expected two-line head comment, got:\n%s", s)
	}
}

// ---------------------------------------------------------------------------
// Marshal entry points: MarshalTo, EncodeFile.
// ---------------------------------------------------------------------------

func TestMarshalToGeneric(t *testing.T) {
	type S struct {
		N int `json:"n"`
	}
	out, err := MarshalTo(S{N: 42})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"n": 42}` {
		t.Errorf("got %q", out)
	}
}

func TestEncodeFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.jsonc")

	type S struct {
		Name string `json:"name"`
		Tags []int  `json:"tags"`
	}
	in := S{Name: "x", Tags: []int{1, 2, 3}}
	if err := EncodeFile(path, in, WithIndent("  ")); err != nil {
		t.Fatal(err)
	}

	var got S
	if err := DecodeFile(path, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "x" || len(got.Tags) != 3 {
		t.Errorf("round trip lost data: %+v", got)
	}
}

func TestEncodeFileUnsupportedValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.jsonc")
	// Channels can't be encoded; should propagate ErrUnsupportedValue.
	err := EncodeFile(path, make(chan int))
	if !errors.Is(err, ErrUnsupportedValue) {
		t.Errorf("expected ErrUnsupportedValue, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// encodeArrayValue — fixed-size [N]byte and [N]int.
// ---------------------------------------------------------------------------

func TestEncodeFixedSizeByteArray(t *testing.T) {
	v := [4]byte{0x68, 0x65, 0x6c, 0x6c} // "hell"
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	// [4]byte encodes as base64 string (matches stdlib).
	if string(out) != `"aGVsbA=="` {
		t.Errorf("got %q", out)
	}
}

func TestEncodeFixedSizeIntArray(t *testing.T) {
	v := [3]int{10, 20, 30}
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `[10, 20, 30]` {
		t.Errorf("got %q", out)
	}
}

// Array of bytes (not slice).
func TestEncodeArrayOfBytesValue(t *testing.T) {
	v := [3]byte{'a', 'b', 'c'}
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	// [3]byte → base64 encoded, matches stdlib.
	if string(out) != `"YWJj"` {
		t.Errorf("got %q", out)
	}
}

// ---------------------------------------------------------------------------
// isEmptyValue — exercise every kind branch.
// ---------------------------------------------------------------------------

func TestEncodeOmitEmptyAcrossAllKinds(t *testing.T) {
	// Fixed-size arrays aren't "empty" in stdlib semantics (Len is the
	// compile-time size, not zero), so they're always kept; we test
	// length-zero kinds plus pointers/interfaces/numbers/booleans here.
	type S struct {
		Bool    bool           `json:"b,omitempty"`
		Int     int            `json:"i,omitempty"`
		Uint    uint           `json:"u,omitempty"`
		Float   float64        `json:"f,omitempty"`
		Str     string         `json:"s,omitempty"`
		Slice   []int          `json:"sl,omitempty"`
		Map     map[string]int `json:"m,omitempty"`
		Ptr     *int           `json:"p,omitempty"`
		Iface   any            `json:"if,omitempty"`
		NotZero string         `json:"nz,omitempty"`
	}
	v := S{NotZero: "kept"}
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"nz": "kept"}` {
		t.Errorf("got %q", out)
	}
}

// ---------------------------------------------------------------------------
// Encoder error paths.
// ---------------------------------------------------------------------------

func TestEncoderBadCustomMarshalerSignature(t *testing.T) {
	type X struct{ V int }
	// Custom marshaler that returns an error.
	opt := WithCustomMarshaler(func(_ X) ([]byte, error) {
		return nil, errors.New("explode")
	})
	_, err := MarshalWithOptions(X{}, opt)
	if err == nil || !strings.Contains(err.Error(), "explode") {
		t.Errorf("expected propagated custom-marshaler error, got %v", err)
	}
}

func TestEncoderTimeMarshalErrorNotProduced(t *testing.T) {
	// time.Time always marshals successfully for valid times.
	out, err := Marshal(time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), `"`) {
		t.Errorf("got %q", out)
	}
}

// ---------------------------------------------------------------------------
// normalizeLineEndings / sanitizeBlockCommentText helpers.
// ---------------------------------------------------------------------------

func TestNormalizeLineEndings(t *testing.T) {
	cases := map[string]string{
		"":       "",
		"abc":    "abc",
		"a\nb":   "a\nb",
		"a\rb":   "a\nb",
		"a\r\nb": "a\nb",
		"a\r\rb": "a\n\nb",
		"a\n\rb": "a\n\nb",
	}
	for in, want := range cases {
		if got := normalizeLineEndings(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeBlockCommentText(t *testing.T) {
	cases := map[string]string{
		"":                 "",
		"hello":            "hello",
		"a*/b":             "a* /b",
		"*/*/":             "* /* /",
		"safe * / not */ ": "safe * / not * / ",
	}
	for in, want := range cases {
		if got := sanitizeBlockCommentText(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Encoder writeBase64 corner cases — empty and exact-multiple lengths.
// ---------------------------------------------------------------------------

func TestEncodeEmptyByteSlice(t *testing.T) {
	out, err := Marshal([]byte{})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `""` {
		t.Errorf("got %q", out)
	}
}

func TestEncodeNilByteSlice(t *testing.T) {
	var b []byte
	out, err := Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "null" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeByteSliceMultipleOfThree(t *testing.T) {
	// Ensure the no-padding branch of writeBase64 fires.
	out, err := Marshal([]byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"YWJj"` {
		t.Errorf("got %q", out)
	}
}

// ---------------------------------------------------------------------------
// encodeSpecialType — RawValue / json.RawMessage / json.Number empty cases.
// ---------------------------------------------------------------------------

func TestEncodeRawValueEmpty(t *testing.T) {
	var rv RawValue
	out, err := Marshal(rv)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "null" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeJSONRawMessageEmpty(t *testing.T) {
	out, err := Marshal(json.RawMessage(""))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "null" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeJSONNumberEmpty(t *testing.T) {
	out, err := Marshal(json.Number(""))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "0" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeNilBigIntPointer(t *testing.T) {
	var p *big.Int
	out, err := Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "null" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeNilBigFloatPointer(t *testing.T) {
	var p *big.Float
	out, err := Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "null" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeBigIntValue(t *testing.T) {
	bi := *big.NewInt(123456789012345)
	out, err := Marshal(bi)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "123456789012345" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeBigFloatValue(t *testing.T) {
	bf, _, _ := big.ParseFloat("1.5", 10, 53, big.ToNearestEven)
	out, err := Marshal(*bf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "1.5") {
		t.Errorf("got %q", out)
	}
}

func TestEncodeEmptyMapSlice(t *testing.T) {
	out, err := Marshal(MapSlice{})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "{}" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeBigIntPointer(t *testing.T) {
	bi := big.NewInt(42)
	out, err := Marshal(bi)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "42" {
		t.Errorf("got %q", out)
	}
}

// ---------------------------------------------------------------------------
// mapKeyString — TextMarshaler error path.
// ---------------------------------------------------------------------------

type erringTextMarshaler struct{}

func (erringTextMarshaler) MarshalText() ([]byte, error) {
	return nil, errors.New("text-marshal failed")
}

func TestEncodeMapTextMarshalerKeyError(t *testing.T) {
	m := map[erringTextMarshaler]int{{}: 1}
	_, err := Marshal(m)
	if err == nil || !strings.Contains(err.Error(), "text-marshal failed") {
		t.Errorf("expected propagated error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// encodeStruct — `commented` tag option.
// ---------------------------------------------------------------------------

func TestEncodeCommentedTag(t *testing.T) {
	type S struct {
		A int `json:"a"`
		B int `json:"b" jsonc:",commented"`
	}
	out, err := MarshalWithOptions(S{A: 1, B: 2}, WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": 1\n  /* \"b\": 2 */\n}"
	if string(out) != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

// TestEncodeCommentedTagRoundTrip checks that output with commented
// members decodes again, with those members absent.
func TestEncodeCommentedTagRoundTrip(t *testing.T) {
	type Inner struct {
		X string `json:"x"`
	}
	type S struct {
		A int    `json:"a" jsonc:",commented"`
		B int    `json:"b"`
		C Inner  `json:"c" jsonc:",commented" comment:"about c"`
		D string `json:"d" jsonc:",commented"`
	}
	in := S{A: 1, B: 2, C: Inner{X: "has */ inside"}, D: "last"}
	for _, opts := range [][]EncodeOption{
		{WithIndent("  ")},
		{WithIndent("  "), WithTrailingComma(true)},
	} {
		out, err := MarshalWithOptions(in, opts...)
		if err != nil {
			t.Fatal(err)
		}
		var got S
		if err := Unmarshal(out, &got); err != nil {
			t.Fatalf("output does not decode: %v\n%s", err, out)
		}
		if got != (S{B: 2}) {
			t.Errorf("decoded %+v, want only B set\n%s", got, out)
		}
		if !strings.Contains(string(out), "// about c") {
			t.Errorf("head comment missing:\n%s", out)
		}
	}

	type All struct {
		A int `json:"a" jsonc:",commented"`
	}
	out, err := MarshalWithOptions(All{A: 1}, WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	if err := Unmarshal(out, &All{}); err != nil {
		t.Errorf("all-commented output does not decode: %v\n%s", err, out)
	}
}

// ---------------------------------------------------------------------------
// Cycle detection in nested struct.
// ---------------------------------------------------------------------------

type cyclicStruct struct {
	Self *cyclicStruct `json:"self,omitempty"`
	Tag  string        `json:"tag"`
}

func TestEncodeNestedCycle(t *testing.T) {
	c := &cyclicStruct{Tag: "root"}
	c.Self = c
	_, err := Marshal(c)
	if !errors.Is(err, ErrUnsupportedValue) {
		t.Errorf("expected ErrUnsupportedValue, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Pointers-of-pointers chain dereferencing.
// ---------------------------------------------------------------------------

func TestEncodeDoublePointer(t *testing.T) {
	x := 5
	p := &x
	pp := &p
	out, err := Marshal(pp)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "5" {
		t.Errorf("got %q", out)
	}
}

// ---------------------------------------------------------------------------
// Encoder.SetIndent runtime switch.
// ---------------------------------------------------------------------------

func TestEncoderSetIndentRuntimeSwitch(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.SetIndent("    ")
	if err := enc.Encode(map[string]int{"k": 1}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "    \"k\"") {
		t.Errorf("expected 4-space indent, got %q", buf.String())
	}
}

// ---------------------------------------------------------------------------
// Encoder.EncodeContext — direct, value propagation, writer error.
// ---------------------------------------------------------------------------

// ctxKey is shared between encoder and decoder context tests.
type ctxKey struct{}

type ctxAware struct {
	Got string
}

func (c *ctxAware) MarshalJSONC(ctx context.Context) ([]byte, error) {
	if v := ctx.Value(ctxKey{}); v != nil {
		c.Got = v.(string)
	}
	return []byte(`"ctxValue:` + c.Got + `"`), nil
}

func TestEncoderEncodeContextDirect(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.EncodeContext(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "42\n" {
		t.Errorf("got %q", buf.String())
	}
}

func TestEncoderEncodeContextPropagatesValue(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	ctx := context.WithValue(context.Background(), ctxKey{}, "hello")
	if err := enc.EncodeContext(ctx, &ctxAware{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "ctxValue:hello") {
		t.Errorf("expected propagated context value, got %s", buf.String())
	}
}

type erroringWriter struct{}

func (erroringWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write boom")
}

func TestEncoderEncodeContextWriterError(t *testing.T) {
	enc := NewEncoder(erroringWriter{})
	if err := enc.Encode(42); err == nil || !strings.Contains(err.Error(), "write boom") {
		t.Errorf("expected propagated write error, got %v", err)
	}
}

func TestEncoderEmptyArrayThroughStream(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.Encode([]int{}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "[]\n" {
		t.Errorf("got %q", buf.String())
	}
}

func TestEncoderEncodeUnsupportedReturnsError(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.Encode(make(chan int)); err == nil {
		t.Error("expected ErrUnsupportedValue propagated through Encoder")
	}
}

// ---------------------------------------------------------------------------
// encodeMarshaler — pointer-receiver dispatch through a slice element.
// ---------------------------------------------------------------------------

type ptrMarshaler struct {
	Tag string
}

func (p *ptrMarshaler) MarshalJSONC() ([]byte, error) {
	return []byte(`"ptr:` + p.Tag + `"`), nil
}

func TestEncodeMarshalerOnPointerReceiver(t *testing.T) {
	v := []ptrMarshaler{{Tag: "x"}}
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `["ptr:x"]` {
		t.Errorf("got %q", out)
	}
}

// ---------------------------------------------------------------------------
// Map key ordering options.
// ---------------------------------------------------------------------------

func TestEncodeMapWithInsertionOrder(t *testing.T) {
	// A native map has no insertion order, so the encoder falls back to
	// lexicographic order. Repeat to catch randomized map iteration.
	m := map[string]int{"e": 5, "c": 3, "a": 1, "d": 4, "b": 2, "f": 6, "h": 8, "g": 7}
	for range 20 {
		out, err := MarshalWithOptions(m, WithMapKeyOrder(MapKeyOrderInsertion))
		if err != nil {
			t.Fatal(err)
		}
		const want = `{"a": 1, "b": 2, "c": 3, "d": 4, "e": 5, "f": 6, "g": 7, "h": 8}`
		if string(out) != want {
			t.Fatalf("got %s, want %s", out, want)
		}
	}
}

func TestEncodeMapWithExplicitLexicographicOrder(t *testing.T) {
	out, err := MarshalWithOptions(
		map[string]int{"c": 3, "a": 1, "b": 2},
		WithMapKeyOrder(MapKeyOrderLexicographic),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), `{"a"`) {
		t.Errorf("expected lex-ordered output starting with a: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Float formatting branches.
// ---------------------------------------------------------------------------

func TestEncodeFloatFormatting(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0.5, "0.5"},
		{-0.0001, "-0.0001"},
		{1e10, "10000000000"},
		{1e22, "1e+22"},
		{2.5e-8, "2.5e-8"},
	}
	for _, c := range cases {
		out, err := Marshal(c.in)
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != c.want {
			t.Errorf("for %v got %q, want %q", c.in, out, c.want)
		}
	}
}

func TestEncodeFloat32Small(t *testing.T) {
	v := float32(1e-7)
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "e") {
		t.Errorf("expected exponent form for small float32, got %q", out)
	}
}

func TestEncodeFloat32Large(t *testing.T) {
	v := float32(1e22)
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "e") {
		t.Errorf("expected exponent form for large float32, got %q", out)
	}
}

// ---------------------------------------------------------------------------
// writeQuotedString — exercise control char + escape branches.
// ---------------------------------------------------------------------------

func TestEncodeStringControlCharacters(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"\x00", "\"\\u0000\""},
		{"\x01", "\"\\u0001\""},
		{"\x07", "\"\\u0007\""},
		{"\x1f", "\"\\u001f\""},
		{"\b", "\"\\b\""},
		{"\f", "\"\\f\""},
		{"\n", "\"\\n\""},
		{"\r", "\"\\r\""},
		{"\t", "\"\\t\""},
		{"\"", "\"\\\"\""},
		{"\\", "\"\\\\\""},
	}
	for _, c := range cases {
		out, err := Marshal(c.in)
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != c.want {
			t.Errorf("for %q got %q, want %q", c.in, out, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// fieldByIndexNoAlloc — encoder skips a pointer-embedded field that's nil.
// ---------------------------------------------------------------------------

func TestEncodePointerEmbeddedNilFieldSkipped(t *testing.T) {
	type Inner struct {
		Val int `json:"val"`
	}
	type Outer struct {
		*Inner
		Tag string `json:"tag"`
	}
	o := Outer{Tag: "x"} // Inner is nil.
	out, err := Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	// "val" should not appear because Inner is nil.
	if strings.Contains(string(out), "val") {
		t.Errorf("expected nil embedded fields to be skipped, got %s", out)
	}
}

// ---------------------------------------------------------------------------
// Multiline and orphan-comment AST emission.
// ---------------------------------------------------------------------------

func TestEncodeArrayMultilineOption(t *testing.T) {
	out, err := MarshalWithOptions([]int{1, 2, 3}, WithArrayMultiline(true))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "\n") {
		t.Errorf("WithArrayMultiline should produce multi-line output, got %q", out)
	}
}

// ---------------------------------------------------------------------------
// asString / commented tag combination.
// ---------------------------------------------------------------------------

type asStringWithCommented struct {
	N int `json:"n,string"`
	C int `json:"c,string" jsonc:",commented"`
}

func TestEncodeFieldAsStringAndCommented(t *testing.T) {
	out, err := MarshalWithOptions(asStringWithCommented{N: 7, C: 8}, WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"n": "7"`) {
		t.Errorf("expected n as string, got %s", out)
	}
	if !strings.Contains(string(out), "/*") {
		t.Errorf("expected commented form for c, got %s", out)
	}
}

// ---------------------------------------------------------------------------
// asString tag on non-encodable kinds.
// ---------------------------------------------------------------------------

type nonEncodableField struct {
	Items []int `json:"items,string"`
}

func TestEncodeStringTagOnNonPrimitive(t *testing.T) {
	v := nonEncodableField{Items: []int{1, 2, 3}}
	// asString has no effect on slices; the value is encoded normally.
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "[1, 2, 3]") {
		t.Errorf("got %s", out)
	}
}

// ---------------------------------------------------------------------------
// TextMarshaler value-receiver branch.
// ---------------------------------------------------------------------------

type textMarshalerVal struct {
	N int
}

func (t textMarshalerVal) MarshalText() ([]byte, error) {
	return []byte(strconv.Itoa(t.N)), nil
}

func TestEncodeTextMarshalerValueReceiver(t *testing.T) {
	out, err := Marshal(textMarshalerVal{N: 42})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"42"` {
		t.Errorf("got %q", out)
	}
}

// ---------------------------------------------------------------------------
// Error propagation through nested encoders.
// ---------------------------------------------------------------------------

type erringInner struct{ V int }

func TestEncodeStructFieldErrorPropagates(t *testing.T) {
	// Custom marshaler that errors. Wrap in a struct so the error must
	// propagate back through encodeStruct/encodeFieldValue.
	type Outer struct {
		I erringInner `json:"i"`
	}
	opt := WithCustomMarshaler(func(_ erringInner) ([]byte, error) {
		return nil, errors.New("nested-boom")
	})
	_, err := MarshalWithOptions(Outer{}, opt)
	if err == nil || !strings.Contains(err.Error(), "nested-boom") {
		t.Errorf("expected propagated error, got %v", err)
	}
}

func TestEncodeArrayElementErrorPropagates(t *testing.T) {
	opt := WithCustomMarshaler(func(_ erringInner) ([]byte, error) {
		return nil, errors.New("array-boom")
	})
	_, err := MarshalWithOptions([]erringInner{{V: 1}}, opt)
	if err == nil || !strings.Contains(err.Error(), "array-boom") {
		t.Errorf("expected propagated error, got %v", err)
	}
}

func TestEncodeMapValueErrorPropagates(t *testing.T) {
	opt := WithCustomMarshaler(func(_ erringInner) ([]byte, error) {
		return nil, errors.New("map-boom")
	})
	_, err := MarshalWithOptions(map[string]erringInner{"k": {}}, opt)
	if err == nil || !strings.Contains(err.Error(), "map-boom") {
		t.Errorf("expected propagated error, got %v", err)
	}
}

func TestEncodeMapSliceValueErrorPropagates(t *testing.T) {
	opt := WithCustomMarshaler(func(_ erringInner) ([]byte, error) {
		return nil, errors.New("ms-boom")
	})
	ms := MapSlice{{Key: "k", Value: erringInner{}}}
	_, err := MarshalWithOptions(ms, opt)
	if err == nil || !strings.Contains(err.Error(), "ms-boom") {
		t.Errorf("expected propagated error, got %v", err)
	}
}

func TestEncodeFixedArrayElementErrorPropagates(t *testing.T) {
	opt := WithCustomMarshaler(func(_ erringInner) ([]byte, error) {
		return nil, errors.New("fixed-boom")
	})
	v := [2]erringInner{{V: 1}, {V: 2}}
	_, err := MarshalWithOptions(v, opt)
	if err == nil || !strings.Contains(err.Error(), "fixed-boom") {
		t.Errorf("expected propagated error, got %v", err)
	}
}

func TestEncodeInterfaceFieldWithErrorValue(t *testing.T) {
	type Holder struct {
		V any `json:"v"`
	}
	opt := WithCustomMarshaler(func(_ erringInner) ([]byte, error) {
		return nil, errors.New("iface-boom")
	})
	_, err := MarshalWithOptions(Holder{V: erringInner{}}, opt)
	if err == nil || !strings.Contains(err.Error(), "iface-boom") {
		t.Errorf("expected propagated error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Slice / map with nil pointer values.
// ---------------------------------------------------------------------------

func intPtr(v int) *int { return &v }

func TestEncodeSliceWithNilPointer(t *testing.T) {
	v := []*int{intPtr(1), nil, intPtr(3)}
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "[1, null, 3]" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeMapWithNilValuePointer(t *testing.T) {
	v := map[string]*int{"a": intPtr(1), "b": nil}
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"b": null`) {
		t.Errorf("got %q", out)
	}
}

// ---------------------------------------------------------------------------
// WithComment with deep combined dot+bracket path.
// ---------------------------------------------------------------------------

func TestEncodeWithCommentDeepCombinedPath(t *testing.T) {
	type Inner struct {
		V int `json:"v"`
	}
	v := []Inner{{V: 1}, {V: 2}}
	out, err := MarshalWithOptions(v,
		WithIndent("  "),
		WithComment(map[string][]Comment{
			"[1].v": {{Position: HeadCommentPos, Text: "deep"}},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "// deep") {
		t.Errorf("expected deep head comment, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// Round-trip of a struct slice through MarshalIndent + Unmarshal.
// ---------------------------------------------------------------------------

type structForArray struct {
	Name string `json:"name"`
	Tags []int  `json:"tags"`
}

func TestRoundTripArrayOfStructs(t *testing.T) {
	in := []structForArray{
		{Name: "a", Tags: []int{1, 2}},
		{Name: "b", Tags: []int{3, 4}},
	}
	out, err := MarshalIndent(in, "  ")
	if err != nil {
		t.Fatal(err)
	}
	var got []structForArray
	if err := Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "a" || got[1].Tags[1] != 4 {
		t.Errorf("round trip: %+v", got)
	}
}

type valueMarshaler struct{ N int }

func (v valueMarshaler) MarshalJSON() ([]byte, error) { return []byte(strconv.Itoa(v.N)), nil }

type readingMarshaler struct{ N int }

func (m *readingMarshaler) MarshalJSON() ([]byte, error) { return []byte(strconv.Itoa(m.N)), nil }

type nilPtrMarshaler struct{}

func (p *nilPtrMarshaler) MarshalJSONC() ([]byte, error) {
	if p == nil {
		return []byte(`"nil receiver"`), nil
	}
	return []byte(`"set"`), nil
}

func TestNilPtrMarshaler(t *testing.T) {
	var vr *valueMarshaler
	tests := []struct {
		name string
		v    any
		want string
	}{
		{"nil *time.Time field", struct{ T *time.Time }{}, `{"T": null}`},
		{"nil value-receiver pointer", struct{ V *valueMarshaler }{}, `{"V": null}`},
		{"nil value-receiver in any", struct{ V any }{vr}, `{"V": null}`},
		{"nil pointer receiver is not called", struct{ P *nilPtrMarshaler }{}, `{"P": null}`},
		{"nil *regexp.Regexp field", struct{ M *regexp.Regexp }{}, `{"M": null}`},
		{"nil reading pointer receiver", struct{ M *readingMarshaler }{}, `{"M": null}`},
		{"nil map value", map[string]*readingMarshaler{"a": nil}, `{"a": null}`},
		{"nil slice element", []*readingMarshaler{nil, {N: 2}}, `[null, 2]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Marshal(tt.v)
			if err != nil || string(got) != tt.want {
				t.Errorf("got %s, %v; want %s", got, err, tt.want)
			}
		})
	}
}

func TestUnexportedEmbeddedEncode(t *testing.T) {
	v := struct {
		unexportedInt
		A int
	}{5, 1}
	got, err := Marshal(v)
	if err != nil || string(got) != `{"A": 1}` {
		t.Errorf("got %s, %v", got, err)
	}
}

func TestMarshalCyclicMapAndSlice(t *testing.T) {
	m := map[string]any{}
	m["self"] = m
	if _, err := Marshal(m); !errors.Is(err, ErrUnsupportedValue) {
		t.Errorf("map: err = %v, want ErrUnsupportedValue", err)
	}
	s := []any{nil}
	s[0] = s
	if _, err := Marshal(s); !errors.Is(err, ErrUnsupportedValue) {
		t.Errorf("slice: err = %v, want ErrUnsupportedValue", err)
	}
	ms := MapSlice{{Key: "a"}}
	ms[0].Value = ms
	if _, err := Marshal(ms); !errors.Is(err, ErrUnsupportedValue) {
		t.Errorf("MapSlice: err = %v, want ErrUnsupportedValue", err)
	}
	// Deep but acyclic nesting still encodes.
	var deep any = "x"
	for range 1500 {
		deep = []any{deep}
	}
	if _, err := Marshal(deep); err != nil {
		t.Errorf("deep acyclic: %v", err)
	}
}

func TestNestedMapSliceIndent(t *testing.T) {
	v := map[string]any{"outer": map[string]any{"ms": MapSlice{{Key: "a", Value: 1}, {Key: "b", Value: map[string]int{"c": 2}}}}}
	got, err := MarshalIndent(v, "  ")
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "outer": {
    "ms": {
      "a": 1,
      "b": {
        "c": 2
      }
    }
  }
}`
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestMarshalInvalidUTF8(t *testing.T) {
	got, err := Marshal("a\xffb")
	if err != nil || string(got) != `"a�b"` {
		t.Errorf("got %s, %v", got, err)
	}
	got, err = Marshal("é€😀")
	if err != nil || string(got) != `"é€😀"` {
		t.Errorf("valid multi-byte: got %s, %v", got, err)
	}
}

func TestMarshalInvalidNumber(t *testing.T) {
	for _, n := range []json.Number{"abc", "01", "1.", "+1", "1e"} {
		if got, err := Marshal(n); !errors.Is(err, ErrUnsupportedValue) {
			t.Errorf("Marshal(%q) = %s, %v; want ErrUnsupportedValue", n, got, err)
		}
	}
	for _, n := range []json.Number{"0", "-1.5e10", "123"} {
		if got, err := Marshal(n); err != nil || string(got) != string(n) {
			t.Errorf("Marshal(%q) = %s, %v", n, got, err)
		}
	}
}

func TestCommentPathBracketsDottedKeys(t *testing.T) {
	v := map[string]any{
		"a.b": 1,
		"a":   map[string]int{"b": 2},
	}
	got, err := MarshalWithOptions(v, WithIndent("  "), WithComment(map[string][]Comment{
		`["a.b"]`: {{Position: LineCommentPos, Text: "dotted"}},
		"a.b":     {{Position: LineCommentPos, Text: "nested"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)
	if !strings.Contains(out, `"a.b": 1 // dotted`) || !strings.Contains(out, `"b": 2 // nested`) ||
		strings.Count(out, "//") != 2 {
		t.Errorf("got\n%s", out)
	}
}

func TestRawValueTrailingLineComment(t *testing.T) {
	type S struct {
		A RawValue `json:"a"`
		B int      `json:"b"`
	}
	v := S{A: RawValue("  1 /* x */ // note"), B: 2}
	for _, opts := range [][]EncodeOption{nil, {WithIndent("  ")}} {
		out, err := MarshalWithOptions(v, opts...)
		if err != nil {
			t.Fatal(err)
		}
		if !Valid(out) || !strings.Contains(string(out), `"a": 1,`) {
			t.Errorf("%q", out)
		}
	}
	out, err := Marshal(S{A: RawValue("\n{\"x\": 1} /* kept */\n"), B: 2})
	if err != nil || string(out) != `{"a": {"x": 1} /* kept */, "b": 2}` {
		t.Errorf("block comment: %q, %v", out, err)
	}
}

type htmlMarshaler struct{}

func (htmlMarshaler) MarshalJSON() ([]byte, error) { return []byte(`{"<k>":"<b>&amp; "}`), nil }

func TestEscapeHTMLInMarshalerOutput(t *testing.T) {
	v := map[string]any{"m": htmlMarshaler{}, "r": json.RawMessage(`"<"`)}
	out, err := MarshalWithOptions(v, WithEscapeHTML(true))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.ContainsAny(out, "<>&") {
		t.Errorf("WithEscapeHTML left HTML characters: %s", out)
	}
	if out, _ := Marshal(v); !strings.Contains(string(out), `"<b>&amp; "`) {
		t.Errorf("without WithEscapeHTML the source text is kept: %s", out)
	}
}

type bomMarshaler struct{}

func (bomMarshaler) MarshalJSON() ([]byte, error) { return []byte("\xef\xbb\xbf1"), nil }

func TestMarshalerOutputErrors(t *testing.T) {
	_, err := Marshal(bomMarshaler{})
	if err == nil || !strings.Contains(err.Error(), "byte order mark") ||
		!strings.Contains(err.Error(), "within that output") {
		t.Errorf("err = %v", err)
	}
}
