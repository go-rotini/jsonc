package jsonc

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Scalars: strings, numbers, booleans, null
// ---------------------------------------------------------------------------

func TestDecodeStringIntoString(t *testing.T) {
	var s string
	if err := Unmarshal([]byte(`"hello"`), &s); err != nil {
		t.Fatal(err)
	}
	if s != "hello" {
		t.Errorf("got %q", s)
	}
}

func TestDecodeStringWithEscapes(t *testing.T) {
	var s string
	if err := Unmarshal([]byte(`"line1\nline2\ttab"`), &s); err != nil {
		t.Fatal(err)
	}
	if s != "line1\nline2\ttab" {
		t.Errorf("got %q", s)
	}
}

func TestDecodeNumbersIntoIntKinds(t *testing.T) {
	cases := []struct {
		src  string
		want any
	}{
		{`42`, int(42)},
		{`-7`, int8(-7)},
		{`1000`, int16(1000)},
		{`100000`, int32(100000)},
		{`9999999999`, int64(9999999999)},
		{`200`, uint8(200)},
		{`50000`, uint16(50000)},
		{`123456`, uint32(123456)},
	}
	for _, tc := range cases {
		ptr := reflect.New(reflect.TypeOf(tc.want))
		if err := Unmarshal([]byte(tc.src), ptr.Interface()); err != nil {
			t.Errorf("%q into %T: %v", tc.src, tc.want, err)
			continue
		}
		got := ptr.Elem().Interface()
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q into %T: got %v, want %v", tc.src, tc.want, got, tc.want)
		}
	}
}

func TestDecodeNumberIntoFloat(t *testing.T) {
	var f64 float64
	if err := Unmarshal([]byte(`3.14`), &f64); err != nil {
		t.Fatal(err)
	}
	if f64 != 3.14 {
		t.Errorf("got %v", f64)
	}

	var f32 float32
	if err := Unmarshal([]byte(`1.5`), &f32); err != nil {
		t.Fatal(err)
	}
	if f32 != 1.5 {
		t.Errorf("got %v", f32)
	}

	// Integer-shaped number into float is fine.
	if err := Unmarshal([]byte(`42`), &f64); err != nil {
		t.Fatal(err)
	}
	if f64 != 42.0 {
		t.Errorf("got %v", f64)
	}
}

func TestDecodeFloatShapedIntoIntFails(t *testing.T) {
	// 1.5 into an int target → TypeError (no implicit truncation).
	var i int
	err := Unmarshal([]byte(`1.5`), &i)
	if err == nil {
		t.Fatal("expected TypeError")
	}
	if !errors.Is(err, ErrType) {
		t.Errorf("err = %v, want ErrType", err)
	}
}

func TestDecodeIntegerOverflow(t *testing.T) {
	var i int8
	err := Unmarshal([]byte(`200`), &i)
	if err == nil {
		t.Fatal("expected OverflowError")
	}
	if !errors.Is(err, ErrOverflow) {
		t.Errorf("err = %v, want ErrOverflow", err)
	}
}

func TestDecodeBoolean(t *testing.T) {
	var b bool
	if err := Unmarshal([]byte(`true`), &b); err != nil {
		t.Fatal(err)
	}
	if !b {
		t.Error("got false")
	}
	if err := Unmarshal([]byte(`false`), &b); err != nil {
		t.Fatal(err)
	}
	if b {
		t.Error("got true")
	}
}

func TestDecodeNullIntoPointer(t *testing.T) {
	x := 42
	p := &x
	if err := Unmarshal([]byte(`null`), &p); err != nil {
		t.Fatal(err)
	}
	if p != nil {
		t.Errorf("p = %v, want nil", *p)
	}
}

func TestDecodeNullIntoNonPointer(t *testing.T) {
	// stdlib parity: null into non-pointer types leaves the field at its
	// zero value. We pre-populate to verify it gets reset.
	x := 42
	if err := Unmarshal([]byte(`null`), &x); err != nil {
		t.Fatal(err)
	}
	if x != 0 {
		t.Errorf("x = %d, want 0", x)
	}
}

func TestDecodeNullIntoInterface(t *testing.T) {
	var v any = "preexisting"
	if err := Unmarshal([]byte(`null`), &v); err != nil {
		t.Fatal(err)
	}
	if v != nil {
		t.Errorf("v = %v, want nil", v)
	}
}

// ---------------------------------------------------------------------------
// Type errors
// ---------------------------------------------------------------------------

