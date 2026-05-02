# go-rotini/jsonc

A Go JSONC (JSON with Comments) encoding and decoding package, JWCC-compatible (line + block comments, optional trailing commas), positioned as a drop-in replacement for [`encoding/json`](https://pkg.go.dev/encoding/json) backed by the [JSONTestSuite](https://github.com/nst/JSONTestSuite) and [HuJSON](https://github.com/tailscale/hujson) conformance fixtures.

This package is used as the default JSONC support package for [rotini](https://github.com/go-rotini/rotini).

## Features

- Full [JSONC](https://jsonc.org/) support: `//` line comments, `/* ... */` block comments, trailing commas
- JWCC-superset by default; opt-in `WithStrictJSON` for [RFC 8259](https://www.rfc-editor.org/rfc/rfc8259) conformance
- Drop-in compatibility with `encoding/json` for the standard JSON subset (identical struct tags, `json.Marshaler`/`json.Unmarshaler` honored, `json.Number`/`json.RawMessage` work in fields)
- Comment-preserving round-trips through the AST and `RawValue`
- Generic `UnmarshalTo[T]` / `MarshalTo[T]` APIs and type-safe custom marshaler/unmarshaler registration
- Streaming `Encoder` / `Decoder` matching `encoding/json` semantics (`More()`, `InputOffset()`)
- Struct field tags: `omitempty`, `string` (stdlib), `omitzero`, `commented`, `required`, `default=<value>` (jsonc extensions)
- `comment:"text"` separate tag for emitting head comments
- Encode options: indent, HTML escape, array multiline, comments, trailing commas, custom marshalers
- Decode options: strict mode, strict-JSON mode, ordered maps, defaults, validators, custom unmarshalers, `UseNumber`
- AST access via `Parse`, `Walk`, `Filter`, and `Node` tree manipulation with `NodeToBytes`
- Path query engine with two syntaxes: rotini-style `$.foo[0]` (`PathString`) and RFC 6901 JSON Pointer `/foo/0` (`PathPointer`)
- RFC 6902 JSON Patch application via `Patch.Apply`
- Whole-document helpers: `Format`, `Minimize`, `ToJSON` / `StripComments`
- `Valid` function for quick syntax validation without full decoding
- `FormatError` for human-readable error output with source line and column pointer
- Context-aware encoding/decoding via `EncodeContext` / `DecodeContext`
- `MapSlice` / `MapItem` for ordered map support
- Deferred decoding with `RawValue` (interoperates with `json.RawMessage`)
- `DecodeFile` / `EncodeFile` convenience for reading and writing JSONC files
- DoS protection: depth limiting, key count limiting, document size limiting, node count limiting

## Installation

```bash
go get github.com/go-rotini/jsonc
```

Requires Go 1.23 or later.

## Quick Start

```go
package main

import (
	"fmt"
	"log"

	"github.com/go-rotini/jsonc"
)

type Config struct {
	Title    string   `json:"title"`
	Server   Server   `json:"server"`
	Database Database `json:"database"`
}

type Server struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type Database struct {
	Hosts   []string `json:"hosts"`
	Enabled bool     `json:"enabled"`
}

func main() {
	// Decode JSONC (with comments and trailing commas) into a struct.
	src := []byte(`{
		// Top-level configuration
		"title": "Example",
		"server": {
			"host": "localhost",
			"port": 8080,  /* default port */
		},
		"database": {
			"hosts": ["db1", "db2", "db3",],
			"enabled": true,
		},
	}`)

	var cfg Config
	if err := jsonc.Unmarshal(src, &cfg); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%+v\n", cfg)

	// Generic unmarshal (no pointer required).
	cfg2, err := jsonc.UnmarshalTo[Config](src)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%+v\n", cfg2)

	// Marshal with two-space indent.
	out, err := jsonc.MarshalIndent(cfg, "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(out))
}
```

## Drop-in for `encoding/json`

For inputs that are valid RFC 8259 JSON, `jsonc` is a drop-in replacement:

```go
// Before
import "encoding/json"
err := json.Unmarshal(data, &v)

// After
import "github.com/go-rotini/jsonc"
err := jsonc.Unmarshal(data, &v)
```

Tags, custom marshalers, and `json.Number` / `json.RawMessage` continue to work unchanged. The only behavioral differences:

- HTML escaping is off by default (use `WithEscapeHTML(true)` for parity).
- `time.Duration` encodes as int64 nanoseconds, matching `encoding/json` (use `WithDurationAsString(true)` for `"1h30m"` output).

## Whole-Document Transformations

```go
// Pretty-print, preserve comments and trailing commas.
out, _ := jsonc.Format(data)

// Compact while preserving comments.
out, _ := jsonc.Minimize(data)

// Strip comments and trailing commas to produce strict RFC 8259 JSON.
std, _ := jsonc.ToJSON(data)
```

## Path Queries

```go
src := []byte(`{
  "server": {
    "port": 8080,
    "hosts": ["a.example", "b.example"]
  }
}`)
f, _ := jsonc.Parse(src)

// Rotini-style: $, .name, ["name"], [N], [*], ..name
p, _ := jsonc.PathString("$.server.hosts[1]")
host, _ := p.ReadString(f.Root)
fmt.Println(host) // b.example

// RFC 6901 JSON Pointer
p2, _ := jsonc.PathPointer("/server/port")
port, _ := p2.ReadString(f.Root)
fmt.Println(port) // 8080
```

## RFC 6902 JSON Patch

```go
doc := []byte(`{"a": 1, "b": 2}`)
patch := []byte(`[
  {"op": "replace", "path": "/a", "value": 100},
  {"op": "add", "path": "/c", "value": 3}
]`)

f, _ := jsonc.Parse(doc)
p, _ := jsonc.ParsePatch(patch)
out, _ := p.Apply(f.Root) // input is never mutated
bytes, _ := jsonc.NodeToBytes(out)
fmt.Println(string(bytes)) // {"a": 100, "b": 2, "c": 3}
```

## Streaming

```go
dec := jsonc.NewDecoder(reader)
for dec.More() {
	var v Item
	if err := dec.Decode(&v); err != nil {
		return err
	}
	process(v)
}
```

## Comparison with related packages

| Feature | `encoding/json` | [hujson] | [json5] | `jsonc` |
| --- | --- | --- | --- | --- |
| Line/block comments | ❌ | ✅ | ✅ | ✅ |
| Trailing commas | ❌ | ✅ | ✅ | ✅ |
| Drop-in stdlib API | n/a | partial | ❌ | ✅ |
| AST + path queries | ❌ | partial | ❌ | ✅ |
| RFC 6902 patch | ❌ | ❌ | ❌ | ✅ |
| Generic `UnmarshalTo[T]` | ❌ | ❌ | ❌ | ✅ |
| Comment-preserving round-trips | ❌ | ✅ | ❌ | ✅ |

[hujson]: https://github.com/tailscale/hujson
[json5]: https://github.com/json5/json5

## Documentation

Full API reference is available on [pkg.go.dev](https://pkg.go.dev/github.com/go-rotini/jsonc).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on how to contribute to this project.

## Code of Conduct

This project follows a code of conduct to ensure a welcoming community. See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## Security

To report a vulnerability, see [SECURITY.md](SECURITY.md).

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
