package jsonc

import (
	"bytes"
	"context"
	"encoding"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Marshaler is implemented by types that can encode themselves into raw
// JSONC bytes. Existing [encoding/json.Marshaler] implementations are also
// honored at lower priority, so types that already implement that interface
// continue to work unchanged.
type Marshaler interface {
	MarshalJSONC() ([]byte, error)
}

// MarshalerContext is like [Marshaler] but receives a context, set via
// [Encoder.EncodeContext].
type MarshalerContext interface {
	MarshalJSONC(ctx context.Context) ([]byte, error)
}

// encoder is the internal encoder shared by AST-based emission
// ([encoder.encodeNode], used by [NodeToBytes]) and reflection-based
// emission ([encoder.encode], used by [Marshal] and friends).
type encoder struct {
	opts *encoderOptions
	buf  bytes.Buffer
	ctx  context.Context //nolint:containedctx // by design — set via Encoder.EncodeContext
	// visited tracks the addresses of pointer values currently on the
	// encoding stack; used to detect cycles. Keyed by reflect.Value pointer
	// address.
	visited map[uintptr]bool
}

// newNodeEncoder constructs an encoder configured for AST emission (used
// by [NodeToBytes] / [NodeToBytesWithOptions] and by the decoder for
// re-emitting raw bytes of container nodes).
func newNodeEncoder(opts *encoderOptions) *encoder {
	return &encoder{opts: opts, ctx: context.Background()}
}

// newReflectEncoder constructs an encoder configured for reflection-based
// emission (used by [Marshal] and [Encoder]).
func newReflectEncoder(opts *encoderOptions) *encoder {
	return &encoder{
		opts:    opts,
		ctx:     context.Background(),
		visited: make(map[uintptr]bool),
	}
}

// encodeNode emits the AST rooted at n as JSONC bytes. The output is
// indent-aware (driven by opts.indent) and preserves comment annotations
// when output is multi-line.
func (e *encoder) encodeNode(n *node) ([]byte, error) {
	if n == nil {
		return nil, nil
	}
	e.writeNode(n, 0)
	return e.buf.Bytes(), nil
}

// writeNode emits a single node at the given indent depth.
func (e *encoder) writeNode(n *node, depth int) {
	if n == nil {
		return
	}
	// Head comment first, on its own line(s) when output is multi-line.
	if n.headComment != "" && e.multiline() && !e.opts.strictJSONOutput {
		e.writeCommentBlock(n.headComment, depth)
	}

	switch n.kind {
	case nodeObject:
		e.writeObject(n, depth)
	case nodeArray:
		e.writeArray(n, depth)
	case nodeKeyValue:
		// Only used as a child of nodeObject; emit "key": value.
		e.writeQuotedString(n.key)
		e.buf.WriteString(": ")
		if len(n.children) > 0 {
			e.writeNode(n.children[0], depth)
		} else {
			e.buf.WriteString("null")
		}
	case nodeString:
		if n.rawValue != "" {
			e.buf.WriteString(n.rawValue)
		} else {
			e.writeQuotedString(n.value)
		}
	case nodeNumber, nodeBool, nodeNull:
		if n.rawValue != "" {
			e.buf.WriteString(n.rawValue)
		} else {
			e.buf.WriteString(n.value)
		}
	case nodeComment:
		// Orphan comment — emit verbatim if multi-line allowed.
		if !e.opts.strictJSONOutput {
			e.writeOrphanComment(n, depth)
		}
	}

	// Inline comment (on the same line, after the node value) — multi-line
	// only.
	if n.comment != "" && e.multiline() && !e.opts.strictJSONOutput {
		e.buf.WriteString(" // ")
		e.buf.WriteString(n.comment)
	}
}

// writeObject emits an object container. It dispatches to a multi-line or
// compact emitter based on encoder options.
func (e *encoder) writeObject(n *node, depth int) {
	if e.multiline() {
		e.writeObjectMultiline(n, depth)
		return
	}
	e.writeObjectCompact(n)
}

func (e *encoder) writeObjectCompact(n *node) {
	e.buf.WriteByte('{')
	first := true
	for _, child := range n.children {
		if child.kind == nodeComment {
			continue // dropped in compact mode
		}
		if !first {
			e.buf.WriteByte(',')
			e.buf.WriteByte(' ')
		}
		e.writeNode(child, 0)
		first = false
	}
	e.buf.WriteByte('}')
}

func (e *encoder) writeObjectMultiline(n *node, depth int) {
	if len(n.children) == 0 && n.footComment == "" {
		e.buf.WriteString("{}")
		return
	}
	e.buf.WriteByte('{')
	e.buf.WriteByte('\n')

	// Filter comment children apart from value children for trailing-comma
	// handling.
	memberCount := 0
	for _, child := range n.children {
		if child.kind != nodeComment {
			memberCount++
		}
	}

	emittedMembers := 0
	for _, child := range n.children {
		if child.kind == nodeComment {
			if !e.opts.strictJSONOutput {
				e.writeOrphanComment(child, depth+1)
			}
			continue
		}
		// Head comment of the child is emitted by writeNode.
		e.writeIndent(depth + 1)
		e.writeNode(child, depth+1)
		emittedMembers++
		if emittedMembers < memberCount {
			e.buf.WriteByte(',')
		} else if e.opts.trailingComma && !e.opts.strictJSONOutput {
			e.buf.WriteByte(',')
		}
		e.buf.WriteByte('\n')
	}

	if n.footComment != "" && !e.opts.strictJSONOutput {
		e.writeCommentBlock(n.footComment, depth+1)
	}
	e.writeIndent(depth)
	e.buf.WriteByte('}')
}

// writeArray emits an array container.
func (e *encoder) writeArray(n *node, depth int) {
	if e.opts.arrayMultiline || e.multiline() {
		e.writeArrayMultiline(n, depth)
		return
	}
	e.writeArrayCompact(n)
}

func (e *encoder) writeArrayCompact(n *node) {
	e.buf.WriteByte('[')
	first := true
	for _, child := range n.children {
		if child.kind == nodeComment {
			continue
		}
		if !first {
			e.buf.WriteByte(',')
			e.buf.WriteByte(' ')
		}
		e.writeNode(child, 0)
		first = false
	}
	e.buf.WriteByte(']')
}

func (e *encoder) writeArrayMultiline(n *node, depth int) {
	if len(n.children) == 0 && n.footComment == "" {
		e.buf.WriteString("[]")
		return
	}
	e.buf.WriteByte('[')
	e.buf.WriteByte('\n')

	memberCount := 0
	for _, child := range n.children {
		if child.kind != nodeComment {
			memberCount++
		}
	}

	emittedMembers := 0
	for _, child := range n.children {
		if child.kind == nodeComment {
			if !e.opts.strictJSONOutput {
				e.writeOrphanComment(child, depth+1)
			}
			continue
		}
		e.writeIndent(depth + 1)
		e.writeNode(child, depth+1)
		emittedMembers++
		if emittedMembers < memberCount {
			e.buf.WriteByte(',')
		} else if e.opts.trailingComma && !e.opts.strictJSONOutput {
			e.buf.WriteByte(',')
		}
		e.buf.WriteByte('\n')
	}

	if n.footComment != "" && !e.opts.strictJSONOutput {
		e.writeCommentBlock(n.footComment, depth+1)
	}
	e.writeIndent(depth)
	e.buf.WriteByte(']')
}

// writeIndent emits the indentation prefix for the given depth.
func (e *encoder) writeIndent(depth int) {
	if e.opts.indent == "" {
		return
	}
	for range depth {
		e.buf.WriteString(e.opts.indent)
	}
}

// writeCommentBlock emits a (possibly multi-line) comment block at the
// given depth. Each non-empty line is prefixed with "// ".
func (e *encoder) writeCommentBlock(text string, depth int) {
	for line := range strings.SplitSeq(text, "\n") {
		e.writeIndent(depth)
		e.buf.WriteString("// ")
		e.buf.WriteString(line)
		e.buf.WriteByte('\n')
	}
}

// writeOrphanComment emits a CommentNode at the given depth.
func (e *encoder) writeOrphanComment(n *node, depth int) {
	switch n.style {
	case styleBlockComment:
		e.writeIndent(depth)
		e.buf.WriteString("/*")
		e.buf.WriteString(n.value)
		e.buf.WriteString("*/")
		e.buf.WriteByte('\n')
	default: // line
		e.writeIndent(depth)
		e.buf.WriteString("//")
		e.buf.WriteString(n.value)
		e.buf.WriteByte('\n')
	}
}

// writeQuotedString writes s as a JSON-quoted string, escaping per RFC 8259.
// HTML escape is applied when opts.escapeHTML is true.
func (e *encoder) writeQuotedString(s string) {
	e.buf.WriteByte('"')
	for i := range len(s) {
		c := s[i]
		switch c {
		case '"':
			e.buf.WriteString(`\"`)
		case '\\':
			e.buf.WriteString(`\\`)
		case '\b':
			e.buf.WriteString(`\b`)
		case '\f':
			e.buf.WriteString(`\f`)
		case '\n':
			e.buf.WriteString(`\n`)
		case '\r':
			e.buf.WriteString(`\r`)
		case '\t':
			e.buf.WriteString(`\t`)
		case '<', '>', '&':
			if e.opts.escapeHTML {
				e.buf.WriteString(`\u00`)
				e.buf.WriteByte(hexDigit(c >> 4))
				e.buf.WriteByte(hexDigit(c & 0xF))
			} else {
				e.buf.WriteByte(c)
			}
		default:
			if c < 0x20 {
				e.buf.WriteString(`\u00`)
				e.buf.WriteByte(hexDigit(c >> 4))
				e.buf.WriteByte(hexDigit(c & 0xF))
			} else {
				e.buf.WriteByte(c)
			}
		}
	}
	e.buf.WriteByte('"')
}

// hexDigit returns the lowercase hex digit for n (0-15).
func hexDigit(n byte) byte {
	if n < 10 {
		return '0' + n
	}
	return 'a' + n - 10
}

// multiline reports whether the encoder is in multi-line mode (i.e., indent
// is non-empty).
func (e *encoder) multiline() bool {
	return e.opts.indent != ""
}

// Reflection-based encoding follows.

// encode is the top-level reflection entry point. It encodes v (which must
// be a Go value of any supported type) to JSONC.
func (e *encoder) encode(v any) ([]byte, error) {
	if v == nil {
		e.buf.WriteString("null")
		return e.buf.Bytes(), nil
	}
	rv := reflect.ValueOf(v)
	if err := e.encodeValue(rv, 0); err != nil {
		return nil, err
	}
	return e.buf.Bytes(), nil
}

// encodeValue writes the JSONC representation of rv at the given depth.
// Returns ErrUnsupportedValue (wrapped) for channel/func/complex/cyclic
// values, and propagates errors from user-defined Marshaler implementations.
func (e *encoder) encodeValue(rv reflect.Value, depth int) error {
	if !rv.IsValid() {
		e.buf.WriteString("null")
		return nil
	}

	// Special-cased types take priority — they produce a specific source
	// form (raw bytes, raw number text, base64 string, etc.) regardless of
	// any interface they may also satisfy.
	if handled, err := e.encodeSpecialType(rv); handled {
		return err
	}

	// Custom marshalers (registered via WithCustomMarshaler).
	if handled, err := e.encodeCustomMarshaler(rv); handled {
		return err
	}

	// Marshaler interfaces (priority order).
	if handled, err := e.encodeMarshaler(rv); handled {
		return err
	}

	switch rv.Kind() {
	case reflect.Pointer:
		return e.encodePointer(rv, depth)
	case reflect.Interface:
		if rv.IsNil() {
			e.buf.WriteString("null")
			return nil
		}
		return e.encodeValue(rv.Elem(), depth)
	case reflect.Bool:
		if rv.Bool() {
			e.buf.WriteString("true")
		} else {
			e.buf.WriteString("false")
		}
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		e.buf.WriteString(strconv.FormatInt(rv.Int(), 10))
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		e.buf.WriteString(strconv.FormatUint(rv.Uint(), 10))
		return nil
	case reflect.Float32, reflect.Float64:
		return e.encodeFloat(rv)
	case reflect.String:
		e.writeQuotedString(rv.String())
		return nil
	case reflect.Slice:
		return e.encodeSlice(rv, depth)
	case reflect.Array:
		return e.encodeArrayValue(rv, depth)
	case reflect.Map:
		return e.encodeMap(rv, depth)
	case reflect.Struct:
		return e.encodeStruct(rv, depth)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedValue, rv.Type())
	}
}

