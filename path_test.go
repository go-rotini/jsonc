package jsonc

import (
	"errors"
	"strings"
	"testing"
)

func mustParseRoot(t *testing.T, src string) *Node {
	t.Helper()
	return mustParse(t, src).Root
}

// ---------------------------------------------------------------------------
// PathString parsing
// ---------------------------------------------------------------------------

func TestPathStringRootDollar(t *testing.T) {
	p, err := PathString("$.foo")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.segments) != 1 || p.segments[0].name != "foo" {
		t.Errorf("got %+v", p.segments)
	}
}

func TestPathStringNoLeadingDollar(t *testing.T) {
	p, err := PathString(".foo.bar")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.segments) != 2 {
		t.Errorf("got %+v", p.segments)
	}
}

func TestPathStringIndex(t *testing.T) {
	p, err := PathString("$.users[2].name")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.segments) != 3 {
		t.Fatalf("got %d segments", len(p.segments))
	}
	if p.segments[1].kind != segIndex || p.segments[1].index != 2 {
		t.Errorf("index segment wrong: %+v", p.segments[1])
	}
}

func TestPathStringWildcard(t *testing.T) {
	p, err := PathString("$.users[*]")
	if err != nil {
		t.Fatal(err)
	}
	if p.segments[1].kind != segWildcard {
		t.Errorf("expected wildcard, got %+v", p.segments[1])
	}
}

func TestPathStringQuotedName(t *testing.T) {
	p, err := PathString(`$["weird key"]`)
	if err != nil {
		t.Fatal(err)
	}
	if p.segments[0].name != "weird key" {
		t.Errorf("got %q", p.segments[0].name)
	}
}

func TestPathStringRecursive(t *testing.T) {
	p, err := PathString("$..name")
	if err != nil {
		t.Fatal(err)
	}
	if p.segments[0].kind != segRecursive || p.segments[0].name != "name" {
		t.Errorf("got %+v", p.segments[0])
	}
}

func TestPathStringInvalid(t *testing.T) {
	cases := []string{"", "$.", "$..", "$[", "$[abc]"}
	for _, c := range cases {
		if _, err := PathString(c); !errors.Is(err, ErrPathSyntax) {
			t.Errorf("PathString(%q) expected ErrPathSyntax, got %v", c, err)
		}
	}
}

// ---------------------------------------------------------------------------
// PathPointer
// ---------------------------------------------------------------------------

func TestPathPointerEmpty(t *testing.T) {
	p, err := PathPointer("")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.segments) != 0 {
		t.Errorf("empty pointer should have no segments")
	}
}

func TestPathPointerSimple(t *testing.T) {
	p, err := PathPointer("/foo/0/bar")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.segments) != 3 {
		t.Fatalf("got %d segments", len(p.segments))
	}
	if p.segments[0].name != "foo" || p.segments[2].name != "bar" {
		t.Errorf("got %+v", p.segments)
	}
}

func TestPathPointerEscapes(t *testing.T) {
	p, err := PathPointer("/a~1b/c~0d")
	if err != nil {
		t.Fatal(err)
	}
	if p.segments[0].name != "a/b" || p.segments[1].name != "c~d" {
		t.Errorf("escape decoding wrong: %+v", p.segments)
	}
}

