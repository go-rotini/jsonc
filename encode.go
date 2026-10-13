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
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Marshaler is implemented by types that encode themselves as JSONC. The
// returned bytes must hold exactly one JSONC value; the encoder parses
// them and writes the value in the surrounding output's layout, so
// indentation and [WithStrictJSONOutput] apply. Comments before or after
// the value are dropped; comments inside it are kept in multi-line
// output that is not strict JSON. A type's
// [encoding/json.Marshaler] implementation is used when it has no
// MarshalJSONC method.
type Marshaler interface {
	MarshalJSONC() ([]byte, error)
}

// MarshalerContext is like [Marshaler] but receives the context passed to
// [Encoder.EncodeContext] or set with [Encoder.SetContext]; other encoding
// functions pass [context.Background]. It takes precedence over [Marshaler].
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
	// visited tracks the pointers currently on the encoding stack, and the
	// maps and slices once nesting passes cycleCheckDepth; used to detect
	// cycles.
	visited map[visitKey]bool
	// nesting counts the maps and slices currently on the encoding stack.
	nesting int
	// path is the current dot-and-bracket path during reflection encoding,
	// used to look up [Comment] annotations registered via [WithComment].
	// Each segment is the field/key (".name") or array index ("[N]"); the
	// leading dot of the first segment is stripped by currentPath.
	path []string
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
		visited: make(map[visitKey]bool),
	}
}

// cycleCheckDepth is the map and slice nesting depth past which the
// encoder starts checking them for cycles. Shallower values cannot be
// cyclic without being deeper too, so the check costs nothing for them.
const cycleCheckDepth = 1000

// visitKey identifies a pointer, map or slice on the encoding stack. A
// slice is identified by its data pointer and length, so a sub-slice of
// itself is not mistaken for the same value.
type visitKey struct {
	kind reflect.Kind
	ptr  uintptr
	len  int
}

// enterContainer records that the map or slice rv is being encoded. It
// returns an error if rv is already on the stack (a cycle); otherwise the
// caller must pass the returned key to [encoder.leaveContainer] when done.
func (e *encoder) enterContainer(rv reflect.Value) (visitKey, error) {
	if e.visited == nil {
		return visitKey{}, nil
	}
	e.nesting++
	if e.nesting <= cycleCheckDepth {
		return visitKey{}, nil
	}
	key := visitKey{kind: rv.Kind(), ptr: rv.Pointer()}
	if rv.Kind() == reflect.Slice {
		key.len = rv.Len()
	}
	if e.visited[key] {
		e.nesting--
		return visitKey{}, fmt.Errorf("%w: cyclic value of type %s", ErrUnsupportedValue, rv.Type())
	}
	e.visited[key] = true
	return key, nil
}

// leaveContainer undoes [encoder.enterContainer].
func (e *encoder) leaveContainer(key visitKey) {
	if e.visited == nil {
		return
	}
	if key.kind != reflect.Invalid {
		delete(e.visited, key)
	}
	e.nesting--
}

// encodeNode emits the AST rooted at n as JSONC bytes. The output is
// indent-aware (driven by opts.indent) and preserves comment annotations
// when output is multi-line.
func (e *encoder) encodeNode(n *node) ([]byte, error) {
	if n == nil {
		return nil, nil
	}
	e.writeHeadComments(n, 0)
	e.writeValue(n, 0)
	if e.nodeComments() {
		e.writeLineComments(n)
		if after := afterComments(n); after != "" {
			e.buf.WriteByte('\n')
			e.writeCommentBlock(after, 0)
		}
	}
	return e.buf.Bytes(), nil
}

// afterComments returns the comments written on the lines after n: its
// foot comment unless n is a container (which writes it before its
// closing delimiter), then its trail comment. A member also carries its
// value's trail comment.
func afterComments(n *node) string {
	var after string
	if !isContainer(n) {
		after = n.footComment
	}
	if n.kind == nodeKeyValue && len(n.children) > 0 {
		after = appendCommentBlock(after, n.children[0].trailComment)
	}
	return appendCommentBlock(after, n.trailComment)
}

// nodeComments reports whether AST comment annotations are written: only
// in multi-line output that is not strict JSON.
func (e *encoder) nodeComments() bool {
	return e.multiline() && !e.opts.strictJSONOutput
}

// isContainer reports whether n is an object or array, whose foot comment
// belongs before its closing delimiter.
func isContainer(n *node) bool {
	return n.kind == nodeObject || n.kind == nodeArray
}

// writeHeadComments writes the comments that go on the lines before n. For
// a member, the value's head comment joins the member's.
func (e *encoder) writeHeadComments(n *node, depth int) {
	if !e.nodeComments() {
		return
	}
	head := n.headComment
	if n.kind == nodeKeyValue && len(n.children) > 0 {
		head = appendCommentBlock(head, n.children[0].headComment)
	}
	if head != "" {
		e.writeCommentBlock(head, depth)
	}
}

// writeLineComments writes the comments that go after n on its last line:
// a member's value's and then n's own. All but the last use the /* */ form
// so they cannot hide the ones after them.
func (e *encoder) writeLineComments(n *node) {
	texts := make([]string, 0, 2)
	if n.kind == nodeKeyValue && len(n.children) > 0 && n.children[0].comment != "" {
		texts = append(texts, n.children[0].comment)
	}
	if n.comment != "" {
		texts = append(texts, n.comment)
	}
	for i, text := range texts {
		if i < len(texts)-1 {
			e.writeInlineBlockComment(text)
			continue
		}
		e.writeInlineComment(text)
	}
}