// encodePointer handles pointer kinds with cycle detection.
func (e *encoder) encodePointer(rv reflect.Value, depth int) error {
	if rv.IsNil() {
		e.buf.WriteString("null")
		return nil
	}
	addr := rv.Pointer()
	if e.visited != nil && e.visited[addr] {
		return fmt.Errorf("%w: cyclic value of type %s", ErrUnsupportedValue, rv.Type())
	}
	if e.visited != nil {
		e.visited[addr] = true
		defer delete(e.visited, addr)
	}
	return e.encodeValue(rv.Elem(), depth)
}

// encodeFloat writes a float, rejecting Inf and NaN per stdlib semantics.
func (e *encoder) encodeFloat(rv reflect.Value) error {
	f := rv.Float()
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return fmt.Errorf("%w: float %v cannot be encoded as JSON", ErrUnsupportedValue, f)
	}
	bits := 64
	if rv.Kind() == reflect.Float32 {
		bits = 32
	}
	// Use the shortest round-trippable representation. strconv.FormatFloat
	// with 'g' precision -1 gives stdlib-like output but stdlib actually
	// uses a custom approach; we follow the same pattern as encoding/json.
	abs := math.Abs(f)
	fmtByte := byte('f')
	if abs != 0 {
		if bits == 64 && (abs < 1e-6 || abs >= 1e21) {
			fmtByte = 'e'
		}
		if bits == 32 && (float32(abs) < 1e-6 || float32(abs) >= 1e21) {
			fmtByte = 'e'
		}
	}
	out := strconv.AppendFloat(nil, f, fmtByte, -1, bits)
	if fmtByte == 'e' {
		// Match stdlib: strip leading zero from exponent (e.g., "1e+09" → "1e+9").
		for i := range len(out) - 1 {
			if out[i] == 'e' && (out[i+1] == '+' || out[i+1] == '-') && i+2 < len(out) && out[i+2] == '0' && i+3 < len(out) {
				out = append(out[:i+2], out[i+3:]...)
				break
			}
		}
	}
	e.buf.Write(out)
	return nil
}