func TestDecodeTypeMismatch(t *testing.T) {
	cases := []struct {
		src    string
		target any
	}{
		{`"hello"`, new(int)},
		{`42`, new(string)},
		{`true`, new(int)},
		{`[1, 2, 3]`, new(map[string]int)},
		{`{"a": 1}`, new([]int)},
	}
	for _, tc := range cases {
		err := Unmarshal([]byte(tc.src), tc.target)
		if err == nil {
			t.Errorf("%q into %T: expected error", tc.src, tc.target)
			continue
		}
		if !errors.Is(err, ErrType) {
			t.Errorf("%q into %T: err = %v, want ErrType", tc.src, tc.target, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Arrays
// ---------------------------------------------------------------------------

func TestDecodeArrayIntoSlice(t *testing.T) {
	var s []int
	if err := Unmarshal([]byte(`[1, 2, 3]`), &s); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, []int{1, 2, 3}) {
		t.Errorf("got %v", s)
	}
}

func TestDecodeArrayIntoFixedSizeArray(t *testing.T) {
	var arr [3]int
	if err := Unmarshal([]byte(`[1, 2, 3]`), &arr); err != nil {
		t.Fatal(err)
	}
	if arr != [3]int{1, 2, 3} {
		t.Errorf("got %v", arr)
	}
}

func TestDecodeArrayLengthMismatch(t *testing.T) {
	var arr [3]int
	err := Unmarshal([]byte(`[1, 2]`), &arr)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrType) {
		t.Errorf("err = %v, want ErrType", err)
	}
}

func TestDecodeMixedArrayIntoInterface(t *testing.T) {
	var v any
	if err := Unmarshal([]byte(`[1, "two", true, null, 3.14]`), &v); err != nil {
		t.Fatal(err)
	}
	arr, ok := v.([]any)
	if !ok || len(arr) != 5 {
		t.Fatalf("got %T %v", v, v)
	}
	if arr[0] != float64(1) {
		t.Errorf("[0] = %v", arr[0])
	}
	if arr[1] != "two" {
		t.Errorf("[1] = %v", arr[1])
	}
	if arr[2] != true {
		t.Errorf("[2] = %v", arr[2])
	}
	if arr[3] != nil {
		t.Errorf("[3] = %v", arr[3])
	}
	if arr[4] != 3.14 {
		t.Errorf("[4] = %v", arr[4])
	}
}

// ---------------------------------------------------------------------------
// Objects → struct
// ---------------------------------------------------------------------------

func TestDecodeObjectIntoStruct(t *testing.T) {
	type Cfg struct {
		Name string `json:"name"`
		Port int    `json:"port"`
		Tags []string
	}
	src := `{"name": "demo", "port": 8080, "Tags": ["a", "b"]}`
	var c Cfg
	if err := Unmarshal([]byte(src), &c); err != nil {
		t.Fatal(err)
	}
	if c.Name != "demo" || c.Port != 8080 {
		t.Errorf("got %+v", c)
	}
	if !reflect.DeepEqual(c.Tags, []string{"a", "b"}) {
		t.Errorf("Tags = %v", c.Tags)
	}
}

func TestDecodeStructCaseInsensitive(t *testing.T) {
	// stdlib parity: JSON keys match struct fields case-insensitively when
	// no exact match exists.
	type Cfg struct {
		Name string `json:"name"`
	}
	var c Cfg
	if err := Unmarshal([]byte(`{"NAME": "demo"}`), &c); err != nil {
		t.Fatal(err)
	}
	if c.Name != "demo" {
		t.Errorf("got %q", c.Name)
	}
}

func TestDecodeStrictMode(t *testing.T) {
	type Cfg struct {
		Name string `json:"name"`
	}
	src := `{"name": "demo", "extra": 42}`

	// Default: ignored.
	var c Cfg
	if err := Unmarshal([]byte(src), &c); err != nil {
		t.Fatal(err)
	}

	// Strict: error.
	err := UnmarshalWithOptions([]byte(src), &c, WithStrict())
	if err == nil {
		t.Fatal("expected UnknownFieldError")
	}
	if !errors.Is(err, ErrUnknownField) {
		t.Errorf("err = %v", err)
	}
}

func TestDecodeNestedStruct(t *testing.T) {
	type Inner struct {
		X int
	}
	type Outer struct {
		Inner Inner
	}
	var o Outer
	if err := Unmarshal([]byte(`{"Inner": {"X": 7}}`), &o); err != nil {
		t.Fatal(err)
	}
	if o.Inner.X != 7 {
		t.Errorf("got %+v", o)
	}
}

func TestDecodeEmbeddedStruct(t *testing.T) {
	type Common struct {
		Name string `json:"name"`
	}
	type Outer struct {
		Common
		Port int `json:"port"`
	}
	var o Outer
	src := `{"name": "demo", "port": 8080}`
	if err := Unmarshal([]byte(src), &o); err != nil {
		t.Fatal(err)
	}
	if o.Name != "demo" || o.Port != 8080 {
		t.Errorf("got %+v", o)
	}
}

// ---------------------------------------------------------------------------
// Objects → map
// ---------------------------------------------------------------------------

func TestDecodeObjectIntoStringMap(t *testing.T) {
	var m map[string]int
	src := `{"a": 1, "b": 2, "c": 3}`
	if err := Unmarshal([]byte(src), &m); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m, map[string]int{"a": 1, "b": 2, "c": 3}) {
		t.Errorf("got %v", m)
	}
}

func TestDecodeObjectIntoIntKeyedMap(t *testing.T) {
	var m map[int]string
	src := `{"1": "one", "2": "two"}`
	if err := Unmarshal([]byte(src), &m); err != nil {
		t.Fatal(err)
	}
	if m[1] != "one" || m[2] != "two" {
		t.Errorf("got %v", m)
	}
}

type customKey struct {
	prefix string
	num    int
}

func (k *customKey) UnmarshalText(data []byte) error {
	if len(data) < 2 || data[0] != 'k' {
		return errors.New("bad key")
	}
	k.prefix = "k"
	n := 0
	for _, c := range data[1:] {
		if c < '0' || c > '9' {
			return errors.New("bad digit")
		}
		n = n*10 + int(c-'0')
	}
	k.num = n
	return nil
}

func TestDecodeObjectIntoTextUnmarshalerKeyedMap(t *testing.T) {
	var m map[customKey]int
	src := `{"k1": 10, "k2": 20}`
	if err := Unmarshal([]byte(src), &m); err != nil {
		t.Fatal(err)
	}
	if m[customKey{prefix: "k", num: 1}] != 10 {
		t.Errorf("got %v", m)
	}
}

// ---------------------------------------------------------------------------
// Untyped (any) decoding
// ---------------------------------------------------------------------------

func TestDecodeIntoAny(t *testing.T) {
	cases := []struct {
		src  string
		want any
	}{
		{`"hello"`, "hello"},
		{`42`, float64(42)},
		{`true`, true},
		{`null`, nil},
		{`3.14`, 3.14},
	}
	for _, tc := range cases {
		var v any
		if err := Unmarshal([]byte(tc.src), &v); err != nil {
			t.Errorf("%q: %v", tc.src, err)
			continue
		}
		if !reflect.DeepEqual(v, tc.want) {
			t.Errorf("%q: got %v (%T), want %v (%T)", tc.src, v, v, tc.want, tc.want)
		}
	}
}

func TestDecodeIntoAnyObject(t *testing.T) {
	var v any
	src := `{"a": 1, "b": [2, 3], "c": {"nested": true}}`
	if err := Unmarshal([]byte(src), &v); err != nil {
		t.Fatal(err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("got %T", v)
	}
	if m["a"] != float64(1) {
		t.Errorf("a = %v", m["a"])
	}
	arr, ok := m["b"].([]any)
	if !ok || len(arr) != 2 {
		t.Errorf("b = %v", m["b"])
	}
	nested, ok := m["c"].(map[string]any)
	if !ok || nested["nested"] != true {
		t.Errorf("c = %v", m["c"])
	}
}

func TestDecodeIntoAnyOrderedMap(t *testing.T) {
	var v any
	src := `{"first": 1, "second": 2, "third": 3}`
	if err := UnmarshalWithOptions([]byte(src), &v, WithOrderedMap()); err != nil {
		t.Fatal(err)
	}
	ms, ok := v.(MapSlice)
	if !ok {
		t.Fatalf("got %T", v)
	}
	if len(ms) != 3 {
		t.Fatalf("len = %d", len(ms))
	}
	wantKeys := []string{"first", "second", "third"}
	for i, want := range wantKeys {
		if ms[i].Key != want {
			t.Errorf("[%d] key = %q, want %q", i, ms[i].Key, want)
		}
	}
}

// ---------------------------------------------------------------------------
// json.Number / WithUseNumber
// ---------------------------------------------------------------------------

func TestDecodeNumberIntoJSONNumber(t *testing.T) {
	var n json.Number
	if err := Unmarshal([]byte(`123456789012345`), &n); err != nil {
		t.Fatal(err)
	}
	if string(n) != "123456789012345" {
		t.Errorf("got %q", n)
	}
}

func TestDecodeUseNumberOption(t *testing.T) {
	var v any
	if err := UnmarshalWithOptions([]byte(`42`), &v, WithUseNumber()); err != nil {
		t.Fatal(err)
	}
	num, ok := v.(json.Number)
	if !ok {
		t.Fatalf("got %T, want json.Number", v)
	}
	if string(num) != "42" {
		t.Errorf("got %q", num)
	}
}

// ---------------------------------------------------------------------------
// big.Int / big.Float
// ---------------------------------------------------------------------------

func TestDecodeNumberIntoBigInt(t *testing.T) {
	var bi *big.Int
	src := `9999999999999999999999999`
	if err := Unmarshal([]byte(src), &bi); err != nil {
		t.Fatal(err)
	}
	if bi == nil || bi.String() != src {
		t.Errorf("got %v", bi)
	}
}

func TestDecodeNumberIntoBigFloat(t *testing.T) {
	var bf *big.Float
	src := `3.141592653589793238462643383279`
	if err := Unmarshal([]byte(src), &bf); err != nil {
		t.Fatal(err)
	}
	if bf == nil || bf.Sign() <= 0 {
		t.Errorf("got %v", bf)
	}
}

// ---------------------------------------------------------------------------
// time.Time / time.Duration
// ---------------------------------------------------------------------------

func TestDecodeStringIntoTime(t *testing.T) {
	var ts time.Time
	src := `"2024-01-15T10:30:00Z"`
	if err := Unmarshal([]byte(src), &ts); err != nil {
		t.Fatal(err)
	}
	if ts.Year() != 2024 || ts.Month() != 1 || ts.Day() != 15 {
		t.Errorf("got %v", ts)
	}
}

func TestDecodeNumberIntoDuration(t *testing.T) {
	// stdlib parity: number → int64 nanoseconds.
	var d time.Duration
	if err := Unmarshal([]byte(`1500000000`), &d); err != nil {
		t.Fatal(err)
	}
	if d != 1500*time.Millisecond {
		t.Errorf("got %v", d)
	}
}

func TestDecodeStringIntoDuration(t *testing.T) {
	// JSONC extension: human-readable string parsed via time.ParseDuration.
	var d time.Duration
	if err := Unmarshal([]byte(`"1h30m"`), &d); err != nil {
		t.Fatal(err)
	}
	if d != 90*time.Minute {
		t.Errorf("got %v", d)
	}
}

// ---------------------------------------------------------------------------
// []byte (base64)
// ---------------------------------------------------------------------------

func TestDecodeStringIntoBytes(t *testing.T) {
	var b []byte
	src := `"aGVsbG8="` // base64("hello")
	if err := Unmarshal([]byte(src), &b); err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello" {
		t.Errorf("got %q", b)
	}
}

func TestDecodeBadBase64(t *testing.T) {
	var b []byte
	err := Unmarshal([]byte(`"!!!"`), &b)
	if err == nil {
		t.Error("expected error for bad base64")
	}
}

// ---------------------------------------------------------------------------
// RawValue / json.RawMessage
// ---------------------------------------------------------------------------

func TestDecodeIntoRawValue(t *testing.T) {
	type Cfg struct {
		Plugin RawValue `json:"plugin"`
	}
	src := `{"plugin": {"version": "1.0", "enabled": true}}`
	var c Cfg
	if err := Unmarshal([]byte(src), &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Plugin) == 0 {
		t.Error("Plugin RawValue empty")
	}

	// RawValue can be re-decoded.
	var inner struct {
		Version string `json:"version"`
		Enabled bool   `json:"enabled"`
	}
	if err := c.Plugin.Unmarshal(&inner); err != nil {
		t.Fatal(err)
	}
	if inner.Version != "1.0" || !inner.Enabled {
		t.Errorf("inner = %+v", inner)
	}
}

func TestDecodeIntoJSONRawMessage(t *testing.T) {
	type Cfg struct {
		Raw json.RawMessage `json:"raw"`
	}
	src := `{"raw": {"x": 1}}`
	var c Cfg
	if err := Unmarshal([]byte(src), &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Raw) == 0 {
		t.Error("RawMessage empty")
	}
}

// ---------------------------------------------------------------------------
// Unmarshaler interfaces
// ---------------------------------------------------------------------------

type structuredUnmarshal struct {
	Doubled int
}

func (s *structuredUnmarshal) UnmarshalJSONC(unmarshal func(any) error) error {
	var n int
	if err := unmarshal(&n); err != nil {
		return err
	}
	s.Doubled = n * 2
	return nil
}

func TestDecodeUnmarshaler(t *testing.T) {
	var s structuredUnmarshal
	if err := Unmarshal([]byte(`21`), &s); err != nil {
		t.Fatal(err)
	}
	if s.Doubled != 42 {
		t.Errorf("Doubled = %d, want 42", s.Doubled)
	}
}

type bytesUnmarshal struct {
	Raw string
}

func (b *bytesUnmarshal) UnmarshalJSONC(data []byte) error {
	b.Raw = string(data)
	return nil
}

func TestDecodeBytesUnmarshaler(t *testing.T) {
	var b bytesUnmarshal
	if err := Unmarshal([]byte(`"hello"`), &b); err != nil {
		t.Fatal(err)
	}
	if b.Raw == "" {
		t.Error("Raw empty")
	}
}

type ctxUnmarshal struct {
	Got string
}

func (c *ctxUnmarshal) UnmarshalJSONC(_ context.Context, unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	c.Got = "ctx:" + s
	return nil
}

func TestDecodeUnmarshalerContext(t *testing.T) {
	var c ctxUnmarshal
	dec := NewDecoder(strings.NewReader(`"hello"`))
	if err := dec.Decode(&c); err != nil {
		t.Fatal(err)
	}
	if c.Got != "ctx:hello" {
		t.Errorf("Got = %q", c.Got)
	}
}

// ---------------------------------------------------------------------------
// json.Unmarshaler interop
// ---------------------------------------------------------------------------

type stdlibUnmarshal struct {
	Tag string
}

func (s *stdlibUnmarshal) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.Tag = "std:" + raw
	return nil
}