// writeChildren emits the members or elements of a multi-line container,
// each on its own line at depth, with its comments around it: head
// comments above, line comments after the comma, and [afterComments] on
// the lines below.
func (e *encoder) writeChildren(n *node, depth int) {
	// Count value-bearing children up front so trailing-comma logic can
	// distinguish "last member" from "last child" (CommentNode siblings
	// don't count).
	memberCount := 0
	for _, child := range n.children {
		if child.kind != nodeComment {
			memberCount++
		}
	}

	emitted := 0
	for _, child := range n.children {
		if child.kind == nodeComment {
			if !e.opts.strictJSONOutput {
				e.writeOrphanComment(child, depth)
			}
			continue
		}
		e.writeHeadComments(child, depth)
		e.writeIndent(depth)
		e.writeValue(child, depth)
		emitted++
		if emitted < memberCount || (e.opts.trailingComma && !e.opts.strictJSONOutput) {
			e.buf.WriteByte(',')
		}
		if e.nodeComments() {
			e.writeLineComments(child)
		}
		e.buf.WriteByte('\n')
		if e.nodeComments() {
			if after := afterComments(child); after != "" {
				e.writeCommentBlock(after, depth)
			}
		}
	}
}

// writeValue emits n without its own head, line or (for a member or
// scalar) foot comments; [encoder.writeChildren] and
// [encoder.encodeNode] place those.
func (e *encoder) writeValue(n *node, depth int) {
	switch n.kind {
	case nodeObject:
		e.writeObject(n, depth)
	case nodeArray:
		e.writeArray(n, depth)
	case nodeKeyValue:
		e.writeSourceString(n.rawKey, n.key)
		e.buf.WriteString(": ")
		if len(n.children) > 0 {
			e.writeValue(n.children[0], depth)
		} else {
			e.buf.WriteString("null")
		}
	case nodeString:
		e.writeSourceString(n.rawValue, n.value)
	case nodeNumber, nodeBool, nodeNull:
		if n.rawValue != "" {
			e.buf.WriteString(n.rawValue)
		} else {
			e.buf.WriteString(n.value)
		}
	case nodeComment:
		if !e.opts.strictJSONOutput {
			e.writeOrphanComment(n, depth)
		}
	}
}

// writeSourceString writes a string from its source text raw when
// [encoder.rawStringOK] allows it, and otherwise quotes value.
func (e *encoder) writeSourceString(raw, value string) {
	if e.rawStringOK(raw) {
		e.buf.WriteString(raw)
		return
	}
	e.writeQuotedString(value)
}

