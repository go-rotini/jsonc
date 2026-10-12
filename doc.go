// Package jsonc implements JSONC (JSON with Comments) encoding and decoding.
//
// JSONC extends [RFC 8259] JSON with two human-friendly features:
//   - C-style comments — single-line (//) and block (/* ... */)
//   - Optional trailing commas in arrays and objects (per JWCC)
//
// Every valid JSON document is a valid JSONC document. The encoder produces
// standard JSON by default. Comments are opt-in, from [WithComment], the
// "comment" struct tag, or a parsed AST; trailing commas are opt-in via
// [WithTrailingComma].
//
// The API follows the conventions of [encoding/json]: use [Marshal] and
// [Unmarshal] for one-shot conversions, [Encoder] and [Decoder] for streams,
// and struct field tags to control mapping between JSONC keys and Go fields.
//
// For low-level AST access, [Parse] returns a [File] containing a [Node] tree
// that can be inspected, mutated with [Path] queries, and re-serialized with
// [NodeToBytes].
//
// # Compatibility With encoding/json
//
// For RFC 8259 JSON input, this package is close to a drop-in replacement
// for [encoding/json]: it reads the same "json" struct tags, honors
// [encoding/json.Marshaler] and [encoding/json.Unmarshaler], and supports
// [encoding/json.Number] and [encoding/json.RawMessage] fields. An
// additional "jsonc" tag carries jsonc-specific options.
//
// Differences from encoding/json:
//   - Duplicate object keys are rejected with a [DuplicateKeyError]; use
//     [WithAllowDuplicateKeys] for last-wins behavior.
//   - HTML characters are not escaped; use [WithEscapeHTML].
//   - Compact output puts a space after ':' and ',' ({"a": 1, "b": 2}).
//   - JSON null sets a non-pointer target to its zero value rather than
//     leaving it unchanged.
//   - A JSON array decoded into a Go array must have exactly the array's
//     length.
//
// # JSONC Extensions
//
// On top of the JSON grammar, this package accepts:
//
//	{
//	    // line comment to end of line
//	    "key": "value", /* block comment */
//	    "list": [1, 2, 3,], // trailing comma
//	}
//
// The parser attaches comments to nearby AST nodes ([Node.HeadComment],
// [Node.Comment], [Node.FootComment]). Comments are written back only in
// multi-line output: [Format] keeps them in place, as // comments unless a
// comment spans lines, while [Minimize] and other compact output drop them.
// A comment between a key and its value has no place in the AST and is
// dropped, with a warning in [File.Warnings]. Trailing commas are accepted on input and are not
// carried through; [WithTrailingComma] adds them to multi-line output. Use
// [WithStrictJSON] to reject these extensions and require strict JSON.
//
// # Type Mapping
//
// Decode (JSONC → Go) supports:
//
//	JSONC string  → string, []byte (base64), time.Time (RFC 3339),
//	                time.Duration ("1h30m"), encoding.TextUnmarshaler, any
//	JSONC number  → all integer/float types, big.Int, big.Float, Number,
//	                time.Duration (int64 nanoseconds), any
//	JSONC bool    → bool, any
//	JSONC null    → nil pointer/interface, zero value of any other type
//	JSONC array   → slice, fixed-size array, []any
//	JSONC object  → struct, map[K]V (K = string, integer, or
//	                TextUnmarshaler), map[string]any, MapSlice
//
// Numbers decoded into any become float64, or [Number] with [WithUseNumber];
// objects become map[string]any, or [MapSlice] with [WithOrderedMap].
//
// Encode (Go → JSONC) is the inverse, with [MarshalerContext], [Marshaler],
// [encoding/json.Marshaler] and [encoding.TextMarshaler] tried in that order
// before default reflection. time.Duration encodes as int64 nanoseconds, or
// as a string with [WithDurationAsString].
//
// # Struct Tags
//
// Struct fields may be annotated with "json" tags (recommended for stdlib
// compatibility) or "jsonc" tags (for jsonc-specific options). When both are
// present, the field name comes from "jsonc" if specified, else "json"; the
// option lists are layered.
//
//	type Config struct {
//	    Name     string `json:"name,omitempty"`
//	    Email    string `json:"email" jsonc:",required"`
//	    Port     int    `jsonc:"port,default=8080"`
//	    Internal string `json:"-"`
//	}
//
// Either tag accepts omitempty, omitzero, string and "-". As in
// encoding/json, omitzero omits a field whose IsZero method reports true, or
// which is the zero value when it has none. The "jsonc" tag adds:
//   - required: decoding fails with a [SyntaxError] when the key is absent
//     (an explicit null counts as present).
//   - default=<value>: the value applied when the key is absent; it takes
//     effect only with [WithDefaults].
//   - commented: in multi-line output, write the whole member inside /* */,
//     so it shows as a hint but is not decoded.
//
// A separate "comment" tag attaches a head comment to the field in
// multi-line output:
//
//	type Server struct {
//	    Port int `json:"port" comment:"Listening port"`
//	}
//
// # Custom Marshalers
//
// Types can implement [Marshaler], [MarshalerContext], [Unmarshaler],
// [BytesUnmarshaler], or [UnmarshalerContext] for jsonc-aware serialization.
// Stdlib [encoding/json.Marshaler] and [encoding/json.Unmarshaler] are also
// honored at lower priority.
//
// # Strict Modes
//
// Two independent strictness modes are supported:
//
//   - [WithStrict] — return [UnknownFieldError] if a JSONC key has no
//     corresponding struct field (matches encoding/json's
//     DisallowUnknownFields).
//   - [WithStrictJSON] — reject comments and trailing commas during parsing
//     (RFC 8259 conformance). The encode-side counterpart is
//     [WithStrictJSONOutput].
//
// # Whole-Document Transformations
//
// [Format] pretty-prints JSONC, keeping comments, with configurable
// indentation. [Minimize] compacts JSONC onto one line, dropping comments.
// [ToJSON] (and its alias [StripComments]) strips comments and trailing
// commas to produce strict RFC 8259 JSON, useful when piping into systems
// that only accept standard JSON:
//
//	std, err := jsonc.ToJSON(data)
//	if err != nil { return err }
//	return json.Unmarshal(std, &v)
//
// # Path Queries
//
// [PathString] compiles a small JSONPath-like expression ($, .name,
// ["name"], [N], [*], ..name) into a [Path] that selects nodes from an AST. [PathPointer]
// compiles an [RFC 6901] JSON Pointer. Once compiled, a Path supports
// [Path.Read] / [Path.ReadFirst] / [Path.ReadString], plus structural
// mutations [Path.Replace], [Path.Append], and [Path.Delete].
//
// # JSON Patch
//
// [ParsePatch] decodes an [RFC 6902] JSON Patch document into a [Patch], and
// [Patch.Apply] applies all operations (add, remove, replace, move, copy,
// test) atomically against an AST: the input AST is never modified, and a
// failing operation returns an error and no result. A failed "test" returns
// an error wrapping [ErrPatchTestFailed]; it compares values as JSON, so
// numbers compare by value.
//
// # Raw Values
//
// [RawValue] holds undecoded JSONC bytes, analogous to [encoding/json.RawMessage].
// Use it as a struct field to defer decoding, or to round-trip arbitrary
// nested JSONC without losing comments and trailing commas. Call
// [RawValue.Standardize] to convert the raw bytes to RFC 8259 JSON.
//
// # Error Handling
//
// Decoding errors are returned as typed values that support [errors.Is]:
//
//	if errors.Is(err, jsonc.ErrSyntax) { ... }
//	if errors.Is(err, jsonc.ErrDuplicateKey) { ... }
//
// Use [FormatError] to produce a human-readable error with a source pointer.
//
// [RFC 8259]: https://www.rfc-editor.org/rfc/rfc8259
// [RFC 6901]: https://www.rfc-editor.org/rfc/rfc6901
// [RFC 6902]: https://www.rfc-editor.org/rfc/rfc6902
package jsonc