// encodeSpecialType handles types whose source form must be preserved
// verbatim. Returns (handled, err). When handled is true, err is the result
// of writing the value (or nil on success).
func (e *encoder) encodeSpecialType(rv reflect.Value) (bool, error) {
	switch rv.Type() {
	case reflect.TypeFor[RawValue]():
		raw := rv.Bytes()
		if len(raw) == 0 {
			e.buf.WriteString("null")
			return true, nil
		}
		// Re-emit through the parser+AST encoder to enforce strict-JSON
		// output if requested, otherwise pass through.
		if e.opts.strictJSONOutput {
			out, err := ToJSON(raw)
			if err != nil {
				return true, err
			}
			e.buf.Write(out)
			return true, nil
		}
		e.buf.Write(raw)
		return true, nil
	case reflect.TypeFor[json.RawMessage]():
		raw := rv.Bytes()
		if len(raw) == 0 {
			e.buf.WriteString("null")
			return true, nil
		}
		e.buf.Write(raw)
		return true, nil
	case reflect.TypeFor[json.Number]():
		s := rv.String()
		if s == "" {
			e.buf.WriteString("0")
			return true, nil
		}
		e.buf.WriteString(s)
		return true, nil
	case reflect.TypeFor[time.Time]():
		t, ok := rv.Interface().(time.Time)
		if !ok {
			return false, nil
		}
		out, err := t.MarshalJSON()
		if err != nil {
			return true, fmt.Errorf("jsonc: marshal time.Time: %w", err)
		}
		e.buf.Write(out)
		return true, nil
	case reflect.TypeFor[time.Duration]():
		d := time.Duration(rv.Int())
		if e.opts.durationAsString {
			e.writeQuotedString(d.String())
		} else {
			e.buf.WriteString(strconv.FormatInt(int64(d), 10))
		}
		return true, nil
	case reflect.TypeFor[big.Int]():
		bi, ok := rv.Interface().(big.Int)
		if !ok {
			return false, nil
		}
		e.buf.WriteString(bi.String())
		return true, nil
	case reflect.TypeFor[*big.Int]():
		if rv.IsNil() {
			e.buf.WriteString("null")
			return true, nil
		}
		bi, ok := rv.Interface().(*big.Int)
		if !ok || bi == nil {
			e.buf.WriteString("null")
			return true, nil
		}
		e.buf.WriteString(bi.String())
		return true, nil
	case reflect.TypeFor[big.Float]():
		bf, ok := rv.Interface().(big.Float)
		if !ok {
			return false, nil
		}
		e.buf.WriteString(bf.Text('g', -1))
		return true, nil
	case reflect.TypeFor[*big.Float]():
		if rv.IsNil() {
			e.buf.WriteString("null")
			return true, nil
		}
		bf, ok := rv.Interface().(*big.Float)
		if !ok || bf == nil {
			e.buf.WriteString("null")
			return true, nil
		}
		e.buf.WriteString(bf.Text('g', -1))
		return true, nil
	case reflect.TypeFor[MapSlice]():
		ms, ok := rv.Interface().(MapSlice)
		if !ok {
			return false, nil
		}
		return true, e.encodeMapSlice(ms, 0)
	}
	return false, nil
}