// rawStringOK reports whether a string's source text raw can be written as
// is: it is valid UTF-8 and, under [WithEscapeHTML], holds none of the
// characters that option escapes.
func (e *encoder) rawStringOK(raw string) bool {
	return raw != "" && utf8.ValidString(raw) &&
		(!e.opts.escapeHTML || !strings.ContainsAny(raw, "<>&\u2028\u2029"))
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
		// Comments are dropped in compact output regardless of options.
		if child.kind == nodeComment {
			continue
		}
		if !first {
			e.buf.WriteByte(',')
			e.buf.WriteByte(' ')
		}
		e.writeValue(child, 0)
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
	e.writeChildren(n, depth+1)
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
		e.writeValue(child, 0)
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
	e.writeChildren(n, depth+1)
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
// given depth, one // comment per line. A space follows the // unless the
// line is empty or already starts with one, and trailing blanks are
// dropped. Line endings (\r, \r\n, \n) are normalized so embedded CRs do
// not prematurely terminate the // comment scope. Leading blank lines are
// dropped, as the parser drops them when it reads the output back.
func (e *encoder) writeCommentBlock(text string, depth int) {
	started := false
	for line := range strings.SplitSeq(normalizeLineEndings(text), "\n") {
		if !started && strings.TrimSpace(line) == "" {
			continue
		}
		started = true
		e.writeIndent(depth)
		e.writeLineCommentText(line)
		e.buf.WriteByte('\n')
	}
}

// writeLineCommentText writes "//" and text, with one space between them
// unless text is blank or starts with a space or tab. Trailing blanks are
// dropped.
func (e *encoder) writeLineCommentText(text string) {
	text = strings.TrimRight(text, " \t")
	e.buf.WriteString("//")
	if text != "" && text[0] != ' ' && text[0] != '\t' {
		e.buf.WriteByte(' ')
	}
	e.buf.WriteString(text)
}

// writeInlineComment emits an inline comment after a value on the same
// line. If the text contains a newline (or a CR — which would also
// terminate a // comment), it falls back to the /* … */ form so the
// comment does not leak past its intended position.
func (e *encoder) writeInlineComment(text string) {
	if strings.ContainsAny(text, "\n\r") {
		e.writeInlineBlockComment(text)
		return
	}
	e.buf.WriteByte(' ')
	e.writeLineCommentText(text)
}

// writeInlineBlockComment emits text as a /* … */ comment after a value,
// with a space inside each delimiter unless text already has one there.
func (e *encoder) writeInlineBlockComment(text string) {
	text = sanitizeBlockCommentText(text)
	isBlank := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' }
	e.buf.WriteString(" /*")
	if text == "" || !isBlank(text[0]) {
		e.buf.WriteByte(' ')
	}
	e.buf.WriteString(text)
	if text == "" || !isBlank(text[len(text)-1]) {
		e.buf.WriteByte(' ')
	}
	e.buf.WriteString("*/")
}

// normalizeLineEndings collapses \r\n and bare \r to \n so callers can
// split on '\n' alone.
func normalizeLineEndings(text string) string {
	if !strings.ContainsRune(text, '\r') {
		return text
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

// sanitizeBlockCommentText defangs any "*/" sequence in the text so it
// cannot prematurely terminate the surrounding /* … */ block. The
// substitution preserves visual intent: "*/" → "* /". Idempotent.
func sanitizeBlockCommentText(text string) string {
	if !strings.Contains(text, "*/") {
		return text
	}
	return strings.ReplaceAll(text, "*/", "* /")
}

// writeOrphanComment emits a CommentNode at the given depth.
func (e *encoder) writeOrphanComment(n *node, depth int) {
	switch n.style {
	case styleBlockComment:
		e.writeIndent(depth)
		e.buf.WriteString("/*")
		e.buf.WriteString(sanitizeBlockCommentText(n.value))
		e.buf.WriteString("*/")
		e.buf.WriteByte('\n')
	default:
		e.writeIndent(depth)
		e.buf.WriteString("//")
		// The scanner stops a line comment at any line terminator, so the
		// stored value should not contain one. Normalize anyway so a
		// programmatically constructed Node cannot leak a newline that
		// would close the comment scope mid-token.
		e.buf.WriteString(strings.ReplaceAll(normalizeLineEndings(n.value), "\n", " "))
		e.buf.WriteByte('\n')
	}
}

// writeQuotedString writes s as a JSON-quoted string, escaping per RFC 8259.
// When opts.escapeHTML is true, <, > and & and the line separators U+2028
// and U+2029 are escaped as well, as in encoding/json.
func (e *encoder) writeQuotedString(s string) {
	e.buf.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		// U+2028 and U+2029 encode as E2 80 A8 and E2 80 A9.
		if c == 0xE2 && e.opts.escapeHTML && i+2 < len(s) && s[i+1] == 0x80 && s[i+2]&^1 == 0xA8 {
			e.buf.WriteString(`\u202`)
			e.buf.WriteByte(hexDigit(s[i+2] & 0xF))
			i += 2
			continue
		}
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
			switch {
			case c < 0x20:
				e.buf.WriteString(`\u00`)
				e.buf.WriteByte(hexDigit(c >> 4))
				e.buf.WriteByte(hexDigit(c & 0xF))
			case c < utf8.RuneSelf:
				e.buf.WriteByte(c)
			default:
				// Invalid UTF-8 is written as U+FFFD, as in encoding/json.
				r, size := utf8.DecodeRuneInString(s[i:])
				if r == utf8.RuneError && size == 1 {
					e.buf.WriteString(`�`)
					continue
				}
				e.buf.WriteString(s[i : i+size])
				i += size - 1
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

// pushPathKey appends an object-member segment to the encoding path stack
// used by [WithComment] lookups: ".name", or `["name"]` when name contains
// "." or "[", so it cannot be confused with a nested path.
func (e *encoder) pushPathKey(name string) {
	if strings.ContainsAny(name, ".[") {
		e.path = append(e.path, `["`+name+`"]`)
		return
	}
	e.path = append(e.path, "."+name)
}

// pushPathIndex appends an array-element segment ("[i]") to the encoding
// path stack.
func (e *encoder) pushPathIndex(i int) {
	e.path = append(e.path, "["+strconv.Itoa(i)+"]")
}

// popPath removes the last segment from the encoding path stack.
func (e *encoder) popPath() {
	if len(e.path) > 0 {
		e.path = e.path[:len(e.path)-1]
	}
}

// currentPath joins the path stack into the dotted/bracketed form used as
// the key in [WithComment]. Returns "" at the root.
func (e *encoder) currentPath() string {
	if len(e.path) == 0 {
		return ""
	}
	return strings.TrimPrefix(strings.Join(e.path, ""), ".")
}

// commentsAt returns the [Comment] annotations registered for the current
// path that match position pos. Returns nil in compact (non-multiline)
// output, in strict-JSON output mode, or when no comments are registered —
// in any of those cases the encoder skips comment emission entirely.
func (e *encoder) commentsAt(pos CommentPosition) []Comment {
	if !e.multiline() || e.opts.strictJSONOutput || len(e.opts.comments) == 0 {
		return nil
	}
	all := e.opts.comments[e.currentPath()]
	if len(all) == 0 {
		return nil
	}
	out := make([]Comment, 0, len(all))
	for _, c := range all {
		if c.Position == pos {
			out = append(out, c)
		}
	}
	return out
}

// emitInlineComments appends each Comment after a value on the same line.
// Each comment uses the `//` form when single-line and switches to
// `/* … */` when its text contains a newline or carriage return, so
// embedded line terminators do not leak past the value.
func (e *encoder) emitInlineComments(comments []Comment) {
	for _, c := range comments {
		e.writeInlineComment(c.Text)
	}
}

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

	// Special-cased types take priority over the marshaler interfaces:
	// a raw-bytes / raw-number / base64 path preserves the source form,
	// whereas the interface dispatch would re-encode through the user code.
	if handled, err := e.encodeSpecialType(rv, depth); handled {
		return err
	}

	if handled, err := e.encodeCustomMarshaler(rv, depth); handled {
		return err
	}

	if handled, err := e.encodeMarshaler(rv, depth); handled {
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
	key := visitKey{kind: reflect.Pointer, ptr: rv.Pointer()}
	if e.visited != nil && e.visited[key] {
		return fmt.Errorf("%w: cyclic value of type %s", ErrUnsupportedValue, rv.Type())
	}
	if e.visited != nil {
		e.visited[key] = true
		defer delete(e.visited, key)
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
	// Match encoding/json's float formatting: choose 'e' vs 'f' based on
	// magnitude thresholds, then strip a leading zero from the exponent.
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
		// e.g. "1e+09" -> "1e+9", matching stdlib output.
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
func (e *encoder) encodeSpecialType(rv reflect.Value, depth int) (bool, error) {
	switch rv.Type() {
	case reflect.TypeFor[RawValue]():
		raw := rv.Bytes()
		if len(raw) == 0 {
			e.buf.WriteString("null")
			return true, nil
		}
		// The bytes must be one JSONC value. In strict-JSON-output mode the
		// parsed value is re-emitted to drop comments and trailing commas;
		// otherwise the source text is written verbatim, less the blanks
		// around it and any comments after a // comment that follows it,
		// which would hide what comes next.
		if !e.opts.strictJSONOutput {
			if _, err := parseEmbedded(raw, false); err != nil {
				return true, fmt.Errorf("jsonc: invalid RawValue: %w", err)
			}
			e.buf.Write(rawValueText(raw))
			return true, nil
		}
		return true, e.writeEmbedded(raw, false, depth, "RawValue", nil)
	case reflect.TypeFor[json.RawMessage]():
		raw := rv.Bytes()
		if len(raw) == 0 {
			e.buf.WriteString("null")
			return true, nil
		}
		return true, e.writeEmbedded(raw, true, depth, "json.RawMessage", nil)
	case reflect.TypeFor[json.Number]():
		s := rv.String()
		if s == "" {
			e.buf.WriteString("0")
			return true, nil
		}
		if !isNumberText(s) {
			return true, fmt.Errorf("%w: invalid number literal %q", ErrUnsupportedValue, s)
		}
		e.buf.WriteString(s)
		return true, nil
	case reflect.TypeFor[time.Time]():
		t, ok := reflect.TypeAssert[time.Time](rv)
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
		bi, ok := reflect.TypeAssert[big.Int](rv)
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
		bi, ok := reflect.TypeAssert[*big.Int](rv)
		if !ok || bi == nil {
			e.buf.WriteString("null")
			return true, nil
		}
		e.buf.WriteString(bi.String())
		return true, nil
	case reflect.TypeFor[big.Float]():
		bf, ok := reflect.TypeAssert[big.Float](rv)
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
		bf, ok := reflect.TypeAssert[*big.Float](rv)
		if !ok || bf == nil {
			e.buf.WriteString("null")
			return true, nil
		}
		e.buf.WriteString(bf.Text('g', -1))
		return true, nil
	case reflect.TypeFor[MapSlice]():
		ms, ok := reflect.TypeAssert[MapSlice](rv)
		if !ok {
			return false, nil
		}
		key, err := e.enterContainer(rv)
		if err != nil {
			return true, err
		}
		defer e.leaveContainer(key)
		return true, e.encodeMapSlice(ms, depth)
	}
	return false, nil
}

// rawValueText returns the part of raw, a valid JSONC value, that a
// RawValue writes: from its first token to its last, or to the end of the
// value when a // comment follows the value.
func rawValueText(raw []byte) []byte {
	sc := scanner{src: raw, line: 1, col: 1, rawOnly: true}
	start, valueEnd, end := -1, 0, 0
	lineCommentAfter := false
	for {
		tok, err := sc.nextValueToken()
		if err != nil || tok.kind == tokenEOF {
			break
		}
		if start < 0 {
			start = tok.pos.Offset
		}
		switch tok.kind {
		case tokenLineComment:
			lineCommentAfter = true
			end = sc.pos
		case tokenBlockComment:
			end = sc.pos
		default:
			valueEnd, end = sc.pos, sc.pos
			lineCommentAfter = false
		}
	}
	if start < 0 {
		return raw
	}
	if lineCommentAfter {
		return raw[start:valueEnd]
	}
	return raw[start:end]
}

// encodeCustomMarshaler invokes a user-registered marshaler for rv's type,
// if one exists, and writes its output with [encoder.writeEmbedded].
func (e *encoder) encodeCustomMarshaler(rv reflect.Value, depth int) (bool, error) {
	if e.opts.customMarshalers == nil {
		return false, nil
	}
	fn, ok := e.opts.customMarshalers[rv.Type()]
	if !ok {
		return false, nil
	}
	result := reflect.ValueOf(fn).Call([]reflect.Value{rv})
	if !result[1].IsNil() {
		err, ok := reflect.TypeAssert[error](result[1])
		if ok {
			return true, err
		}
	}
	out, ok := reflect.TypeAssert[[]byte](result[0])
	if !ok {
		return true, fmt.Errorf("%w: custom marshaler for %s did not return []byte", ErrUnsupportedValue, rv.Type())
	}
	return true, e.writeEmbedded(out, false, depth, "custom marshaler", rv.Type())
}

// encodeMarshaler dispatches to user-defined marshaler interfaces in
// priority order: MarshalerContext, Marshaler, json.Marshaler,
// TextMarshaler. Pointer-receiver methods are tried first because the
// pointer method set is a superset of the value method set.
func (e *encoder) encodeMarshaler(rv reflect.Value, depth int) (bool, error) {
	if rv.CanAddr() {
		if handled, err := e.encodeMarshalerForValue(rv.Addr(), depth); handled {
			return true, err
		}
	}
	if rv.CanInterface() {
		return e.encodeMarshalerForValue(rv, depth)
	}
	return false, nil
}

// encodeMarshalerForValue tries each marshaler interface against rv. A nil
// pointer is never passed to a method; [encoder.encodePointer] writes null
// for it, as encoding/json does.
func (e *encoder) encodeMarshalerForValue(rv reflect.Value, depth int) (bool, error) {
	if !rv.CanInterface() || rv.Kind() == reflect.Interface {
		return false, nil
	}
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		return false, nil
	}
	iface := rv.Interface()

	if m, ok := iface.(MarshalerContext); ok {
		out, err := m.MarshalJSONC(e.ctx)
		if err != nil {
			return true, err
		}
		return true, e.writeEmbedded(out, false, depth, "MarshalJSONC", rv.Type())
	}
	if m, ok := iface.(Marshaler); ok {
		out, err := m.MarshalJSONC()
		if err != nil {
			return true, err
		}
		return true, e.writeEmbedded(out, false, depth, "MarshalJSONC", rv.Type())
	}
	if m, ok := iface.(json.Marshaler); ok {
		out, err := m.MarshalJSON()
		if err != nil {
			return true, err
		}
		return true, e.writeEmbedded(out, true, depth, "MarshalJSON", rv.Type())
	}
	if m, ok := iface.(encoding.TextMarshaler); ok {
		text, err := m.MarshalText()
		if err != nil {
			return true, err
		}
		e.writeQuotedString(string(text))
		return true, nil
	}
	return false, nil
}

// embeddedMaxDepth bounds the nesting of marshaler output, as
// encoding/json does.
const embeddedMaxDepth = 10000

// parseEmbedded parses the output of a marshaler or a raw value, which
// must be exactly one value: strict RFC 8259 JSON when strictJSON is set,
// otherwise JSONC. Duplicate keys are allowed, as encoding/json allows
// them in such output; the last one wins.
func parseEmbedded(data []byte, strictJSON bool) (*node, error) {
	if bytes.HasPrefix(data, utf8BOM) {
		return nil, &SyntaxError{Message: "unexpected byte order mark", Pos: Position{Line: 1, Column: 1}}
	}
	opts := &decoderOptions{strictJSON: strictJSON, allowDuplicateKeys: true, maxDepth: embeddedMaxDepth}
	return newParser(data, opts).parse()
}

// writeEmbedded validates data with [parseEmbedded] and writes the value at
// depth in the encoder's layout, so indentation and strict JSON output
// apply. source, and t when not nil, name the source in the error.
func (e *encoder) writeEmbedded(data []byte, strictJSON bool, depth int, source string, t reflect.Type) error {
	if e.relayout(data, strictJSON, depth) {
		return nil
	}
	n, err := parseEmbedded(data, strictJSON)
	if err != nil {
		if t != nil {
			source += " for " + t.String()
		}
		return fmt.Errorf("jsonc: invalid output from %s (the position is within that output): %w", source, err)
	}
	e.writeValue(n, depth)
	return nil
}

// relayout writes data as [encoder.writeEmbedded] would, without building
// a tree, when data is plain JSON: no comments, trailing commas or
// duplicate keys. Otherwise it writes nothing and returns false, and the
// caller takes the tree path, which writes the same output or reports the
// error.
func (e *encoder) relayout(data []byte, strictJSON bool, depth int) bool {
	start := e.buf.Len()
	ok, scalar := e.relayoutScalar(data, strictJSON)
	if !scalar {
		ok = e.relayoutTokens(data, strictJSON, depth)
	}
	if !ok {
		e.buf.Truncate(start)
	}
	return ok
}

// relayoutScalar handles data whose first token is a scalar, the usual
// output of a marshaler. scalar reports whether it did; ok whether data
// was that scalar alone, now written.
func (e *encoder) relayoutScalar(data []byte, strictJSON bool) (ok, scalar bool) {
	sc := scanner{src: data, line: 1, col: 1, strictJSON: strictJSON, rawOnly: true}
	tok, err := sc.nextValueToken()
	if err != nil {
		return false, true
	}
	raw := data[tok.pos.Offset:sc.pos]
	switch tok.kind {
	case tokenString:
		if !e.relayoutString(raw) {
			return false, true
		}
	case tokenNumber, tokenTrue, tokenFalse, tokenNull:
		e.buf.Write(raw)
	default:
		return false, false
	}
	tok, err = sc.nextValueToken()
	return err == nil && tok.kind == tokenEOF, true
}

// nextValueToken is [scanner.next] skipping line breaks.
func (s *scanner) nextValueToken() (token, error) {
	for {
		tok, err := s.next()
		if err != nil || tok.kind != tokenNewline {
			return tok, err
		}
	}
}

// relayoutFrame is an object or array open during [encoder.relayout].
type relayoutFrame struct {
	object    bool
	multiline bool
	depth     int // the container's depth; its children are at depth+1
	children  int // children written so far
	keys      int // index in the keys slice of this object's first key
}

// What [encoder.relayoutTokens] expects next.
const (
	relayoutValue = iota // a value; in an array, or ']' unless after a comma
	relayoutKey          // a key; or '}' unless after a comma
	relayoutColon        // ':' after a key
	relayoutNext         // ',' or the closing delimiter; the end after the root
)

func (e *encoder) relayoutTokens(data []byte, strictJSON bool, depth int) bool {
	sc := scanner{src: data, line: 1, col: 1, strictJSON: strictJSON, rawOnly: true}
	var stackBuf [8]relayoutFrame
	stack := stackBuf[:0]
	var keysBuf [16][]byte
	keys := keysBuf[:0] // keys of the open objects, for the duplicate check
	want := relayoutValue
	afterComma := false
	for {
		tok, err := sc.next()
		if err != nil {
			return false
		}
		raw := data[tok.pos.Offset:sc.pos]
		switch tok.kind {
		case tokenNewline:
			continue
		case tokenEOF:
			return want == relayoutNext && len(stack) == 0
		}
		var top *relayoutFrame
		if len(stack) > 0 {
			top = &stack[len(stack)-1]
		}
		switch want {
		case relayoutKey:
			if tok.kind == tokenObjectEnd && !afterComma {
				if !e.relayoutClose(top, keys) {
					return false
				}
				keys = keys[:top.keys]
				stack = stack[:len(stack)-1]
				want = relayoutNext
				continue
			}
			if tok.kind != tokenString {
				return false
			}
			key, ok := relayoutKeyText(raw)
			if !ok {
				return false
			}
			keys = append(keys, key)
			e.relayoutChild(top)
			if !e.relayoutString(raw) {
				return false
			}
			want = relayoutColon
		case relayoutColon:
			if tok.kind != tokenNameSeparator {
				return false
			}
			e.buf.WriteString(": ")
			want = relayoutValue
			afterComma = false
		case relayoutValue:
			if tok.kind == tokenArrayEnd && top != nil && !top.object && !afterComma {
				e.relayoutClose(top, nil)
				stack = stack[:len(stack)-1]
				want = relayoutNext
				continue
			}
			if top != nil && !top.object {
				e.relayoutChild(top)
			}
			switch tok.kind {
			case tokenObjectStart, tokenArrayStart:
				if len(stack) >= embeddedMaxDepth {
					return false
				}
				d := depth
				if top != nil {
					d = top.depth + 1
				}
				object := tok.kind == tokenObjectStart
				e.buf.WriteByte(raw[0])
				stack = append(stack, relayoutFrame{
					object:    object,
					multiline: e.multiline() || (!object && e.opts.arrayMultiline),
					depth:     d,
					keys:      len(keys),
				})
				want = relayoutValue
				if object {
					want = relayoutKey
				}
				afterComma = false
				continue
			case tokenString:
				if !e.relayoutString(raw) {
					return false
				}
			case tokenNumber, tokenTrue, tokenFalse, tokenNull:
				e.buf.Write(raw)
			default:
				return false
			}
			want = relayoutNext
		case relayoutNext:
			switch {
			case top == nil:
				return false
			case tok.kind == tokenValueSeparator:
				afterComma = true
				want = relayoutValue
				if top.object {
					want = relayoutKey
				}
			case (tok.kind == tokenObjectEnd && top.object) || (tok.kind == tokenArrayEnd && !top.object):
				if !e.relayoutClose(top, keys) {
					return false
				}
				keys = keys[:top.keys]
				stack = stack[:len(stack)-1]
				afterComma = false
			default:
				return false
			}
		}
	}
}

// relayoutChild writes what goes before a container's next child: the
// separator after the previous one, then the line break and indentation
// in multi-line layout.
func (e *encoder) relayoutChild(f *relayoutFrame) {
	switch {
	case f.children > 0 && f.multiline:
		e.buf.WriteString(",\n")
	case f.children > 0:
		e.buf.WriteString(", ")
	case f.multiline:
		e.buf.WriteByte('\n')
	}
	if f.multiline {
		e.writeIndent(f.depth + 1)
	}
	f.children++
}

// relayoutClose checks an object's keys (keys from f.keys on) for
// duplicates and writes the container's closing delimiter. It returns
// false on a duplicate key.
func (e *encoder) relayoutClose(f *relayoutFrame, keys [][]byte) bool {
	if f.object {
		own := keys[f.keys:]
		if len(own) > 1 {
			slices.SortFunc(own, bytes.Compare)
			for i := 1; i < len(own); i++ {
				if bytes.Equal(own[i-1], own[i]) {
					return false
				}
			}
		}
	}
	if f.children > 0 && f.multiline {
		if e.opts.trailingComma && !e.opts.strictJSONOutput {
			e.buf.WriteByte(',')
		}
		e.buf.WriteByte('\n')
		e.writeIndent(f.depth)
	}
	if f.object {
		e.buf.WriteByte('}')
	} else {
		e.buf.WriteByte(']')
	}
	return true
}

// relayoutString writes a scanned string as [encoder.writeSourceString]
// does. It returns false if the string does not unquote, which the
// scanner already rules out.
func (e *encoder) relayoutString(raw []byte) bool {
	if len(raw) > 0 && utf8.Valid(raw) && (!e.opts.escapeHTML || !bytes.ContainsAny(raw, "<>&\u2028\u2029")) {
		e.buf.Write(raw)
		return true
	}
	value, err := unquoteString(string(raw))
	if err != nil {
		return false
	}
	e.writeQuotedString(value)
	return true
}

// relayoutKeyText returns the unescaped text of a scanned key, as the
// parser compares keys.
func relayoutKeyText(raw []byte) ([]byte, bool) {
	body := raw[1 : len(raw)-1]
	if bytes.IndexByte(body, '\\') < 0 && utf8.Valid(body) {
		return body, true
	}
	key, err := unquoteString(string(raw))
	if err != nil {
		return nil, false
	}
	return []byte(key), true
}

// encodeSlice handles slice values, base64-encoding []byte to match
// encoding/json.
func (e *encoder) encodeSlice(rv reflect.Value, depth int) error {
	if rv.IsNil() {
		e.buf.WriteString("null")
		return nil
	}
	if rv.Type().Elem().Kind() == reflect.Uint8 {
		e.writeBase64(rv.Bytes())
		return nil
	}
	key, err := e.enterContainer(rv)
	if err != nil {
		return err
	}
	defer e.leaveContainer(key)
	return e.encodeListLike(rv, depth)
}

// encodeArrayValue handles array values (fixed-size, including [N]byte).
func (e *encoder) encodeArrayValue(rv reflect.Value, depth int) error {
	if rv.Type().Elem().Kind() == reflect.Uint8 {
		// Arrays don't expose Bytes(); copy into a slice for base64 encoding.
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
	lastFootEmitted := false
	for i := range n {
		e.pushPathIndex(i)

		if multi && !e.opts.strictJSONOutput {
			for _, c := range e.commentsAt(HeadCommentPos) {
				e.writeCommentBlock(c.Text, depth+1)
			}
		}

		if multi {
			e.writeIndent(depth + 1)
		}
		if err := e.encodeValue(rv.Index(i), depth+1); err != nil {
			e.popPath()
			return err
		}

		isLast := i == n-1
		switch {
		case !isLast:
			e.buf.WriteByte(',')
		case multi && e.opts.trailingComma && !e.opts.strictJSONOutput:
			e.buf.WriteByte(',')
		}
		if multi && !e.opts.strictJSONOutput {
			e.emitInlineComments(e.commentsAt(LineCommentPos))
		}
		if !isLast {
			if multi {
				e.buf.WriteByte('\n')
			} else {
				e.buf.WriteByte(' ')
			}
		}

		if multi && !e.opts.strictJSONOutput {
			feet := e.commentsAt(FootCommentPos)
			if len(feet) > 0 {
				if isLast {
					e.buf.WriteByte('\n')
					lastFootEmitted = true
				}
				for _, c := range feet {
					e.writeCommentBlock(c.Text, depth+1)
				}
			}
		}

		e.popPath()
	}
	if multi {
		if !lastFootEmitted {
			e.buf.WriteByte('\n')
		}
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
	// Inlined base64 (matches encoding/base64.StdEncoding) so the encoded
	// bytes go straight into e.buf without an intermediate allocation.
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
	key, err := e.enterContainer(rv)
	if err != nil {
		return err
	}
	defer e.leaveContainer(key)

	pairs := make([]mapKV, len(keys))
	for i, k := range keys {
		s, err := mapKeyString(k)
		if err != nil {
			return err
		}
		pairs[i] = mapKV{keyStr: s, val: rv.MapIndex(k)}
	}
	sortMapPairs(pairs)

	multi := e.multiline()
	e.buf.WriteByte('{')
	if multi {
		e.buf.WriteByte('\n')
	}
	lastFootEmitted := false
	for i, p := range pairs {
		e.pushPathKey(p.keyStr)

		if multi && !e.opts.strictJSONOutput {
			for _, c := range e.commentsAt(HeadCommentPos) {
				e.writeCommentBlock(c.Text, depth+1)
			}
		}

		if multi {
			e.writeIndent(depth + 1)
		}
		e.writeQuotedString(p.keyStr)
		e.buf.WriteString(": ")
		if err := e.encodeValue(p.val, depth+1); err != nil {
			e.popPath()
			return err
		}

		isLast := i == len(pairs)-1
		switch {
		case !isLast:
			e.buf.WriteByte(',')
		case multi && e.opts.trailingComma && !e.opts.strictJSONOutput:
			e.buf.WriteByte(',')
		}
		if multi && !e.opts.strictJSONOutput {
			e.emitInlineComments(e.commentsAt(LineCommentPos))
		}
		if !isLast {
			if multi {
				e.buf.WriteByte('\n')
			} else {
				e.buf.WriteByte(' ')
			}
		}

		if multi && !e.opts.strictJSONOutput {
			feet := e.commentsAt(FootCommentPos)
			if len(feet) > 0 {
				if isLast {
					e.buf.WriteByte('\n')
					lastFootEmitted = true
				}
				for _, c := range feet {
					e.writeCommentBlock(c.Text, depth+1)
				}
			}
		}

		e.popPath()
	}
	if multi {
		if !lastFootEmitted {
			e.buf.WriteByte('\n')
		}
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
	lastFootEmitted := false
	for i, item := range ms {
		e.pushPathKey(item.Key)

		if multi && !e.opts.strictJSONOutput {
			for _, c := range e.commentsAt(HeadCommentPos) {
				e.writeCommentBlock(c.Text, depth+1)
			}
		}

		if multi {
			e.writeIndent(depth + 1)
		}
		e.writeQuotedString(item.Key)
		e.buf.WriteString(": ")
		if err := e.encodeValue(reflect.ValueOf(item.Value), depth+1); err != nil {
			e.popPath()
			return err
		}

		isLast := i == len(ms)-1
		switch {
		case !isLast:
			e.buf.WriteByte(',')
		case multi && e.opts.trailingComma && !e.opts.strictJSONOutput:
			e.buf.WriteByte(',')
		}
		if multi && !e.opts.strictJSONOutput {
			e.emitInlineComments(e.commentsAt(LineCommentPos))
		}
		if !isLast {
			if multi {
				e.buf.WriteByte('\n')
			} else {
				e.buf.WriteByte(' ')
			}
		}

		if multi && !e.opts.strictJSONOutput {
			feet := e.commentsAt(FootCommentPos)
			if len(feet) > 0 {
				if isLast {
					e.buf.WriteByte('\n')
					lastFootEmitted = true
				}
				for _, c := range feet {
					e.writeCommentBlock(c.Text, depth+1)
				}
			}
		}

		e.popPath()
	}
	if multi {
		if !lastFootEmitted {
			e.buf.WriteByte('\n')
		}
		e.writeIndent(depth)
	}
	e.buf.WriteByte('}')
	return nil
}

// mapKeyString converts a map-key reflect.Value into the string used as the
// JSON object key.
func mapKeyString(k reflect.Value) (string, error) {
	if k.CanInterface() {
		if tm, ok := reflect.TypeAssert[encoding.TextMarshaler](k); ok {
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

// sortMapPairs sorts native map entries by key. Every [MapKeyOrder] sorts
// them: a Go map has no insertion order, so [MapKeyOrderInsertion] applies
// only to a [MapSlice] (handled by encodeMapSlice).
func sortMapPairs(pairs []mapKV) {
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].keyStr < pairs[j].keyStr
	})
}

// encodeStruct emits a struct as a JSONC object, honoring tags, omitempty,
// omitzero, asString, required, and comment annotations.
func (e *encoder) encodeStruct(rv reflect.Value, depth int) error {
	sf := getStructFields(rv.Type())

	// Resolve omitempty / omitzero up front so the trailing-comma logic
	// below can rely on len(emits)-1 being the true last emitted member.
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

	// lastFootEmitted is set when a foot comment from WithComment was
	// emitted after the final member. In that case the comment block
	// already ends in '\n' so the closing brace block must skip its own
	// leading newline to avoid a blank line before '}'.
	lastFootEmitted := false

	// A `commented` member is written whole inside /* */ in multi-line
	// output, so it takes no part in comma placement: commas follow only
	// members that have a later uncommented member.
	commentedOut := func(em emit) bool {
		return em.fi.commented && multi && !e.opts.strictJSONOutput
	}
	lastActive := -1
	for i, em := range emits {
		if !commentedOut(em) {
			lastActive = i
		}
	}

	for i, em := range emits {
		e.pushPathKey(em.fi.name)

		// Head comments: from WithComment first, then from the `comment` tag.
		if multi && !e.opts.strictJSONOutput {
			for _, c := range e.commentsAt(HeadCommentPos) {
				e.writeCommentBlock(c.Text, depth+1)
			}
			if em.comment != "" {
				e.writeCommentBlock(em.comment, depth+1)
			}
		}

		if multi {
			e.writeIndent(depth + 1)
		}

		if commentedOut(em) {
			// The member is a comment, so it has no value position for
			// line comments; head and foot comments still apply.
			e.buf.WriteString("/* ")
			start := e.buf.Len()
			e.writeQuotedString(em.fi.name)
			e.buf.WriteString(": ")
			if err := e.encodeFieldValue(em.fi, em.val, depth+1); err != nil {
				e.popPath()
				return err
			}
			body := sanitizeBlockCommentText(string(e.buf.Bytes()[start:]))
			e.buf.Truncate(start)
			e.buf.WriteString(body)
			e.buf.WriteString(" */")
		} else {
			e.writeQuotedString(em.fi.name)
			e.buf.WriteString(": ")
			if err := e.encodeFieldValue(em.fi, em.val, depth+1); err != nil {
				e.popPath()
				return err
			}

			// Comma must precede any inline comment so the output reads
			// `value, // inline` rather than `value // inline,`.
			switch {
			case i < lastActive:
				e.buf.WriteByte(',')
			case multi && e.opts.trailingComma && !e.opts.strictJSONOutput:
				e.buf.WriteByte(',')
			}

			if multi && !e.opts.strictJSONOutput {
				e.emitInlineComments(e.commentsAt(LineCommentPos))
			}
		}

		isLast := i == len(emits)-1

		if !isLast {
			if multi {
				e.buf.WriteByte('\n')
			} else {
				e.buf.WriteByte(' ')
			}
		}

		if multi && !e.opts.strictJSONOutput {
			feet := e.commentsAt(FootCommentPos)
			if len(feet) > 0 {
				if isLast {
					e.buf.WriteByte('\n')
					lastFootEmitted = true
				}
				for _, c := range feet {
					e.writeCommentBlock(c.Text, depth+1)
				}
			}
		}

		e.popPath()
	}

	if multi {
		if !lastFootEmitted {
			e.buf.WriteByte('\n')
		}
		e.writeIndent(depth)
	}
	e.buf.WriteByte('}')
	return nil
}

// encodeFieldValue handles per-field options such as `,string`.
func (e *encoder) encodeFieldValue(fi fieldInfo, fv reflect.Value, depth int) error {
	// As in encoding/json, `,string` also applies through an unnamed
	// pointer; a nil pointer is written as null.
	if fi.asString && fv.Kind() == reflect.Pointer && fv.Type().Name() == "" && !fv.IsNil() &&
		isStringEncodable(fv.Elem()) {
		fv = fv.Elem()
	}
	if fi.asString && isStringEncodable(fv) {
		// Two-pass approach: encode the primitive into the buffer, snapshot
		// the bytes, truncate, and re-emit as a JSON-quoted string. Avoids
		// duplicating the per-kind formatting logic.
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
	if fi.omitZero && isZeroField(fv) {
		return true
	}
	if (fi.omitEmpty || e.opts.omitEmpty) && isEmptyValue(fv) {
		return true
	}
	return false
}

// isZeroField reports whether fv is zero for omitzero: a nil pointer or
// interface, the result of an IsZero() bool method when the value has one,
// or else reflect's zero check. This matches encoding/json.
func isZeroField(fv reflect.Value) bool {
	switch fv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if fv.IsNil() {
			return true
		}
	}
	if fv.CanInterface() {
		if z, ok := reflect.TypeAssert[interface{ IsZero() bool }](fv); ok {
			return z.IsZero()
		}
		if fv.CanAddr() {
			if z, ok := reflect.TypeAssert[interface{ IsZero() bool }](fv.Addr()); ok {
				return z.IsZero()
			}
		}
	}
	return fv.IsZero()
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

// isNumberText reports whether s is a JSON number.
func isNumberText(s string) bool {
	if s == "" || (s[0] != '-' && !isDigit(s[0])) {
		return false
	}
	sc := &scanner{src: []byte(s), line: 1, col: 1}
	return sc.scanNumber() == nil && sc.atEnd()
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
