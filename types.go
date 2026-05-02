package jsonc

import "encoding/json"

// MapSlice is an ordered slice of key-value pairs. It is used as the decoded
// representation of JSONC objects when [WithOrderedMap] is enabled, preserving
// the original member order that a plain map[string]any would lose.
type MapSlice []MapItem

// MapItem is a single key-value pair within a [MapSlice]. JSON object keys are
// always strings, so [MapItem.Key] is typed as string (unlike the YAML and
// TOML packages which permit any-typed keys).
type MapItem struct {
	// Key is the unescaped JSON object member name.
	Key string
	// Value is the decoded value for this member.
	Value any
}

// Number is an alias for [encoding/json.Number]. It preserves the original
// source text of a JSONC number, allowing callers to convert to int64 or
// float64 on demand without losing precision.
//
// The alias lets callers use a single type across both packages — values of
// type [encoding/json.Number] and jsonc.Number are interchangeable.
type Number = json.Number

// MapKeyOrder controls how object keys are emitted by the encoder.
type MapKeyOrder int

const (
	// MapKeyOrderLexicographic sorts native map[K]V keys lexicographically
	// when encoding. This matches the default behavior of
	// [encoding/json.Marshal].
	MapKeyOrderLexicographic MapKeyOrder = iota

	// MapKeyOrderInsertion preserves the order of [MapSlice] entries on
	// encode. It has no effect on native map[K]V values, which Go iterates
	// in randomized order — for those, the encoder always falls back to
	// lexicographic ordering for stability.
	MapKeyOrderInsertion
)
