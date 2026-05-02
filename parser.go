package jsonc

import (
	"fmt"
	"strings"
)

// parser is the recursive-descent parser. It consumes a token slice
// produced by [scanner.scan] and produces a *node tree representing the
// JSONC document.
//
// Comment association rules (see §12.6 #5 of the requirements doc):
//   - HeadComment: comments preceding a node, separated from the previous
//     token by at least one newline. Multiple stacked comments form a
//     newline-joined string.
//   - Comment (inline): a single comment on the same line as the value's
//     last token.
//   - FootComment: comments after the last child of an array/object, before
//     the closing delimiter, separated by at least one newline from the
//     last child.
//   - Comments that don't fit any slot become CommentNode children of the
//     enclosing container.
type parser struct {
	tokens   []token
	pos      int
	opts     *decoderOptions
	depth    int
	keys     int
	nodes    int
	warnings []string
}

func newParser(tokens []token, opts *decoderOptions) *parser {
	return &parser{tokens: tokens, opts: opts}
}

// parse parses the entire token stream into a single root *node, rejecting
// trailing data after the value. (Streaming multi-value consumption is
// handled at the Decoder layer; one-shot parses must consume the whole
// input.)
func (p *parser) parse() (*node, error) {
	// Consume StreamStart.
	p.consumeIfKind(tokenStreamStart)

	// Skip leading whitespace/newlines/comments before the value. Capture
	// any leading comments as the head comment of the root.
	leadHead := p.collectHeadComments()

	// Empty document.
	tk := p.peek()
	if tk.kind == tokenEOF || tk.kind == tokenStreamEnd {
		return nil, &SyntaxError{Message: "expected JSON value, got EOF", Pos: tk.pos}
	}

	root, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	if leadHead != "" {
		if root.headComment == "" {
			root.headComment = leadHead
		} else {
			root.headComment = leadHead + "\n" + root.headComment
		}
	}

	// Consume any trailing whitespace/newlines/comments after the value.
	// Comments here are attached as the root's foot comment; non-comment
	// content is rejected with "extra data after value".
	if err := p.consumeTrailing(root); err != nil {
		return nil, err
	}
	return root, nil
}

// parseValue parses any JSONC value: object, array, string, number,
// boolean, or null.
func (p *parser) parseValue() (*node, error) {
	if err := p.enterDepth(); err != nil {
		return nil, err
	}
	defer p.exitDepth()

	tk := p.peek()
	switch tk.kind {
	case tokenObjectStart:
		return p.parseObject()
	case tokenArrayStart:
		return p.parseArray()
	case tokenString:
		return p.parseStringValue()
	case tokenNumber:
		return p.parseNumberValue()
	case tokenTrue, tokenFalse:
		return p.parseBoolValue()
	case tokenNull:
		return p.parseNullValue()
	case tokenLineComment, tokenBlockComment, tokenNewline:
		return nil, &SyntaxError{
			Message: "expected JSON value (comments/whitespace not consumed before value position)",
			Pos:     tk.pos,
		}
	default:
		return nil, &SyntaxError{
			Message: fmt.Sprintf("expected JSON value, got %s", tk.String()),
			Pos:     tk.pos,
		}
	}
}

// parseObject parses an object literal. The opening '{' is at p.peek().
func (p *parser) parseObject() (*node, error) {
	openTk := p.peek()
	p.bump() // consume '{'
	obj := &node{kind: nodeObject, pos: openTk.pos}
	if err := p.bumpNodeCount(); err != nil {
		return nil, err
	}

	pendingHead := ""
	justConsumedComma := false
	for {
		// At each loop top, ingest any whitespace+comments since the
		// previous member (or since '{' on first iteration).
		captured := p.collectHeadComments()
		pendingHead = appendCommentBlock(pendingHead, captured)

		tk := p.peek()
		if tk.kind == tokenObjectEnd {
			if justConsumedComma && p.opts.strictJSON {
				return nil, &StrictJSONError{Feature: "trailing comma", Pos: tk.pos}
			}
			if pendingHead != "" {
				obj.footComment = appendCommentBlock(obj.footComment, pendingHead)
			}
			p.bump() // consume '}'
			obj.comment = p.collectTrailingInlineComment()
			return obj, nil
		}

		if tk.kind != tokenString {
			return nil, &SyntaxError{
				Message: fmt.Sprintf("expected object key (string) or '}', got %s", tk.String()),
				Pos:     tk.pos,
			}
		}

		member, err := p.parseMember(pendingHead)
		if err != nil {
			return nil, err
		}
		pendingHead = ""
		justConsumedComma = false

		// Duplicate key detection (default) / last-wins (with option).
		if !p.opts.allowDuplicateKeys {
			for _, existing := range obj.children {
				if existing.kind == nodeKeyValue && existing.key == member.key {
					return nil, &DuplicateKeyError{Key: member.key, Pos: member.pos}
				}
			}
		} else {
			for i, existing := range obj.children {
				if existing.kind == nodeKeyValue && existing.key == member.key {
					obj.children = append(obj.children[:i], obj.children[i+1:]...)
					break
				}
			}
		}
		obj.children = append(obj.children, member)
		p.keys++
		if p.opts.maxKeys > 0 && p.keys > p.opts.maxKeys {
			return nil, &SyntaxError{Message: "maximum key count exceeded", Pos: member.pos}
		}

		// Inline comment on the same line as the member's value.
		if c := p.collectTrailingInlineComment(); c != "" {
			member.comment = c
		}

		// Now ingest any newlines+comments before ',' or '}'. Anything
		// found here is "between member and separator" — attach to
		// member.footComment if followed by ',' (so it stays with the
		// member); attach to obj.footComment if followed by '}'.
		between := p.collectHeadComments()

		tk = p.peek()
		switch tk.kind {
		case tokenValueSeparator:
			if between != "" {
				member.footComment = appendCommentBlock(member.footComment, between)
			}
			p.bump()
			justConsumedComma = true
		case tokenObjectEnd:
			// Object closes after the member. Any between-comments are
			// container foot comments.
			if between != "" {
				obj.footComment = appendCommentBlock(obj.footComment, between)
			}
		default:
			return nil, &SyntaxError{
				Message: fmt.Sprintf("expected ',' or '}', got %s", tk.String()),
				Pos:     tk.pos,
			}
		}
	}
}

