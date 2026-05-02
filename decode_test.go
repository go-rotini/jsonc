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
