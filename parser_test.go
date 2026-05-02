package jsonc

import (
	"errors"
	"strings"
	"testing"
)

// mustParse is a test helper that calls Parse and fails the test on error.
func mustParse(t *testing.T, src string, opts ...DecodeOption) *File {
	t.Helper()
	f, err := Parse([]byte(src), opts...)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return f
}

func TestParseScalars(t *testing.T) {
	cases := []struct {
		src      string
		wantKind NodeKind
		wantRaw  string
	}{
		{`"hello"`, StringNode, `"hello"`},
		{`42`, NumberNode, "42"},
		{`-1.5`, NumberNode, "-1.5"},
		{`1e10`, NumberNode, "1e10"},
		{`true`, BooleanNode, "true"},
		{`false`, BooleanNode, "false"},
		{`null`, NullNode, "null"},
	}
	for _, tc := range cases {
		f := mustParse(t, tc.src)
		if f.Root == nil {
			t.Errorf("%q: nil root", tc.src)
			continue
		}
		if f.Root.Kind != tc.wantKind {
			t.Errorf("%q: kind = %v, want %v", tc.src, f.Root.Kind, tc.wantKind)
		}
		if f.Root.RawValue != tc.wantRaw {
			t.Errorf("%q: rawValue = %q, want %q", tc.src, f.Root.RawValue, tc.wantRaw)
		}
	}
}

func TestParseStringUnescape(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{`"hello"`, "hello"},
		{`""`, ""},
		{`"escapes: \" \\ \/"`, `escapes: " \ /`},
		{`"\b\f\n\r\t"`, "\b\f\n\r\t"},
		{`"é"`, "é"},
		{`"😀"`, "😀"},      // surrogate pair → U+1F600
		{`"\uD800"`, "�"}, // lone surrogate → U+FFFD
	}
	for _, tc := range cases {
		f := mustParse(t, tc.src)
		if f.Root.Value != tc.want {
			t.Errorf("Parse(%q): Value = %q, want %q", tc.src, f.Root.Value, tc.want)
		}
	}
}

func TestParseEmptyObject(t *testing.T) {
	f := mustParse(t, `{}`)
	if f.Root.Kind != ObjectNode {
		t.Fatalf("kind = %v", f.Root.Kind)
	}
	if len(f.Root.Children) != 0 {
		t.Errorf("children = %d, want 0", len(f.Root.Children))
	}
}

func TestParseEmptyArray(t *testing.T) {
	f := mustParse(t, `[]`)
	if f.Root.Kind != ArrayNode {
		t.Fatalf("kind = %v", f.Root.Kind)
	}
	if len(f.Root.Children) != 0 {
		t.Errorf("children = %d, want 0", len(f.Root.Children))
	}
}

func TestParseObjectMembers(t *testing.T) {
	src := `{"a": 1, "b": "two", "c": true, "d": null}`
	f := mustParse(t, src)
	if f.Root.Kind != ObjectNode {
		t.Fatalf("kind = %v", f.Root.Kind)
	}
	if len(f.Root.Children) != 4 {
		t.Fatalf("members = %d, want 4", len(f.Root.Children))
	}
	wantKeys := []string{"a", "b", "c", "d"}
	for i, want := range wantKeys {
		kv := f.Root.Children[i]
		if kv.Kind != KeyValueNode {
			t.Errorf("[%d] kind = %v", i, kv.Kind)
		}
		if kv.Key != want {
			t.Errorf("[%d] key = %q, want %q", i, kv.Key, want)
		}
		if len(kv.Children) != 1 {
			t.Errorf("[%d] children = %d, want 1", i, len(kv.Children))
		}
	}
}

func TestParseNestedObject(t *testing.T) {
	src := `{"server": {"host": "localhost", "port": 8080}}`
	f := mustParse(t, src)
	server := f.Root.Children[0].Children[0]
	if server.Kind != ObjectNode {
		t.Fatalf("inner kind = %v", server.Kind)
	}
	if len(server.Children) != 2 {
		t.Errorf("inner members = %d", len(server.Children))
	}
}

