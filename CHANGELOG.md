# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] — Unreleased

Initial release of the `go-rotini/jsonc` package — a Go JSONC (JSON with Comments) encoding and decoding library, JWCC-compatible (line + block comments, optional trailing commas), positioned as a drop-in replacement for `encoding/json`.

### Scanner / Parser
- Synchronous UTF-8 scanner with full RFC 8259 string-escape grammar, JSONC line and block comments, well-formed surrogate pair handling, and BOM stripping (UTF-16/32 BOMs are rejected with descriptive errors).
- Recursive-descent parser producing an AST with nodes for `ObjectNode`, `ArrayNode`, `KeyValueNode`, scalar kinds, and orphan `CommentNode`. Comments are attached as `HeadComment` / `Comment` (inline) / `FootComment`.
- Source-byte preservation on container nodes — `RawValue` for an object or array returns the verbatim source slice, including comments and trailing commas (as documented).
- DoS protection: configurable max nesting depth (default 100), max keys, max document size, max node count.
- `WithStrictJSON()` decode option rejects comments and trailing commas for strict RFC 8259 conformance.

### Decoder
- `Unmarshal`, `UnmarshalWithOptions`, generic `UnmarshalTo[T]`, `DecodeFile`.
- Streaming `Decoder` matching `encoding/json.Decoder` semantics: `Decode`, `DecodeContext`, `More`, `InputOffset`, `SetContext`.
- Type mappings: scalars → all int/uint/float widths; `*big.Int`, `*big.Float`, `Number` (alias of `json.Number`), `time.Duration` (int64 nanoseconds — matches stdlib — or string with `WithDurationAsString`), `time.Time` (RFC 3339), `[]byte` (base64), `encoding.TextUnmarshaler`-keyed maps.
- Unmarshaler dispatch priority: `UnmarshalerContext` → `Unmarshaler` → `BytesUnmarshaler` → `json.Unmarshaler` → `TextUnmarshaler` → custom (registered via `WithCustomUnmarshaler[T]`) → default reflection. Special types (`RawValue`, `json.RawMessage`, `json.Number`, `time.Time`) bypass interface dispatch to preserve verbatim source.
- Strict mode (`WithStrict`) rejects unknown fields; `MaxKeys`, `MaxDepth`, `MaxDocumentSize`, `MaxNodes` enforce DoS limits.
- Default values from `default=` tag (when `WithDefaults` is set), required-field enforcement (null satisfies presence per RFC).

### Encoder
- `Marshal`, `MarshalIndent`, `MarshalWithOptions`, generic `MarshalTo[T]`, `EncodeFile`.
- Streaming `Encoder`: `Encode`, `EncodeContext`, `SetIndent`, `SetEscapeHTML`, `SetContext`.
- Marshaler dispatch priority: `MarshalerContext` → `Marshaler` → `json.Marshaler` → `TextMarshaler` → custom → default reflection.
- Cycle detection for self-referential pointer graphs.
- Inf/NaN encoding rejected with `ErrUnsupportedValue` (matches stdlib).
- Map key ordering: lexicographic by default (deterministic output); `MapSlice` preserves insertion order.
- Struct field ordering matches `encoding/json` (declaration order, tag-renamed fields keep their position).
- HTML escaping is **off** by default (this differs from `encoding/json` for cleaner output) — use `WithEscapeHTML(true)` for stdlib parity.
- `WithComment(map[string][]Comment)` — path-keyed comment injection for reflection encoding. Path syntax matches the encoder's path stack (dot for object members, bracket for array indices: `server.port`, `tags[0]`, `users.alice.email`). Supports head, line, and foot positions. Honored only in multi-line, non-strict-output mode.
- Comment emission hardening — inline comments fall back to `/* … */` when their text contains a newline or carriage return, instead of leaking past the value through a `//` comment that the embedded line terminator would close. `writeCommentBlock` normalizes `\r\n` and bare `\r` to `\n` before splitting. Block-form output sanitizes any embedded `*/` to `* /` so user-supplied `Comment.Text` cannot prematurely close the surrounding block. (Fixes a `FuzzFormat`-found regression on input `{}/*\r0*/`.)

### Whole-Document Helpers
- `Format` — pretty-print JSONC, preserve comments, configurable indent (default 2 spaces).
- `Minimize` — compact JSONC, preserve comments by default.
- `ToJSON` / `StripComments` — strip comments and trailing commas to produce strict RFC 8259 JSON.

### Path & Patch
- `PathString` — rotini-style expression compiler (`$`, `.name`, `["name"]`, `[N]`, `[*]`, `..name`).
- `PathPointer` — RFC 6901 JSON Pointer compiler with `~0` / `~1` escape decoding.
- Path operations: `Read`, `ReadFirst`, `ReadString`, `ReadPositions`, `Replace`, `Append`, `Delete`.
- `ParsePatch` + `Patch.Apply` — full RFC 6902 JSON Patch (add, remove, replace, move, copy, test); operates on a deep clone so the input AST is never mutated.

### Struct Tags
- `json` tag honored for stdlib compatibility (`omitempty`, `string`, `-`).
- `jsonc` tag adds JSONC-specific options: `omitzero`, `commented`, `required`, `default=<value>`.
- `comment:"..."` tag attaches a head comment to a field.

### Error Handling
- Typed errors with `errors.Is` support: `ErrSyntax`, `ErrType`, `ErrUnknownField`, `ErrDuplicateKey`, `ErrValidation`, `ErrDefault`, `ErrOverflow`, `ErrStrictJSON`, `ErrPathSyntax`, `ErrPathNotFound`, `ErrPointerSyntax`, `ErrPatchSyntax`, `ErrNilPointer`, `ErrDocumentSize`, `ErrUnsupportedValue`.
- `FormatError` produces a human-readable message with source line and column pointer (optional ANSI color).

### Other
- `Valid` for quick syntax validation without full decoding.
- `MapSlice` / `MapItem` for ordered map decoding via `WithOrderedMap`.
- `RawValue` for deferred decoding; `json.RawMessage` interop in fields.
- Custom marshaler/unmarshaler registration via `WithCustomMarshaler[T]` / `WithCustomUnmarshaler[T]`.
- Struct validation via `StructValidator` interface.

### Testing
- Six fuzz targets: `FuzzUnmarshal`, `FuzzScanner`, `FuzzValid`, `FuzzRoundTrip`, `FuzzFormat`, `FuzzMinimize`.
- Acceptance fixtures: tsconfig.json, vscode-settings.json, kitchen-sink.jsonc.
- JSONTestSuite conformance harness (gated on cloned testdata; clones the latest of `master` via `make clone-test-suite`, matching the yaml/toml-package pattern of tracking the upstream test suite head).
- Stdlib compatibility tests verifying parity with `encoding/json` for the standard JSON subset.
- 15 runnable Examples for godoc rendering.

[0.1.0]: https://github.com/go-rotini/jsonc/releases/tag/v0.1.0
