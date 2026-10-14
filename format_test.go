package jsonc

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestToJSONStripsComments(t *testing.T) {
	src := `{
		// line comment
		"a": 1, /* block */
		"b": [1, 2, 3,], // trailing
	}`
	out, err := ToJSON([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "//") || strings.Contains(string(out), "/*") {
		t.Errorf("output should not contain comments: %s", out)
	}
	if strings.Contains(string(out), ",}") || strings.Contains(string(out), ",]") {
		t.Errorf("output should not contain trailing commas: %s", out)
	}
}

func TestStripCommentsAlias(t *testing.T) {
	out1, err := ToJSON([]byte(`{"a":1 /* x */}`))
	if err != nil {
		t.Fatal(err)
	}
	out2, err := StripComments([]byte(`{"a":1 /* x */}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(out1) != string(out2) {
		t.Errorf("ToJSON and StripComments should produce identical output")
	}
}

func TestFormatPrettyPrints(t *testing.T) {
	src := `{"a":1,"b":[1,2,3]}`
	out, err := Format([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "\n") {
		t.Errorf("Format should produce multi-line output: %s", out)
	}
	if !strings.Contains(string(out), "  ") {
		t.Errorf("Format should use 2-space indent by default: %s", out)
	}
}

func TestFormatPreservesComments(t *testing.T) {
	src := `{
		// keep me
		"a": 1
	}`
	out, err := Format([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "keep me") {
		t.Errorf("Format should preserve comments: %s", out)
	}
}

func TestFormatCustomIndent(t *testing.T) {
	out, err := Format([]byte(`{"a":1}`), WithIndent("\t"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "\t\"a\"") {
		t.Errorf("expected tab indent: %q", out)
	}
}

func TestMinimizeRemovesWhitespace(t *testing.T) {
	src := `{
		"a": 1,
		"b": [1, 2, 3]
	}`
	out, err := Minimize([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "\n") {
		t.Errorf("Minimize should not produce newlines: %q", out)
	}
}

func TestFormatRoundTrip(t *testing.T) {
	src := `{
  "name": "test",
  "values": [1, 2, 3]
}`
	out, err := Format([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	// Format result should itself be valid JSONC.
	if _, err := Format(out); err != nil {
		t.Errorf("Format output should be re-parseable: %v", err)
	}
}

func TestFormatInvalidInput(t *testing.T) {
	_, err := Format([]byte(`{not valid`))
	if err == nil {
		t.Fatal("expected error on invalid input")
	}
}

// TestFormatCommentWithEmbeddedNewline pins a regression discovered by
// FuzzFormat: a trailing block comment whose CR was normalized to LF gets
// captured as the value's inline comment. Naively re-emitting it as
// `// <text>` would leak the text past the comment and corrupt the
// document. The encoder must fall back to /* … */ form.
func TestFormatCommentWithEmbeddedNewline(t *testing.T) {
	in := []byte("{}/*\r0*/")
	out, err := Format(in)
	if err != nil {
		t.Fatal(err)
	}
	// Output must round-trip through Parse without any "extra data" complaint.
	if _, err := Parse(out); err != nil {
		t.Errorf("Format output not re-parseable: %v\nout=%q", err, out)
	}
}

// TestFormatCommentWithStarSlashSequence pins the defense against `*/`
// appearing inside text that the encoder must wrap in /* … */. We can't
// produce this through the parser (the scanner stops at the first */), but
// it is reachable via WithComment user input — exercised in the encoder
// suite. This test just confirms Format is stable for the parser-source
// case where a block comment's value already cannot contain */ but the
// trailing-content path still produces sane output.
func TestFormatTrailingBlockCommentRoundTrip(t *testing.T) {
	in := []byte(`{"a": 1} /* trailing */`)
	out, err := Format(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(out); err != nil {
		t.Errorf("not re-parseable: %v\nout=%q", err, out)
	}
}

// TestFormatCommentPlacement pins where Format writes each kind of comment:
// output equal to the canonical input, and stable when formatted again.
func TestFormatCommentPlacement(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			name: "head comment on member",
			in:   "{\n  // head\n  \"a\": 1\n}",
			want: "{\n  // head\n  \"a\": 1\n}",
		},
		{
			name: "nested head comment",
			in:   "{\"o\": {\n// x\n\"a\": 1}}",
			want: "{\n  \"o\": {\n    // x\n    \"a\": 1\n  }\n}",
		},
		{
			name: "no space after slashes",
			in:   "{\n  //tight\n  \"a\": 1 //also\n}",
			want: "{\n  // tight\n  \"a\": 1 // also\n}",
		},
		{
			name: "comment after comma stays on its member",
			in:   "{\n  \"a\": 1, // about a\n  \"b\": 2\n}",
			want: "{\n  \"a\": 1, // about a\n  \"b\": 2\n}",
		},
		{
			name: "inline comment before comma keeps the comma",
			in:   "{\n  \"a\": 1 // about a\n  , \"b\": 2\n}",
			want: "{\n  \"a\": 1, // about a\n  \"b\": 2\n}",
		},
		{
			name: "inline comment after container value",
			in:   "{\n  \"a\": {\"x\": 1} /* c */,\n  \"b\": 2\n}",
			want: "{\n  \"a\": {\n    \"x\": 1\n  }, // c\n  \"b\": 2\n}",
		},
		{
			name: "array element comment after comma",
			in:   "[\n  1, // one\n  2\n]",
			want: "[\n  1, // one\n  2\n]",
		},
		{
			name: "member foot comment",
			in:   "{\n  \"a\": 1\n  // after a\n  ,\n  \"b\": 2\n}",
			want: "{\n  \"a\": 1,\n  // after a\n  \"b\": 2\n}",
		},
		{
			name: "element foot comment",
			in:   "[\n  1\n  // after 1\n  ,\n  2\n]",
			want: "[\n  1,\n  // after 1\n  2\n]",
		},
		{
			name: "container foot comment",
			in:   "{\n  \"a\": 1\n  // end\n}",
			want: "{\n  \"a\": 1\n  // end\n}",
		},
		{
			name: "trailing root comment",
			in:   "// top\n{\n  \"a\": 1\n}\n// trailing\n",
			want: "// top\n{\n  \"a\": 1\n}\n// trailing\n",
		},
		{
			name: "scalar root comments",
			in:   "// top\n1 // same line\n// after\n",
			want: "// top\n1 // same line\n// after\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Format([]byte(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != tc.want {
				t.Fatalf("Format:\n%s\nwant:\n%s", out, tc.want)
			}
			again, err := Format(out)
			if err != nil {
				t.Fatal(err)
			}
			if string(again) != tc.want {
				t.Errorf("Format is not stable:\n%s\nwant:\n%s", again, tc.want)
			}
		})
	}
}

// TestNodeToBytesCommentPlacement covers the same placement through the
// public AST.
func TestNodeToBytesCommentPlacement(t *testing.T) {
	in := "{\n  // head\n  \"a\": 1, // about a\n  \"b\": [\n    1 // one\n    ,\n    2\n  ]\n}"
	f, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	out, err := NodeToBytesWithOptions(f.Root, WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  // head\n  \"a\": 1, // about a\n  \"b\": [\n    1, // one\n    2\n  ]\n}"
	if string(out) != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestFormatKeySurrogate(t *testing.T) {
	src := `{"\ud800": "\ud800", "\u0041": 1}`
	got, err := Format([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"\\ud800\": \"\\ud800\",\n  \"\\u0041\": 1\n}"
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatIdempotentBlankComments(t *testing.T) {
	for _, src := range []string{"{}// ", "{}//", "{} /**/", "1 // \n", "{\n  // \n  \"a\": 1 /* */\n}", "[1, //\n 2]", "{/*\r0*/}", "/*\n \n x */ 1"} {
		once, err := Format([]byte(src))
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		twice, err := Format(once)
		if err != nil {
			t.Fatal(err)
		}
		if string(once) != string(twice) {
			t.Errorf("%q: Format = %q, then %q", src, once, twice)
		}
	}
}

func TestToJSONInvalidUTF8(t *testing.T) {
	src := []byte("{\"k\xff\": \"a\xffb\"}")
	for name, fn := range map[string]func([]byte) ([]byte, error){
		"ToJSON": ToJSON,
		"Format": func(b []byte) ([]byte, error) { return Format(b) },
	} {
		got, err := fn(src)
		if err != nil {
			t.Fatal(err)
		}
		if !utf8.Valid(got) {
			t.Errorf("%s output is not UTF-8: %q", name, got)
		}
	}
}