func TestParseArrayOfMixed(t *testing.T) {
	src := `[1, "two", 3.0, true, null, [], {}]`
	f := mustParse(t, src)
	if f.Root.Kind != ArrayNode {
		t.Fatalf("kind = %v", f.Root.Kind)
	}
	wantKinds := []NodeKind{
		NumberNode, StringNode, NumberNode, BooleanNode, NullNode, ArrayNode, ObjectNode,
	}
	if len(f.Root.Children) != len(wantKinds) {
		t.Fatalf("got %d elements, want %d", len(f.Root.Children), len(wantKinds))
	}
	for i, want := range wantKinds {
		if f.Root.Children[i].Kind != want {
			t.Errorf("[%d] kind = %v, want %v", i, f.Root.Children[i].Kind, want)
		}
	}
}

func TestParseTrailingCommas(t *testing.T) {
	cases := []string{
		`[1,]`,
		`[1, 2, 3,]`,
		`{"a": 1,}`,
		`{"a": 1, "b": 2,}`,
		`{"nested": [1, 2,], "obj": {"x": 1,},}`,
	}
	for _, src := range cases {
		if _, err := Parse([]byte(src)); err != nil {
			t.Errorf("Parse(%q): %v (trailing commas should be allowed by default)", src, err)
		}
	}
}

func TestParseTrailingCommaRejectedInStrictJSON(t *testing.T) {
	cases := []string{
		`[1,]`,
		`{"a": 1,}`,
	}
	for _, src := range cases {
		_, err := Parse([]byte(src), WithStrictJSON())
		if err == nil {
			t.Errorf("Parse(%q, WithStrictJSON): expected error", src)
			continue
		}
	}
}

func TestParseCommentsRejectedInStrictJSON(t *testing.T) {
	cases := []string{
		"// comment\n42",
		"/* block */ 42",
		"42 // trailing",
	}
	for _, src := range cases {
		_, err := Parse([]byte(src), WithStrictJSON())
		if err == nil {
			t.Errorf("Parse(%q, WithStrictJSON): expected error", src)
			continue
		}
		if !errors.Is(err, ErrStrictJSON) {
			t.Errorf("Parse(%q, WithStrictJSON): err = %v, expected ErrStrictJSON", src, err)
		}
	}
}

func TestParseHeadComment(t *testing.T) {
	src := "// pre\n42"
	f := mustParse(t, src)
	if !strings.Contains(f.Root.HeadComment, "pre") {
		t.Errorf("HeadComment = %q, want contains 'pre'", f.Root.HeadComment)
	}
}

func TestParseInlineComment(t *testing.T) {
	src := `42 // inline`
	f := mustParse(t, src)
	if !strings.Contains(f.Root.FootComment, "inline") {
		// Trailing comment after a top-level value is captured as foot
		// (no following content).
		t.Errorf("FootComment = %q, want contains 'inline'", f.Root.FootComment)
	}
}

func TestParseMemberInlineComment(t *testing.T) {
	src := `{"a": 1 /* inline */ , "b": 2}`
	f := mustParse(t, src)
	first := f.Root.Children[0]
	if !strings.Contains(first.Comment, "inline") {
		t.Errorf("first member Comment = %q, want contains 'inline'", first.Comment)
	}
}

func TestParseFootComment(t *testing.T) {
	src := "{\n  \"a\": 1\n  // tail\n}"
	f := mustParse(t, src)
	if !strings.Contains(f.Root.FootComment, "tail") {
		t.Errorf("FootComment = %q, want contains 'tail'", f.Root.FootComment)
	}
}

func TestParseDuplicateKeyDefault(t *testing.T) {
	src := `{"a": 1, "a": 2}`
	_, err := Parse([]byte(src))
	if err == nil {
		t.Error("expected DuplicateKeyError")
		return
	}
	var dup *DuplicateKeyError
	if !errors.As(err, &dup) {
		t.Errorf("err = %T, want *DuplicateKeyError", err)
	}
	if dup.Key != "a" {
		t.Errorf("Key = %q, want a", dup.Key)
	}
}