// encodeCustomMarshaler invokes a user-registered marshaler for rv's type,
// if one exists. The result bytes are passed through verbatim.
func (e *encoder) encodeCustomMarshaler(rv reflect.Value) (bool, error) {
	if e.opts.customMarshalers == nil {
		return false, nil
	}
	fn, ok := e.opts.customMarshalers[rv.Type()]
	if !ok {
		return false, nil
	}
	result := reflect.ValueOf(fn).Call([]reflect.Value{rv})
	if !result[1].IsNil() {
		err, ok := result[1].Interface().(error)
		if ok {
			return true, err
		}
	}
	out, ok := result[0].Interface().([]byte)
	if !ok {
		return true, fmt.Errorf("%w: custom marshaler for %s did not return []byte", ErrUnsupportedValue, rv.Type())
	}
	e.buf.Write(out)
	return true, nil
}

// encodeMarshaler dispatches to user-defined marshaler interfaces in
// priority order: MarshalerContext, Marshaler, json.Marshaler,
// TextMarshaler.
func (e *encoder) encodeMarshaler(rv reflect.Value) (bool, error) {
	// Check pointer-receiver methods first (they have the larger method set).
	if rv.CanAddr() {
		if handled, err := e.encodeMarshalerForValue(rv.Addr()); handled {
			return true, err
		}
	}
	// Fall back to value-receiver methods.
	if rv.CanInterface() {
		return e.encodeMarshalerForValue(rv)
	}
	return false, nil
}

