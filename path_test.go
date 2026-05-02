package jsonc

import (
	"errors"
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
