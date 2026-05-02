package jsonc

import (
	"reflect"
	"testing"
)

func TestParseTagPairEmpty(t *testing.T) {
	fi := parseTagPair("", "")
	if fi.skip || fi.name != "" || fi.omitEmpty {
		t.Errorf("empty tags: %+v", fi)
	}
}

func TestParseTagPairNameOnly(t *testing.T) {
	tests := []struct {
		jsonTag, jsoncTag string
		wantName          string
	}{
		{`name`, ``, "name"},
		{``, `name`, "name"},
		{`json_name`, `jsonc_name`, "jsonc_name"}, // jsonc wins
		{`name`, `,required`, "name"},             // jsonc has options but no name → use json name
	}
	for _, tt := range tests {
		fi := parseTagPair(tt.jsonTag, tt.jsoncTag)
		if fi.name != tt.wantName {
			t.Errorf("parseTagPair(%q, %q): name = %q, want %q",
				tt.jsonTag, tt.jsoncTag, fi.name, tt.wantName)
		}
	}
}

func TestParseTagPairSkip(t *testing.T) {
	cases := []struct{ json, jsonc string }{
		{"-", ""},
		{"", "-"},
		{"-", "name"},
		{"name", "-"},
	}
	for _, tc := range cases {
		fi := parseTagPair(tc.json, tc.jsonc)
		if !fi.skip {
			t.Errorf(`parseTagPair(%q, %q): skip = false, want true`, tc.json, tc.jsonc)
		}
	}
}

func TestParseTagPairOmitEmpty(t *testing.T) {
	cases := []struct{ json, jsonc string }{
		{`name,omitempty`, ``},
		{``, `name,omitempty`},
		{`name`, `,omitempty`},
	}
	for _, tc := range cases {
		fi := parseTagPair(tc.json, tc.jsonc)
		if !fi.omitEmpty {
			t.Errorf(`parseTagPair(%q, %q): omitEmpty = false`, tc.json, tc.jsonc)
		}
	}
}

func TestParseTagPairAsString(t *testing.T) {
	// "string" option works in either tag (matches stdlib).
	fi := parseTagPair(`name,string`, ``)
	if !fi.asString {
		t.Error(`json:"name,string": asString = false`)
	}
	fi = parseTagPair(``, `name,string`)
	if !fi.asString {
		t.Error(`jsonc:"name,string": asString = false`)
	}
}

func TestParseTagPairJSONCOnlyOptions(t *testing.T) {
	// omitzero, commented, required, default= are jsonc-only.
	fi := parseTagPair(``, `name,omitzero`)
	if !fi.omitZero {
		t.Error(`jsonc:"name,omitzero" failed`)
	}

	fi = parseTagPair(``, `name,commented`)
	if !fi.commented {
		t.Error(`jsonc:"name,commented" failed`)
	}

	fi = parseTagPair(``, `name,required`)
	if !fi.required {
		t.Error(`jsonc:"name,required" failed`)
	}

	fi = parseTagPair(``, `name,default=8080`)
	if !fi.hasDefault || fi.defaultValue != "8080" {
		t.Errorf(`jsonc:"name,default=8080": got hasDefault=%v defaultValue=%q`,
			fi.hasDefault, fi.defaultValue)
	}
}

func TestParseTagPairJSONCOnlyOptionsIgnoredInJSONTag(t *testing.T) {
	// jsonc-only options placed in the json tag are silently ignored
	// (stdlib wouldn't recognize them either).
	fi := parseTagPair(`name,omitzero,commented,required,default=8080`, ``)
	if fi.omitZero || fi.commented || fi.required || fi.hasDefault {
		t.Errorf("jsonc-only options leaked from json tag: %+v", fi)
	}
}

func TestParseTagPairLayered(t *testing.T) {
	// Combined json + jsonc tags: name from jsonc, omitempty from json,
	// required from jsonc.
	fi := parseTagPair(`email,omitempty`, `,required`)
	if fi.name != "email" {
		t.Errorf("name = %q", fi.name)
	}
	if !fi.omitEmpty {
		t.Error("omitEmpty not picked up from json")
	}
	if !fi.required {
		t.Error("required not picked up from jsonc")
	}
}

func TestStructFieldsSimple(t *testing.T) {
	type Cfg struct {
		Name  string `json:"name"`
		Count int    `json:"count,omitempty"`
		Skip  string `json:"-"`
		Bare  string
	}
	sf := getStructFields(reflect.TypeOf(Cfg{}))
	wantNames := []string{"name", "count", "Bare"}
	if len(sf.fields) != len(wantNames) {
		t.Fatalf("len(fields) = %d, want %d (got %v)", len(sf.fields), len(wantNames), sf.fields)
	}
	for i, want := range wantNames {
		if sf.fields[i].name != want {
			t.Errorf("[%d] name = %q, want %q", i, sf.fields[i].name, want)
		}
	}
	if !sf.fields[1].omitEmpty {
		t.Error("count should have omitEmpty")
	}
	// Skip field absent.
	if _, ok := sf.byName["Skip"]; ok {
		t.Error("Skip should not be in byName")
	}
}