// parseMember parses a single object member: <string> ':' <value>. head is
// the (already collected) head comment for this member.
func (p *parser) parseMember(head string) (*node, error) {
	tk := p.peek()
	if tk.kind != tokenString {
		return nil, &SyntaxError{
			Message: fmt.Sprintf("expected object key (string), got %s", tk.String()),
			Pos:     tk.pos,
		}
	}
	memberPos := tk.pos
	keyText, err := unquoteString(tk.value)
	if err != nil {
		return nil, &SyntaxError{Message: fmt.Sprintf("invalid object key: %v", err), Pos: tk.pos}
	}
	p.bump()

	// Allow whitespace/comments between key and ':'.
	p.skipWhitespaceCommentsAndNewlines()

	tk = p.peek()
	if tk.kind != tokenNameSeparator {
		return nil, &SyntaxError{
			Message: fmt.Sprintf("expected ':' after key, got %s", tk.String()),
			Pos:     tk.pos,
		}
	}
	p.bump() // consume ':'
	p.skipWhitespaceCommentsAndNewlines()

	val, err := p.parseValue()
	if err != nil {
		return nil, err
	}

	kv := &node{
		kind:        nodeKeyValue,
		key:         keyText,
		pos:         memberPos,
		headComment: head,
		children:    []*node{val},
	}
	if err := p.bumpNodeCount(); err != nil {
		return nil, err
	}
	return kv, nil
}

// parseArray parses an array literal. The opening '[' is at p.peek().
func (p *parser) parseArray() (*node, error) {
	openTk := p.peek()
	p.bump() // consume '['
	arr := &node{kind: nodeArray, pos: openTk.pos}
	if err := p.bumpNodeCount(); err != nil {
		return nil, err
	}

	pendingHead := ""
	justConsumedComma := false
	for {
		captured := p.collectHeadComments()
		pendingHead = appendCommentBlock(pendingHead, captured)

		tk := p.peek()
		if tk.kind == tokenArrayEnd {
			if justConsumedComma && p.opts.strictJSON {
				return nil, &StrictJSONError{Feature: "trailing comma", Pos: tk.pos}
			}
			if pendingHead != "" {
				arr.footComment = appendCommentBlock(arr.footComment, pendingHead)
			}
			p.bump()
			arr.comment = p.collectTrailingInlineComment()
			return arr, nil
		}

		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		if pendingHead != "" {
			val.headComment = appendCommentBlock(val.headComment, pendingHead)
			pendingHead = ""
		}
		justConsumedComma = false
		arr.children = append(arr.children, val)

		if c := p.collectTrailingInlineComment(); c != "" {
			val.comment = c
		}

		between := p.collectHeadComments()

		tk = p.peek()
		switch tk.kind {
		case tokenValueSeparator:
			if between != "" {
				val.footComment = appendCommentBlock(val.footComment, between)
			}
			p.bump()
			justConsumedComma = true
		case tokenArrayEnd:
			if between != "" {
				arr.footComment = appendCommentBlock(arr.footComment, between)
			}
		default:
			return nil, &SyntaxError{
				Message: fmt.Sprintf("expected ',' or ']', got %s", tk.String()),
				Pos:     tk.pos,
			}
		}
	}
}

func (p *parser) parseStringValue() (*node, error) {
	tk := p.peek()
	val, err := unquoteString(tk.value)
	if err != nil {
		return nil, &SyntaxError{Message: fmt.Sprintf("invalid string: %v", err), Pos: tk.pos}
	}
	n := &node{
		kind:     nodeString,
		value:    val,
		rawValue: tk.value,
		pos:      tk.pos,
	}
	p.bump()
	if err := p.bumpNodeCount(); err != nil {
		return nil, err
	}
	return n, nil
}

