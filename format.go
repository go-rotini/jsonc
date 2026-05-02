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
