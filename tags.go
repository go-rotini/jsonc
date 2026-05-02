package jsonc

import (
	"reflect"
	"strings"
	"sync"
)

// fieldInfo is the parsed metadata for a single struct field. It is the
// merged result of the `json` and `jsonc` tags plus the separate `comment`
// tag.
type fieldInfo struct {
	name         string
	index        []int
	omitEmpty    bool
	omitZero     bool
	asString     bool // json:"...,string" — encode primitives as JSON string
	commented    bool
	required     bool
	skip         bool
	defaultValue string
	hasDefault   bool
	comment      string
}

// parseTagPair parses both the json and jsonc tag values for a single
// struct field and returns the merged metadata. The precedence rules are:
//
//   - field name: jsonc tag name wins if non-empty; else json tag name; else
//     the Go field name (caller fills in if both tag names are empty).
//   - "-": skip if either tag has it.
//   - "omitempty", "string": honored from either tag.
//   - "omitzero", "commented", "required", "default=<value>": honored from
//     the jsonc tag only (silently ignored if placed in the json tag —
//     stdlib won't recognize them anyway).
func parseTagPair(jsonTag, jsoncTag string) fieldInfo {
	var fi fieldInfo

	jsonName, jsonOpts := splitTag(jsonTag)
	jsoncName, jsoncOpts := splitTag(jsoncTag)

	// '-' in either tag means skip.
	if jsonName == "-" || jsoncName == "-" {
		fi.skip = true
		return fi
	}

	// Field name: jsonc wins, else json, else empty (caller fills in).
	switch {
	case jsoncName != "":
		fi.name = jsoncName
	case jsonName != "":
		fi.name = jsonName
	}

	// Stdlib-compatible options honored from either tag.
	for _, opts := range [][]string{jsonOpts, jsoncOpts} {
		for _, opt := range opts {
			switch opt {
			case "omitempty":
				fi.omitEmpty = true
			case "string":
				fi.asString = true
			}
		}
	}

	// JSONC-only options honored from the jsonc tag.
	for _, opt := range jsoncOpts {
		switch opt {
		case "omitzero":
			fi.omitZero = true
		case "commented":
			fi.commented = true
		case "required":
			fi.required = true
		default:
			if v, ok := strings.CutPrefix(opt, "default="); ok {
				fi.defaultValue = v
				fi.hasDefault = true
			}
		}
	}

	return fi
}

// splitTag splits a tag value (the `json:"..."` quoted contents) into its
// name and option list. Empty string returns ("", nil).
func splitTag(tag string) (name string, opts []string) {
	if tag == "" {
		return "", nil
	}
	parts := strings.Split(tag, ",")
	return parts[0], parts[1:]
}

// structFields holds the parsed metadata for a struct type, used by both
// encoder and decoder. byName maps field names (case-sensitive) to indices
// into fields. conflicts records duplicate field names at the same depth
// (these fields are treated as if they don't exist, matching encoding/json).
type structFields struct {
	fields    []fieldInfo
	byName    map[string]int
	conflicts []string
}

// structFieldCache memoizes structFields lookups by reflect.Type. It is
// safe for concurrent use.
var structFieldCache sync.Map

// getStructFields returns the (cached) structFields for t, computing them
// on first lookup. t must be a struct type.
func getStructFields(t reflect.Type) *structFields {
	if cached, ok := structFieldCache.Load(t); ok {
		sf, _ := cached.(*structFields)
		return sf
	}
	sf := &structFields{
		byName: make(map[string]int),
	}
	collectFields(t, nil, sf)
	structFieldCache.Store(t, sf)
	return sf
}

// collectFields walks a struct type (recursively for embedded structs) and
// populates sf with field metadata. The traversal order matches
// encoding/json: outer fields before inner, declaration order preserved
// per level. Anonymous (embedded) struct fields are inlined unless they
// have an explicit tag name.
func collectFields(t reflect.Type, index []int, sf *structFields) {
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() && !f.Anonymous {
			continue
		}

		fi := parseTagPair(f.Tag.Get("json"), f.Tag.Get("jsonc"))
		if fi.skip {
			continue
		}

		if commentTag := f.Tag.Get("comment"); commentTag != "" {
			fi.comment = commentTag
		}

		fi.index = make([]int, len(index)+1)
		copy(fi.index, index)
		fi.index[len(index)] = i

		// Anonymous (embedded) struct fields with no explicit name are
		// inlined into the parent. This matches encoding/json behavior.
		if f.Anonymous && fi.name == "" {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				collectFields(ft, fi.index, sf)
				continue
			}
		}

		// No name yet — use the Go field name verbatim (matches stdlib
		// encoding/json, which does NOT lowercase field names).
		if fi.name == "" {
			fi.name = f.Name
		}

		if idx, exists := sf.byName[fi.name]; exists {
			existing := sf.fields[idx]
			switch {
			case len(fi.index) == len(existing.index):
				// Same depth — both lose, mark as conflict.
				sf.conflicts = append(sf.conflicts, fi.name)
			case len(fi.index) < len(existing.index):
				// Shallower wins.
				sf.fields[idx] = fi
			}
			continue
		}

		sf.fields = append(sf.fields, fi)
		sf.byName[fi.name] = len(sf.fields) - 1
	}
}
