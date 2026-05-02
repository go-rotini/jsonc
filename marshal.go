package jsonc

import (
	"context"
	"fmt"
	"io"
	"os"
)

// Marshal encodes v as JSONC bytes using default options. The output is
// compact (no indent, no comments) — for human-readable output, use
// [MarshalIndent] or [MarshalWithOptions] with [WithIndent].
//
// v may be any Go value supported by the encoder; see the package
// documentation for the type-mapping table.
func Marshal(v any) ([]byte, error) {
	return MarshalWithOptions(v)
}

// MarshalIndent is a convenience wrapper that produces multi-line output
// with the given indent string. Equivalent to
// MarshalWithOptions(v, WithIndent(indent)).
func MarshalIndent(v any, indent string) ([]byte, error) {
	return MarshalWithOptions(v, WithIndent(indent))
}

// MarshalWithOptions encodes v as JSONC with the given options.
func MarshalWithOptions(v any, opts ...EncodeOption) ([]byte, error) {
	o := defaultEncodeOptions()
	for _, opt := range opts {
		opt(o)
	}
	enc := newReflectEncoder(o)
	return enc.encode(v)
}

// MarshalTo encodes v as JSONC and returns the bytes; the type parameter
// exists for symmetry with [UnmarshalTo] when the caller knows the value
// type at the call site.
func MarshalTo[T any](v T, opts ...EncodeOption) ([]byte, error) {
	return MarshalWithOptions(v, opts...)
}

// EncodeFile encodes v as JSONC and writes the result to the given file
// path. The file is created (or truncated) with mode 0o644.
func EncodeFile(path string, v any, opts ...EncodeOption) error {
	out, err := MarshalWithOptions(v, opts...)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, out, 0o644); err != nil { //nolint:gosec // user-controlled path; mode chosen for config files
		return fmt.Errorf("jsonc: %w", err)
	}
	return nil
}

// Encoder writes JSONC values to an output stream. It mirrors
// [encoding/json.Encoder]: each call to [Encoder.Encode] writes one value
// followed by a newline.
type Encoder struct {
	w    io.Writer
	opts *encoderOptions
	ctx  context.Context //nolint:containedctx // by design — set via SetContext
}

// NewEncoder creates an [Encoder] that writes to w with the given options.
func NewEncoder(w io.Writer, opts ...EncodeOption) *Encoder {
	o := defaultEncodeOptions()
	for _, opt := range opts {
		opt(o)
	}
	return &Encoder{
		w:    w,
		opts: o,
		ctx:  context.Background(),
	}
}

// Encode writes the JSONC encoding of v to the stream, followed by a
// newline. Any error from the underlying writer is returned.
func (enc *Encoder) Encode(v any) error {
	return enc.EncodeContext(enc.ctx, v)
}

// EncodeContext is like [Encoder.Encode] but uses ctx for any
// [MarshalerContext] dispatch encountered during encoding.
func (enc *Encoder) EncodeContext(ctx context.Context, v any) error {
	e := newReflectEncoder(enc.opts)
	e.ctx = ctx
	out, err := e.encode(v)
	if err != nil {
		return err
	}
	if _, err := enc.w.Write(out); err != nil {
		return fmt.Errorf("jsonc: %w", err)
	}
	if _, err := enc.w.Write([]byte("\n")); err != nil {
		return fmt.Errorf("jsonc: %w", err)
	}
	return nil
}

// SetIndent configures the indent string for subsequent [Encoder.Encode]
// calls. Equivalent to applying [WithIndent] at construction time.
func (enc *Encoder) SetIndent(indent string) {
	enc.opts.indent = indent
}

// SetEscapeHTML controls HTML-character escaping. Default is false, matching
// the package-level [Marshal] default and differing from
// [encoding/json.Encoder] (which defaults to true).
func (enc *Encoder) SetEscapeHTML(b bool) {
	enc.opts.escapeHTML = b
}

// SetContext sets the context used by subsequent [Encoder.Encode] calls.
func (enc *Encoder) SetContext(ctx context.Context) {
	enc.ctx = ctx
}
