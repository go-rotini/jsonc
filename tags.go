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
	tagged       bool // the name comes from a tag
}

// parseTagPair parses both the json and jsonc tag values for a single
// struct field and returns the merged metadata. The precedence rules are:
//
//   - field name: jsonc tag name wins if non-empty; else json tag name; else
//     the Go field name (caller fills in if both tag names are empty).
//   - "-": skip if either tag has it.
//   - "omitempty", "omitzero", "string": honored from either tag.
//   - "commented", "required", "default=<value>": honored from the jsonc tag
//     only (ignored in the json tag).
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
			case "omitzero":
				fi.omitZero = true
			case "string":
				fi.asString = true
			}
		}
	}

	// JSONC-only options honored from the jsonc tag.
	for _, opt := range jsoncOpts {
		switch opt {
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
// into fields. Names that several fields share at the same depth have no
// entry, matching encoding/json.
type structFields struct {
	fields []fieldInfo
	byName map[string]int
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
	collectFields(t, sf)
	structFieldCache.Store(t, sf)
	return sf
}

// collectFields walks a struct type (recursively for embedded structs) and
// populates sf with field metadata, following encoding/json: fields are
// ordered by their position in the struct, embedded structs without a tag
// name are inlined, and of several fields with the same name the
// shallowest wins. Several at the same shallowest depth are all dropped,
// unless exactly one of them is named by a tag, which then wins.
func collectFields(t reflect.Type, sf *structFields) {
	var all []fieldInfo
	collectCandidates(t, nil, map[reflect.Type]bool{t: true}, &all)

	// Group by name, keeping the first-seen order of names.
	groups := make(map[string][]int)
	var names []string
	for i, fi := range all {
		if _, ok := groups[fi.name]; !ok {
			names = append(names, fi.name)
		}
		groups[fi.name] = append(groups[fi.name], i)
	}
	keep := make([]bool, len(all))
	for _, name := range names {
		if winner, ok := dominantField(all, groups[name]); ok {
			keep[winner] = true
		}
	}
	for i, fi := range all {
		if keep[i] {
			sf.fields = append(sf.fields, fi)
			sf.byName[fi.name] = len(sf.fields) - 1
		}
	}
}

// dominantField returns the index in all of the field that wins among the
// same-named candidates idx, or false if none does.
func dominantField(all []fieldInfo, idx []int) (int, bool) {
	minDepth := len(all[idx[0]].index)
	for _, i := range idx[1:] {
		minDepth = min(minDepth, len(all[i].index))
	}
	winner, count, tagged := -1, 0, 0
	for _, i := range idx {
		if len(all[i].index) != minDepth {
			continue
		}
		count++
		if all[i].tagged {
			tagged++
			winner = i
		} else if count == 1 {
			winner = i
		}
	}
	switch {
	case count == 1:
		return winner, true
	case tagged == 1:
		return winner, true
	default:
		return -1, false
	}
}

// collectCandidates appends every field of t, inlining embedded structs,
// in struct order. onPath holds the struct types being walked, so a type
// that embeds itself is not walked again.
func collectCandidates(t reflect.Type, index []int, onPath map[reflect.Type]bool, out *[]fieldInfo) {
	for i := range t.NumField() {
		f := t.Field(i)
		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if !f.IsExported() && (!f.Anonymous || ft.Kind() != reflect.Struct) {
			// Unexported fields are ignored, except embedded structs, whose
			// exported fields are promoted.
			continue
		}

		fi := parseTagPair(f.Tag.Get("json"), f.Tag.Get("jsonc"))
		if fi.skip {
			continue
		}
		fi.tagged = fi.name != ""

		if commentTag := f.Tag.Get("comment"); commentTag != "" {
			fi.comment = commentTag
		}

		fi.index = make([]int, len(index)+1)
		copy(fi.index, index)
		fi.index[len(index)] = i

		if !f.IsExported() && fi.name != "" {
			// A named unexported embedded struct is a field that cannot be
			// set or read.
			continue
		}

		// Embedded structs with no explicit name are inlined into the
		// parent.
		if f.Anonymous && ft.Kind() == reflect.Struct && fi.name == "" {
			if !onPath[ft] {
				onPath[ft] = true
				collectCandidates(ft, fi.index, onPath, out)
				delete(onPath, ft)
			}
			continue
		}

		// No name yet — use the Go field name verbatim (matches stdlib
		// encoding/json, which does NOT lowercase field names).
		if fi.name == "" {
			fi.name = f.Name
		}
		*out = append(*out, fi)
	}
}