// encodeMarshalerForValue tries each marshaler interface against rv.
func (e *encoder) encodeMarshalerForValue(rv reflect.Value) (bool, error) {
	if !rv.CanInterface() {
		return false, nil
	}
	iface := rv.Interface()

	if m, ok := iface.(MarshalerContext); ok {
		out, err := m.MarshalJSONC(e.ctx)
		if err != nil {
			return true, err
		}
		e.buf.Write(out)
		return true, nil
	}
	if m, ok := iface.(Marshaler); ok {
		out, err := m.MarshalJSONC()
		if err != nil {
			return true, err
		}
		e.buf.Write(out)
		return true, nil
	}
	if m, ok := iface.(json.Marshaler); ok {
		out, err := m.MarshalJSON()
		if err != nil {
			return true, err
		}
		e.buf.Write(out)
		return true, nil
	}
	if m, ok := iface.(encoding.TextMarshaler); ok {
		// TextMarshaler produces a string value.
		text, err := m.MarshalText()
		if err != nil {
			return true, err
		}
		e.writeQuotedString(string(text))
		return true, nil
	}
	return false, nil
}

// encodeSlice handles slice values (including []byte → base64).
func (e *encoder) encodeSlice(rv reflect.Value, depth int) error {
	if rv.IsNil() {
		e.buf.WriteString("null")
		return nil
	}
	// []byte is encoded as a base64 string, matching stdlib.
	if rv.Type().Elem().Kind() == reflect.Uint8 {
		e.writeBase64(rv.Bytes())
		return nil
	}
	return e.encodeListLike(rv, depth)
}

