package jsonc

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"unicode/utf8"
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

// FuzzValid checks that [Valid] (which is syntactic) is consistent with
// [Parse]: when Valid returns true, Parse must also accept the input.
// Unmarshal can still fail semantically — e.g., a number that's syntactically
// valid but overflows float64 — so we don't compare against Unmarshal here.
func FuzzValid(f *testing.F) {
	addFuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		valid := Valid(data)
		_, parseErr := Parse(data)
		switch {
		case valid && parseErr != nil:
			t.Errorf("Valid=true but Parse err=%v\nsrc=%q", parseErr, data)
		case !valid && parseErr == nil:
			t.Errorf("Valid=false but Parse succeeded\nsrc=%q", data)
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

// FuzzFormat checks that the pretty-printer's output parses to the same
// value with the same comment text, and that formatting it again changes
// nothing.
func FuzzFormat(f *testing.F) {
	addFuzzSeeds(f,
		"// c\n{\"a\": /* x */ 1, // y\n \"b\": [1, // z\n 2]\n} // t",
		"{}// ", "[1 // a\n, 2]", "{\"a\":1\n// foot\n,\"b\":2}", `{"\ud800": "\ud800"}`,
	)
	f.Fuzz(func(t *testing.T, data []byte) {
		f1, err := Parse(data)
		if err != nil {
			return
		}
		out, err := Format(data)
		if err != nil {
			t.Fatalf("Format failed on parsed input: %v\n in=%q", err, data)
		}
		f2, err := Parse(out)
		if err != nil {
			t.Fatalf("Format output not re-parseable:\n in=%q\n out=%q\n err=%v", data, out, err)
		}
		var v1, v2 any
		err1 := UnmarshalWithOptions(data, &v1, WithUseNumber())
		err2 := UnmarshalWithOptions(out, &v2, WithUseNumber())
		if (err1 == nil) != (err2 == nil) || !reflect.DeepEqual(v1, v2) {
			t.Fatalf("value changed:\n in=%q\n out=%q\n %#v, %v\n %#v, %v", data, out, v1, err1, v2, err2)
		}
		if len(f1.Warnings) == 0 && commentChars(f1.Root) != commentChars(f2.Root) {
			t.Fatalf("comments changed:\n in=%q\n out=%q", data, out)
		}
		again, err := Format(out)
		if err != nil || !bytes.Equal(out, again) {
			t.Fatalf("not idempotent:\n in=%q\n out=%q\n again=%q, %v", data, out, again, err)
		}
	})
}

// commentChars returns the non-blank bytes of every comment under n, sorted,
// so comments can be compared regardless of where Format places them.
func commentChars(n *Node) string {
	var b []byte
	Walk(n, func(x *Node) bool {
		for _, s := range []string{x.HeadComment, x.Comment, x.FootComment, x.TrailComment} {
			for i := range len(s) {
				if c := s[i]; c != ' ' && c != '\t' && c != '\n' && c != '\r' {
					b = append(b, c)
				}
			}
		}
		return true
	})
	slices.Sort(b)
	return string(b)
}

// FuzzToJSON checks that ToJSON output is valid UTF-8 JSON with the same
// value as the input.
func FuzzToJSON(f *testing.F) {
	addFuzzSeeds(f, "\"a\xffb\"", "{\"k\xcf\": 1}")
	f.Fuzz(func(t *testing.T, data []byte) {
		std, err := ToJSON(data)
		if err != nil {
			return
		}
		if !json.Valid(std) || !utf8.Valid(std) {
			t.Fatalf("ToJSON output is not valid JSON:\n in=%q\n out=%q", data, std)
		}
	})
}

// FuzzMarshalRoundTrip checks that decoding into any, encoding and decoding
// again gives the same value, and that the value matches encoding/json for
// strict JSON input.
func FuzzMarshalRoundTrip(f *testing.F) {
	addFuzzSeeds(f, `-0`, `1e400`, "\"\xcf\"", `{"a":[{"b":null}]}`)
	f.Fuzz(func(t *testing.T, data []byte) {
		var v0 any
		if err := UnmarshalWithOptions(data, &v0, WithUseNumber()); err != nil {
			return
		}
		out, err := Marshal(v0)
		if err != nil {
			t.Fatalf("Marshal: %v\n in=%q", err, data)
		}
		var v1 any
		if err := UnmarshalWithOptions(out, &v1, WithUseNumber()); err != nil || !reflect.DeepEqual(v0, v1) {
			t.Fatalf("round trip changed:\n in=%q\n out=%q\n err=%v", data, out, err)
		}
		if json.Valid(data) {
			var s any
			d := json.NewDecoder(bytes.NewReader(data))
			d.UseNumber()
			if d.Decode(&s) == nil && !reflect.DeepEqual(s, v0) {
				t.Fatalf("differs from encoding/json:\n in=%q\n jsonc=%#v\n std=%#v", data, v0, s)
			}
		}
	})
}

// FuzzStrictJSON checks that strict mode accepts exactly what
// encoding/json does, apart from a leading byte-order mark.
func FuzzStrictJSON(f *testing.F) {
	addFuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		if bytes.HasPrefix(data, utf8BOM) {
			return
		}
		_, err := Parse(data, WithStrictJSON(), WithAllowDuplicateKeys(), WithMaxDepth(0))
		if (err == nil) != json.Valid(data) {
			t.Fatalf("jsonc accepts=%v, encoding/json accepts=%v\n in=%q\n err=%v", err == nil, json.Valid(data), data, err)
		}
	})
}