func TestDecodeStdlibUnmarshaler(t *testing.T) {
	var s stdlibUnmarshal
	// Existing json.Unmarshaler implementation receives standardized JSON
	// (comments stripped) so it works on JSONC input.
	src := `/* preceded */ "hello" /* trailing */`
	if err := Unmarshal([]byte(src), &s); err != nil {
		t.Fatal(err)
	}
	if s.Tag != "std:hello" {
		t.Errorf("Tag = %q", s.Tag)
	}
}

// ---------------------------------------------------------------------------
// Custom unmarshaler option
// ---------------------------------------------------------------------------

type myStruct struct {
	X int
}

func TestDecodeCustomUnmarshaler(t *testing.T) {
	src := `"x42"`
	var v myStruct
	opt := WithCustomUnmarshaler(func(out *myStruct, data []byte) error {
		// Ignore the wrapping quotes.
		s := string(data)
		s = s[1 : len(s)-1]
		if len(s) < 2 || s[0] != 'x' {
			return errors.New("bad")
		}
		n := 0
		for _, c := range s[1:] {
			n = n*10 + int(c-'0')
		}
		out.X = n
		return nil
	})
	if err := UnmarshalWithOptions([]byte(src), &v, opt); err != nil {
		t.Fatal(err)
	}
	if v.X != 42 {
		t.Errorf("X = %d", v.X)
	}
}