// encodeArrayValue handles array values (fixed-size, including [N]byte).
func (e *encoder) encodeArrayValue(rv reflect.Value, depth int) error {
	if rv.Type().Elem().Kind() == reflect.Uint8 {
		// Copy the array into a slice so we can base64-encode it.
		buf := make([]byte, rv.Len())
		for i := range rv.Len() {
			buf[i] = byte(rv.Index(i).Uint())
		}
		e.writeBase64(buf)
		return nil
	}
	return e.encodeListLike(rv, depth)
}

// encodeListLike emits a slice or array as a JSONC array.
func (e *encoder) encodeListLike(rv reflect.Value, depth int) error {
	n := rv.Len()
	if n == 0 {
		e.buf.WriteString("[]")
		return nil
	}
	multi := e.multiline() || e.opts.arrayMultiline
	e.buf.WriteByte('[')
	if multi {
		e.buf.WriteByte('\n')
	}
	for i := range n {
		if multi {
			e.writeIndent(depth + 1)
		}
		if err := e.encodeValue(rv.Index(i), depth+1); err != nil {
			return err
		}
		if i < n-1 {
			e.buf.WriteByte(',')
			if multi {
				e.buf.WriteByte('\n')
			} else {
				e.buf.WriteByte(' ')
			}
		} else if multi && e.opts.trailingComma && !e.opts.strictJSONOutput {
			e.buf.WriteByte(',')
		}
	}
	if multi {
		e.buf.WriteByte('\n')
		e.writeIndent(depth)
	}
	e.buf.WriteByte(']')
	return nil
}

// writeBase64 writes the standard base64 encoding of data as a JSON string.
func (e *encoder) writeBase64(data []byte) {
	if data == nil {
		e.buf.WriteString("null")
		return
	}
	e.buf.WriteByte('"')
	const enc = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	// Manual base64 to avoid an extra allocation; matches encoding/base64.StdEncoding.
	for i := 0; i < len(data); i += 3 {
		var b1, b2, b3 byte
		b1 = data[i]
		switch {
		case i+2 < len(data):
			b2 = data[i+1]
			b3 = data[i+2]
			e.buf.WriteByte(enc[b1>>2])
			e.buf.WriteByte(enc[(b1&0x03)<<4|b2>>4])
			e.buf.WriteByte(enc[(b2&0x0f)<<2|b3>>6])
			e.buf.WriteByte(enc[b3&0x3f])
		case i+1 < len(data):
			b2 = data[i+1]
			e.buf.WriteByte(enc[b1>>2])
			e.buf.WriteByte(enc[(b1&0x03)<<4|b2>>4])
			e.buf.WriteByte(enc[(b2&0x0f)<<2])
			e.buf.WriteByte('=')
		default:
			e.buf.WriteByte(enc[b1>>2])
			e.buf.WriteByte(enc[(b1&0x03)<<4])
			e.buf.WriteByte('=')
			e.buf.WriteByte('=')
		}
	}
	e.buf.WriteByte('"')
}