func (p *parser) parseNumberValue() (*node, error) {
	tk := p.peek()
	n := &node{
		kind:     nodeNumber,
		value:    tk.value, // numbers are stored verbatim — parsed lazily at decode time
		rawValue: tk.value,
		pos:      tk.pos,
	}
	p.bump()
	if err := p.bumpNodeCount(); err != nil {
		return nil, err
	}
	return n, nil
}

func (p *parser) parseBoolValue() (*node, error) {
	tk := p.peek()
	n := &node{
		kind:     nodeBool,
		value:    tk.value,
		rawValue: tk.value,
		pos:      tk.pos,
	}
	p.bump()
	if err := p.bumpNodeCount(); err != nil {
		return nil, err
	}
	return n, nil
}

func (p *parser) parseNullValue() (*node, error) {
	tk := p.peek()
	n := &node{
		kind:     nodeNull,
		value:    "null",
		rawValue: "null",
		pos:      tk.pos,
	}
	p.bump()
	if err := p.bumpNodeCount(); err != nil {
		return nil, err
	}
	return n, nil
}

// collectHeadComments consumes whitespace, newlines, and comments at the
// current position, returning the joined comment text. Used at structural
// boundaries (start of document, after '{' or '[', after ','). Multiple
// stacked comments are joined with newlines.
//
// This function does not distinguish between "head" and "inline" comments
// — it consumes whatever it finds. The caller is responsible for using
// [collectTrailingInlineComment] in positions where a same-line comment
// should be classified as inline.
func (p *parser) collectHeadComments() string {
	var sb strings.Builder
	for {
		tk := p.peek()
		switch tk.kind {
		case tokenNewline:
			p.bump()
		case tokenLineComment, tokenBlockComment:
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(tk.value)
			p.bump()
		default:
			return sb.String()
		}
	}
}

// collectTrailingInlineComment returns the next comment if it appears on
// the same line (i.e., before any newline), consuming it.
func (p *parser) collectTrailingInlineComment() string {
	saved := p.pos
	for {
		tk := p.peek()
		switch tk.kind {
		case tokenNewline:
			// No inline comment found before the newline.
			p.pos = saved
			return ""
		case tokenLineComment, tokenBlockComment:
			text := tk.value
			p.bump()
			return text
		default:
			p.pos = saved
			return ""
		}
	}
}

// skipWhitespaceCommentsAndNewlines consumes any combination of
// whitespace, newlines, and comments. Comments encountered are dropped (no
// node attached) — used for the very few JSONC positions where a comment
// has no clear annotation slot (e.g., between a key and its ':').
func (p *parser) skipWhitespaceCommentsAndNewlines() {
	for {
		tk := p.peek()
		switch tk.kind {
		case tokenNewline, tokenLineComment, tokenBlockComment:
			p.bump()
		default:
			return
		}
	}
}

// consumeTrailing handles tokens after the root value. Whitespace, newlines,
// and comments are accepted. Comments are appended to the root's foot
// comment. Any other token triggers an "extra data after value" error.
func (p *parser) consumeTrailing(root *node) error {
	for {
		tk := p.peek()
		switch tk.kind {
		case tokenEOF, tokenStreamEnd:
			return nil
		case tokenNewline:
			p.bump()
		case tokenLineComment, tokenBlockComment:
			root.footComment = appendCommentBlock(root.footComment, tk.value)
			p.bump()
		default:
			return &SyntaxError{
				Message: fmt.Sprintf("extra data after value: %s", tk.String()),
				Pos:     tk.pos,
			}
		}
	}
}

// peek returns the token at the current position, or a synthetic EOF if
// past end.
func (p *parser) peek() token {
	if p.pos >= len(p.tokens) {
		return token{kind: tokenEOF}
	}
	return p.tokens[p.pos]
}

// bump advances past the current token. It is a no-op at end of input.
func (p *parser) bump() {
	if p.pos < len(p.tokens) {
		p.pos++
	}
}

// consumeIfKind advances past the current token if its kind matches.
func (p *parser) consumeIfKind(kind tokenKind) {
	if p.pos < len(p.tokens) && p.tokens[p.pos].kind == kind {
		p.pos++
	}
}

// enterDepth increments the nesting counter and returns an error if the
// configured maxDepth is exceeded.
func (p *parser) enterDepth() error {
	p.depth++
	if p.opts.maxDepth > 0 && p.depth > p.opts.maxDepth {
		return &SyntaxError{
			Message: "maximum nesting depth exceeded",
			Pos:     p.peek().pos,
		}
	}
	return nil
}

func (p *parser) exitDepth() {
	p.depth--
}

// bumpNodeCount tracks the AST node count for the maxNodes limit.
func (p *parser) bumpNodeCount() error {
	p.nodes++
	if p.opts.maxNodes > 0 && p.nodes > p.opts.maxNodes {
		return &SyntaxError{
			Message: "maximum node count exceeded",
			Pos:     p.peek().pos,
		}
	}
	return nil
}

// appendCommentBlock joins existing and new comment text with a newline,
// returning the result. Empty inputs are handled cleanly.
func appendCommentBlock(existing, addition string) string {
	switch {
	case existing == "":
		return addition
	case addition == "":
		return existing
	default:
		return existing + "\n" + addition
	}
}
