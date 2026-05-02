package jsonc

import (
	"errors"
	"testing"
)

func TestEncodeOptionsDefaults(t *testing.T) {
	o := defaultEncodeOptions()
	if o.indent != "" {
		t.Errorf("default indent = %q, want empty", o.indent)
	}
	if o.escapeHTML {
		t.Error("default escapeHTML = true, want false")
	}
	if o.arrayMultiline {
		t.Error("default arrayMultiline = true, want false")
	}
	if o.omitEmpty {
		t.Error("default omitEmpty = true, want false")
	}
	if o.trailingComma {
		t.Error("default trailingComma = true, want false")
	}
	if o.durationAsString {
		t.Error("default durationAsString = true, want false")
	}
	if o.strictJSONOutput {
		t.Error("default strictJSONOutput = true, want false")
	}
	if o.mapKeyOrder != MapKeyOrderLexicographic {
		t.Errorf("default mapKeyOrder = %v, want lexicographic", o.mapKeyOrder)
	}
	if o.comments != nil {
		t.Errorf("default comments = %v, want nil", o.comments)
	}
	if o.customMarshalers != nil {
		t.Errorf("default customMarshalers = %v, want nil", o.customMarshalers)
	}
}

func TestDecodeOptionsDefaults(t *testing.T) {
	o := defaultDecodeOptions()
	if o.maxDepth != 100 {
		t.Errorf("default maxDepth = %d, want 100", o.maxDepth)
	}
	if o.strict {
		t.Error("default strict = true, want false")
	}
	if o.strictJSON {
		t.Error("default strictJSON = true, want false")
	}
	if o.allowDuplicateKeys {
		t.Error("default allowDuplicateKeys = true, want false (rejection is the default)")
	}
	if o.useNumber {
		t.Error("default useNumber = true, want false")
	}
	if o.useOrderedMap {
		t.Error("default useOrderedMap = true, want false")
	}
	if o.applyDefaults {
		t.Error("default applyDefaults = true, want false")
	}
	if o.maxKeys != 0 || o.maxDocumentSize != 0 || o.maxNodes != 0 {
		t.Errorf("default limits non-zero: keys=%d docSize=%d nodes=%d",
			o.maxKeys, o.maxDocumentSize, o.maxNodes)
	}
}

func TestEncodeOptionApplication(t *testing.T) {
	o := defaultEncodeOptions()
	WithIndent("  ")(o)
	if o.indent != "  " {
		t.Errorf("WithIndent: indent = %q", o.indent)
	}

	WithIndentN(4)(o)
	if o.indent != "    " {
		t.Errorf("WithIndentN(4): indent = %q", o.indent)
	}

	WithIndentN(0)(o)
	if o.indent != "" {
		t.Errorf("WithIndentN(0): indent = %q", o.indent)
	}

	WithIndentN(-3)(o)
	if o.indent != "" {
		t.Errorf("WithIndentN(-3): indent = %q (expected empty)", o.indent)
	}

	WithEscapeHTML(true)(o)
	if !o.escapeHTML {
		t.Error("WithEscapeHTML(true) failed")
	}

	WithArrayMultiline(true)(o)
	if !o.arrayMultiline {
		t.Error("WithArrayMultiline(true) failed")
	}

	WithOmitEmpty(true)(o)
	if !o.omitEmpty {
		t.Error("WithOmitEmpty(true) failed")
	}

	WithTrailingComma(true)(o)
	if !o.trailingComma {
		t.Error("WithTrailingComma(true) failed")
	}

	WithDurationAsString(true)(o)
	if !o.durationAsString {
		t.Error("WithDurationAsString(true) failed")
	}

	WithStrictJSONOutput(true)(o)
	if !o.strictJSONOutput {
		t.Error("WithStrictJSONOutput(true) failed")
	}

	WithMapKeyOrder(MapKeyOrderInsertion)(o)
	if o.mapKeyOrder != MapKeyOrderInsertion {
		t.Errorf("WithMapKeyOrder(Insertion) failed; got %v", o.mapKeyOrder)
	}

	comments := map[string][]Comment{"foo": {{Position: HeadCommentPos, Text: "x"}}}
	WithComment(comments)(o)
	if o.comments == nil || len(o.comments) != 1 {
		t.Errorf("WithComment failed; got %v", o.comments)
	}

	WithCustomMarshaler[int](func(int) ([]byte, error) { return nil, nil })(o)
	if o.customMarshalers == nil || len(o.customMarshalers) != 1 {
		t.Errorf("WithCustomMarshaler failed; got %v", o.customMarshalers)
	}
}

func TestDecodeOptionApplication(t *testing.T) {
	o := defaultDecodeOptions()

	WithStrict()(o)
	if !o.strict {
		t.Error("WithStrict() failed")
	}
	WithStrictJSON()(o)
	if !o.strictJSON {
		t.Error("WithStrictJSON() failed")
	}
	WithAllowDuplicateKeys()(o)
	if !o.allowDuplicateKeys {
		t.Error("WithAllowDuplicateKeys() failed")
	}
	WithUseNumber()(o)
	if !o.useNumber {
		t.Error("WithUseNumber() failed")
	}
	WithOrderedMap()(o)
	if !o.useOrderedMap {
		t.Error("WithOrderedMap() failed")
	}
	WithDefaults()(o)
	if !o.applyDefaults {
		t.Error("WithDefaults() failed")
	}
	WithMaxDepth(50)(o)
	if o.maxDepth != 50 {
		t.Errorf("WithMaxDepth(50): got %d", o.maxDepth)
	}
	WithMaxKeys(200)(o)
	if o.maxKeys != 200 {
		t.Errorf("WithMaxKeys(200): got %d", o.maxKeys)
	}
	WithMaxDocumentSize(1 << 20)(o)
	if o.maxDocumentSize != 1<<20 {
		t.Errorf("WithMaxDocumentSize: got %d", o.maxDocumentSize)
	}
	WithMaxNodes(1000)(o)
	if o.maxNodes != 1000 {
		t.Errorf("WithMaxNodes(1000): got %d", o.maxNodes)
	}

	v := &fakeValidator{}
	WithValidator(v)(o)
	if o.validator != v {
		t.Errorf("WithValidator: got %v", o.validator)
	}

	fn := func(_ *int, _ []byte) error { return errors.New("nope") }
	WithCustomUnmarshaler(fn)(o)
	if o.customUnmarshalers == nil || len(o.customUnmarshalers) != 1 {
		t.Errorf("WithCustomUnmarshaler: got %v", o.customUnmarshalers)
	}
}

type fakeValidator struct{ called bool }

func (v *fakeValidator) Struct(_ any) error {
	v.called = true
	return nil
}

func TestCommentPositionConstants(t *testing.T) {
	if HeadCommentPos == LineCommentPos || LineCommentPos == FootCommentPos {
		t.Error("CommentPosition constants collapsed")
	}
	if int(HeadCommentPos) != 0 {
		t.Errorf("HeadCommentPos = %d, want 0", HeadCommentPos)
	}
}

func TestStructValidatorInterface(t *testing.T) {
	var v StructValidator = &fakeValidator{}
	if err := v.Struct(struct{}{}); err != nil {
		t.Errorf("Struct returned %v", err)
	}
	if !v.(*fakeValidator).called {
		t.Error("validator not called")
	}
}