// encodeMap handles map values. Key types: string, integers, and types
// implementing TextMarshaler.
func (e *encoder) encodeMap(rv reflect.Value, depth int) error {
	if rv.IsNil() {
		e.buf.WriteString("null")
		return nil
	}
	keys := rv.MapKeys()
	if len(keys) == 0 {
		e.buf.WriteString("{}")
		return nil
	}

	// Build (string-key, reflect.Value) pairs, then sort for deterministic
	// output (lexicographic by default).
	pairs := make([]mapKV, len(keys))
	for i, k := range keys {
		s, err := mapKeyString(k)
		if err != nil {
			return err
		}
		pairs[i] = mapKV{keyStr: s, val: rv.MapIndex(k)}
	}
	sortMapPairs(pairs, e.opts.mapKeyOrder)

	multi := e.multiline()
	e.buf.WriteByte('{')
	if multi {
		e.buf.WriteByte('\n')
	}
	for i, p := range pairs {
		if multi {
			e.writeIndent(depth + 1)
		}
		e.writeQuotedString(p.keyStr)
		e.buf.WriteString(": ")
		if err := e.encodeValue(p.val, depth+1); err != nil {
			return err
		}
		if i < len(pairs)-1 {
			e.buf.WriteByte(',')
			if multi {
				e.buf.WriteByte('\n')
			} else {
				e.buf.WriteByte(' ')
			}
		} else if multi && e.opts.trailingComma && !e.opts.strictJSONOutput {
			e.buf.WriteByte(',')
		}
	}
	if multi {
		e.buf.WriteByte('\n')
		e.writeIndent(depth)
	}
	e.buf.WriteByte('}')
	return nil
}

// encodeMapSlice emits a MapSlice as an object, preserving insertion order.
func (e *encoder) encodeMapSlice(ms MapSlice, depth int) error {
	if len(ms) == 0 {
		e.buf.WriteString("{}")
		return nil
	}
	multi := e.multiline()
	e.buf.WriteByte('{')
	if multi {
		e.buf.WriteByte('\n')
	}
	for i, item := range ms {
		if multi {
			e.writeIndent(depth + 1)
		}
		e.writeQuotedString(item.Key)
		e.buf.WriteString(": ")
		if err := e.encodeValue(reflect.ValueOf(item.Value), depth+1); err != nil {
			return err
		}
		if i < len(ms)-1 {
			e.buf.WriteByte(',')
			if multi {
				e.buf.WriteByte('\n')
			} else {
				e.buf.WriteByte(' ')
			}
		} else if multi && e.opts.trailingComma && !e.opts.strictJSONOutput {
			e.buf.WriteByte(',')
		}
	}
	if multi {
		e.buf.WriteByte('\n')
		e.writeIndent(depth)
	}
	e.buf.WriteByte('}')
	return nil
}

