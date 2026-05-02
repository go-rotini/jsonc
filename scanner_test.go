package jsonc

import (
	"errors"
	"strings"
	"testing"
)

// scanAll is a test helper that scans the input and returns tokens (excluding
// stream start/end and EOF for compactness) and any error.
func scanAll(t *testing.T, src string) ([]token, error) {
	t.Helper()
	s, err := newScanner([]byte(src))
	if err != nil {
		return nil, err
	}
	toks, err := s.scan()
	if err != nil {
		return nil, err
	}
	// Trim StreamStart, EOF, StreamEnd for easier assertions.
	out := make([]token, 0, len(toks))
	for _, tk := range toks {
		switch tk.kind {
		case tokenStreamStart, tokenStreamEnd, tokenEOF:
			continue
		default:
			out = append(out, tk)
		}
	}
	return out, nil
}

// kinds extracts just the token kinds for sequence-only assertions.
func kinds(toks []token) []tokenKind {
	out := make([]tokenKind, len(toks))
	for i, tk := range toks {
		out[i] = tk.kind
	}
	return out
}

func TestScannerEmpty(t *testing.T) {
	toks, err := scanAll(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(toks) != 0 {
		t.Errorf("empty input: got %v, want 0 tokens", toks)
	}
}

func TestScannerWhitespaceOnly(t *testing.T) {
	toks, err := scanAll(t, "   \t  ")
	if err != nil {
		t.Fatal(err)
	}
	if len(toks) != 0 {
		t.Errorf("whitespace-only: got %v", toks)
	}
}

func TestScannerNewlines(t *testing.T) {
	cases := map[string]int{
		"\n":       1,
		"\r\n":     1,
		"\r":       1,
		"\n\n":     2,
		"\r\n\r\n": 2,
		"\n\r\n\r": 3,
	}
	for src, want := range cases {
		toks, err := scanAll(t, src)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if len(toks) != want {
			t.Errorf("%q: got %d newlines, want %d", src, len(toks), want)
		}
		for _, tk := range toks {
			if tk.kind != tokenNewline {
				t.Errorf("%q: unexpected kind %v", src, tk.kind)
			}
		}
	}
}

func TestScannerStructuralChars(t *testing.T) {
	toks, err := scanAll(t, "{}[],:")
	if err != nil {
		t.Fatal(err)
	}
	want := []tokenKind{
		tokenObjectStart, tokenObjectEnd,
		tokenArrayStart, tokenArrayEnd,
		tokenValueSeparator, tokenNameSeparator,
	}
	got := kinds(toks)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] got %v, want %v", i, got[i], want[i])
		}
	}
}