func TestParseDuplicateKeyAllowed(t *testing.T) {
	src := `{"a": 1, "a": 2}`
	f, err := Parse([]byte(src), WithAllowDuplicateKeys())
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Root.Children) != 1 {
		t.Fatalf("members = %d, want 1 (last-wins)", len(f.Root.Children))
	}
	val := f.Root.Children[0].Children[0]
	if val.Value != "2" {
		t.Errorf("last-wins value = %q, want 2", val.Value)
	}
}

func TestParseTopLevelInvalidValues(t *testing.T) {
	cases := []string{
		``,
		`   `,
		`// only comment`,
		`/* only block */`,
	}
	for _, src := range cases {
		_, err := Parse([]byte(src))
		if err == nil {
			t.Errorf("Parse(%q): expected error", src)
		}
	}
}

func TestParseMalformedObjects(t *testing.T) {
	cases := []string{
		`{`,               // unclosed
		`{"a"}`,           // missing :
		`{"a": }`,         // missing value
		`{"a": 1`,         // unclosed
		`{"a": 1, "b"}`,   // missing :value on second
		`{"a" 1}`,         // missing :
		`{1: "x"}`,        // non-string key
		`{"a": 1 "b": 2}`, // missing comma
	}
	for _, src := range cases {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("Parse(%q): expected error", src)
		}
	}
}

func TestParseMalformedArrays(t *testing.T) {
	cases := []string{
		`[`,      // unclosed
		`[1`,     // unclosed
		`[1, 2`,  // unclosed
		`[1 2]`,  // missing comma
		`[,1]`,   // leading comma
		`[1,,2]`, // double comma
	}
	for _, src := range cases {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("Parse(%q): expected error", src)
		}
	}
}

func TestParseExtraDataAfterValue(t *testing.T) {
	cases := []string{
		`42 43`,
		`true false`,
		`{"a": 1} {"b": 2}`,
	}
	for _, src := range cases {
		_, err := Parse([]byte(src))
		if err == nil {
			t.Errorf("Parse(%q): expected error", src)
			continue
		}
		var synErr *SyntaxError
		if !errors.As(err, &synErr) {
			t.Errorf("Parse(%q): err = %T, want *SyntaxError", src, err)
		}
	}
}

func TestParseTrailingWhitespaceOK(t *testing.T) {
	cases := []string{
		`42 `,
		"42\n",
		"42\n\n   \n",
		`42 // trailing comment`,
		"42\n// trailing\n",
	}
	for _, src := range cases {
		if _, err := Parse([]byte(src)); err != nil {
			t.Errorf("Parse(%q): %v (trailing whitespace/comments should be OK)", src, err)
		}
	}
}

func TestParseMaxDepth(t *testing.T) {
	src := strings.Repeat("[", 200) + strings.Repeat("]", 200)
	_, err := Parse([]byte(src), WithMaxDepth(10))
	if err == nil {
		t.Error("expected max-depth error")
	}
}

func TestParseMaxNodes(t *testing.T) {
	src := `[1, 2, 3, 4, 5, 6, 7, 8, 9, 10]`
	_, err := Parse([]byte(src), WithMaxNodes(3))
	if err == nil {
		t.Error("expected max-nodes error")
	}
}

func TestParseMaxKeys(t *testing.T) {
	src := `{"a": 1, "b": 2, "c": 3, "d": 4}`
	_, err := Parse([]byte(src), WithMaxKeys(2))
	if err == nil {
		t.Error("expected max-keys error")
	}
}

func TestParseMaxDocumentSize(t *testing.T) {
	src := `{"a": 1, "b": 2}`
	_, err := Parse([]byte(src), WithMaxDocumentSize(5))
	if !errors.Is(err, ErrDocumentSize) {
		t.Errorf("err = %v, want ErrDocumentSize", err)
	}
}