// mapKeyString converts a map-key reflect.Value into the string used as the
// JSON object key.
func mapKeyString(k reflect.Value) (string, error) {
	if k.CanInterface() {
		if tm, ok := k.Interface().(encoding.TextMarshaler); ok {
			text, err := tm.MarshalText()
			if err != nil {
				return "", err
			}
			return string(text), nil
		}
	}
	switch k.Kind() {
	case reflect.String:
		return k.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(k.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(k.Uint(), 10), nil
	}
	return "", fmt.Errorf("%w: unsupported map key type %s", ErrUnsupportedValue, k.Type())
}

// mapKV is a (string-key, reflect.Value) pair used during map encoding.
type mapKV struct {
	keyStr string
	val    reflect.Value
}

// sortMapPairs sorts a slice of (key, value) pairs according to the
// configured ordering policy.
func sortMapPairs(pairs []mapKV, order MapKeyOrder) {
	switch order {
	case MapKeyOrderInsertion:
		// Caller order — leave alone. Note: Go map iteration is
		// non-deterministic, so this option is most meaningful when the
		// input is a MapSlice (handled separately).
		return
	default: // MapKeyOrderLexicographic
		sort.Slice(pairs, func(i, j int) bool {
			return pairs[i].keyStr < pairs[j].keyStr
		})
	}
}

// encodeStruct emits a struct as a JSONC object, honoring tags, omitempty,
// omitzero, asString, required, and comment annotations.
func (e *encoder) encodeStruct(rv reflect.Value, depth int) error {
	sf := getStructFields(rv.Type())

	// Collect fields that will actually be emitted, after omit checks.
	type emit struct {
		fi      fieldInfo
		val     reflect.Value
		comment string
	}
	emits := make([]emit, 0, len(sf.fields))
	for _, fi := range sf.fields {
		fv := fieldByIndexNoAlloc(rv, fi.index)
		if !fv.IsValid() {
			continue
		}
		if e.shouldOmit(fi, fv) {
			continue
		}
		emits = append(emits, emit{fi: fi, val: fv, comment: fi.comment})
	}

	if len(emits) == 0 {
		e.buf.WriteString("{}")
		return nil
	}

	multi := e.multiline()
	e.buf.WriteByte('{')
	if multi {
		e.buf.WriteByte('\n')
	}
	for i, em := range emits {
		// Emit comment header (from `comment:"..."` tag) on its own line(s)
		// when multi-line.
		if multi && em.comment != "" && !e.opts.strictJSONOutput {
			e.writeCommentBlock(em.comment, depth+1)
		}
		if multi {
			e.writeIndent(depth + 1)
		}
		e.writeQuotedString(em.fi.name)
		e.buf.WriteString(": ")

		if em.fi.commented && !e.opts.strictJSONOutput {
			// Emit the value preceded by "//" so it is a comment in source —
			// requires multi-line output to be meaningful.
			if multi {
				e.buf.WriteString("/* ")
				if err := e.encodeFieldValue(em.fi, em.val, depth+1); err != nil {
					return err
				}
				e.buf.WriteString(" */")
				if i < len(emits)-1 {
					// commented members produce no trailing comma since they
					// have no value position; emit nothing.
					_ = i
				}
				e.buf.WriteByte('\n')
				continue
			}
		}

		if err := e.encodeFieldValue(em.fi, em.val, depth+1); err != nil {
			return err
		}
		if i < len(emits)-1 {
			e.buf.WriteByte(',')
			if multi {
				e.buf.WriteByte('\n')
			} else {
				e.buf.WriteByte(' ')
			}
		} else if multi && e.opts.trailingComma && !e.opts.strictJSONOutput {
			e.buf.WriteByte(',')
		}
	}
	if multi {
		e.buf.WriteByte('\n')
		e.writeIndent(depth)
	}
	e.buf.WriteByte('}')
	return nil
}

// encodeFieldValue handles per-field options such as `,string`.
func (e *encoder) encodeFieldValue(fi fieldInfo, fv reflect.Value, depth int) error {
	if fi.asString && isStringEncodable(fv) {
		// Encode primitive into the buffer, capture the bytes, truncate, then
		// re-emit as a JSON-quoted string.
		start := e.buf.Len()
		if err := e.encodeValue(fv, depth); err != nil {
			return err
		}
		encoded := append([]byte(nil), e.buf.Bytes()[start:]...)
		e.buf.Truncate(start)
		e.writeQuotedString(string(encoded))
		return nil
	}
	return e.encodeValue(fv, depth)
}

// shouldOmit reports whether a field should be omitted under omitempty/
// omitzero rules.
func (e *encoder) shouldOmit(fi fieldInfo, fv reflect.Value) bool {
	if fi.omitZero && fv.IsZero() {
		return true
	}
	if (fi.omitEmpty || e.opts.omitEmpty) && isEmptyValue(fv) {
		return true
	}
	return false
}

// fieldByIndexNoAlloc returns the field at the given index path. Unlike the
// decoder's variant, it does not allocate intermediate pointer-to-struct
// fields — for encoding, a nil intermediate pointer means "absent" and we
// return an invalid Value so the field is skipped.
func fieldByIndexNoAlloc(rv reflect.Value, index []int) reflect.Value {
	for i, idx := range index {
		if i > 0 {
			if rv.Kind() == reflect.Pointer {
				if rv.IsNil() {
					return reflect.Value{}
				}
				rv = rv.Elem()
			}
		}
		rv = rv.Field(idx)
	}
	return rv
}

// isEmptyValue reports whether rv is the empty value for stdlib's
// omitempty: false bool, 0 numbers, "" string, nil pointer/interface, and
// zero-length slice/map/array.
func isEmptyValue(rv reflect.Value) bool {
	switch rv.Kind() {
	case reflect.Bool:
		return !rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return rv.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() == 0
	case reflect.String, reflect.Array, reflect.Slice, reflect.Map:
		return rv.Len() == 0
	case reflect.Pointer, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

// isStringEncodable reports whether rv is a primitive that can be wrapped
// in a JSON string for the `,string` tag option.
func isStringEncodable(rv reflect.Value) bool {
	switch rv.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.String:
		return true
	}
	return false
}
