// Package jsonc implements JSONC (JSON with Comments) encoding and decoding.
//
// JSONC extends [RFC 8259] JSON with two human-friendly features:
//   - C-style comments — single-line (//) and block (/* ... */)
//   - Optional trailing commas in arrays and objects (per JWCC)
//
// Every valid JSON document is a valid JSONC document. The encoder produces
// standard JSON by default; comments and trailing commas are opt-in via
// [WithComment] and [WithTrailingComma], or carried through from a parsed AST.
//
// The API follows the conventions of [encoding/json]: use [Marshal] and
// [Unmarshal] for one-shot conversions, [Encoder] and [Decoder] for streaming,
// and struct field tags to control mapping between JSONC keys and Go fields.
//
// For low-level AST access, [Parse] returns a [File] containing a [Node] tree
// that can be inspected, mutated with [Path] queries, and re-serialized with
// [NodeToBytes].
//
// # Drop-In Compatibility With encoding/json
//
// For inputs that are valid RFC 8259 JSON, this package is a drop-in
// replacement for [encoding/json]: identical struct tags, identical Marshal
// and Unmarshal semantics, identical interface contracts ([encoding/json.Marshaler]
// and [encoding/json.Unmarshaler] are honored, [encoding/json.Number] and
// [encoding/json.RawMessage] work in fields). The standard tag is "json"; an
// additional "jsonc" tag is recognized for jsonc-specific options.
//
// Notable defaults that differ from encoding/json:
//   - HTML escaping is off by default (use [WithEscapeHTML] for parity).
//   - time.Duration encodes as int64 nanoseconds, matching encoding/json
//     (use [WithDurationAsString] for "1h30m"-style output).
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
// Comments are scanned, attached to nearby AST nodes (HeadComment, Comment,
// FootComment), and preserved through Format/Minimize and AST round-trips.
// Trailing commas are accepted and silently dropped from RFC 8259 output.
// Use [WithStrictJSON] to reject these extensions and require strict JSON.
//
// # Type Mapping
//
// Decode (JSONC → Go) supports:
//
//	JSONC string  → string, []byte (base64), time.Time, time.Duration,
//	                Number, encoding.TextUnmarshaler, any
//	JSONC number  → all integer/float types, *big.Int, *big.Float, Number,
//	                time.Duration (int64 nanoseconds), any
//	JSONC bool    → bool, any
//	JSONC null    → nil pointer/interface, zero value of any other type
//	JSONC array   → slice, fixed-size array, []any
//	JSONC object  → struct, map[K]V (K = string, integer, or
//	                TextUnmarshaler), map[string]any, [MapSlice]
//
// Encode (Go → JSONC) is the inverse, with [Marshaler] / [MarshalerContext] /
// [encoding/json.Marshaler] / [encoding.TextMarshaler] dispatched in priority
// order before default reflection.
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
// The "json" tag honors stdlib options: omitempty, string, -.
// The "jsonc" tag adds: omitzero, commented, required, default=<value>.
// A separate "comment" tag attaches a head comment to the field:
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
// [Format] pretty-prints JSONC, preserving comments and trailing commas with
// configurable indentation. [Minimize] compacts JSONC, also preserving
// comments. [ToJSON] (and its alias [StripComments]) strips comments and
// trailing commas to produce strict RFC 8259 JSON, useful when piping into
// systems that only accept stdlib JSON:
//
//	std, err := jsonc.ToJSON(data)
//	if err != nil { return err }
//	return json.Unmarshal(std, &v)
//
// # Path Queries
//
// [PathString] compiles a rotini-style expression ($, .name, ["name"], [N],
// [*], ..name) into a [Path] that selects nodes from an AST. [PathPointer]
// compiles an [RFC 6901] JSON Pointer. Once compiled, a Path supports
// [Path.Read] / [Path.ReadFirst] / [Path.ReadString], plus structural
// mutations [Path.Replace], [Path.Append], and [Path.Delete].
//
// # JSON Patch
//
// [ParsePatch] decodes an [RFC 6902] JSON Patch document into a [Patch], and
// [Patch.Apply] applies all operations (add, remove, replace, move, copy,
// test) atomically against an AST. The input AST is never mutated — Apply
// works on a deep clone.
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
