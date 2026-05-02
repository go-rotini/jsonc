# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## 0.1.0

### Added
- JSONC scanner, parser, encoder, and decoder
- JWCC superset by default: line and block comments, optional trailing commas
- Drop-in compatibility with `encoding/json` for the standard JSON subset
- Marshal/Unmarshal with struct tag support (`json` and `jsonc` tags)
- Generic UnmarshalTo[T] and MarshalTo[T] APIs
- Streaming Encoder/Decoder
- Path query engine with rotini-style (`$.foo[0]`) and RFC 6901 JSON Pointer (`/foo/0`) syntaxes
- RFC 6902 JSON Patch application
- Format, Minimize, ToJSON / StripComments helpers
- AST access via Parse, Walk, Filter, and NodeToBytes
- Comment preservation through AST round-trips and RawValue
- MapSlice/MapItem for ordered map support
- RawValue for deferred decoding (interoperates with `json.RawMessage`)
- DecodeFile and EncodeFile for reading and writing JSONC files from disk
- Custom marshaler/unmarshaler interfaces and option-based registration
- Struct validation via StructValidator interface
- Strict modes: `WithStrict()` for unknown fields and `WithStrictJSON()` / `WithStrictJSONOutput()` for RFC 8259 conformance
- Fuzz testing corpus
- JSONTestSuite conformance tests
- DoS protection defaults; max nesting depth 100, max keys, document size, and node count configurable