// ---------------------------------------------------------------------------
// Defaults / required
// ---------------------------------------------------------------------------

func TestDecodeDefaults(t *testing.T) {
	type Cfg struct {
		Name    string        `jsonc:"name,default=anon"`
		Port    int           `jsonc:"port,default=8080"`
		Active  bool          `jsonc:"active,default=true"`
		Pi      float64       `jsonc:"pi,default=3.14"`
		Timeout time.Duration `jsonc:"timeout,default=30s"`
	}
	var c Cfg
	if err := UnmarshalWithOptions([]byte(`{}`), &c, WithDefaults()); err != nil {
		t.Fatal(err)
	}
	if c.Name != "anon" || c.Port != 8080 || !c.Active || c.Pi != 3.14 || c.Timeout != 30*time.Second {
		t.Errorf("got %+v", c)
	}
}

func TestDecodeDefaultsOnlyWhenAbsent(t *testing.T) {
	type Cfg struct {
		Port int `jsonc:"port,default=8080"`
	}
	var c Cfg
	if err := UnmarshalWithOptions([]byte(`{"port": 9090}`), &c, WithDefaults()); err != nil {
		t.Fatal(err)
	}
	if c.Port != 9090 {
		t.Errorf("Port = %d (default should not have applied)", c.Port)
	}
}

