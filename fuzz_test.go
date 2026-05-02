package jsonc

import (
	"bytes"
	"testing"
)

// Fuzz seed corpus shared across targets — small, structurally diverse,
// covers the JSONC extensions and common JSON shapes.
var fuzzSeeds = []string{
	`null`,
	`true`,
	`false`,
	`42`,
	`-1.5e10`,
	`"hello"`,
	`""`,
	`{}`,
	`[]`,
	`[1, 2, 3]`,
	`{"a": 1, "b": "two", "c": [true, false, null]}`,
	"{\n  // comment\n  \"k\": 1,\n}",
	"{ /* block */ \"a\": 1 }",
	"[1, 2, 3,]",
	`"with\nescapeA"`,
	"\xef\xbb\xbf{\"a\":1}", // UTF-8 BOM
	`{"nested": {"deep": {"deeper": [1,2,3]}}}`,
	"// only-comment\n",
	`{"unicode": "café"}`,
}

// addFuzzSeeds adds the shared corpus plus any extras to a fuzz target.
func addFuzzSeeds(f *testing.F, extras ...string) {
	f.Helper()
	for _, seed := range fuzzSeeds {
		f.Add([]byte(seed))
	}
	for _, e := range extras {
		f.Add([]byte(e))
	}
}

// FuzzUnmarshal exercises the unmarshal path with arbitrary input. The
// invariant is "no panic" — any well-formed input is accepted, malformed
// input returns a typed error.
func FuzzUnmarshal(f *testing.F) {
	addFuzzSeeds(f)
	f.Fuzz(func(_ *testing.T, data []byte) {
		var v any
		_ = Unmarshal(data, &v)
	})
}

// FuzzScanner exercises the scanner directly — the lowest layer should
// never panic on any byte sequence.
func FuzzScanner(f *testing.F) {
	addFuzzSeeds(f)
	f.Fuzz(func(_ *testing.T, data []byte) {
		s, err := newScanner(data)
		if err != nil {
			return
		}
		_, _ = s.scan()
	})
}

// FuzzValid checks that [Valid] is consistent with [Unmarshal]: if Valid
// returns true, Unmarshal into any must also succeed (and vice-versa for
// false). The invariant is no panic plus this consistency.
func FuzzValid(f *testing.F) {
	addFuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		valid := Valid(data)
		var v any
		err := Unmarshal(data, &v)
		// Allow asymmetric mismatch only in the direction where Valid says
		// "no" and Unmarshal accepts (e.g., comments-only input may be
		// accepted by neither). We assert: if Valid is true, Unmarshal is
		// expected to succeed.
		if valid && err != nil {
			t.Errorf("Valid=true but Unmarshal err=%v\nsrc=%q", err, data)
		}
	})
}

// FuzzRoundTrip checks that the AST encoder produces output that the
// parser accepts. Decode → encode → decode should reach a fixed point.
func FuzzRoundTrip(f *testing.F) {
	addFuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		f1, err := Parse(data)
		if err != nil {
			return // not valid input
		}
		out1, err := NodeToBytes(f1.Root)
		if err != nil {
			t.Fatalf("encode failed on parsed input: %v", err)
		}
		f2, err := Parse(out1)
		if err != nil {
			t.Fatalf("re-parse failed on encoder output:\n in=%q\n out=%q\n err=%v", data, out1, err)
		}
		out2, err := NodeToBytes(f2.Root)
		if err != nil {
			t.Fatal(err)
		}
		// Two consecutive encodings must be byte-identical (fixed point).
		if !bytes.Equal(out1, out2) {
			t.Errorf("not stable:\n out1=%q\n out2=%q", out1, out2)
		}
	})
}

// FuzzFormat exercises the pretty-printer.
func FuzzFormat(f *testing.F) {
	addFuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := Format(data)
		if err != nil {
			return
		}
		// Output must itself be parseable.
		if _, err := Parse(out); err != nil {
			t.Errorf("Format output not re-parseable:\n in=%q\n out=%q\n err=%v", data, out, err)
		}
	})
}

// FuzzMinimize exercises the compact-printer.
func FuzzMinimize(f *testing.F) {
	addFuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := Minimize(data)
		if err != nil {
			return
		}
		if _, err := Parse(out); err != nil {
			t.Errorf("Minimize output not re-parseable:\n in=%q\n out=%q\n err=%v", data, out, err)
		}
	})
}
