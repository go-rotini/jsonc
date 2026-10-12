package jsonc

import "encoding/json"

// MapSlice is an ordered slice of key-value pairs. Objects decoded into any
// become MapSlice values when [WithOrderedMap] is set, preserving member
// order. The encoder always writes a MapSlice in slice order.
type MapSlice []MapItem

// MapItem is a single key-value pair within a [MapSlice].
type MapItem struct {
	// Key is the unescaped JSON object member name.
	Key string
	// Value is the decoded value for this member.
	Value any
}

// Number is an alias for [encoding/json.Number]. It holds the source text
// of a JSONC number, so callers can convert to int64 or float64 on demand
// without losing precision.
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