func TestParseValidIntegration(t *testing.T) {
	good := []string{
		`{}`,
		`[]`,
		`null`,
		`true`,
		`42`,
		`{"a": [1, 2], "b": {"c": null}}`,
		`{ /* comment */ "a": 1, }`,
	}
	for _, src := range good {
		if !Valid([]byte(src)) {
			t.Errorf("Valid(%q) = false, want true", src)
		}
	}
	bad := []string{
		``,
		`{`,
		`{"a"}`,
		`42 43`,
		`{"a": 1, "a": 2}`, // duplicate key rejected by default
	}
	for _, src := range bad {
		if Valid([]byte(src)) {
			t.Errorf("Valid(%q) = true, want false", src)
		}
	}
}

// ---------------------------------------------------------------------------
// Document size limits and trailing trivia.
// ---------------------------------------------------------------------------

func TestParseDocumentSizeLimit(t *testing.T) {
	src := strings.Repeat("a", 100)
	_, err := Parse([]byte(src), WithMaxDocumentSize(50))
	if !errors.Is(err, ErrDocumentSize) {
		t.Errorf("expected ErrDocumentSize, got %v", err)
	}
}

func TestParseRespectsMaxDocumentSize(t *testing.T) {
	if _, err := Parse([]byte(`{"x": 1}`), WithMaxDocumentSize(2)); err == nil {
		t.Error("expected size error")
	}
}

func TestParseLimitsCombined(t *testing.T) {
	if _, err := Parse([]byte(`{"a": 1}`), WithMaxKeys(0)); err != nil {
		// Zero means no limit — should succeed.
		t.Errorf("MaxKeys=0 should mean unlimited: %v", err)
	}
	if _, err := Parse([]byte(`[1,2,3]`), WithMaxNodes(0)); err != nil {
		// Zero means no limit.
		t.Errorf("MaxNodes=0 should mean unlimited: %v", err)
	}
}

func TestParseTrailingWhitespaceAccepted(t *testing.T) {
	if _, err := Parse([]byte(`42   `)); err != nil {
		t.Errorf("trailing whitespace should be accepted: %v", err)
	}
	if _, err := Parse([]byte(`42 // comment`)); err != nil {
		t.Errorf("trailing comment should be accepted: %v", err)
	}
}

func TestParseEmptyDocumentAfterBOM(t *testing.T) {
	if _, err := Parse([]byte("\xef\xbb\xbf")); err == nil {
		t.Error("expected error: empty document after BOM")
	}
}

func TestParseInputWithoutBOM(t *testing.T) {
	if _, err := Parse([]byte(`42`)); err != nil {
		t.Errorf("Parse without BOM: %v", err)
	}
}

func TestParsePartialBOMBytes(t *testing.T) {
	// Just `\xef\xbb` — incomplete BOM, scanner should reject.
	if _, err := Parse([]byte{0xef, 0xbb}); err == nil {
		t.Error("expected error on partial BOM")
	}
}

func TestParseLayeredCommentBlocks(t *testing.T) {
	src := `// first
	// second
	// third
	{
	  "k": 1
	}`
	if _, err := Parse([]byte(src)); err != nil {
		t.Errorf("layered comments: %v", err)
	}
}

func TestParseStringErrorInKey(t *testing.T) {
	src := `{"\x00bad": 1}`
	if _, err := Parse([]byte(src)); err == nil {
		t.Error("expected parse error on malformed key")
	}
}

func TestParseTrailingTokenInArray(t *testing.T) {
	if _, err := Parse([]byte(`[1, 2`)); err == nil {
		t.Error("expected unterminated array")
	}
}

func TestParseStringInvalidEscapeInArrayElement(t *testing.T) {
	if _, err := Parse([]byte(`["\q"]`)); err == nil {
		t.Error("expected invalid escape error")
	}
}
