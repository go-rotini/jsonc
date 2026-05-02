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
// and Unmarshal semantics, identical interface contracts (json.Marshaler and
// json.Unmarshaler are honored, json.Number and json.RawMessage work in
// fields). The standard tag is "json"; an additional "jsonc" tag is recognized
// for jsonc-specific options.
//
// Notable defaults that differ from encoding/json:
//   - HTML escaping is off by default (use [WithEscapeHTML] for parity).
//   - time.Duration encodes as int64 nanoseconds, matching encoding/json
//     (use [WithDurationAsString] for "1h30m"-style output).
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
package jsonc
