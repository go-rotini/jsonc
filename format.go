package jsonc

// ToJSON converts JSONC input to standard RFC 8259 JSON by removing
// comments and trailing commas. The structure and values are preserved
// unchanged; only the JSONC extensions are stripped.
//
// This is the most common conversion when piping JSONC into stdlib
// [encoding/json.Unmarshal]:
//
//	std, err := jsonc.ToJSON(data)
//	if err != nil { return err }
//	return json.Unmarshal(std, &v)
func ToJSON(data []byte) ([]byte, error) {
	root, err := parseDocument(data, defaultDecodeOptions())
	if err != nil {
		return nil, err
	}
	opts := defaultEncodeOptions()
	opts.strictJSONOutput = true
	enc := newNodeEncoder(opts)
	return enc.encodeNode(root)
}

// StripComments is an alias for [ToJSON]. Provided for discoverability —
// callers searching for "strip comments from JSON" find it readily.
func StripComments(data []byte) ([]byte, error) {
	return ToJSON(data)
}

// Format pretty-prints JSONC input. Comments and trailing commas are
// preserved; the output uses two-space indentation by default. Pass
// additional [EncodeOption] values to customize the output (e.g.,
// [WithIndent], [WithIndentN], [WithTrailingComma]).
//
// Format is a whole-document transformation: the input must be a single
// JSONC value (with optional surrounding whitespace and comments). To
// produce strict RFC 8259 JSON, use [ToJSON] instead.
func Format(data []byte, opts ...EncodeOption) ([]byte, error) {
	root, err := parseDocument(data, defaultDecodeOptions())
	if err != nil {
		return nil, err
	}
	o := defaultEncodeOptions()
	o.indent = "  " // default — overridable by opts
	for _, opt := range opts {
		opt(o)
	}
	enc := newNodeEncoder(o)
	return enc.encodeNode(root)
}

// Minimize compacts JSONC input by removing redundant whitespace.
// Comments are preserved by default; pass [WithStrictJSONOutput](true)
// (equivalent to using [ToJSON]) to drop them. Trailing commas are dropped
// in compact output regardless.
func Minimize(data []byte, opts ...EncodeOption) ([]byte, error) {
	root, err := parseDocument(data, defaultDecodeOptions())
	if err != nil {
		return nil, err
	}
	o := defaultEncodeOptions()
	o.indent = "" // explicit: compact output
	for _, opt := range opts {
		opt(o)
	}
	enc := newNodeEncoder(o)
	return enc.encodeNode(root)
}
