package jsonc

import (
	"errors"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ParsePatch
// ---------------------------------------------------------------------------

func TestParsePatchSimple(t *testing.T) {
	src := `[
		{"op": "add", "path": "/a", "value": 1},
		{"op": "remove", "path": "/b"}
	]`
	p, err := ParsePatch([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 2 {
		t.Fatalf("got %d ops", len(p))
	}
	if p[0].Op != "add" || p[0].Path != "/a" || p[0].Value == nil {
		t.Errorf("op[0]: %+v", p[0])
	}
	if p[1].Op != "remove" || p[1].Path != "/b" {
		t.Errorf("op[1]: %+v", p[1])
	}
}

func TestParsePatchInvalid(t *testing.T) {
	_, err := ParsePatch([]byte(`{not array`))
	if !errors.Is(err, ErrPatchSyntax) {
		t.Errorf("expected ErrPatchSyntax, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// add / remove / replace / move / copy / test
// ---------------------------------------------------------------------------

func applyPatchString(t *testing.T, doc, patch string) string {
	t.Helper()
	root := mustParseRoot(t, doc)
	p, err := ParsePatch([]byte(patch))
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Apply(root)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := NodeToBytes(out)
	if err != nil {
		t.Fatal(err)
	}
	return string(bytes)
}

func TestPatchAddObjectMember(t *testing.T) {
	got := applyPatchString(t,
		`{"a": 1}`,
		`[{"op": "add", "path": "/b", "value": 2}]`)
	if !strings.Contains(got, `"b"`) || !strings.Contains(got, "2") {
		t.Errorf("got %s", got)
	}
}

func TestPatchAddArrayElement(t *testing.T) {
	got := applyPatchString(t,
		`{"items": [1, 3]}`,
		`[{"op": "add", "path": "/items/1", "value": 2}]`)
	if !strings.Contains(got, "1, 2, 3") {
		t.Errorf("got %s", got)
	}
}

func TestPatchAddArrayAppend(t *testing.T) {
	got := applyPatchString(t,
		`{"items": [1, 2]}`,
		`[{"op": "add", "path": "/items/-", "value": 3}]`)
	if !strings.Contains(got, "1, 2, 3") {
		t.Errorf("got %s", got)
	}
}

func TestPatchAddRoot(t *testing.T) {
	got := applyPatchString(t,
		`{"a": 1}`,
		`[{"op": "add", "path": "", "value": {"b": 2}}]`)
	if !strings.Contains(got, `"b"`) || strings.Contains(got, `"a"`) {
		t.Errorf("got %s", got)
	}
}

func TestPatchRemoveObjectMember(t *testing.T) {
	got := applyPatchString(t,
		`{"a": 1, "b": 2}`,
		`[{"op": "remove", "path": "/a"}]`)
	if strings.Contains(got, `"a"`) {
		t.Errorf("got %s", got)
	}
}

func TestPatchRemoveArrayElement(t *testing.T) {
	got := applyPatchString(t,
		`{"items": [1, 2, 3]}`,
		`[{"op": "remove", "path": "/items/1"}]`)
	if !strings.Contains(got, "1, 3") {
		t.Errorf("got %s", got)
	}
}

func TestPatchReplace(t *testing.T) {
	got := applyPatchString(t,
		`{"a": 1}`,
		`[{"op": "replace", "path": "/a", "value": 99}]`)
	if !strings.Contains(got, "99") {
		t.Errorf("got %s", got)
	}
}

func TestPatchReplaceMissing(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	p, _ := ParsePatch([]byte(`[{"op": "replace", "path": "/missing", "value": 99}]`))
	if _, err := p.Apply(root); err == nil {
		t.Fatal("expected error replacing missing key")
	}
}

func TestPatchMove(t *testing.T) {
	got := applyPatchString(t,
		`{"a": 1, "b": 2}`,
		`[{"op": "move", "from": "/a", "path": "/c"}]`)
	if !strings.Contains(got, `"c"`) || strings.Contains(got, `"a"`) {
		t.Errorf("got %s", got)
	}
}

func TestPatchMoveIntoDescendantRejected(t *testing.T) {
	root := mustParseRoot(t, `{"a": {"b": 1}}`)
	p, _ := ParsePatch([]byte(`[{"op": "move", "from": "/a", "path": "/a/b/c"}]`))
	if _, err := p.Apply(root); err == nil {
		t.Fatal("expected error moving into descendant")
	}
}

func TestPatchCopy(t *testing.T) {
	got := applyPatchString(t,
		`{"a": 1}`,
		`[{"op": "copy", "from": "/a", "path": "/b"}]`)
	if !strings.Contains(got, `"a"`) || !strings.Contains(got, `"b"`) {
		t.Errorf("got %s", got)
	}
}

func TestPatchTestSucceeds(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	p, _ := ParsePatch([]byte(`[{"op": "test", "path": "/a", "value": 1}]`))
	if _, err := p.Apply(root); err != nil {
		t.Errorf("test should pass: %v", err)
	}
}

func TestPatchTestFails(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	p, _ := ParsePatch([]byte(`[{"op": "test", "path": "/a", "value": 99}]`))
	_, err := p.Apply(root)
	if !errors.Is(err, ErrPatchTestFailed) {
		t.Errorf("expected ErrPatchTestFailed, got %v", err)
	}
	if errors.Is(err, ErrPatchSyntax) {
		t.Errorf("a failed test is not a syntax error: %v", err)
	}
}

func TestPatchTestComparesNumbersByValue(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1, "b": [100], "c": -0, "d": 0.5}`)
	for _, tc := range []struct {
		path, value string
		want        bool
	}{
		{"/a", "1.0", true},
		{"/a", "1e0", true},
		{"/a", "10E-1", true},
		{"/a", "1.5", false},
		{"/b/0", "1e2", true},
		{"/b/0", "0.1e3", true},
		{"/b/0", "1e3", false},
		{"/c", "0", true},
		{"/c", "0.0e5", true},
		{"/d", "5e-1", true},
		{"/d", "-0.5", false},
		{"/a", `"1"`, false},
	} {
		p, err := ParsePatch([]byte(`[{"op": "test", "path": "` + tc.path + `", "value": ` + tc.value + `}]`))
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.Apply(root)
		if got := err == nil; got != tc.want {
			t.Errorf("test %s == %s: passed=%v, want %v (err %v)", tc.path, tc.value, got, tc.want, err)
		}
		if err != nil && !errors.Is(err, ErrPatchTestFailed) {
			t.Errorf("test %s == %s: err = %v, want ErrPatchTestFailed", tc.path, tc.value, err)
		}
	}
}

func TestPatchUnknownOpRejected(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	p, _ := ParsePatch([]byte(`[{"op": "frobnicate", "path": "/a"}]`))
	if _, err := p.Apply(root); !errors.Is(err, ErrPatchSyntax) {
		t.Errorf("expected ErrPatchSyntax, got %v", err)
	}
}

func TestPatchSequence(t *testing.T) {
	// Series of operations form a single transformation.
	got := applyPatchString(t,
		`{"a": 1, "b": 2}`,
		`[
			{"op": "add", "path": "/c", "value": 3},
			{"op": "remove", "path": "/a"},
			{"op": "replace", "path": "/b", "value": 20}
		]`)
	if strings.Contains(got, `"a"`) {
		t.Errorf("a should be removed: %s", got)
	}
	if !strings.Contains(got, `"c"`) || !strings.Contains(got, "20") {
		t.Errorf("c added and b replaced: %s", got)
	}
}

func TestPatchOriginalUnchanged(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	p, _ := ParsePatch([]byte(`[{"op": "add", "path": "/b", "value": 2}]`))
	if _, err := p.Apply(root); err != nil {
		t.Fatal(err)
	}
	// Original should not have been mutated.
	if len(root.Children) != 1 {
		t.Errorf("Apply should not mutate input, got %d children", len(root.Children))
	}
}

// ---------------------------------------------------------------------------
// Patch test op — deep equality across containers and kinds.
// ---------------------------------------------------------------------------

func TestPatchTestDeepEqualityArray(t *testing.T) {
	root := mustParseRoot(t, `{"items": [1, 2, 3]}`)
	p, _ := ParsePatch([]byte(`[{"op": "test", "path": "/items", "value": [1, 2, 3]}]`))
	if _, err := p.Apply(root); err != nil {
		t.Errorf("array deep-equal test should pass: %v", err)
	}
}

func TestPatchTestDeepEqualityArrayMismatchSize(t *testing.T) {
	root := mustParseRoot(t, `{"items": [1, 2]}`)
	p, _ := ParsePatch([]byte(`[{"op": "test", "path": "/items", "value": [1, 2, 3]}]`))
	if _, err := p.Apply(root); err == nil {
		t.Error("test should fail on array length mismatch")
	}
}

func TestPatchTestDeepEqualityObject(t *testing.T) {
	root := mustParseRoot(t, `{"user": {"name": "alice", "age": 30}}`)
	p, _ := ParsePatch([]byte(`[{"op": "test", "path": "/user", "value": {"name": "alice", "age": 30}}]`))
	if _, err := p.Apply(root); err != nil {
		t.Errorf("object deep-equal test should pass: %v", err)
	}
}

func TestPatchTestDeepEqualityObjectMismatchSize(t *testing.T) {
	root := mustParseRoot(t, `{"user": {"name": "alice"}}`)
	p, _ := ParsePatch([]byte(`[{"op": "test", "path": "/user", "value": {"name": "alice", "age": 30}}]`))
	if _, err := p.Apply(root); err == nil {
		t.Error("test should fail on object size mismatch")
	}
}

func TestPatchTestDeepEqualityObjectMismatchValue(t *testing.T) {
	root := mustParseRoot(t, `{"user": {"name": "alice"}}`)
	p, _ := ParsePatch([]byte(`[{"op": "test", "path": "/user", "value": {"name": "bob"}}]`))
	if _, err := p.Apply(root); err == nil {
		t.Error("test should fail on value mismatch")
	}
}

func TestPatchTestDeepEqualityObjectMissingKey(t *testing.T) {
	root := mustParseRoot(t, `{"user": {"name": "alice"}}`)
	p, _ := ParsePatch([]byte(`[{"op": "test", "path": "/user", "value": {"email": "alice@example"}}]`))
	if _, err := p.Apply(root); err == nil {
		t.Error("test should fail on missing key")
	}
}

func TestPatchTestKindMismatch(t *testing.T) {
	root := mustParseRoot(t, `{"x": 1}`)
	p, _ := ParsePatch([]byte(`[{"op": "test", "path": "/x", "value": "1"}]`))
	if _, err := p.Apply(root); err == nil {
		t.Error("test should fail when kinds differ (number vs string)")
	}
}

func TestPatchTestRoot(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	p, _ := ParsePatch([]byte(`[{"op": "test", "path": "", "value": {"a": 1}}]`))
	if _, err := p.Apply(root); err != nil {
		t.Errorf("test against root should pass: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Patch traversal failure paths.
// ---------------------------------------------------------------------------

func TestPatchAddTraverseScalarRejected(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	// Try to add into /a/b — a is a scalar, not a container.
	p, _ := ParsePatch([]byte(`[{"op": "add", "path": "/a/b", "value": 2}]`))
	if _, err := p.Apply(root); err == nil {
		t.Error("expected error: cannot descend into scalar")
	}
}

func TestPatchRemoveOutOfRangeIndex(t *testing.T) {
	root := mustParseRoot(t, `{"items": [1, 2]}`)
	p, _ := ParsePatch([]byte(`[{"op": "remove", "path": "/items/99"}]`))
	if _, err := p.Apply(root); err == nil {
		t.Error("expected error: index out of range")
	}
}

func TestPatchRemoveMissingMember(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	p, _ := ParsePatch([]byte(`[{"op": "remove", "path": "/missing"}]`))
	if _, err := p.Apply(root); err == nil {
		t.Error("expected error: missing member")
	}
}

func TestPatchRemoveRoot(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	p, _ := ParsePatch([]byte(`[{"op": "remove", "path": ""}]`))
	if _, err := p.Apply(root); err == nil {
		t.Error("expected error: cannot remove root")
	}
}

func TestPatchInvalidPointer(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	p, _ := ParsePatch([]byte(`[{"op": "add", "path": "no-leading-slash", "value": 1}]`))
	if _, err := p.Apply(root); err == nil {
		t.Error("expected error: pointer must start with /")
	}
}

func TestPatchAddArrayOutOfRange(t *testing.T) {
	root := mustParseRoot(t, `{"items": [1, 2]}`)
	p, _ := ParsePatch([]byte(`[{"op": "add", "path": "/items/99", "value": 3}]`))
	if _, err := p.Apply(root); err == nil {
		t.Error("expected error: insert index out of range")
	}
}

func TestPatchAddMissingValue(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	// "add" requires a value field.
	p, _ := ParsePatch([]byte(`[{"op": "add", "path": "/b"}]`))
	if _, err := p.Apply(root); err == nil {
		t.Error("expected error: add without value")
	}
}

// ---------------------------------------------------------------------------
// Move and copy edge cases.
// ---------------------------------------------------------------------------

func TestPatchMoveSamePath(t *testing.T) {
	// from == path is a no-op.
	root := mustParseRoot(t, `{"a": 1}`)
	p, _ := ParsePatch([]byte(`[{"op": "move", "from": "/a", "path": "/a"}]`))
	if _, err := p.Apply(root); err != nil {
		t.Errorf("expected no-op, got %v", err)
	}
}

func TestPatchMoveNonexistentSource(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	p, _ := ParsePatch([]byte(`[{"op": "move", "from": "/missing", "path": "/b"}]`))
	if _, err := p.Apply(root); err == nil {
		t.Error("expected error moving from missing path")
	}
}

func TestPatchCopyNonexistentSource(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	p, _ := ParsePatch([]byte(`[{"op": "copy", "from": "/missing", "path": "/b"}]`))
	if _, err := p.Apply(root); err == nil {
		t.Error("expected error copying from missing path")
	}
}

// ---------------------------------------------------------------------------
// Walking too-deep paths through scalars and missing-target test ops.
// ---------------------------------------------------------------------------

func TestPatchTestThroughScalarRejected(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	p, _ := ParsePatch([]byte(`[{"op": "test", "path": "/a/b", "value": 1}]`))
	if _, err := p.Apply(root); err == nil {
		t.Error("expected error: cannot descend into scalar")
	}
}

func TestPatchCopyTooDeepInScalar(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	p, _ := ParsePatch([]byte(`[{"op": "copy", "from": "/a/b", "path": "/c"}]`))
	if _, err := p.Apply(root); err == nil {
		t.Error("expected error from too-deep from-path")
	}
}

func TestPatchTestNonexistentPath(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	p, _ := ParsePatch([]byte(`[{"op": "test", "path": "/missing", "value": 1}]`))
	if _, err := p.Apply(root); err == nil {
		t.Error("expected error from missing path in test op")
	}
}

func TestPatchTestMissingValue(t *testing.T) {
	root := mustParseRoot(t, `{"a": 1}`)
	// A test op without a value field should be rejected.
	p, _ := ParsePatch([]byte(`[{"op": "test", "path": "/a"}]`))
	if _, err := p.Apply(root); err == nil {
		t.Error("expected error: test without value")
	}
}

// ---------------------------------------------------------------------------
// ParsePatch rejects malformed value field.
// ---------------------------------------------------------------------------

func TestParsePatchInvalidValue(t *testing.T) {
	src := `[{"op": "add", "path": "/a", "value": @@@}]`
	_, err := ParsePatch([]byte(src))
	if err == nil {
		t.Error("expected ErrPatchSyntax on malformed value")
	}
}

func applyPatch(t *testing.T, doc, patch string) (string, error) {
	t.Helper()
	f, err := Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParsePatch([]byte(patch))
	if err != nil {
		return "", err
	}
	out, err := p.Apply(f.Root)
	if err != nil {
		return "", err
	}
	b, err := NodeToBytes(out)
	if err != nil {
		t.Fatal(err)
	}
	return string(b), nil
}

func TestPatchReplaceArray(t *testing.T) {
	got, err := applyPatch(t, `{"a":[1,2,3]}`, `[{"op":"replace","path":"/a/0","value":9}]`)
	if err != nil || got != `{"a": [9, 2, 3]}` {
		t.Errorf("got %s, %v", got, err)
	}
	if _, err := applyPatch(t, `{"a":[1]}`, `[{"op":"replace","path":"/a/1","value":9}]`); !errors.Is(err, ErrPathNotFound) {
		t.Errorf("replace past end: err = %v", err)
	}
}

func TestPatchReplaceArrayKeepsComments(t *testing.T) {
	f, err := Parse([]byte("[\n  // first\n  1, // one\n  2\n]"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Patch{{Op: "replace", Path: "/0", Value: &Node{Kind: NumberNode, Value: "9"}}}.Apply(f.Root)
	if err != nil {
		t.Fatal(err)
	}
	if out.Children[0].HeadComment != " first" || out.Children[0].Comment != " one" {
		t.Errorf("comments = %q, %q", out.Children[0].HeadComment, out.Children[0].Comment)
	}
}

func TestPatchPointerIndex(t *testing.T) {
	for _, path := range []string{"/a/01", "/a/+1", "/a/-0", "/a/ 1", "/a/1x"} {
		if _, err := applyPatch(t, `{"a":[1,2,3]}`, `[{"op":"remove","path":"`+path+`"}]`); err == nil {
			t.Errorf("remove %s: no error", path)
		}
	}
	for _, path := range []string{"/a~2", "/a~", "/~x"} {
		if _, err := applyPatch(t, `{"a":1}`, `[{"op":"remove","path":"`+path+`"}]`); !errors.Is(err, ErrPointerSyntax) {
			t.Errorf("remove %s: err = %v, want ErrPointerSyntax", path, err)
		}
	}
	got, err := applyPatch(t, `{"a~/b":[1,2,3]}`, `[{"op":"remove","path":"/a~0~1b/1"}]`)
	if err != nil || got != `{"a~/b": [1, 3]}` {
		t.Errorf("escaped remove: got %s, %v", got, err)
	}
}

func TestPatchMissingPath(t *testing.T) {
	for _, patch := range []string{
		`[{"op":"add","value":{"pwned":true}}]`,
		`[{"op":"move","path":"/b"}]`,
		`[{"op":"copy","path":"/b"}]`,
	} {
		if _, err := ParsePatch([]byte(patch)); !errors.Is(err, ErrPatchSyntax) {
			t.Errorf("%s: err = %v, want ErrPatchSyntax", patch, err)
		}
	}
	if _, err := applyPatch(t, `{"a":1}`, `[{"op":"move","from":"/nope","path":"/nope"}]`); !errors.Is(err, ErrPathNotFound) {
		t.Errorf("move missing onto itself: err = %v", err)
	}
	got, err := applyPatch(t, `{"a":1}`, `[{"op":"move","from":"/a","path":"/a"}]`)
	if err != nil || got != `{"a": 1}` {
		t.Errorf("move onto itself: got %s, %v", got, err)
	}
}

func TestPatchNilRoot(t *testing.T) {
	n := &Node{Kind: NumberNode, Value: "1"}
	for _, op := range []PatchOp{
		{Op: "add", Path: "/a", Value: n},
		{Op: "add", Path: "/a/b", Value: n},
		{Op: "remove", Path: "/a"},
		{Op: "replace", Path: "/a", Value: n},
		{Op: "move", From: "/a", Path: "/b"},
		{Op: "copy", From: "/a", Path: "/b"},
		{Op: "test", Path: "/a", Value: n},
	} {
		if _, err := (Patch{op}).Apply(nil); !errors.Is(err, ErrPathNotFound) {
			t.Errorf("%s: err = %v, want ErrPathNotFound", op.Op, err)
		}
	}
	out, err := Patch{{Op: "add", Path: "", Value: n}}.Apply(nil)
	if err != nil || out.Value != "1" {
		t.Errorf("add at root: %v, %v", out, err)
	}
}

func TestPatchMoveKeepsMemberComments(t *testing.T) {
	f, err := Parse([]byte("{\n  // about a\n  \"a\": 1, // one\n  \"b\": 2\n}"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Patch{{Op: "move", From: "/a", Path: "/c"}}.Apply(f.Root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := NodeToBytesWithOptions(out, WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "// about a\n  \"c\": 1 // one") {
		t.Errorf("got\n%s", got)
	}
}
