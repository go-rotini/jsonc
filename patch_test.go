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
	if _, err := p.Apply(root); !errors.Is(err, ErrPatchSyntax) {
		t.Errorf("expected ErrPatchSyntax (test failed), got %v", err)
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