func TestScannerLiterals(t *testing.T) {
	cases := []struct {
		src  string
		kind tokenKind
	}{
		{"true", tokenTrue},
		{"false", tokenFalse},
		{"null", tokenNull},
	}
	for _, tc := range cases {
		toks, err := scanAll(t, tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if len(toks) != 1 || toks[0].kind != tc.kind {
			t.Errorf("%q: got %v, want kind=%v", tc.src, toks, tc.kind)
		}
	}
}

func TestScannerLiteralBoundary(t *testing.T) {
	// Identifier-like sequences starting with true/false/null must be rejected.
	bad := []string{"trueish", "falsehood", "nullable"}
	for _, src := range bad {
		_, err := scanAll(t, src)
		if err == nil {
			t.Errorf("%q: expected error, got none", src)
		}
	}
}

func TestScannerStrings(t *testing.T) {
	cases := []string{
		`""`,
		`"hello"`,
		`"with spaces"`,
		`"escapes: \" \\ \/ \b \f \n \r \t"`,
		`"unicode: é"`,
		`"surrogate pair: 😀"`,
		`"lone surrogate: \uD800"`, // accepted at scan time
		`"quotes inside: \" works"`,
	}
	for _, src := range cases {
		toks, err := scanAll(t, src)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if len(toks) != 1 || toks[0].kind != tokenString {
			t.Errorf("%q: unexpected tokens %v", src, toks)
			continue
		}
		if toks[0].value != src {
			t.Errorf("%q: token value = %q, want %q", src, toks[0].value, src)
		}
	}
}

func TestScannerStringErrors(t *testing.T) {
	cases := []string{
		`"unterminated`,
		`"bad escape \q"`,
		`"short unicode \u00"`,
		`"non-hex \uZZZZ"`,
		"\"control\x01here\"", // control char (not tab) in string
		"\"newline\nin string\"",
	}
	for _, src := range cases {
		_, err := scanAll(t, src)
		if err == nil {
			t.Errorf("%q: expected error, got none", src)
		}
		var synErr *SyntaxError
		if err != nil && !errors.As(err, &synErr) {
			t.Errorf("%q: expected *SyntaxError, got %T", src, err)
		}
	}
}

func TestScannerNumbers(t *testing.T) {
	good := []string{
		"0", "-0", "1", "-1", "10", "100", "12345",
		"0.5", "1.5", "-1.5", "0.0", "1.0",
		"1e0", "1E0", "1e+10", "1e-10", "1.5e10",
		"0e0", "1.5E+12", "-1.5e-12",
	}
	for _, src := range good {
		toks, err := scanAll(t, src)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if len(toks) != 1 || toks[0].kind != tokenNumber || toks[0].value != src {
			t.Errorf("%q: unexpected tokens %v", src, toks)
		}
	}
}

func TestScannerNumberErrors(t *testing.T) {
	bad := []string{
		"01",    // leading zero
		"00",    // leading zero
		"-01",   // leading zero after sign
		"+1",    // leading plus
		".5",    // missing integer part
		"1.",    // missing fractional digits
		"1e",    // missing exponent digits
		"1e+",   // missing exponent digits after sign
		"1.5e",  // missing exponent digits
		"-",     // bare sign
		"1.5.6", // double decimal — second '.' is not a number char, so this scans as 1.5 then '.' as bad token
	}
	for _, src := range bad {
		_, err := scanAll(t, src)
		if err == nil {
			t.Errorf("%q: expected error, got none", src)
		}
	}
}

func TestScannerLineComments(t *testing.T) {
	src := "// this is a line comment\n"
	toks, err := scanAll(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(toks) != 2 || toks[0].kind != tokenLineComment {
		t.Fatalf("got %v", toks)
	}
	if toks[0].value != " this is a line comment" {
		t.Errorf("comment text = %q", toks[0].value)
	}
	if toks[1].kind != tokenNewline {
		t.Errorf("expected newline after line comment")
	}
}

func TestScannerLineCommentEndings(t *testing.T) {
	cases := []string{
		"// LF\n",
		"// CRLF\r\n",
		"// CR\r",
		"// at EOF",
	}
	for _, src := range cases {
		toks, err := scanAll(t, src)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if len(toks) == 0 || toks[0].kind != tokenLineComment {
			t.Errorf("%q: missing line comment token", src)
		}
	}
}

func TestScannerBlockComments(t *testing.T) {
	src := "/* hello */"
	toks, err := scanAll(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(toks) != 1 || toks[0].kind != tokenBlockComment {
		t.Fatalf("got %v", toks)
	}
	if toks[0].value != " hello " {
		t.Errorf("comment text = %q", toks[0].value)
	}
}

func TestScannerMultilineBlockComment(t *testing.T) {
	src := "/*\nline 1\nline 2\n*/"
	toks, err := scanAll(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(toks) != 1 || toks[0].kind != tokenBlockComment {
		t.Fatalf("got %v", toks)
	}
	if !strings.Contains(toks[0].value, "line 1") || !strings.Contains(toks[0].value, "line 2") {
		t.Errorf("comment lost lines: %q", toks[0].value)
	}
}

func TestScannerBlockCommentNoNesting(t *testing.T) {
	// /* outer /* inner */ outer */ — should terminate at the FIRST */, leaving
	// "outer */" as garbage that the scanner will try to tokenize and reject.
	src := "/* outer /* inner */ outer */"
	_, err := scanAll(t, src)
	if err == nil {
		t.Error("expected error from un-nested block comment leftover")
	}
}

func TestScannerUnterminatedBlockComment(t *testing.T) {
	_, err := scanAll(t, "/* never closed")
	if err == nil {
		t.Error("expected unterminated block comment error")
	}
}

func TestScannerStrictJSONRejectsComments(t *testing.T) {
	cases := []string{"// line\n", "/* block */"}
	for _, src := range cases {
		s, err := newScanner([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		s.strictJSON = true
		_, err = s.scan()
		if err == nil {
			t.Errorf("%q: expected error in strict JSON", src)
			continue
		}
		var sj *StrictJSONError
		if !errors.As(err, &sj) {
			t.Errorf("%q: expected *StrictJSONError, got %T", src, err)
		}
		if !errors.Is(err, ErrStrictJSON) {
			t.Errorf("%q: errors.Is(ErrStrictJSON) failed", src)
		}
	}
}

func TestScannerBOMHandling(t *testing.T) {
	// UTF-8 BOM stripped silently.
	src := "\xEF\xBB\xBF" + `"hello"`
	toks, err := scanAll(t, src)
	if err != nil {
		t.Fatalf("UTF-8 BOM: %v", err)
	}
	if len(toks) != 1 || toks[0].kind != tokenString {
		t.Errorf("unexpected: %v", toks)
	}

	// UTF-16 BOMs rejected with a clear message.
	rejects := map[string][]byte{
		"UTF-16 LE": {0xFF, 0xFE, '"', 0x00},
		"UTF-16 BE": {0xFE, 0xFF, 0x00, '"'},
	}
	for name, b := range rejects {
		_, err := newScanner(b)
		if err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestScannerComplexInput(t *testing.T) {
	src := `{
  // top-level
  "name": "demo",  /* inline */
  "count": 42,
  "items": [1, 2, 3,],
  "nested": {
    "key": null,
    "active": true,
  },
}`
	toks, err := scanAll(t, src)
	if err != nil {
		t.Fatal(err)
	}
	// Verify we produced the right kinds in order, ignoring whitespace/newlines
	// and concentrating on structural and value tokens.
	wantKinds := []tokenKind{
		tokenObjectStart,
		tokenLineComment,
		tokenString, tokenNameSeparator, tokenString, tokenValueSeparator, tokenBlockComment,
		tokenString, tokenNameSeparator, tokenNumber, tokenValueSeparator,
		tokenString, tokenNameSeparator, tokenArrayStart, tokenNumber, tokenValueSeparator, tokenNumber, tokenValueSeparator, tokenNumber, tokenValueSeparator, tokenArrayEnd, tokenValueSeparator,
		tokenString, tokenNameSeparator, tokenObjectStart,
		tokenString, tokenNameSeparator, tokenNull, tokenValueSeparator,
		tokenString, tokenNameSeparator, tokenTrue, tokenValueSeparator,
		tokenObjectEnd, tokenValueSeparator,
		tokenObjectEnd,
	}
	got := []tokenKind{}
	for _, tk := range toks {
		if tk.kind == tokenNewline {
			continue
		}
		got = append(got, tk.kind)
	}
	if len(got) != len(wantKinds) {
		t.Fatalf("got %d non-newline tokens, want %d\ngot: %v", len(got), len(wantKinds), got)
	}
	for i := range got {
		if got[i] != wantKinds[i] {
			t.Errorf("[%d] got %v, want %v", i, got[i], wantKinds[i])
		}
	}
}

func TestScannerPositionTracking(t *testing.T) {
	src := "{\n  \"a\": 1\n}"
	s, err := newScanner([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	toks, err := s.scan()
	if err != nil {
		t.Fatal(err)
	}
	// Find the string token "a" — should be on line 2 column 3.
	var stringTok *token
	for i := range toks {
		if toks[i].kind == tokenString {
			stringTok = &toks[i]
			break
		}
	}
	if stringTok == nil {
		t.Fatal("string token not found")
	}
	if stringTok.pos.Line != 2 || stringTok.pos.Column != 3 {
		t.Errorf(`"a" position = %d:%d, want 2:3`, stringTok.pos.Line, stringTok.pos.Column)
	}
}

func TestValidFunction(t *testing.T) {
	good := []string{
		"{}",
		"[]",
		`"hello"`,
		"null",
		"true",
		"false",
		"42",
		`{"a": 1}`,
		`{"a": 1, "b": [1, 2, 3]}`,
		`{ /* with comments */ "a": 1, }`, // JSONC by default
	}
	for _, src := range good {
		if !Valid([]byte(src)) {
			t.Errorf("Valid(%q) = false, want true", src)
		}
	}

	bad := []string{
		`{"a"}`, // missing :value (parser-level)
		`"unterminated`,
		`/* unclosed`,
	}
	// Note: the current Valid() only runs the scanner. Some of these would only
	// be rejected by the parser (Phase 3). For now, only check inputs that the
	// scanner already rejects.
	scannerRejects := []string{
		`"unterminated`,
		`/* unclosed`,
		"/* nested /* inner */ leftover */",
	}
	_ = bad
	for _, src := range scannerRejects {
		if Valid([]byte(src)) {
			t.Errorf("Valid(%q) = true, want false", src)
		}
	}
}