func TestDecodeRequired(t *testing.T) {
	type Cfg struct {
		Name string `jsonc:"name,required"`
	}
	var c Cfg
	err := Unmarshal([]byte(`{}`), &c)
	if err == nil {
		t.Fatal("expected required error")
	}
}

func TestDecodeRequiredSatisfiedByNull(t *testing.T) {
	// Null key satisfies `required` — the key IS present.
	type Cfg struct {
		Name *string `jsonc:"name,required"`
	}
	var c Cfg
	if err := Unmarshal([]byte(`{"name": null}`), &c); err != nil {
		t.Fatal(err)
	}
	if c.Name != nil {
		t.Errorf("Name = %v, want nil", c.Name)
	}
}

// ---------------------------------------------------------------------------
// Validators
// ---------------------------------------------------------------------------

type alwaysFails struct{}

func (alwaysFails) Struct(_ any) error {
	return errors.New("validator says no")
}

func TestDecodeValidator(t *testing.T) {
	type Cfg struct {
		Port int `json:"port"`
	}
	var c Cfg
	err := UnmarshalWithOptions([]byte(`{"port": 8080}`), &c, WithValidator(alwaysFails{}))
	if err == nil {
		t.Fatal("expected ValidationError")
	}
	if !errors.Is(err, ErrValidation) {
		t.Errorf("err = %v", err)
	}
}

// ---------------------------------------------------------------------------
// Duplicate keys
// ---------------------------------------------------------------------------

func TestDecodeDuplicateKeysDefault(t *testing.T) {
	var m map[string]int
	err := Unmarshal([]byte(`{"a": 1, "a": 2}`), &m)
	if err == nil {
		t.Fatal("expected DuplicateKeyError")
	}
	if !errors.Is(err, ErrDuplicateKey) {
		t.Errorf("err = %v", err)
	}
}

func TestDecodeDuplicateKeysAllowed(t *testing.T) {
	var m map[string]int
	if err := UnmarshalWithOptions([]byte(`{"a": 1, "a": 2}`), &m, WithAllowDuplicateKeys()); err != nil {
		t.Fatal(err)
	}
	if m["a"] != 2 {
		t.Errorf("a = %d, want 2 (last-wins)", m["a"])
	}
}

// ---------------------------------------------------------------------------
// Strict-JSON mode
// ---------------------------------------------------------------------------

func TestDecodeStrictJSONRejectsComments(t *testing.T) {
	var m map[string]any
	src := `{"a": 1 /* comment */}`
	err := UnmarshalWithOptions([]byte(src), &m, WithStrictJSON())
	if err == nil {
		t.Fatal("expected StrictJSONError")
	}
	if !errors.Is(err, ErrStrictJSON) {
		t.Errorf("err = %v", err)
	}
}

// ---------------------------------------------------------------------------
// JSONC with comments — verify they don't interfere with decode
// ---------------------------------------------------------------------------

func TestDecodeWithComments(t *testing.T) {
	src := `{
  // server settings
  "host": "localhost",  /* default */
  "port": 8080,         // standard port
  // trailing
}`
	type Cfg struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	var c Cfg
	if err := Unmarshal([]byte(src), &c); err != nil {
		t.Fatal(err)
	}
	if c.Host != "localhost" || c.Port != 8080 {
		t.Errorf("got %+v", c)
	}
}

// ---------------------------------------------------------------------------
// Nil pointer / errors
// ---------------------------------------------------------------------------

func TestDecodeNilPointer(t *testing.T) {
	err := Unmarshal([]byte(`{}`), nil)
	if !errors.Is(err, ErrNilPointer) {
		t.Errorf("err = %v, want ErrNilPointer", err)
	}
}

func TestDecodeNonPointer(t *testing.T) {
	var c struct{}
	err := Unmarshal([]byte(`{}`), c)
	if !errors.Is(err, ErrNilPointer) {
		t.Errorf("err = %v, want ErrNilPointer", err)
	}
}

// ---------------------------------------------------------------------------
// nodeKindName — exercised by type errors against special-cased Go types.
// ---------------------------------------------------------------------------

func TestNodeKindNameForAllKinds(t *testing.T) {
	// Decoding non-string into time.Time triggers nodeKindName via typeErrorf.
	type S struct {
		T time.Time `json:"t"`
	}
	cases := map[string]string{
		`{"t": 42}`:      "number",
		`{"t": true}`:    "boolean",
		`{"t": null}`:    "null",
		`{"t": [1]}`:     "array",
		`{"t": {"a":1}}`: "object",
	}
	for src, want := range cases {
		var s S
		err := Unmarshal([]byte(src), &s)
		if err == nil {
			t.Errorf("%s: expected type error, got nil", src)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: expected error to mention %q, got %v", src, want, err)
		}
	}
}

func TestNodeKindNameForNumberIntoJSONNumber(t *testing.T) {
	// Decoding non-number into json.Number also hits nodeKindName.
	type S struct {
		N Number `json:"n"`
	}
	var s S
	err := Unmarshal([]byte(`{"n": "not a number"}`), &s)
	if err == nil {
		t.Fatal("expected type error")
	}
	if !strings.Contains(err.Error(), "string") {
		t.Errorf("expected error to mention 'string', got %v", err)
	}
}

