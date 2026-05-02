package jsonc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// RawValue is raw JSONC that has not been decoded. It can be used to
// delay decoding or to pass through a JSONC value without interpreting
// it. Analogous to [encoding/json.RawMessage].
//
// RawValue includes any comments and whitespace that are syntactically
// inside the value's bounds. To strip comments and trailing commas,
// producing standard JSON, use [RawValue.Standardize].
type RawValue []byte

// Unmarshal decodes the raw JSONC value into v.
func (r RawValue) Unmarshal(v any, opts ...DecodeOption) error {
	return UnmarshalWithOptions(r, v, opts...)
}

// MarshalJSONC returns the raw bytes verbatim. Implements [Marshaler].
func (r RawValue) MarshalJSONC() ([]byte, error) {
	if len(r) == 0 {
		return []byte("null"), nil
	}
	return r, nil
}

// UnmarshalJSONC stores the raw bytes verbatim. Implements [BytesUnmarshaler].
func (r *RawValue) UnmarshalJSONC(data []byte) error {
	*r = append((*r)[:0], data...)
	return nil
}

// Standardize returns a copy of the raw bytes with comments and trailing
// commas removed, producing valid RFC 8259 JSON.
func (r RawValue) Standardize() ([]byte, error) {
	return ToJSON(r)
}

// Unmarshal decodes JSONC data into v. The input may include line and
// block comments and trailing commas (per JWCC). For RFC 8259-strict
// behavior, use [UnmarshalWithOptions] with [WithStrictJSON].
//
// v must be a non-nil pointer to a Go value. The Go target may be any
// type supported by the decoder; see the package documentation for the
// type-mapping table.
func Unmarshal(data []byte, v any) error {
	return UnmarshalWithOptions(data, v)
}

// UnmarshalWithOptions decodes JSONC data into v with the given options.
func UnmarshalWithOptions(data []byte, v any, opts ...DecodeOption) error {
	o := defaultDecodeOptions()
	for _, opt := range opts {
		opt(o)
	}
	if o.maxDocumentSize > 0 && len(data) > o.maxDocumentSize {
		return ErrDocumentSize
	}

	root, err := parseDocument(data, o)
	if err != nil {
		return err
	}

	d := newDecoder(o)
	return d.decode(root, v)
}

// UnmarshalTo decodes JSONC data into a new value of type T.
func UnmarshalTo[T any](data []byte, opts ...DecodeOption) (T, error) {
	var v T
	err := UnmarshalWithOptions(data, &v, opts...)
	return v, err
}

// DecodeFile reads and decodes the JSONC file at path into v.
func DecodeFile(path string, v any, opts ...DecodeOption) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("jsonc: %w", err)
	}
	return UnmarshalWithOptions(data, v, opts...)
}

// Decoder reads JSONC values from an input stream. It supports decoding
// multiple top-level values back-to-back from a single reader, matching
// [encoding/json.Decoder] semantics.
type Decoder struct {
	r          io.Reader
	opts       *decoderOptions
	ctx        context.Context //nolint:containedctx // by design
	buf        []byte          // unread portion of input (after the most recent decode)
	loaded     bool            // true once we've read from r
	useNumber  bool            // mirrored from opts for ergonomic methods
	strict     bool            // mirrored from opts
	inputBytes int64           // total bytes read so far
	consumed   int64           // bytes consumed by previous decodes
}

// NewDecoder creates a [Decoder] that reads from r.
func NewDecoder(r io.Reader, opts ...DecodeOption) *Decoder {
	o := defaultDecodeOptions()
	for _, opt := range opts {
		opt(o)
	}
	return &Decoder{
		r:         r,
		opts:      o,
		ctx:       context.Background(),
		useNumber: o.useNumber,
		strict:    o.strict,
	}
}

// Decode reads the next JSONC value from the underlying reader and decodes
// it into v.
func (dec *Decoder) Decode(v any) error {
	return dec.DecodeContext(dec.ctx, v)
}

// DecodeContext reads the next JSONC value with the given context.
func (dec *Decoder) DecodeContext(ctx context.Context, v any) error {
	if err := dec.ensureLoaded(); err != nil {
		return err
	}

	// Find the start of the next value (skip leading whitespace/comments).
	startOffset, err := dec.findValueStart()
	if err != nil {
		return err
	}
	if startOffset < 0 {
		return io.EOF
	}

	// Try to parse a value from the loaded buffer. If the parser succeeds
	// without "extra data" complaint, advance past the consumed portion.
	endOffset, err := dec.parseOneValue(ctx, startOffset, v)
	if err != nil {
		return err
	}
	dec.consumed = int64(endOffset)
	return nil
}

// More reports whether there is another JSONC value waiting to be decoded.
func (dec *Decoder) More() bool {
	if err := dec.ensureLoaded(); err != nil {
		return false
	}
	startOffset, err := dec.findValueStart()
	if err != nil {
		return false
	}
	return startOffset >= 0
}

// InputOffset returns the byte offset (in the input stream) of the next
// value's first byte (after consumed bytes and whitespace/comments).
func (dec *Decoder) InputOffset() int64 {
	return dec.consumed
}

