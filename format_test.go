package jsonc

import (
	"strings"
	"testing"
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