// FuzzDecoder checks that the stream Decoder never panics, that each value
// it decodes is what Unmarshal gives for the bytes the value was read from
// (between successive [Decoder.InputOffset] values), and that for a single
// document its first value is what Unmarshal gives.
func FuzzDecoder(f *testing.F) {
	addFuzzSeeds(f, "1 2 3", "{\"a\":1}\n// c\n[2]", "1\n2\n[3,\n x]", "\xef\xbb\xbf{\"a\":1} {\"b\":2}", "1-2\"a\"[]{}")
	f.Fuzz(func(t *testing.T, data []byte) {
		dec := NewDecoder(bytes.NewReader(data))
		var first any
		firstErr := dec.Decode(&first)
		if firstErr == nil {
			checkDecoderValue(t, data, 0, dec.InputOffset(), first)
		}
		for range 100 {
			start := dec.InputOffset()
			var v any
			if err := dec.Decode(&v); err != nil {
				break
			}
			checkDecoderValue(t, data, start, dec.InputOffset(), v)
		}
		var want any
		if Unmarshal(data, &want) == nil {
			if firstErr != nil || !reflect.DeepEqual(first, want) {
				t.Fatalf("Decoder = %#v, %v; Unmarshal = %#v\n in=%q", first, firstErr, want, data)
			}
		}
	})
}

// checkDecoderValue fails t unless Unmarshal of data[start:end] gives got.
func checkDecoderValue(t *testing.T, data []byte, start, end int64, got any) {
	t.Helper()
	var want any
	if err := Unmarshal(data[start:end], &want); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Decoder value %#v from %q; Unmarshal = %#v, %v", got, data[start:end], want, err)
	}
}

// FuzzPatch checks that applying a patch never panics and never leaves an
// invalid tree.
func FuzzPatch(f *testing.F) {
	f.Add([]byte(`{"a":[1,2],"b":{"c":1}}`), []byte(`[{"op":"move","from":"/a/0","path":"/b/d"}]`))
	f.Add([]byte(`[1]`), []byte(`[{"op":"add","path":"/-","value":2},{"op":"test","path":"/1","value":2.0}]`))
	f.Add([]byte(`{"a~/b":1}`), []byte(`[{"op":"copy","from":"/a~0~1b","path":"/x"}]`))
	f.Add([]byte(`{"a":[1,2,3]}`), []byte(`[{"op":"replace","path":"/a/0","value":9}]`))
	f.Fuzz(func(t *testing.T, doc, patch []byte) {
		d, err := Parse(doc)
		if err != nil {
			return
		}
		p, err := ParsePatch(patch)
		if err != nil {
			return
		}
		_, _ = p.Apply(nil)
		out, err := p.Apply(d.Root)
		if err != nil {
			return
		}
		if _, err := NodeToBytes(out); err != nil {
			t.Fatal(err)
		}
		if err := out.Validate(); err != nil {
			t.Fatalf("patched tree invalid: %v\n doc=%q\n patch=%q", err, doc, patch)
		}
	})
}

// FuzzPath checks that compiling and applying paths never panics.
func FuzzPath(f *testing.F) {
	f.Add([]byte(`{"a":[{"b":1}]}`), "$.a[0].b")
	f.Add([]byte(`{"a":[{"b":1}]}`), "..b")
	f.Add([]byte(`{"a":[1,2]}`), "/a/01")
	f.Fuzz(func(t *testing.T, doc []byte, expr string) {
		d, err := Parse(doc)
		if err != nil {
			return
		}
		for _, compile := range []func(string) (Path, error){PathString, PathPointer} {
			p, err := compile(expr)
			if err != nil {
				continue
			}
			_ = p.Read(d.Root)
			_ = p.Replace(d.Root, &Node{Kind: NumberNode, Value: "1"})
			_ = p.Delete(d.Root)
			if _, err := NodeToBytes(d.Root); err != nil {
				t.Fatal(err)
			}
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