// ---------------------------------------------------------------------------
// numberToBigOrError — float-shaped into big.Int rejection, and bare type.
// ---------------------------------------------------------------------------

func TestDecodeFloatShapedIntoBigInt(t *testing.T) {
	var bi *big.Int
	if err := Unmarshal([]byte(`3.14`), &bi); err == nil {
		t.Error("float-shaped number should not decode into *big.Int")
	}
}

func TestDecodeIntoBigFloatValue(t *testing.T) {
	// Decoding into a value-type big.Float (not pointer) goes through
	// numberToBigOrError's big.Float case.
	var bf big.Float
	if err := Unmarshal([]byte(`2.71828`), &bf); err != nil {
		t.Fatal(err)
	}
	if bf.Sign() <= 0 {
		t.Errorf("decoded value should be positive, got %v", &bf)
	}
}

func TestDecodeNumberIntoUnsupportedType(t *testing.T) {
	type Custom struct{ N int }
	var c Custom
	// Decoding a number into a struct should hit the default branch.
	if err := Unmarshal([]byte(`42`), &c); err == nil {
		t.Error("expected type error decoding number into struct")
	}
}

// ---------------------------------------------------------------------------
// applyDefault — every supported kind.
// ---------------------------------------------------------------------------

func TestDecodeApplyDefaultsAllKinds(t *testing.T) {
	type S struct {
		B  bool          `jsonc:"b,default=true"`
		I  int           `jsonc:"i,default=42"`
		I8 int8          `jsonc:"i8,default=7"`
		U  uint          `jsonc:"u,default=99"`
		U8 uint8         `jsonc:"u8,default=200"`
		F  float64       `jsonc:"f,default=3.14"`
		S  string        `jsonc:"s,default=hello"`
		D  time.Duration `jsonc:"d,default=1500ms"`
	}
	var v S
	if err := UnmarshalWithOptions([]byte(`{}`), &v, WithDefaults()); err != nil {
		t.Fatal(err)
	}
	switch {
	case !v.B, v.I != 42, v.I8 != 7, v.U != 99, v.U8 != 200, v.F != 3.14,
		v.S != "hello", v.D != 1500*time.Millisecond:
		t.Errorf("defaults not applied: %+v", v)
	}
}

func TestDecodeApplyDefaultInvalid(t *testing.T) {
	cases := []struct {
		name string
		src  string
		into any
	}{
		{"bad bool", `{}`, &struct {
			B bool `jsonc:"b,default=notabool"`
		}{}},
		{"bad int", `{}`, &struct {
			N int `jsonc:"n,default=xyz"`
		}{}},
		{"bad uint", `{}`, &struct {
			N uint `jsonc:"n,default=neg"`
		}{}},
		{"bad float", `{}`, &struct {
			F float64 `jsonc:"f,default=word"`
		}{}},
		{"bad duration", `{}`, &struct {
			D time.Duration `jsonc:"d,default=hello"`
		}{}},
	}
	for _, c := range cases {
		err := UnmarshalWithOptions([]byte(c.src), c.into, WithDefaults())
		if err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
		}
	}
}

func TestDecodeApplyDefaultOverflow(t *testing.T) {
	type S struct {
		N int8 `jsonc:"n,default=999"`
	}
	var v S
	err := UnmarshalWithOptions([]byte(`{}`), &v, WithDefaults())
	if err == nil {
		t.Error("expected overflow error in default")
	}
}

func TestDecodeApplyDefaultUnsupportedKind(t *testing.T) {
	type S struct {
		Items []int `jsonc:"items,default=[1,2,3]"`
	}
	var v S
	err := UnmarshalWithOptions([]byte(`{}`), &v, WithDefaults())
	if err == nil {
		t.Error("expected error: default not supported for slice kind")
	}
}

// ---------------------------------------------------------------------------
// buildMapKey — every supported key type.
// ---------------------------------------------------------------------------

func TestDecodeMapWithIntegerKeyOverflow(t *testing.T) {
	var m map[int8]string
	err := Unmarshal([]byte(`{"999": "x"}`), &m)
	if err == nil {
		t.Error("expected overflow on int8 key")
	}
}

func TestDecodeMapWithUnsignedKey(t *testing.T) {
	var m map[uint]string
	if err := Unmarshal([]byte(`{"42": "x"}`), &m); err != nil {
		t.Fatal(err)
	}
	if m[42] != "x" {
		t.Errorf("got %+v", m)
	}
}

func TestDecodeMapWithUnsignedKeyOverflow(t *testing.T) {
	var m map[uint8]string
	err := Unmarshal([]byte(`{"999": "x"}`), &m)
	if err == nil {
		t.Error("expected overflow on uint8 key")
	}
}

func TestDecodeMapWithMalformedIntKey(t *testing.T) {
	var m map[int]string
	err := Unmarshal([]byte(`{"abc": "x"}`), &m)
	if err == nil {
		t.Error("expected error on non-numeric int key")
	}
}

func TestDecodeMapWithMalformedUintKey(t *testing.T) {
	var m map[uint]string
	err := Unmarshal([]byte(`{"-5": "x"}`), &m)
	if err == nil {
		t.Error("expected error on negative uint key")
	}
}

// ---------------------------------------------------------------------------
// numberToInterface — float, integer, huge integer (overflow → float64).
// ---------------------------------------------------------------------------

