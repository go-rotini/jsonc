# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Added
- JSONC (JSON with Comments) scanner, parser, encoder, and decoder
- JWCC superset support: line and block comments, optional trailing commas
- Marshal/Unmarshal with struct tag support (both `json` and `jsonc` tags)
- Generic UnmarshalTo[T] API
- Streaming Encoder/Decoder matching `encoding/json` semantics
- AST access via Parse, Walk, Filter, and NodeToBytes with comment preservation
- Comment-preserving round-trips through the AST
- Path query engine with rotini-style (`$.foo[0]`) and RFC 6901 JSON Pointer (`/foo/0`) syntaxes
- RFC 6902 JSON Patch application via `Path.Patch`
- Format, Minimize, ToJSON / StripComments helpers
- MapSlice/MapItem for ordered map support
- RawValue for deferred decoding (also accepts `json.RawMessage`)
- DecodeFile for reading JSONC files from disk
- Custom marshaler/unmarshaler interfaces and option-based registration
- Struct validation via StructValidator interface
- Drop-in compatibility with `encoding/json` for the standard JSON subset
- Two strict modes: `WithStrict()` for unknown fields and `WithStrictJSON()` (decode) / `WithStrictJSONOutput()` (encode) for RFC 8259 conformance
- Fuzz testing corpus
- JSONTestSuite and HuJSON conformance tests
- DoS protection defaults; max nesting depth 100, max keys/document size/node count configurable
