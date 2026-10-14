package jsonc

// ToJSON converts JSONC input to compact RFC 8259 JSON by removing
// comments and trailing commas. Values keep their source text.
//
// Use it to feed JSONC to [encoding/json.Unmarshal]:
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

// StripComments is an alias for [ToJSON].
func StripComments(data []byte) ([]byte, error) {
	return ToJSON(data)
}

// Format pretty-prints JSONC input with two-space indentation, keeping
// comments. Trailing commas in the input are not kept; pass
// [WithTrailingComma] to add them. Other [EncodeOption] values, such as
// [WithIndent] or [WithIndentN], customize the output.
//
// The input must be a single JSONC value, optionally surrounded by
// whitespace and comments. To produce strict RFC 8259 JSON, use [ToJSON].
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

// Minimize compacts JSONC input onto a single line. Comments and trailing
// commas are dropped, since compact output has nowhere to put them, so the
// result is RFC 8259 JSON. Members are separated by ", " and keys by ": ".
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