func TestDecodeFloatIntoInterface(t *testing.T) {
	var v any
	if err := Unmarshal([]byte(`3.14`), &v); err != nil {
		t.Fatal(err)
	}
	if f, ok := v.(float64); !ok || f != 3.14 {
		t.Errorf("got %v (%T)", v, v)
	}
}

func TestDecodeIntegerIntoInterface(t *testing.T) {
	var v any
	if err := Unmarshal([]byte(`42`), &v); err != nil {
		t.Fatal(err)
	}
	// Stdlib produces float64 for integers in interface{} mode.
	if f, ok := v.(float64); !ok || f != 42 {
		t.Errorf("got %v (%T)", v, v)
	}
}

func TestDecodeIntoInterfaceHugeInteger(t *testing.T) {
	var v any
	src := `99999999999999999999` // overflows int64
	if err := Unmarshal([]byte(src), &v); err != nil {
		t.Fatal(err)
	}
	// Stdlib falls back to float64 on overflow.
	if _, ok := v.(float64); !ok {
		t.Errorf("expected float64 fallback, got %T", v)
	}
}

// ---------------------------------------------------------------------------
// Pointer-of-pointer decoding.
// ---------------------------------------------------------------------------

func TestDecodeIntoDoublePointer(t *testing.T) {
	var pp **int
	if err := Unmarshal([]byte(`42`), &pp); err != nil {
		t.Fatal(err)
	}
	if pp == nil || *pp == nil || **pp != 42 {
		t.Errorf("got %v", pp)
	}
}

// ---------------------------------------------------------------------------
// String escapes in unquoteString.
// ---------------------------------------------------------------------------

func TestDecodeAllStringEscapes(t *testing.T) {
	src := `"\b\f\n\r\t\\\"\/A"`
	var s string
	if err := Unmarshal([]byte(src), &s); err != nil {
		t.Fatal(err)
	}
	if s != "\b\f\n\r\t\\\"/A" {
		t.Errorf("got %q", s)
	}
}

func TestDecodeInvalidStringEscape(t *testing.T) {
	if err := Unmarshal([]byte(`"\x"`), new(string)); err == nil {
		t.Error("expected error on \\x escape")
	}
	if err := Unmarshal([]byte(`"\u00"`), new(string)); err == nil {
		t.Error("expected error on truncated \\u")
	}
}

// ---------------------------------------------------------------------------
// decodeString — non-byte slice / bare int64 / invalid duration / struct.
// ---------------------------------------------------------------------------

func TestDecodeStringIntoNonByteSlice(t *testing.T) {
	var v []int
	err := Unmarshal([]byte(`"hello"`), &v)
	if err == nil {
		t.Error("expected type error decoding string into []int")
	}
}

func TestDecodeStringIntoBareInt64(t *testing.T) {
	var v int64
	err := Unmarshal([]byte(`"hello"`), &v)
	if err == nil {
		t.Error("expected type error decoding string into bare int64")
	}
}

func TestDecodeStringInvalidDuration(t *testing.T) {
	var d time.Duration
	err := Unmarshal([]byte(`"not-a-duration"`), &d)
	if err == nil {
		t.Error("expected duration parse error")
	}
}

func TestDecodeStringIntoStruct(t *testing.T) {
	type S struct{ V int }
	var s S
	err := Unmarshal([]byte(`"text"`), &s)
	if err == nil {
		t.Error("expected type error decoding string into struct")
	}
}

// ---------------------------------------------------------------------------
// decodeNumber — into an unsupported kind (struct/map).
// ---------------------------------------------------------------------------

func TestDecodeNumberIntoMap(t *testing.T) {
	var m map[string]int
	err := Unmarshal([]byte(`42`), &m)
	if err == nil {
		t.Error("expected type error decoding number into map")
	}
}

// ---------------------------------------------------------------------------
// decodeArray / decodeObject into unsupported targets.
// ---------------------------------------------------------------------------

func TestDecodeArrayIntoInterface(t *testing.T) {
	var v any
	if err := Unmarshal([]byte(`[1, "two", true, null]`), &v); err != nil {
		t.Fatal(err)
	}
	arr, ok := v.([]any)
	if !ok || len(arr) != 4 {
		t.Errorf("got %v (%T)", v, v)
	}
}

func TestDecodeArrayIntoMap(t *testing.T) {
	var m map[string]int
	err := Unmarshal([]byte(`[1, 2, 3]`), &m)
	if err == nil {
		t.Error("expected type error decoding array into map")
	}
}

func TestDecodeObjectIntoSlice(t *testing.T) {
	var s []int
	err := Unmarshal([]byte(`{"a": 1}`), &s)
	if err == nil {
		t.Error("expected type error decoding object into []int")
	}
}

func TestDecodeObjectIntoUnsupportedKeyMap(t *testing.T) {
	type bad struct{}
	var m map[bad]int
	err := Unmarshal([]byte(`{"a": 1}`), &m)
	if err == nil {
		t.Error("expected error: unsupported map key type")
	}
}

func TestDecodeNestedArrayInIface(t *testing.T) {
	src := `[[1, 2], [3, 4]]`
	var v any
	if err := Unmarshal([]byte(src), &v); err != nil {
		t.Fatal(err)
	}
	outer := v.([]any)
	if len(outer) != 2 || len(outer[0].([]any)) != 2 {
		t.Errorf("got %v", v)
	}
}

// ---------------------------------------------------------------------------
// fieldByIndexAlloc — pointer-to-struct embedded field decoding.
// ---------------------------------------------------------------------------