// SetContext sets the context used by subsequent [Decoder.Decode] calls.
func (dec *Decoder) SetContext(ctx context.Context) {
	dec.ctx = ctx
}

// ensureLoaded reads the entire input into dec.buf on first call. (Phase
// 6 implementation: simple slurp-and-parse. A streaming-token approach is
// possible but not required for v0.1.)
func (dec *Decoder) ensureLoaded() error {
	if dec.loaded {
		return nil
	}
	data, err := io.ReadAll(dec.r)
	if err != nil {
		return fmt.Errorf("jsonc: %w", err)
	}
	dec.buf = data
	dec.inputBytes = int64(len(data))
	dec.loaded = true
	return nil
}

// findValueStart scans dec.buf from dec.consumed onward, skipping
// whitespace and comments. Returns the byte offset of the next non-trivial
// token, or -1 if EOF.
func (dec *Decoder) findValueStart() (int, error) {
	tail := dec.buf[dec.consumed:]
	if len(tail) == 0 {
		return -1, nil
	}
	// Scan from the consumed offset to find the first byte that begins a value.
	// We do this by feeding a scanner over the tail and watching for the first
	// non-whitespace, non-comment, non-newline token.
	sc, err := newScanner(tail)
	if err != nil {
		return -1, err
	}
	sc.strictJSON = dec.opts.strictJSON
	tokens, err := sc.scan()
	if err != nil {
		return -1, err
	}
	for _, tk := range tokens {
		switch tk.kind {
		case tokenStreamStart, tokenStreamEnd, tokenEOF, tokenNewline,
			tokenLineComment, tokenBlockComment:
			continue
		default:
			return int(dec.consumed) + tk.pos.Offset, nil
		}
	}
	return -1, nil
}

// parseOneValue parses a single value starting at offset and decodes it
// into v. Returns the byte offset just after the value (and any trailing
// inline comment that belongs to it). Trailing whitespace/comments after
// the value are NOT consumed — they may belong to the next value or be
// part of the document's footer.
func (dec *Decoder) parseOneValue(ctx context.Context, offset int, v any) (int, error) {
	// Build a bounded sub-document containing the value plus enough trailing
	// content for the parser to know where the value ends. We re-use the
	// general parser but ignore the "extra data" check by parsing from an
	// internal entry point.
	tail := dec.buf[offset:]

	o := *dec.opts
	if o.maxDocumentSize > 0 && len(tail) > o.maxDocumentSize {
		return 0, ErrDocumentSize
	}

	root, consumedFromTail, err := parseSingleValue(tail, &o)
	if err != nil {
		return 0, err
	}

	d := newDecoder(&o)
	d.ctx = ctx
	if err := d.decode(root, v); err != nil {
		return 0, err
	}
	return offset + consumedFromTail, nil
}

// parseDocument parses a complete JSONC document, rejecting trailing
// non-comment data. Used by [Unmarshal].
func parseDocument(data []byte, opts *decoderOptions) (*node, error) {
	s, err := newScanner(data)
	if err != nil {
		return nil, err
	}
	s.strictJSON = opts.strictJSON
	tokens, err := s.scan()
	if err != nil {
		return nil, err
	}
	p := newParser(tokens, opts)
	root, err := p.parse()
	if err != nil {
		return nil, err
	}
	return root, nil
}

// parseSingleValue parses one JSONC value from data and returns the byte
// offset just past it. Trailing whitespace/comments that follow the value
// are NOT included in the returned offset — the caller is responsible for
// consuming them on the next call.
func parseSingleValue(data []byte, opts *decoderOptions) (*node, int, error) {
	s, err := newScanner(data)
	if err != nil {
		return nil, 0, err
	}
	s.strictJSON = opts.strictJSON
	tokens, err := s.scan()
	if err != nil {
		return nil, 0, err
	}

	// Run a streaming-friendly parse: skip leading trivia, parse one value,
	// then return the byte offset of the token immediately following the
	// value.
	p := newParser(tokens, opts)
	p.consumeIfKind(tokenStreamStart)
	leadHead := p.collectHeadComments()
	tk := p.peek()
	if tk.kind == tokenEOF || tk.kind == tokenStreamEnd {
		return nil, 0, io.EOF
	}
	root, err := p.parseValue()
	if err != nil {
		return nil, 0, err
	}
	if leadHead != "" {
		root.headComment = appendCommentBlock(leadHead, root.headComment)
	}

	// Find the byte offset of the next token (or EOF) after the value.
	if p.pos < len(p.tokens) {
		nextTok := p.tokens[p.pos]
		if nextTok.kind == tokenEOF {
			return root, len(data), nil
		}
		// Inline comment after the value belongs to it; subtract it from the
		// "next value start" so the next Decode call can find it as trivia.
		return root, nextTok.pos.Offset, nil
	}
	return root, len(data), nil
}

// Common error checks for v: re-export the stdlib-style error for callers
// who want a consistent type. (errors.Is(err, ErrNilPointer) works.)
var _ = errors.Is

// Compile-time guards: RawValue satisfies the byte-marshaler interfaces so
// it can be used in struct fields.
var (
	_ Marshaler        = RawValue{}
	_ BytesUnmarshaler = (*RawValue)(nil)
	_ json.Marshaler   = json.RawMessage{} // pin import
)