func TestStructFieldsBareUsesGoName(t *testing.T) {
	// Untagged fields use the Go field name verbatim (matches encoding/json,
	// which does NOT lowercase).
	type Cfg struct {
		Title string
		Body  string
	}
	sf := getStructFields(reflect.TypeOf(Cfg{}))
	if sf.fields[0].name != "Title" || sf.fields[1].name != "Body" {
		t.Errorf("got names = %v, want [Title Body]",
			[]string{sf.fields[0].name, sf.fields[1].name})
	}
}

func TestStructFieldsCommentTag(t *testing.T) {
	type Cfg struct {
		Port int `json:"port" comment:"Listening port"`
	}
	sf := getStructFields(reflect.TypeOf(Cfg{}))
	if len(sf.fields) != 1 {
		t.Fatalf("len(fields) = %d", len(sf.fields))
	}
	if sf.fields[0].comment != "Listening port" {
		t.Errorf("comment = %q", sf.fields[0].comment)
	}
}

func TestStructFieldsEmbedded(t *testing.T) {
	type Inner struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	type Outer struct {
		Inner
		Name string `json:"name"`
	}
	sf := getStructFields(reflect.TypeOf(Outer{}))
	wantNames := map[string]bool{"host": true, "port": true, "name": true}
	if len(sf.fields) != 3 {
		t.Fatalf("len(fields) = %d, want 3 (got %v)", len(sf.fields), sf.fields)
	}
	for _, fi := range sf.fields {
		if !wantNames[fi.name] {
			t.Errorf("unexpected field %q", fi.name)
		}
	}
}

func TestStructFieldsEmbeddedNamedWins(t *testing.T) {
	// An embedded struct with a named tag is treated as a regular field
	// (not flattened).
	type Inner struct {
		X int `json:"x"`
	}
	type Outer struct {
		Inner `json:"inner"`
	}
	sf := getStructFields(reflect.TypeOf(Outer{}))
	if len(sf.fields) != 1 || sf.fields[0].name != "inner" {
		t.Errorf("named-embedded: got fields = %v", sf.fields)
	}
}

func TestStructFieldsConflictAtSameDepth(t *testing.T) {
	// Two embedded structs whose fields collide at the same depth produce
	// a conflict entry. We use the jsonc tag (rather than json) for the
	// collision so go vet's structtag check (which only inspects the json
	// tag for sibling duplicates) does not flag the test source.
	type A struct {
		FieldA int `jsonc:"x"`
	}
	type B struct {
		FieldB int `jsonc:"x"`
	}
	type Outer struct {
		A
		B
	}
	sf := getStructFields(reflect.TypeOf(Outer{}))
	if len(sf.conflicts) != 1 || sf.conflicts[0] != "x" {
		t.Errorf("conflicts = %v, want [x]", sf.conflicts)
	}
}

func TestStructFieldsShallowerWinsOverDeeper(t *testing.T) {
	// A field at a shallower depth beats an embedded one with the same name.
	type Inner struct {
		Name string `json:"name"`
	}
	type Outer struct {
		Inner
		Name string `json:"name"` // shallower
	}
	sf := getStructFields(reflect.TypeOf(Outer{}))
	if len(sf.fields) != 1 {
		t.Fatalf("len(fields) = %d, want 1", len(sf.fields))
	}
	// The shallower field has index of length 1; the deeper one has length 2.
	if len(sf.fields[0].index) != 1 {
		t.Errorf("expected shallower field, got index %v", sf.fields[0].index)
	}
}

func TestStructFieldsSkipsUnexportedNonAnonymous(t *testing.T) {
	type Cfg struct {
		Public  string `json:"public"`
		private string //nolint:unused
	}
	sf := getStructFields(reflect.TypeOf(Cfg{}))
	if len(sf.fields) != 1 || sf.fields[0].name != "public" {
		t.Errorf("got fields = %v, want only public", sf.fields)
	}
}

func TestStructFieldsCacheReuse(t *testing.T) {
	type Cfg struct {
		Name string `json:"name"`
	}
	t1 := reflect.TypeOf(Cfg{})
	sf1 := getStructFields(t1)
	sf2 := getStructFields(t1)
	if sf1 != sf2 {
		t.Error("getStructFields did not return cached value on second call")
	}
}

func TestStructFieldsJSONCNameOverridesJSON(t *testing.T) {
	type Cfg struct {
		Field string `json:"field" jsonc:"override"`
	}
	sf := getStructFields(reflect.TypeOf(Cfg{}))
	if sf.fields[0].name != "override" {
		t.Errorf("name = %q, want override (jsonc wins)", sf.fields[0].name)
	}
}

func TestStructFieldsLayeredOptions(t *testing.T) {
	type Cfg struct {
		Email string `json:"email,omitempty" jsonc:",required"`
	}
	sf := getStructFields(reflect.TypeOf(Cfg{}))
	fi := sf.fields[0]
	if fi.name != "email" {
		t.Errorf("name = %q", fi.name)
	}
	if !fi.omitEmpty {
		t.Error("omitEmpty not picked up")
	}
	if !fi.required {
		t.Error("required not picked up")
	}
}