func TestPathPointerInvalid(t *testing.T) {
	if _, err := PathPointer("foo"); !errors.Is(err, ErrPointerSyntax) {
		t.Errorf("expected ErrPointerSyntax, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Read
// ---------------------------------------------------------------------------

func TestPathReadString(t *testing.T) {
	root := mustParseRoot(t, `{"name": "alice", "tags": ["a", "b", "c"]}`)
	p, _ := PathString("$.name")
	got, err := p.ReadString(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "alice" {
		t.Errorf("got %q", got)
	}
}

func TestPathReadIndex(t *testing.T) {
	root := mustParseRoot(t, `{"tags": ["a", "b", "c"]}`)
	p, _ := PathString("$.tags[1]")
	got, err := p.ReadString(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "b" {
		t.Errorf("got %q", got)
	}
}

func TestPathReadWildcard(t *testing.T) {
	root := mustParseRoot(t, `{"tags": ["a", "b", "c"]}`)
	p, _ := PathString("$.tags[*]")
	matches := p.Read(root)
	if len(matches) != 3 {
		t.Errorf("got %d matches", len(matches))
	}
}

func TestPathReadRecursive(t *testing.T) {
	src := `{"a": {"name": "x"}, "b": [{"name": "y"}, {"other": 1}]}`
	root := mustParseRoot(t, src)
	p, _ := PathString("$..name")
	matches := p.Read(root)
	if len(matches) != 2 {
		t.Errorf("expected 2 recursive matches, got %d", len(matches))
	}
}

func TestPathReadNoMatch(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	p, _ := PathString("$.nonexistent")
	matches := p.Read(root)
	if matches != nil {
		t.Errorf("expected nil, got %+v", matches)
	}
	_, err := p.ReadFirst(root)
	if !errors.Is(err, ErrPathNotFound) {
		t.Errorf("expected ErrPathNotFound, got %v", err)
	}
}

func TestPathReadPositions(t *testing.T) {
	root := mustParseRoot(t, "{\n  \"a\": 1\n}")
	p, _ := PathString("$.a")
	positions := p.ReadPositions(root)
	if len(positions) != 1 || positions[0].Line != 2 {
		t.Errorf("got positions %+v", positions)
	}
}

func TestPathPointerRead(t *testing.T) {
	root := mustParseRoot(t, `{"foo": {"bar": [10, 20, 30]}}`)
	p, _ := PathPointer("/foo/bar/1")
	got, err := p.ReadString(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "20" {
		t.Errorf("got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Replace / Append / Delete
// ---------------------------------------------------------------------------

func TestPathReplace(t *testing.T) {
	root := mustParseRoot(t, `{"name": "alice"}`)
	p, _ := PathString("$.name")
	newVal := &Node{Kind: StringNode, Value: "bob", RawValue: `"bob"`}
	if err := p.Replace(root, newVal); err != nil {
		t.Fatal(err)
	}
	got, _ := p.ReadString(root)
	if got != "bob" {
		t.Errorf("got %q", got)
	}
}

func TestPathReplaceNotFound(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	p, _ := PathString("$.nonexistent")
	newVal := &Node{Kind: NumberNode, Value: "42", RawValue: "42"}
	err := p.Replace(root, newVal)
	if !errors.Is(err, ErrPathNotFound) {
		t.Errorf("expected ErrPathNotFound, got %v", err)
	}
}

func TestPathAppendArray(t *testing.T) {
	root := mustParseRoot(t, `{"tags": ["a"]}`)
	p, _ := PathString("$.tags")
	newVal := &Node{Kind: StringNode, Value: "b", RawValue: `"b"`}
	if err := p.Append(root, newVal); err != nil {
		t.Fatal(err)
	}
	pAll, _ := PathString("$.tags[*]")
	matches := pAll.Read(root)
	if len(matches) != 2 {
		t.Errorf("expected 2 elements after append, got %d", len(matches))
	}
}

func TestPathAppendObject(t *testing.T) {
	root := mustParseRoot(t, `{"existing": 1}`)
	p, _ := PathString("$")
	kv := &Node{
		Kind:     KeyValueNode,
		Key:      "added",
		Children: []*Node{{Kind: NumberNode, Value: "2", RawValue: "2"}},
	}
	if err := p.Append(root, kv); err != nil {
		t.Fatal(err)
	}
	pNew, _ := PathString("$.added")
	v, err := pNew.ReadString(root)
	if err != nil || v != "2" {
		t.Errorf("got %q, %v", v, err)
	}
}

func TestPathDelete(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1, "b": 2, "c": 3}`)
	p, _ := PathString("$.b")
	if err := p.Delete(root); err != nil {
		t.Fatal(err)
	}
	pNew, _ := PathString("$.b")
	if _, err := pNew.ReadFirst(root); !errors.Is(err, ErrPathNotFound) {
		t.Error("expected b to be deleted")
	}
	// other fields still present
	pa, _ := PathString("$.a")
	if v, _ := pa.ReadString(root); v != "1" {
		t.Errorf("a = %q", v)
	}
}

func TestPathDeleteArrayElement(t *testing.T) {
	root := mustParseRoot(t, `{"tags": ["a", "b", "c"]}`)
	p, _ := PathString("$.tags[1]")
	if err := p.Delete(root); err != nil {
		t.Fatal(err)
	}
	pAll, _ := PathString("$.tags[*]")
	matches := pAll.Read(root)
	if len(matches) != 2 {
		t.Errorf("expected 2, got %d", len(matches))
	}
}

func TestPathString(t *testing.T) {
	p, _ := PathString("$.foo.bar")
	if p.String() != "$.foo.bar" {
		t.Errorf("got %q", p.String())
	}
}

// ---------------------------------------------------------------------------
// Path.Delete — kv-fallback paths.
// ---------------------------------------------------------------------------

func TestPathDeleteKeyValueChildPath(t *testing.T) {
	// Path delete on a deeply-nested array element exercises findChild's
	// kv-fallback branch.
	root := mustParseRoot(t, `{"a": {"b": {"c": [10, 20, 30]}}}`)
	p, _ := PathString("$.a.b.c[1]")
	if err := p.Delete(root); err != nil {
		t.Fatal(err)
	}
	verify, _ := PathString("$.a.b.c[*]")
	if got := verify.Read(root); len(got) != 2 {
		t.Errorf("expected 2 items after delete, got %d", len(got))
	}
}

func TestPathDeleteUnparented(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	// Path that returns a node not in the parent map should be no-op.
	p, _ := PathString("$.nonexistent")
	if err := p.Delete(root); !errors.Is(err, ErrPathNotFound) {
		t.Errorf("expected ErrPathNotFound, got %v", err)
	}
}

func TestPathDeleteWithMultipleSiblingsExercisesKVFallback(t *testing.T) {
	root := mustParseRoot(t, `{"a": {"x": 1}, "b": {"y": 2}, "c": {"z": 3}}`)
	// Delete /a — Path returns the inner object {"x":1}; Delete must find
	// the wrapping KeyValueNode in the root's children.
	p, _ := PathString("$.a")
	if err := p.Delete(root); err != nil {
		t.Fatal(err)
	}
	verify, _ := PathString("$.a")
	if _, err := verify.ReadFirst(root); !errors.Is(err, ErrPathNotFound) {
		t.Error("expected $.a to be deleted")
	}
}

func TestPathDeleteAtSecondLevel(t *testing.T) {
	root := mustParseRoot(t, `{"outer": {"inner": "value", "keep": "yes"}}`)
	p, _ := PathString("$.outer.inner")
	if err := p.Delete(root); err != nil {
		t.Fatal(err)
	}
	verify, _ := PathString("$.outer.keep")
	if v, _ := verify.ReadString(root); v != "yes" {
		t.Errorf("expected sibling intact, got %q", v)
	}
}

func TestPathDeleteValueWrappedInKV(t *testing.T) {
	// $.a returns the inner number 1; Delete must locate the wrapping
	// KeyValueNode through findChild's KV fallback.
	root := mustParseRoot(t, `{"a": 1}`)
	p, _ := PathString("$.a")
	if err := p.Delete(root); err != nil {
		t.Fatal(err)
	}
	verify, _ := PathString("$.a")
	if _, err := verify.ReadFirst(root); !errors.Is(err, ErrPathNotFound) {
		t.Error("expected $.a to be deleted")
	}
}

// ---------------------------------------------------------------------------
// Path.ReadString and PathPointer index variants.
// ---------------------------------------------------------------------------

func TestPathReadStringNonScalar(t *testing.T) {
	root := mustParseRoot(t, `{"obj": {"k": 1}}`)
	p, _ := PathString("$.obj")
	got, err := p.ReadString(root)
	if err != nil {
		t.Fatal(err)
	}
	// Returns the Node.String() form (e.g., "{object: 1 members}").
	if !strings.Contains(got, "object") {
		t.Errorf("got %q", got)
	}
}

func TestPathPointerNumericTokenIntoArray(t *testing.T) {
	// JSON Pointer with numeric token applied to an array.
	root := mustParseRoot(t, `[10, 20, 30]`)
	p, _ := PathPointer("/1")
	got, err := p.ReadString(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "20" {
		t.Errorf("got %q", got)
	}
}

func TestPathPointerNegativeIndex(t *testing.T) {
	root := mustParseRoot(t, `[10, 20, 30]`)
	// Pointer "/-" is reserved for "one past the last" in some operations
	// but in Read it should not match anything.
	p, _ := PathPointer("/-")
	if matches := p.Read(root); len(matches) != 0 {
		t.Errorf("pointer to '-' should not match in Read, got %d", len(matches))
	}
}

// ---------------------------------------------------------------------------
// Path.Replace and Append edge cases.
// ---------------------------------------------------------------------------

func TestPathReplaceMatchingScalar(t *testing.T) {
	// Replacing a non-KeyValueNode (e.g., an array element or a scalar via
	// recursive descent) writes in place.
	root := mustParseRoot(t, `{"a": [1, 2, 3]}`)
	p, _ := PathString("$.a[*]")
	repl := &Node{Kind: NumberNode, Value: "9", RawValue: "9"}
	if err := p.Replace(root, repl); err != nil {
		t.Fatal(err)
	}
	all, _ := PathString("$.a[*]")
	for _, n := range all.Read(root) {
		if n.Value != "9" {
			t.Errorf("got %v", n.Value)
		}
	}
}

func TestPathReplaceArrayElement(t *testing.T) {
	root := mustParseRoot(t, `[1, 2, 3]`)
	p, _ := PathString("$[1]")
	repl := &Node{Kind: NumberNode, Value: "99", RawValue: "99"}
	if err := p.Replace(root, repl); err != nil {
		t.Fatal(err)
	}
	check, _ := PathString("$[1]")
	if v, _ := check.ReadString(root); v != "99" {
		t.Errorf("got %q", v)
	}
}

func TestPathReplaceArrayWildcardSingle(t *testing.T) {
	root := mustParseRoot(t, `{"a": [1]}`)
	p, _ := PathString("$.a[0]")
	repl := &Node{Kind: NumberNode, Value: "99", RawValue: "99"}
	if err := p.Replace(root, repl); err != nil {
		t.Fatal(err)
	}
	check, _ := PathString("$.a[0]")
	if v, _ := check.ReadString(root); v != "99" {
		t.Errorf("got %q", v)
	}
}

func TestPathReplaceKeyValueWithoutChild(t *testing.T) {
	// Build by hand so the KV has no children initially.
	root := &Node{
		Kind: ObjectNode,
		Children: []*Node{
			{Kind: KeyValueNode, Key: "k"}, // no children
		},
	}
	p, _ := PathString("$.k")
	repl := &Node{Kind: NumberNode, Value: "1", RawValue: "1"}
	// Replace returns ErrPathNotFound because childByName requires a value child.
	if err := p.Replace(root, repl); !errors.Is(err, ErrPathNotFound) {
		t.Errorf("expected ErrPathNotFound, got %v", err)
	}
}

func TestPathReplaceFillsEmptyKV(t *testing.T) {
	// Hand-build a tree where a path query matches a KV value, then
	// remove the value to make it empty, then Replace should populate it.
	// Actually since path queries skip empty-children KVs, this branch is
	// only reachable via patch's opAdd directly.
	t.Skip("not reachable through public Replace path")
}

func TestPathAppendIntoScalarRejected(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	p, _ := PathString("$.a")
	val := &Node{Kind: NumberNode, Value: "2", RawValue: "2"}
	if err := p.Append(root, val); err == nil {
		t.Error("expected error: cannot append into scalar")
	}
}

func TestPathAppendIntoObjectWithoutKVValueRejected(t *testing.T) {
	root := mustParseRoot(t, `{}`)
	p, _ := PathString("$")
	// Appending a non-KeyValueNode into an object should error.
	val := &Node{Kind: NumberNode, Value: "1", RawValue: "1"}
	if err := p.Append(root, val); err == nil {
		t.Error("expected error: object Append requires KeyValueNode")
	}
}

// ---------------------------------------------------------------------------
// PathString syntax error / quoted name / wildcards.
// ---------------------------------------------------------------------------

func TestPathStringInvalidBracketIndex(t *testing.T) {
	if _, err := PathString("$[abc]"); err == nil {
		t.Error("expected syntax error on non-numeric index")
	}
	if _, err := PathString("$[-1]"); err == nil {
		t.Error("expected syntax error on negative index")
	}
}

func TestPathStringSingleQuotedName(t *testing.T) {
	p, err := PathString(`$['weird key']`)
	if err != nil {
		t.Fatal(err)
	}
	if p.segments[0].name != "weird key" {
		t.Errorf("got %q", p.segments[0].name)
	}
}

func TestPathStringEmptyBracket(t *testing.T) {
	if _, err := PathString("$[]"); err == nil {
		t.Error("expected syntax error on empty bracket")
	}
}

func TestPathReadAcrossArrayWithNamedSegment(t *testing.T) {
	root := mustParseRoot(t, `[{"k":1}, {"k":2}]`)
	// Wildcard then descend by name.
	p, _ := PathString("$[*].k")
	matches := p.Read(root)
	if len(matches) != 2 {
		t.Errorf("expected 2 matches, got %d", len(matches))
	}
}

func TestPathWildcardSkipsCommentSiblings(t *testing.T) {
	root := mustParseRoot(t, `[
		// comment
		1,
		// comment
		2
	]`)
	p, _ := PathString("$[*]")
	matches := p.Read(root)
	if len(matches) != 2 {
		t.Errorf("expected 2 elements (comments excluded), got %d", len(matches))
	}
}
