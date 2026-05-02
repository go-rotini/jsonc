package jsonc

import (
	"errors"
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
