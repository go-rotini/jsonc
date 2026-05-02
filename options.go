package jsonc

import "reflect"

// EncodeOption configures the behavior of [Marshal], [MarshalWithOptions],
// and [Encoder].
type EncodeOption func(*encoderOptions)

// Comment attaches a comment to a JSONC node identified by key path when
// encoding with [WithComment].
type Comment struct {
	Position CommentPosition
	Text     string
}

// CommentPosition specifies where a [Comment] appears relative to its node.
type CommentPosition int

const (
	HeadCommentPos CommentPosition = iota // before the node
	LineCommentPos                        // on the same line, after the value
	FootCommentPos                        // after the node, inside its container
)

type encoderOptions struct {
	indent           string
	escapeHTML       bool
	arrayMultiline   bool
	omitEmpty        bool
	trailingComma    bool
	durationAsString bool
	strictJSONOutput bool
	mapKeyOrder      MapKeyOrder
	comments         map[string][]Comment
	customMarshalers map[reflect.Type]any
}

func defaultEncodeOptions() *encoderOptions {
	return &encoderOptions{
		// All defaults are zero-values; no indent, no HTML escape, no
		// trailing comma, lexicographic map ordering, durations as int64
		// nanoseconds (matching encoding/json).
	}
}

// WithIndent sets the per-level indentation string. An empty string (default)
// produces compact output. Common choices are "  " (two spaces), "    "
// (four spaces), or "\t" (tab).
func WithIndent(indent string) EncodeOption {
	return func(o *encoderOptions) { o.indent = indent }
}

// WithIndentN is a convenience that sets the indentation to n spaces.
// Equivalent to WithIndent(strings.Repeat(" ", n)).
func WithIndentN(n int) EncodeOption {
	return func(o *encoderOptions) {
		if n <= 0 {
			o.indent = ""
			return
		}
		buf := make([]byte, n)
		for i := range buf {
			buf[i] = ' '
		}
		o.indent = string(buf)
	}
}

// WithEscapeHTML controls whether <, >, &, U+2028, and U+2029 are escaped
// as their \u00XX equivalents in string output. The default is false (clean
// output); set to true for parity with [encoding/json], which escapes by
// default.
func WithEscapeHTML(b bool) EncodeOption {
	return func(o *encoderOptions) { o.escapeHTML = b }
}

// WithArrayMultiline emits arrays with one element per line, regardless of
// the indent setting. Without this option, arrays follow the compact-or-
// indented rule of the surrounding output.
func WithArrayMultiline(b bool) EncodeOption {
	return func(o *encoderOptions) { o.arrayMultiline = b }
}

// WithOmitEmpty omits struct fields and map entries whose values are the
// zero value for their type. Equivalent to adding ",omitempty" to every
// field tag.
func WithOmitEmpty(b bool) EncodeOption {
	return func(o *encoderOptions) { o.omitEmpty = b }
}

// WithTrailingComma appends a trailing comma after the last element of
// arrays and the last member of objects when output is multi-line. No
// effect on compact (single-line) output.
func WithTrailingComma(b bool) EncodeOption {
	return func(o *encoderOptions) { o.trailingComma = b }
}

// WithDurationAsString encodes [time.Duration] values as a human-readable
// string (e.g., "1h30m") via Duration.String(). The default is to encode
// as int64 nanoseconds, matching [encoding/json].
func WithDurationAsString(b bool) EncodeOption {
	return func(o *encoderOptions) { o.durationAsString = b }
}

// WithStrictJSONOutput ensures the encoded output is valid RFC 8259 JSON:
// no comments are emitted (any comments registered via [WithComment] or
// attached to AST nodes are silently dropped), and no trailing commas are
// emitted regardless of [WithTrailingComma]. Inf and NaN are already
// rejected in default mode; this option is a defense-in-depth guard for
// pipelines that must produce strict JSON.
//
// The encode-side counterpart of the decode-side [WithStrictJSON]; the
// names differ because Go does not allow same-name option functions
// returning different types.
func WithStrictJSONOutput(b bool) EncodeOption {
	return func(o *encoderOptions) { o.strictJSONOutput = b }
}

// WithMapKeyOrder controls how map keys are emitted by the encoder. The
// default is [MapKeyOrderLexicographic].
func WithMapKeyOrder(order MapKeyOrder) EncodeOption {
	return func(o *encoderOptions) { o.mapKeyOrder = order }
}

// WithComment attaches comments to nodes by path. Each key in the map is a
// path expression resolving the node to annotate; each value is a slice of
// [Comment] entries specifying position ([HeadCommentPos] / [LineCommentPos]
// / [FootCommentPos]) and text.
//
// Path syntax mirrors the encoder's own path construction:
//   - Object members use dot notation: "server.port".
//   - Map entries use the string form of the key: "users.alice".
//   - Array elements use bracket-with-index: "tags[0]" or "items[3].name".
//
// Comments are emitted only when output is multi-line (indent is set) and
// not in [WithStrictJSONOutput] mode. In compact or strict-JSON-output
// modes the option is silently ignored.
func WithComment(comments map[string][]Comment) EncodeOption {
	return func(o *encoderOptions) { o.comments = comments }
}

