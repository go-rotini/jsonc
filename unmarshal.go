package jsonc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// RawValue is raw JSONC that has not been decoded, like
// [encoding/json.RawMessage]. Use it to delay decoding or to pass a value
// through unchanged. When decoded from an object or array, it holds the
// source text verbatim, including comments and trailing commas inside the
// value; [RawValue.Standardize] converts it to standard JSON. The encoder
// writes it verbatim (converted under [WithStrictJSONOutput]) and returns
// an error if it is not exactly one JSONC value.
type RawValue []byte

// Unmarshal decodes r into v, as [UnmarshalWithOptions] does.
func (r RawValue) Unmarshal(v any, opts ...DecodeOption) error {
	return UnmarshalWithOptions(r, v, opts...)
}

// MarshalJSONC implements [Marshaler]. It returns r verbatim, or "null" if r
// is empty.
func (r RawValue) MarshalJSONC() ([]byte, error) {
	if len(r) == 0 {
		return []byte("null"), nil
	}
	return r, nil
}

// UnmarshalJSONC implements [BytesUnmarshaler] by storing a copy of data.
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
// v must be a non-nil pointer; otherwise Unmarshal returns [ErrNilPointer].
// See the package documentation for the type mapping. Values that cannot be
// assigned to their targets are collected and reported together in a
// [TypeError] after decoding the rest of the input.
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

// Decoder reads a sequence of top-level JSONC values from an input, like
// [encoding/json.Decoder]. Unlike that type, it reads the whole input into
// memory on the first call to [Decoder.Decode] or [Decoder.More], so it is
// not suited to unbounded streams. With [WithMaxDocumentSize], it reads at
// most that many bytes and fails with [ErrDocumentSize] when the input is
// longer. The other limits apply to each value separately.
//
// The input is scanned once, as values are decoded. A syntax error is
// reported by the Decode call that reaches it, after the values before it
// have been decoded, and every later call returns the same error. Error
// positions count from the start of the input.
type Decoder struct {
	r        io.Reader
	opts     *decoderOptions
	ctx      context.Context //nolint:containedctx // by design
	p        *parser         // parses the input; set once it is loaded
	bom      int             // length of the byte-order mark stripped from the input
	loaded   bool            // true once we've read from r
	err      error           // syntax or limit error; returned by every later call
	consumed int64           // input offset just past the most recently decoded value
}

// NewDecoder creates a [Decoder] that reads from r.
func NewDecoder(r io.Reader, opts ...DecodeOption) *Decoder {
	o := defaultDecodeOptions()
	for _, opt := range opts {
		opt(o)
	}
	return &Decoder{
		r:    r,
		opts: o,
		ctx:  context.Background(),
	}
}

// Decode decodes the next JSONC value into v. It returns [io.EOF] when no
// values remain.
func (dec *Decoder) Decode(v any) error {
	return dec.DecodeContext(dec.ctx, v)
}

// DecodeContext is like [Decoder.Decode] but passes ctx to any
// [UnmarshalerContext] it calls.
func (dec *Decoder) DecodeContext(ctx context.Context, v any) error {
	if err := dec.ensureLoaded(); err != nil {
		return err
	}
	root, err := dec.nextValue()
	if err != nil {
		return err
	}
	d := newDecoder(dec.opts)
	d.ctx = ctx
	return d.decode(root, v)
}

// More reports whether there is another JSONC value waiting to be decoded.
// It also reports true when a syntax error comes next, which the following
// [Decoder.Decode] returns.
func (dec *Decoder) More() bool {
	if err := dec.ensureLoaded(); err != nil || dec.err != nil {
		return false
	}
	dec.p.collectHeadComments()
	return dec.p.peek().kind != tokenEOF
}

// InputOffset returns the input byte offset where the next [Decoder.Decode]
// starts scanning: just after the most recently decoded value, past any
// spaces or tabs that follow it. It is 0 before the
// first value is decoded.
func (dec *Decoder) InputOffset() int64 {
	return dec.consumed
}

// SetContext sets the context used by subsequent [Decoder.Decode] calls.
func (dec *Decoder) SetContext(ctx context.Context) {
	dec.ctx = ctx
}

// ensureLoaded reads the input on first call and prepares the parser. An
// input that cannot be parsed at all also sets dec.err, which later calls
// return.
func (dec *Decoder) ensureLoaded() error {
	if dec.loaded {
		return nil
	}
	r := dec.r
	limit := dec.opts.maxDocumentSize
	if limit > 0 {
		r = io.LimitReader(r, int64(limit)+1)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("jsonc: %w", err)
	}
	dec.loaded = true
	if limit > 0 && len(data) > limit {
		dec.err = ErrDocumentSize
		return dec.err
	}
	src, err := stripBOM(data)
	if err != nil {
		dec.err = err
		return err
	}
	dec.bom = len(data) - len(src)
	dec.p = newParser(src, dec.opts)
	return nil
}

// nextValue parses the next value, skipping whitespace and comments before
// it. It returns io.EOF when none remain.
func (dec *Decoder) nextValue() (*node, error) {
	if dec.err != nil {
		return nil, dec.err
	}
	p := dec.p
	p.depth, p.keys, p.nodes = 0, 0, 0
	lead := p.collectHeadComments()
	if p.peek().kind == tokenEOF {
		return nil, io.EOF
	}
	root, err := p.parseValue()
	if err != nil {
		if p.err != nil {
			err = p.err
		}
		dec.err = err
		return nil, err
	}
	if lead != "" {
		root.headComment = appendCommentBlock(lead, root.headComment)
	}
	dec.consumed = int64(dec.bom + p.valueEnd())
	return root, nil
}

// parseDocument parses a complete JSONC document, rejecting trailing
// non-comment data. Used by [Unmarshal].
func parseDocument(data []byte, opts *decoderOptions) (*node, error) {
	root, _, err := parseDocumentWarnings(data, opts)
	return root, err
}

// parseDocumentWarnings is [parseDocument] that also returns the parser's
// warnings.
func parseDocumentWarnings(data []byte, opts *decoderOptions) (*node, []string, error) {
	src, err := stripBOM(data)
	if err != nil {
		return nil, nil, err
	}
	p := newParser(src, opts)
	root, err := p.parse()
	if err != nil {
		return nil, nil, err
	}
	return root, p.warnings, nil
}

// Pin errors.Is in the import set so the docs that reference it stay valid.
var _ = errors.Is

// Compile-time guards: RawValue satisfies the byte-marshaler interfaces so
// it can be used in struct fields.
var (
	_ Marshaler        = RawValue{}
	_ BytesUnmarshaler = (*RawValue)(nil)
	_ json.Marshaler   = json.RawMessage{} // pin import
)
