package jsonc_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/go-rotini/jsonc"
)

// stdlib_compat_test verifies that jsonc.Marshal and jsonc.Unmarshal behave
// like encoding/json for standard JSON inputs (no comments, no trailing
// commas), so that the package is a true drop-in replacement.

// ---------------------------------------------------------------------------
// Decode parity
// ---------------------------------------------------------------------------

func TestStdlibUnmarshalParity_Scalars(t *testing.T) {
	cases := []string{
		`"hello"`,
		`42`,
		`3.14`,
		`true`,
		`false`,
		`null`,
	}
	for _, src := range cases {
		var stdVal, jsoncVal any
		if err := json.Unmarshal([]byte(src), &stdVal); err != nil {
			t.Fatalf("std error on %q: %v", src, err)
		}
		if err := jsonc.Unmarshal([]byte(src), &jsoncVal); err != nil {
			t.Fatalf("jsonc error on %q: %v", src, err)
		}
		if !reflect.DeepEqual(stdVal, jsoncVal) {
			t.Errorf("parity mismatch on %q: std=%v jsonc=%v", src, stdVal, jsoncVal)
		}
	}
}

func TestStdlibUnmarshalParity_Container(t *testing.T) {
	src := `{"name": "alice", "age": 30, "tags": ["admin", "user"], "meta": null}`
	var stdVal, jsoncVal any
	if err := json.Unmarshal([]byte(src), &stdVal); err != nil {
		t.Fatal(err)
	}
	if err := jsonc.Unmarshal([]byte(src), &jsoncVal); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stdVal, jsoncVal) {
		t.Errorf("parity mismatch:\n std=%v\n jsonc=%v", stdVal, jsoncVal)
	}
}

func TestStdlibUnmarshalParity_Struct(t *testing.T) {
	type Inner struct {
		ID  int    `json:"id"`
		Tag string `json:"tag,omitempty"`
	}
	type Outer struct {
		Name  string `json:"name"`
		Inner Inner  `json:"inner"`
		List  []int  `json:"list"`
	}
	src := `{"name": "x", "inner": {"id": 7}, "list": [1, 2, 3]}`
	var stdV, jsoncV Outer
	if err := json.Unmarshal([]byte(src), &stdV); err != nil {
		t.Fatal(err)
	}
	if err := jsonc.Unmarshal([]byte(src), &jsoncV); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stdV, jsoncV) {
		t.Errorf("parity mismatch:\n std=%+v\n jsonc=%+v", stdV, jsoncV)
	}
}

func TestStdlibUnmarshalParity_NumberOverflow(t *testing.T) {
	// Both stdlib and jsonc should reject overflow when target is uint8.
	src := `{"v": 999}`
	var stdS struct {
		V uint8 `json:"v"`
	}
	var jsoncS struct {
		V uint8 `json:"v"`
	}
	stdErr := json.Unmarshal([]byte(src), &stdS)
	jsoncErr := jsonc.Unmarshal([]byte(src), &jsoncS)
	if (stdErr == nil) != (jsoncErr == nil) {
		t.Errorf("error parity mismatch: std=%v jsonc=%v", stdErr, jsoncErr)
	}
}

func TestStdlibUnmarshalParity_FloatIntoInt(t *testing.T) {
	// stdlib: float into int returns error. jsonc: same.
	src := `1.5`
	var stdN, jsoncN int
	stdErr := json.Unmarshal([]byte(src), &stdN)
	jsoncErr := jsonc.Unmarshal([]byte(src), &jsoncN)
	if (stdErr == nil) != (jsoncErr == nil) {
		t.Errorf("error parity mismatch: std=%v jsonc=%v", stdErr, jsoncErr)
	}
}

// ---------------------------------------------------------------------------
// Encode parity (for the standard JSON subset)
// ---------------------------------------------------------------------------

func TestStdlibMarshalParity_Scalars(t *testing.T) {
	cases := []any{
		"hello",
		42,
		3.14,
		true,
		false,
		nil,
	}
	for _, in := range cases {
		stdOut, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		jsoncOut, err := jsonc.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		// Note: jsonc.Marshal does not HTML-escape by default while
		// json.Marshal does. Skip strings containing HTML-meaningful chars
		// (none of our cases do).
		if string(stdOut) != string(jsoncOut) {
			t.Errorf("for %v: std=%q jsonc=%q", in, stdOut, jsoncOut)
		}
	}
}