// WithCustomMarshaler registers a function that encodes values of type T
// to JSONC bytes, overriding the default encoding for that type. Custom
// marshalers run at lower priority than the [Marshaler] /
// [MarshalerContext] / [json.Marshaler] interfaces — to override a type
// that already implements one of those interfaces, wrap it in a new type.
func WithCustomMarshaler[T any](fn func(T) ([]byte, error)) EncodeOption {
	return func(o *encoderOptions) {
		if o.customMarshalers == nil {
			o.customMarshalers = make(map[reflect.Type]any)
		}
		o.customMarshalers[reflect.TypeFor[T]()] = fn
	}
}

// DecodeOption configures the behavior of [Unmarshal],
// [UnmarshalWithOptions], [UnmarshalTo], [Decoder], and [Parse].
type DecodeOption func(*decoderOptions)

// StructValidator validates a struct after all fields have been decoded.
// Implement this interface to integrate with validation libraries.
type StructValidator interface {
	Struct(v any) error
}

type decoderOptions struct {
	strict             bool
	strictJSON         bool
	allowDuplicateKeys bool
	useNumber          bool
	useOrderedMap      bool
	applyDefaults      bool
	maxDepth           int
	maxKeys            int
	maxDocumentSize    int
	maxNodes           int
	validator          StructValidator
	customUnmarshalers map[reflect.Type]any
}

func defaultDecodeOptions() *decoderOptions {
	return &decoderOptions{
		maxDepth: 100,
	}
}

// WithStrict causes decoding to return an [UnknownFieldError] if a JSONC
// key does not correspond to any field in the target struct. Matches
// [encoding/json.Decoder.DisallowUnknownFields].
func WithStrict() DecodeOption {
	return func(o *decoderOptions) { o.strict = true }
}

// WithStrictJSON causes the parser to reject JSONC extensions (line and
// block comments, trailing commas), enforcing RFC 8259 conformance. The
// rejection is reported as a [StrictJSONError].
func WithStrictJSON() DecodeOption {
	return func(o *decoderOptions) { o.strictJSON = true }
}

// WithAllowDuplicateKeys allows duplicate object keys; the last value wins,
// matching [encoding/json] behavior. The default is to return a
// [DuplicateKeyError].
func WithAllowDuplicateKeys() DecodeOption {
	return func(o *decoderOptions) { o.allowDuplicateKeys = true }
}

// WithUseNumber causes the decoder to materialize numbers as [Number]
// rather than float64 when decoding into interface{}. Matches
// [encoding/json.Decoder.UseNumber]. The option has no effect when
// decoding into typed numeric Go fields — those always parse to the
// target type.
func WithUseNumber() DecodeOption {
	return func(o *decoderOptions) { o.useNumber = true }
}

// WithOrderedMap causes decoding into any (interface{}) to produce
// [MapSlice] values for objects instead of map[string]any, preserving
// member order.
func WithOrderedMap() DecodeOption {
	return func(o *decoderOptions) { o.useOrderedMap = true }
}

// WithDefaults enables applying default values from struct tags. Default
// values are specified with the "default=<value>" tag option (e.g.,
// `jsonc:"port,default=8080"`). Without this option, default tags are
// ignored. Only scalar types are supported: string, bool, int/uint
// variants, float variants, and time.Duration.
func WithDefaults() DecodeOption {
	return func(o *decoderOptions) { o.applyDefaults = true }
}

// WithMaxDepth limits the nesting depth of the decoded value (default
// 100). Deeply nested documents are rejected with a [SyntaxError].
func WithMaxDepth(n int) DecodeOption {
	return func(o *decoderOptions) { o.maxDepth = n }
}

// WithMaxKeys limits the total number of object keys the parser may
// encounter. Zero means no limit.
func WithMaxKeys(n int) DecodeOption {
	return func(o *decoderOptions) { o.maxKeys = n }
}

// WithMaxDocumentSize rejects input that exceeds n bytes before parsing
// begins. Zero means no limit.
func WithMaxDocumentSize(n int) DecodeOption {
	return func(o *decoderOptions) { o.maxDocumentSize = n }
}

// WithMaxNodes limits the total number of AST nodes the parser may
// create. Zero means no limit.
func WithMaxNodes(n int) DecodeOption {
	return func(o *decoderOptions) { o.maxNodes = n }
}

// WithValidator registers a [StructValidator] that is called after each
// struct is fully decoded.
func WithValidator(v StructValidator) DecodeOption {
	return func(o *decoderOptions) { o.validator = v }
}

// WithCustomUnmarshaler registers a function that decodes JSONC bytes
// into a value of type T, overriding the default decoding for that type.
// Custom unmarshalers run at lower priority than the [Unmarshaler] /
// [BytesUnmarshaler] / [UnmarshalerContext] / [json.Unmarshaler]
// interfaces — to override a type that already implements one of those,
// wrap it in a new type.
func WithCustomUnmarshaler[T any](fn func(*T, []byte) error) DecodeOption {
	return func(o *decoderOptions) {
		if o.customUnmarshalers == nil {
			o.customUnmarshalers = make(map[reflect.Type]any)
		}
		o.customUnmarshalers[reflect.TypeFor[T]()] = fn
	}
}
