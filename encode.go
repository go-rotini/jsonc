package jsonc

import (
	"bytes"
	"context"
	"strings"
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

// encoder is the internal encoder. It will be expanded in Phase 7 with
// reflection-based methods (encodeValue, encodeStruct, …); for now it
// supports AST-based emission via [encoder.encodeNode] for the
// [NodeToBytes] / [NodeToBytesWithOptions] entry points.
type encoder struct {
	opts *encoderOptions
	buf  bytes.Buffer
	ctx  context.Context //nolint:containedctx // by design — set via Encoder.EncodeContext
}

// newNodeEncoder constructs an encoder configured for AST emission. The
// name will be reused (as newEncoder, returning the same type) by the
// Phase 7 reflection encoder.
func newNodeEncoder(opts *encoderOptions) *encoder {
	return &encoder{opts: opts, ctx: context.Background()}
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