func TestDecodeIntoPointerEmbeddedStructAllocates(t *testing.T) {
	type Inner struct {
		Val int `json:"val"`
	}
	type Outer struct {
		*Inner
		Tag string `json:"tag"`
	}
	src := `{"val": 7, "tag": "x"}`
	var o Outer
	if err := Unmarshal([]byte(src), &o); err != nil {
		t.Fatal(err)
	}
	if o.Inner == nil || o.Inner.Val != 7 || o.Tag != "x" {
		t.Errorf("got %+v", o)
	}
}

// ---------------------------------------------------------------------------
// TextUnmarshaler key in maps.
// ---------------------------------------------------------------------------

// textKey is a struct (not a string-kind alias) so the buildMapKey path
// reaches the TextUnmarshaler branch instead of the early string-kind
// branch (which matches stdlib semantics).
type textKey struct {
	Raw string
}

func (k *textKey) UnmarshalText(b []byte) error {
	k.Raw = "k:" + string(b)
	return nil
}

func TestDecodeMapWithTextUnmarshalerKey(t *testing.T) {
	src := `{"alpha": 1, "beta": 2}`
	var m map[textKey]int
	if err := Unmarshal([]byte(src), &m); err != nil {
		t.Fatal(err)
	}
	if m[textKey{Raw: "k:alpha"}] != 1 || m[textKey{Raw: "k:beta"}] != 2 {
		t.Errorf("got %+v", m)
	}
}

// ---------------------------------------------------------------------------
// Number overflow paths through strconv.
// ---------------------------------------------------------------------------

func TestDecodeIntOverflowParseInt(t *testing.T) {
	var n int64
	src := `99999999999999999999` // > 2^63 - 1
	err := Unmarshal([]byte(src), &n)
	if err == nil {
		t.Error("expected parse error on int64 overflow")
	}
}

func TestDecodeUintOverflowParseUint(t *testing.T) {
	var n uint64
	src := `99999999999999999999999` // > 2^64 - 1
	err := Unmarshal([]byte(src), &n)
	if err == nil {
		t.Error("expected parse error on uint64 overflow")
	}
}

func TestDecodeFloatShapedIntoUint(t *testing.T) {
	var n uint
	err := Unmarshal([]byte(`1.5`), &n)
	if err == nil {
		t.Error("expected type error on float-shaped number into uint")
	}
}

// ---------------------------------------------------------------------------
// TextUnmarshaler as a struct field VALUE (not a map key).
// ---------------------------------------------------------------------------

type textValueField struct {
	Raw string
}

func (t *textValueField) UnmarshalText(b []byte) error {
	t.Raw = "tu:" + string(b)
	return nil
}

func TestDecodeStringIntoTextUnmarshalerValue(t *testing.T) {
	type Holder struct {
		V textValueField `json:"v"`
	}
	var h Holder
	if err := Unmarshal([]byte(`{"v": "hello"}`), &h); err != nil {
		t.Fatal(err)
	}
	if h.V.Raw != "tu:hello" {
		t.Errorf("got %+v", h)
	}
}

// ---------------------------------------------------------------------------
// Typed *big.Int field (json.Unmarshaler dispatch).
// ---------------------------------------------------------------------------

type bigIntField struct {
	N *big.Int `json:"n"`
}

func TestDecodeBigIntField(t *testing.T) {
	src := `{"n": 12345678901234567890123456789012}`
	var s bigIntField
	if err := Unmarshal([]byte(src), &s); err != nil {
		t.Fatal(err)
	}
	if s.N == nil || s.N.String() != "12345678901234567890123456789012" {
		t.Errorf("got %v", s.N)
	}
}

// ---------------------------------------------------------------------------
// Decode error propagation through array element / nested object member.
// ---------------------------------------------------------------------------

type strictDecode struct {
	N int `json:"n"`
}

func TestDecodeArrayElementError(t *testing.T) {
	// Array of strict structs; an element with a non-numeric "n" produces
	// a type error that propagates through decodeArray's slice loop.
	src := `[{"n": 1}, {"n": "bad"}]`
	var arr []strictDecode
	if err := Unmarshal([]byte(src), &arr); err == nil {
		t.Error("expected type error from bad element")
	}
}

func TestDecodeObjectMemberError(t *testing.T) {
	type Inner struct {
		N int `json:"n"`
	}
	type Outer struct {
		I Inner `json:"i"`
	}
	src := `{"i": {"n": "not-a-number"}}`
	var o Outer
	if err := Unmarshal([]byte(src), &o); err == nil {
		t.Error("expected nested type error")
	}
}

// ---------------------------------------------------------------------------
// Nested type-error accumulation through a strongly-typed slice field.
// ---------------------------------------------------------------------------

type interfaceArrayHolder struct {
	V []int `json:"v"`
}

func TestDecodeNestedTypeErrorAccumulates(t *testing.T) {
	src := `{"v": [1, "two", 3]}`
	var h interfaceArrayHolder
	err := Unmarshal([]byte(src), &h)
	// Type errors are accumulated; the second element fails.
	if err == nil {
		t.Error("expected accumulated type error")
	}
}

// ---------------------------------------------------------------------------
// Pointer to byte slice.
// ---------------------------------------------------------------------------

func TestDecodeIntoPointerToByteSlice(t *testing.T) {
	var b *[]byte
	if err := Unmarshal([]byte(`"aGVsbG8="`), &b); err != nil {
		t.Fatal(err)
	}
	if b == nil || string(*b) != "hello" {
		t.Errorf("got %v", b)
	}
}
