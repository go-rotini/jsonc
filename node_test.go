package jsonc

import (
	"bytes"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestNodeStringScalar(t *testing.T) {
	cases := []struct {
		n    *Node
		want string
	}{
		{&Node{Kind: StringNode, RawValue: `"hello"`, Value: "hello"}, `"hello"`},
		{&Node{Kind: NumberNode, RawValue: "42", Value: "42"}, "42"},
		{&Node{Kind: BooleanNode, RawValue: "true", Value: "true"}, "true"},
		{&Node{Kind: NullNode, RawValue: "null", Value: "null"}, "null"},
	}
	for _, tc := range cases {
		got := tc.n.String()
		if got != tc.want {
			t.Errorf("String() = %q, want %q", got, tc.want)
		}
	}
}

func TestNodeStringContainer(t *testing.T) {
	obj := &Node{Kind: ObjectNode, Children: []*Node{
		{Kind: KeyValueNode, Key: "a"},
		{Kind: KeyValueNode, Key: "b"},
	}}
	if !strings.Contains(obj.String(), "object: 2") {
		t.Errorf("Object String = %q", obj.String())
	}

	arr := &Node{Kind: ArrayNode, Children: []*Node{
		{Kind: NumberNode, Value: "1"},
		{Kind: NumberNode, Value: "2"},
		{Kind: NumberNode, Value: "3"},
	}}
	if !strings.Contains(arr.String(), "array: 3") {
		t.Errorf("Array String = %q", arr.String())
	}

	kv := &Node{Kind: KeyValueNode, Key: "name"}
	if kv.String() != "name" {
		t.Errorf("KeyValueNode String = %q", kv.String())
	}

	cn := &Node{Kind: CommentNode, CommentStyle: LineCommentStyle, Value: " hi"}
	if !strings.Contains(cn.String(), "//") {
		t.Errorf("CommentNode String = %q", cn.String())
	}

	bn := &Node{Kind: CommentNode, CommentStyle: BlockCommentStyle, Value: " block "}
	if !strings.Contains(bn.String(), "/*") {
		t.Errorf("BlockCommentNode String = %q", bn.String())
	}
}

func TestNodeValidateOK(t *testing.T) {
	src := `{"a": 1, "b": [2, 3], "c": {"nested": true}}`
	f := mustParse(t, src)
	if err := f.Root.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestNodeValidateDuplicate(t *testing.T) {
	// Construct an object node with duplicate keys directly (Parse would
	// reject it).
	obj := &Node{Kind: ObjectNode, Children: []*Node{
		{Kind: KeyValueNode, Key: "x", Children: []*Node{{Kind: NumberNode, Value: "1"}}},
		{Kind: KeyValueNode, Key: "x", Children: []*Node{{Kind: NumberNode, Value: "2"}}},
	}}
	err := obj.Validate()
	if err == nil {
		t.Fatal("expected DuplicateKeyError")
	}
	var dup *DuplicateKeyError
	if !errors.As(err, &dup) {
		t.Errorf("err = %T, want *DuplicateKeyError", err)
	}
}

func TestNodeValidateKeyValueChildCount(t *testing.T) {
	// KeyValueNode with 0 children is invalid.
	bad := &Node{Kind: KeyValueNode, Key: "x"} // no children
	err := bad.Validate()
	if err == nil {
		t.Error("expected error for 0-child KeyValueNode")
	}

	// KeyValueNode with 2 children is invalid.
	bad2 := &Node{Kind: KeyValueNode, Key: "x", Children: []*Node{
		{Kind: StringNode, Value: "a"},
		{Kind: StringNode, Value: "b"},
	}}
	if err := bad2.Validate(); err == nil {
		t.Error("expected error for 2-child KeyValueNode")
	}
}

func TestWalk(t *testing.T) {
	src := `{"a": [1, 2], "b": "x"}`
	f := mustParse(t, src)
	count := 0
	Walk(f.Root, func(*Node) bool {
		count++
		return true
	})
	if count == 0 {
		t.Error("Walk visited 0 nodes")
	}
}

func TestWalkEarlyTermination(t *testing.T) {
	src := `{"a": 1, "b": 2}`
	f := mustParse(t, src)
	visited := 0
	Walk(f.Root, func(*Node) bool {
		visited++
		return false // do not recurse
	})
	if visited != 1 {
		t.Errorf("Walk visited %d, want 1 (no recursion)", visited)
	}
}

func TestWalkNil(t *testing.T) {
	called := false
	Walk(nil, func(*Node) bool {
		called = true
		return true
	})
	if called {
		t.Error("Walk(nil) should not call fn")
	}
}

func TestFilter(t *testing.T) {
	src := `{"a": 1, "b": [2, 3, "four"], "c": {"d": 5}}`
	f := mustParse(t, src)
	numbers := Filter(f.Root, func(n *Node) bool { return n.Kind == NumberNode })
	if len(numbers) != 4 {
		t.Errorf("got %d numbers, want 4 (1, 2, 3, 5)", len(numbers))
	}

	strings := Filter(f.Root, func(n *Node) bool { return n.Kind == StringNode })
	if len(strings) != 1 {
		t.Errorf("got %d strings, want 1 (\"four\")", len(strings))
	}
}

func TestFilterEmpty(t *testing.T) {
	src := `42`
	f := mustParse(t, src)
	matches := Filter(f.Root, func(n *Node) bool { return n.Kind == ObjectNode })
	if len(matches) != 0 {
		t.Errorf("got %d matches, want 0", len(matches))
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	// exportNode ↔ importNode preserve all fields.
	internal := &node{
		kind: nodeObject,
		key:  "",
		children: []*node{
			{
				kind: nodeKeyValue, key: "a",
				headComment: "head",
				comment:     "inline",
				children:    []*node{{kind: nodeNumber, value: "1", rawValue: "1"}},
			},
		},
		footComment: "foot",
		pos:         Position{Line: 1, Column: 1},
	}
	exported := exportNode(internal)
	if exported.Kind != ObjectNode {
		t.Errorf("Kind = %v", exported.Kind)
	}
	if exported.FootComment != "foot" {
		t.Errorf("FootComment = %q", exported.FootComment)
	}
	if exported.Children[0].Kind != KeyValueNode {
		t.Errorf("inner kind = %v", exported.Children[0].Kind)
	}
	if exported.Children[0].HeadComment != "head" {
		t.Errorf("HeadComment = %q", exported.Children[0].HeadComment)
	}

	// Round-trip back to internal.
	back := importNode(exported)
	if back.kind != nodeObject {
		t.Errorf("kind = %v", back.kind)
	}
	if back.children[0].headComment != "head" {
		t.Errorf("headComment = %q", back.children[0].headComment)
	}
	if back.children[0].children[0].rawValue != "1" {
		t.Errorf("inner rawValue = %q", back.children[0].children[0].rawValue)
	}
}

func TestExportImportNil(t *testing.T) {
	if exportNode(nil) != nil {
		t.Error("exportNode(nil) should return nil")
	}
	if importNode(nil) != nil {
		t.Error("importNode(nil) should return nil")
	}
}

func TestNodeToBytesScalar(t *testing.T) {
	cases := []string{
		`"hello"`,
		`42`,
		`-1.5`,
		`true`,
		`false`,
		`null`,
	}
	for _, src := range cases {
		f := mustParse(t, src)
		out, err := NodeToBytes(f.Root)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if string(out) != src {
			t.Errorf("%q: NodeToBytes = %q, want %q", src, out, src)
		}
	}
}

func TestNodeToBytesEmptyContainers(t *testing.T) {
	cases := []string{`{}`, `[]`}
	for _, src := range cases {
		f := mustParse(t, src)
		out, err := NodeToBytes(f.Root)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if string(out) != src {
			t.Errorf("%q: NodeToBytes = %q, want %q", src, out, src)
		}
	}
}

func TestNodeToBytesCompactObject(t *testing.T) {
	src := `{"a": 1, "b": "two", "c": true, "d": null}`
	f := mustParse(t, src)
	out, err := NodeToBytes(f.Root)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != src {
		t.Errorf("got %q, want %q", out, src)
	}
}

func TestNodeToBytesCompactArray(t *testing.T) {
	src := `[1, 2, "three", true, null, [4, 5]]`
	f := mustParse(t, src)
	out, err := NodeToBytes(f.Root)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != src {
		t.Errorf("got %q, want %q", out, src)
	}
}

func TestNodeToBytesIndent(t *testing.T) {
	src := `{"a": 1, "b": [2, 3]}`
	f := mustParse(t, src)
	out, err := NodeToBytesWithOptions(f.Root, WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "{\n") {
		t.Errorf("indented output missing leading newline: %q", got)
	}
	if !strings.Contains(got, "  \"a\":") {
		t.Errorf("indented output missing 2-space indent: %q", got)
	}
}

func TestNodeToBytesWithComments(t *testing.T) {
	src := `// head
{
  // member head
  "a": 1
  // foot
}`
	f := mustParse(t, src)
	out, err := NodeToBytesWithOptions(f.Root, WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{"head", "member head", "foot"} {
		if !strings.Contains(got, want) {
			t.Errorf("comment %q lost in round-trip:\n%s", want, got)
		}
	}
}

func TestNodeToBytesStrictJSONOutputDropsComments(t *testing.T) {
	src := `// head
{
  "a": 1 // inline
}`
	f := mustParse(t, src)
	out, err := NodeToBytesWithOptions(f.Root, WithIndent("  "), WithStrictJSONOutput(true))
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if strings.Contains(got, "//") {
		t.Errorf("strict JSON output retained //: %q", got)
	}
	if strings.Contains(got, "/*") {
		t.Errorf("strict JSON output retained /*: %q", got)
	}
}

func TestNodeToBytesNil(t *testing.T) {
	out, err := NodeToBytes(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("NodeToBytes(nil) = %q, want empty", out)
	}
}

func TestNodeToBytesTrailingComma(t *testing.T) {
	src := `{"a": 1, "b": 2}`
	f := mustParse(t, src)
	out, err := NodeToBytesWithOptions(f.Root, WithIndent("  "), WithTrailingComma(true))
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	// Should have a comma before the closing brace
	if !strings.Contains(got, "2,\n}") {
		t.Errorf("trailing comma not emitted:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// writeOrphanComment — orphan CommentNode children of containers.
// ---------------------------------------------------------------------------

func TestWriteOrphanCommentLineStyle(t *testing.T) {
	root := &Node{
		Kind: ArrayNode,
		Children: []*Node{
			{Kind: CommentNode, Value: " orphan line", CommentStyle: LineCommentStyle},
			{Kind: NumberNode, Value: "1", RawValue: "1"},
		},
	}
	out, err := NodeToBytesWithOptions(root, WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "// orphan line") {
		t.Errorf("expected orphan line comment, got %s", out)
	}
}

func TestWriteOrphanCommentBlockStyle(t *testing.T) {
	root := &Node{
		Kind: ObjectNode,
		Children: []*Node{
			{Kind: CommentNode, Value: " orphan block ", CommentStyle: BlockCommentStyle},
			{
				Kind: KeyValueNode,
				Key:  "a",
				Children: []*Node{
					{Kind: NumberNode, Value: "1", RawValue: "1"},
				},
			},
		},
	}
	out, err := NodeToBytesWithOptions(root, WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "/* orphan block */") {
		t.Errorf("expected orphan block comment, got %s", out)
	}
}

func TestEncodeOrphanCommentInsideArray(t *testing.T) {
	root := &Node{
		Kind: ArrayNode,
		Children: []*Node{
			{Kind: NumberNode, Value: "1", RawValue: "1"},
			{Kind: CommentNode, Value: " between ", CommentStyle: BlockCommentStyle},
			{Kind: NumberNode, Value: "2", RawValue: "2"},
		},
	}
	out, err := NodeToBytesWithOptions(root, WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "/* between */") {
		t.Errorf("expected orphan block comment in array, got %s", out)
	}
}

func TestEncodeASTCompactSkipsOrphanComments(t *testing.T) {
	root := &Node{
		Kind: ArrayNode,
		Children: []*Node{
			{Kind: CommentNode, Value: " skip ", CommentStyle: BlockCommentStyle},
			{Kind: NumberNode, Value: "1", RawValue: "1"},
		},
	}
	out, err := NodeToBytes(root) // compact (no indent)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "skip") {
		t.Errorf("compact mode should skip orphan comments, got %s", out)
	}
}

// ---------------------------------------------------------------------------
// Node.String / NodeKind.String over every kind.
// ---------------------------------------------------------------------------

func TestNodeStringEveryKind(t *testing.T) {
	cases := []*Node{
		{Kind: ObjectNode, Children: make([]*Node, 3)},
		{Kind: ArrayNode, Children: make([]*Node, 5)},
		{Kind: KeyValueNode, Key: "k"},
		{Kind: StringNode, Value: "v", RawValue: `"v"`},
		{Kind: NumberNode, Value: "42", RawValue: "42"},
		{Kind: BooleanNode, Value: "true", RawValue: "true"},
		{Kind: NullNode, Value: "null", RawValue: "null"},
		{Kind: CommentNode, Value: " line ", CommentStyle: LineCommentStyle},
		{Kind: CommentNode, Value: " block ", CommentStyle: BlockCommentStyle},
	}
	for _, n := range cases {
		s := n.String()
		if s == "" && n.Kind != 0 {
			t.Errorf("empty String() for kind %v", n.Kind)
		}
	}
}

func TestNodeKindStringDirect(t *testing.T) {
	cases := []struct {
		kind NodeKind
		want string
	}{
		{ObjectNode, "object"},
		{ArrayNode, "array"},
		{KeyValueNode, "key-value"},
		{StringNode, "string"},
		{NumberNode, "number"},
		{BooleanNode, "boolean"},
		{NullNode, "null"},
		{CommentNode, "comment"},
	}
	for _, c := range cases {
		if got := c.kind.String(); got != c.want {
			t.Errorf("kind %d: got %q, want %q", int(c.kind), got, c.want)
		}
	}
	// Default branch.
	if got := NodeKind(99).String(); !strings.Contains(got, "99") {
		t.Errorf("default branch: got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Walk visits and early stops.
// ---------------------------------------------------------------------------

func TestWalkVisitsCommentNode(t *testing.T) {
	root := &Node{
		Kind: ArrayNode,
		Children: []*Node{
			{Kind: CommentNode, Value: " line ", CommentStyle: LineCommentStyle},
			{Kind: NumberNode, Value: "1", RawValue: "1"},
		},
	}
	count := 0
	Walk(root, func(_ *Node) bool {
		count++
		return true
	})
	if count != 3 {
		t.Errorf("expected 3 visits (root + 2 children), got %d", count)
	}
}

func TestWalkEarlyStop(t *testing.T) {
	root := mustParseRoot(t, `{"a": {"b": {"c": 1}}}`)
	count := 0
	Walk(root, func(_ *Node) bool {
		count++
		return false // do not recurse
	})
	if count != 1 {
		t.Errorf("expected 1 visit (root only), got %d", count)
	}
}

func TestValidateOnValidTree(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1, "b": [1, 2, 3]}`)
	if err := root.Validate(); err != nil {
		t.Errorf("Validate on parsed input: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Hand-built AST emission corner cases.
// ---------------------------------------------------------------------------

func TestNodeBridgeImportExportRoundtrip(t *testing.T) {
	// Build a public Node tree and round-trip through NodeToBytes → Parse.
	pub := &Node{
		Kind: ObjectNode,
		Children: []*Node{
			{
				Kind: KeyValueNode,
				Key:  "k",
				Children: []*Node{
					{Kind: StringNode, Value: "v", RawValue: `"v"`},
				},
				Comment:     "inline",
				HeadComment: "head",
				FootComment: "foot",
			},
		},
	}
	out, err := NodeToBytesWithOptions(pub, WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(out)
	if err != nil {
		t.Fatalf("re-parse failed: %v\noutput:\n%s", err, out)
	}
	if f.Root.Kind != ObjectNode {
		t.Errorf("expected ObjectNode, got %v", f.Root.Kind)
	}
}

// nodeRawBytes — exercise container fallback path via NodeToBytes round-trip.
func TestRawValueFromASTNoSourceBytes(t *testing.T) {
	root := &Node{
		Kind: ObjectNode,
		Children: []*Node{
			{
				Kind:     KeyValueNode,
				Key:      "k",
				Children: []*Node{{Kind: NumberNode, Value: "1", RawValue: "1"}},
			},
		},
	}
	out, err := NodeToBytes(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(out); err != nil {
		t.Errorf("re-parse failed: %v", err)
	}
}

// nodeRawBytes for a manually-built scalar with empty rawValue.
func TestRawValueFromArtisanal(t *testing.T) {
	pub := &Node{Kind: StringNode, Value: "hi"} // RawValue intentionally empty
	out, err := NodeToBytes(pub)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"hi"` {
		t.Errorf("got %q", out)
	}
}

// writeNode KeyValueNode with no children — emits "null".
func TestEncodeASTKeyValueNodeWithoutChild(t *testing.T) {
	root := &Node{
		Kind: ObjectNode,
		Children: []*Node{
			{Kind: KeyValueNode, Key: "missing"}, // no children
		},
	}
	out, err := NodeToBytes(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"missing": null`) {
		t.Errorf("expected null fallback for child-less KV, got %s", out)
	}
}

func TestParseWarnings(t *testing.T) {
	f, err := Parse([]byte(`{"a": 1, "a" /* k */ : // v
2}`), WithAllowDuplicateKeys())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"line 1, column 14: comment between a key and its value is dropped",
		"line 1, column 24: comment between a key and its value is dropped",
		`line 1, column 10: duplicate key "a"; the last value wins`,
	}
	if !slices.Equal(f.Warnings, want) {
		t.Errorf("Warnings = %q, want %q", f.Warnings, want)
	}

	clean, err := Parse([]byte(`{"a": 1} // ok`))
	if err != nil {
		t.Fatal(err)
	}
	if clean.Warnings != nil {
		t.Errorf("Warnings = %q, want nil", clean.Warnings)
	}
}

// treeShape returns a copy of n without positions, for comparing trees
// parsed from different text. With noComments, comments are cleared too.
func treeShape(n *Node, noComments bool) *Node {
	out := cloneNode(n)
	Walk(out, func(c *Node) bool {
		c.Pos = Position{}
		if noComments {
			c.Comment, c.HeadComment, c.FootComment, c.TrailComment = "", "", "", ""
		}
		return true
	})
	return out
}

func TestRootTrailCommentRoundTrip(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		foot      string // root FootComment
		trail     string // root TrailComment
		lastClose string // the root's last token in the output
	}{
		{
			name:      "object",
			src:       "// before\n{\n  // head a\n  \"a\": 1, // line a\n  \"b\": [\n    2\n    // foot b\n  ]\n  // foot obj\n}\n// after 1\n// after 2\n",
			foot:      " foot obj",
			trail:     " after 1\n after 2",
			lastClose: "}",
		},
		{
			name:      "array",
			src:       "// before\n[\n  // head 1\n  1,\n  {\"x\": true} // line x\n  // foot arr\n] // line arr\n// after\n",
			foot:      " foot arr",
			trail:     " after",
			lastClose: "]",
		},
		{
			name:      "empty object",
			src:       "{\n  // only\n}\n// after\n",
			foot:      " only",
			trail:     " after",
			lastClose: "}",
		},
		{
			name:      "scalar",
			src:       "// before\n42 // line\n// after\n",
			trail:     " after",
			lastClose: "42",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := mustParse(t, tc.src)
			if f.Root.FootComment != tc.foot {
				t.Errorf("FootComment = %q, want %q", f.Root.FootComment, tc.foot)
			}
			if f.Root.TrailComment != tc.trail {
				t.Errorf("TrailComment = %q, want %q", f.Root.TrailComment, tc.trail)
			}

			out, err := NodeToBytesWithOptions(f.Root, WithIndent("  "))
			if err != nil {
				t.Fatal(err)
			}
			if tc.trail != "" && strings.LastIndex(string(out), "// after") < strings.LastIndex(string(out), tc.lastClose) {
				t.Errorf("trail comment written before the root's end:\n%s", out)
			}
			again := mustParse(t, string(out))
			if got, want := treeShape(again.Root, false), treeShape(f.Root, false); !reflect.DeepEqual(got, want) {
				t.Errorf("tree changed through NodeToBytesWithOptions:\n%s", out)
			}

			compact, err := NodeToBytes(f.Root)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := treeShape(mustParse(t, string(compact)).Root, true), treeShape(f.Root, true); !reflect.DeepEqual(got, want) {
				t.Errorf("tree changed through NodeToBytes: %s", compact)
			}

			formatted, err := Format([]byte(tc.src))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(formatted, out) {
				t.Errorf("Format and NodeToBytesWithOptions differ:\n%s\n---\n%s", formatted, out)
			}
			twice, err := Format(formatted)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(twice, formatted) {
				t.Errorf("Format not stable:\n%s\n---\n%s", formatted, twice)
			}
		})
	}
}

func TestBuiltTrailComment(t *testing.T) {
	root := &Node{Kind: ObjectNode, TrailComment: "after root", Children: []*Node{
		{Kind: KeyValueNode, Key: "a", Children: []*Node{
			{Kind: ArrayNode, FootComment: "in a", TrailComment: "after a"},
		}},
		{Kind: KeyValueNode, Key: "b", TrailComment: "after b", Children: []*Node{
			{Kind: NumberNode, Value: "1"},
		}},
	}}
	out, err := NodeToBytesWithOptions(root, WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": [\n    // in a\n  ],\n  // after a\n  \"b\": 1\n  // after b\n}\n// after root\n"
	if string(out) != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestNodeToBytesKeepsKeys(t *testing.T) {
	f, err := Parse([]byte(`{"café": 1, "\ud800": 2, "a\/b": "x\/y"}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := NodeToBytes(f.Root)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"café": 1, "\ud800": 2, "a\/b": "x\/y"}`; string(out) != want {
		t.Errorf("got %s, want %s", out, want)
	}

	f.Root.Children[2].Key = "c/d"
	out, err = NodeToBytes(cloneNode(f.Root))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"café": 1, "\ud800": 2, "c/d": "x\/y"}`; string(out) != want {
		t.Errorf("renamed: got %s, want %s", out, want)
	}
}

func TestNodeRawKey(t *testing.T) {
	f, err := Parse([]byte(`{"a": 1}`))
	if err != nil {
		t.Fatal(err)
	}
	member := f.Root.Children[0]
	if member.Key != "a" || member.RawKey != `"a"` {
		t.Fatalf("Key, RawKey = %q, %q", member.Key, member.RawKey)
	}
	if out, _ := NodeToBytes(f.Root); string(out) != `{"a": 1}` {
		t.Errorf("unchanged key: %s", out)
	}
	member.Key = "b"
	if out, _ := NodeToBytes(f.Root); string(out) != `{"b": 1}` {
		t.Errorf("renamed key: %s", out)
	}

	// A RawKey that is not one plain string token is dropped, even when it
	// unquotes to Key.
	for _, c := range []struct{ key, raw, want string }{
		{`a": 1, "evil": 2, "b`, `"a": 1, "evil": 2, "b"`, `{"a\": 1, \"evil\": 2, \"b": 1}`},
		{"a\nb", "\"a\nb\"", `{"a\nb": 1}`},
		{"x", `"\u0078"`, `{"\u0078": 1}`},
	} {
		member.Key, member.RawKey = c.key, c.raw
		if out, err := NodeToBytes(f.Root); err != nil || string(out) != c.want {
			t.Errorf("RawKey %q: %s, %v; want %s", c.raw, out, err, c.want)
		}
	}

	// Every field is exported, so a parsed member equals a hand-built one
	// once its source fields are cleared.
	g, err := Parse([]byte(`{"a": 1}`))
	if err != nil {
		t.Fatal(err)
	}
	m := g.Root.Children[0]
	m.Pos, m.RawKey, m.Children[0].Pos, m.Children[0].RawValue = Position{}, "", Position{}, ""
	want := &Node{Kind: KeyValueNode, Key: "a", Children: []*Node{{Kind: NumberNode, Value: "1"}}}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("member = %#v, want %#v", m, want)
	}
	nt := reflect.TypeFor[Node]()
	for i := range nt.NumField() {
		if !nt.Field(i).IsExported() {
			t.Errorf("Node.%s is unexported", nt.Field(i).Name)
		}
	}
}
