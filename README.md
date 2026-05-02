# go-rotini/jsonc

A Go JSONC encoding and decoding package that implements the [JSONC specification](https://jsonc.org/), is JWCC-compatible (line + block comments, optional trailing commas), and is backed by the [JSON Test Suite](https://github.com/nst/JSONTestSuite) conformance tests as a drop-in alternative for [`encoding/json`](https://pkg.go.dev/encoding/json).

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