func TestStdlibMarshalParity_Slice(t *testing.T) {
	in := []int{1, 2, 3}
	stdOut, _ := json.Marshal(in)
	jsoncOut, _ := jsonc.Marshal(in)
	// stdlib: "[1,2,3]"; jsonc: "[1, 2, 3]" — we add a space after commas in
	// compact mode for readability. Verify both decode to the same value
	// rather than asserting byte equality.
	var stdV, jsoncV []int
	if err := json.Unmarshal(stdOut, &stdV); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(jsoncOut, &jsoncV); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stdV, jsoncV) {
		t.Errorf("decoded values differ: %v vs %v", stdV, jsoncV)
	}
}

func TestStdlibMarshalParity_Struct(t *testing.T) {
	type S struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	s := S{Name: "alice", Age: 30}
	stdOut, _ := json.Marshal(s)
	jsoncOut, _ := jsonc.Marshal(s)
	// Both produce the same field order (declaration order) but jsonc adds
	// space after colons and commas. Compare via re-decode.
	var stdV, jsoncV S
	_ = json.Unmarshal(stdOut, &stdV)
	_ = json.Unmarshal(jsoncOut, &jsoncV)
	if !reflect.DeepEqual(stdV, jsoncV) {
		t.Errorf("decoded values differ: %+v vs %+v", stdV, jsoncV)
	}
}

// ---------------------------------------------------------------------------
// Custom Marshaler/Unmarshaler interop
// ---------------------------------------------------------------------------

type stdMarshalImpl struct {
	Tag string
}

func (s stdMarshalImpl) MarshalJSON() ([]byte, error) {
	return []byte(`"std-marshaled:` + s.Tag + `"`), nil
}

func (s *stdMarshalImpl) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.Tag = strings.TrimPrefix(raw, "std-marshaled:")
	return nil
}

func TestStdlibMarshalerInterop(t *testing.T) {
	in := stdMarshalImpl{Tag: "x"}
	out, err := jsonc.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"std-marshaled:x"` {
		t.Errorf("got %q", out)
	}
}

func TestStdlibUnmarshalerInterop(t *testing.T) {
	var got stdMarshalImpl
	if err := jsonc.Unmarshal([]byte(`"std-marshaled:y"`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Tag != "y" {
		t.Errorf("got %q", got.Tag)
	}
}

// ---------------------------------------------------------------------------
// json.Number / json.RawMessage interop
// ---------------------------------------------------------------------------

func TestJSONNumberInterop(t *testing.T) {
	src := `{"big": 9007199254740993}` // > 2^53, exceeds float64 mantissa
	var stdS, jsoncS struct {
		Big json.Number `json:"big"`
	}
	stdDec := json.NewDecoder(strings.NewReader(src))
	stdDec.UseNumber()
	if err := stdDec.Decode(&stdS); err != nil {
		t.Fatal(err)
	}
	if err := jsonc.UnmarshalWithOptions([]byte(src), &jsoncS); err != nil {
		t.Fatal(err)
	}
	if stdS.Big.String() != jsoncS.Big.String() {
		t.Errorf("std=%q jsonc=%q", stdS.Big, jsoncS.Big)
	}
}

func TestJSONRawMessageInterop(t *testing.T) {
	src := `{"meta": {"a": 1, "b": [2, 3]}}`
	var stdS, jsoncS struct {
		Meta json.RawMessage `json:"meta"`
	}
	if err := json.Unmarshal([]byte(src), &stdS); err != nil {
		t.Fatal(err)
	}
	if err := jsonc.Unmarshal([]byte(src), &jsoncS); err != nil {
		t.Fatal(err)
	}
	// Both should preserve the same structural content (after parsing both
	// independently the values must match).
	var a, b any
	if err := json.Unmarshal(stdS.Meta, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(jsoncS.Meta, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("RawMessage parity mismatch: %v vs %v", a, b)
	}
}

// ---------------------------------------------------------------------------
// Round-trip stability
// ---------------------------------------------------------------------------

func TestRoundTripPreservesValue(t *testing.T) {
	original := map[string]any{
		"name":  "test",
		"count": float64(42),
		"items": []any{"a", "b", "c"},
		"meta":  map[string]any{"k": "v"},
	}
	encoded, err := jsonc.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := jsonc.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, decoded) {
		t.Errorf("round trip mismatch:\n in=%v\n out=%v", original, decoded)
	}
}
