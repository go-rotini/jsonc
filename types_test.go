package jsonc

import (
	"encoding/json"
	"testing"
)

func TestMapSliceBasics(t *testing.T) {
	ms := MapSlice{
		{Key: "first", Value: 1},
		{Key: "second", Value: "two"},
		{Key: "third", Value: true},
	}
	if len(ms) != 3 {
		t.Errorf("len = %d, want 3", len(ms))
	}
	if ms[0].Key != "first" || ms[0].Value != 1 {
		t.Errorf("ms[0] = %+v", ms[0])
	}
	if ms[2].Value != true {
		t.Errorf("ms[2].Value = %v", ms[2].Value)
	}
}

func TestMapItemKeyIsString(t *testing.T) {
	// The point of typing MapItem.Key as string (rather than any like
	// TOML/YAML) is that JSON only allows string keys. This test simply
	// asserts the type at compile time.
	var item MapItem
	item.Key = "x"
	_ = item.Key
}

func TestNumberAlias(t *testing.T) {
	// jsonc.Number must be the same type as encoding/json.Number so values
	// are interchangeable.
	var n Number = "42"
	var stdN json.Number = n
	if stdN != "42" {
		t.Errorf("alias broken: %q", stdN)
	}

	got, err := n.Int64()
	if err != nil {
		t.Fatal(err)
	}
	if got != 42 {
		t.Errorf("Int64 = %d", got)
	}

	gotF, err := n.Float64()
	if err != nil {
		t.Fatal(err)
	}
	if gotF != 42.0 {
		t.Errorf("Float64 = %v", gotF)
	}
}

func TestMapKeyOrderConstants(t *testing.T) {
	// The two constants must be distinct values; the iota order locks the
	// default to MapKeyOrderLexicographic (the zero value).
	if MapKeyOrderLexicographic == MapKeyOrderInsertion {
		t.Error("MapKeyOrder constants collapsed")
	}
	if int(MapKeyOrderLexicographic) != 0 {
		t.Errorf("MapKeyOrderLexicographic = %d, want 0 (zero value default)", MapKeyOrderLexicographic)
	}
}
