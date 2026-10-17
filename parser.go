package jsonc

import (
	"fmt"
	"slices"
	"strings"
)

// parser is the recursive-descent parser. It pulls tokens from a scanner
// one at a time, with one token of lookahead, and produces a *node tree
// representing the JSONC document.
//
// Comment association rules:
//   - HeadComment: comments preceding a node, separated from the previous
//     token by at least one newline. Multiple stacked comments form a
//     newline-joined string.
//   - Comment (inline): a single comment on the same line as the value's
//     last token, before or just after the following comma.
//   - FootComment: for a container, comments before its closing delimiter
//     on lines after the last child; for a member or element, comments on
//     the lines between it and its comma.
//   - trailComment: comments after the root value, on later lines (set
//     only on the root).
//   - Comments between a key and its value are dropped, with a warning.
//     The parser never produces CommentNode children.
type parser struct {
	sc       *scanner
	tok      token // lookahead token, valid when have is true
	have     bool
	err      error // the scan error behind a tokenError lookahead
	opts     *decoderOptions
	depth    int
	keys     int
	nodes    int
	warnings []string
	// data holds the scanned source bytes (without a BOM); token offsets
	// index into it. Container nodes record their verbatim source slice
	// from it in rawBytes.
	data []byte
}

// newParser returns a parser for src, which must already be free of a BOM.
func newParser(src []byte, opts *decoderOptions) *parser {
	sc := &scanner{src: src, line: 1, col: 1, strictJSON: opts.strictJSON}
	return &parser{sc: sc, opts: opts, data: src}
}

// parse parses the entire input into a single root *node, rejecting
// trailing data after the value. (Streaming multi-value consumption is
// handled at the Decoder layer; one-shot parses must consume the whole
// input.)
func (p *parser) parse() (*node, error) {
	root, err := p.parseRoot()
	if err != nil && p.err != nil {
		return nil, p.err
	}
	return root, err
}

func (p *parser) parseRoot() (*node, error) {
	// Comments before the value become the root's head comment.
	leadHead := p.collectHeadComments()

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
	// A container collects its own same-line comment after its closing
	// delimiter; a scalar root collects it here.
	if root.comment == "" {
		root.comment = p.collectTrailingInlineComment()
	}

	if err := p.consumeTrailing(root); err != nil {
		return nil, err
	}
	return root, nil
}

// parseValue parses any JSONC value: object, array, string, number,
// boolean, or null.
func (p *parser) parseValue() (*node, error) {
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
	if err := p.enterDepth(); err != nil {
		return nil, err
	}
	defer p.exitDepth()
	openTk := p.peek()
	p.bump()
	obj := &node{kind: nodeObject, pos: openTk.pos}
	if err := p.bumpNodeCount(); err != nil {
		return nil, err
	}

	var members memberIndex
	removed := 0 // members replaced by a later duplicate, left as nil until the object closes
	pendingHead := ""
	justConsumedComma := false
	for {
		captured := p.collectHeadComments()
		pendingHead = appendCommentBlock(pendingHead, captured)

		tk := p.peek()
		if tk.kind == tokenObjectEnd {
			if removed > 0 {
				obj.children = slices.DeleteFunc(obj.children, func(c *node) bool { return c == nil })
			}
			if justConsumedComma && p.opts.strictJSON {
				return nil, &StrictJSONError{Feature: "trailing comma", Pos: tk.pos}
			}
			if pendingHead != "" {
				obj.footComment = appendCommentBlock(obj.footComment, pendingHead)
			}
			closeOffset := tk.pos.Offset + 1
			p.bump()
			obj.comment = p.collectTrailingInlineComment()
			if p.data != nil && openTk.pos.Offset >= 0 && closeOffset <= len(p.data) {
				obj.rawBytes = p.data[openTk.pos.Offset:closeOffset]
			}
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
		if i := members.find(obj.children, member.key); i >= 0 {
			if !p.opts.allowDuplicateKeys {
				return nil, &DuplicateKeyError{Key: member.key, Pos: member.pos}
			}
			p.warnf(member.pos, "duplicate key %q; the last value wins", member.key)
			obj.children[i] = nil
			removed++
		}
		obj.children = append(obj.children, member)
		members.add(obj.children)
		p.keys++
		if p.opts.maxKeys > 0 && p.keys > p.opts.maxKeys {
			return nil, &SyntaxError{Message: "maximum key count exceeded", Pos: member.pos}
		}

		if c := p.collectTrailingInlineComment(); c != "" {
			member.comment = c
		}

		// Comments after the member but before ',' or '}' belong to the
		// member if a comma follows (keeps them with the member when it
		// moves), or to the container if the object closes.
		between := p.collectHeadComments()

		tk = p.peek()
		switch tk.kind {
		case tokenValueSeparator:
			if between != "" {
				member.footComment = appendCommentBlock(member.footComment, between)
			}
			p.bump()
			justConsumedComma = true
			p.attachAfterComma(member)
		case tokenObjectEnd:
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

// memberIndexMin is the member count from which an object's keys are
// looked up through a map instead of a linear scan.
const memberIndexMin = 16

// memberIndex finds an object's members by key while the object is
// parsed. Small objects are scanned; larger ones use a map, so detecting
// duplicates stays linear in the number of members. Members replaced by a
// later duplicate are nil in the children slice.
type memberIndex struct {
	byKey map[string]int
}

// find returns the index in children of the member named key, or -1.
func (m *memberIndex) find(children []*node, key string) int {
	if m.byKey != nil {
		if i, ok := m.byKey[key]; ok {
			return i
		}
		return -1
	}
	for i, c := range children {
		if c != nil && c.key == key {
			return i
		}
	}
	return -1
}

// add records the member just appended to children.
func (m *memberIndex) add(children []*node) {
	last := len(children) - 1
	switch {
	case m.byKey != nil:
		m.byKey[children[last].key] = last
	case len(children) >= memberIndexMin:
		m.byKey = make(map[string]int, 2*len(children))
		for i, c := range children {
			if c != nil {
				m.byKey[c.key] = i
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
	rawKey := tk.value
	keyText, err := unquoteString(tk.value)
	if err != nil {
		return nil, &SyntaxError{Message: fmt.Sprintf("invalid object key: %v", err), Pos: tk.pos}
	}
	p.bump()

	p.skipDroppedComments()

	tk = p.peek()
	if tk.kind != tokenNameSeparator {
		return nil, &SyntaxError{
			Message: fmt.Sprintf("expected ':' after key, got %s", tk.String()),
			Pos:     tk.pos,
		}
	}
	p.bump()
	p.skipDroppedComments()

	val, err := p.parseValue()
	if err != nil {
		return nil, err
	}

	kv := &node{
		kind:        nodeKeyValue,
		key:         keyText,
		rawKey:      rawKey,
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
	if err := p.enterDepth(); err != nil {
		return nil, err
	}
	defer p.exitDepth()
	openTk := p.peek()
	p.bump()
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
			closeOffset := tk.pos.Offset + 1
			p.bump()
			arr.comment = p.collectTrailingInlineComment()
			if p.data != nil && openTk.pos.Offset >= 0 && closeOffset <= len(p.data) {
				arr.rawBytes = p.data[openTk.pos.Offset:closeOffset]
			}
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
			p.attachAfterComma(val)
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
		kind: nodeNumber,
		// Numbers are stored verbatim and parsed lazily at decode time.
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
			return nonBlank(sb.String())
		}
	}
}

// collectTrailingInlineComment returns the next comment if it appears on
// the same line (i.e., before any newline), consuming it.
func (p *parser) collectTrailingInlineComment() string {
	tk := p.peek()
	if tk.kind != tokenLineComment && tk.kind != tokenBlockComment {
		return ""
	}
	p.bump()
	return nonBlank(tk.value)
}

// nonBlank returns text, or "" when text is only whitespace. An empty or
// blank comment carries nothing, and the AST cannot tell an empty comment
// from none, so blank comments are dropped.
func nonBlank(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return text
}

// attachAfterComma gives n a comment that follows its comma on the same
// line, as in `"a": 1, // about a`, so it is not taken as the next
// member's head comment. It becomes n's line comment, or joins n's foot
// comment when n already has one.
func (p *parser) attachAfterComma(n *node) {
	c := p.collectTrailingInlineComment()
	switch {
	case c == "":
	case n.comment == "":
		n.comment = c
	default:
		n.footComment = appendCommentBlock(n.footComment, c)
	}
}

// skipDroppedComments consumes any combination of newlines and comments in
// a position with no annotation slot (between a key and its ':' or value).
// Each dropped comment adds a warning.
func (p *parser) skipDroppedComments() {
	for {
		tk := p.peek()
		switch tk.kind {
		case tokenNewline:
			p.bump()
		case tokenLineComment, tokenBlockComment:
			p.warnf(tk.pos, "comment between a key and its value is dropped")
			p.bump()
		default:
			return
		}
	}
}

// warnf records a non-fatal parse message, prefixed with its position.
func (p *parser) warnf(pos Position, format string, args ...any) {
	p.warnings = append(p.warnings, fmt.Sprintf("line %d, column %d: ", pos.Line, pos.Column)+fmt.Sprintf(format, args...))
}

// consumeTrailing handles tokens after the root value. Whitespace, newlines,
// and comments are accepted. Comments are joined as in
// [collectHeadComments], blank lines included, and appended to the root's
// trail comment. Any other token triggers an "extra data after value" error.
func (p *parser) consumeTrailing(root *node) error {
	var sb strings.Builder
	for {
		tk := p.peek()
		switch tk.kind {
		case tokenEOF, tokenStreamEnd:
			root.trailComment = appendCommentBlock(root.trailComment, nonBlank(sb.String()))
			return nil
		case tokenNewline:
			p.bump()
		case tokenLineComment, tokenBlockComment:
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(tk.value)
			p.bump()
		default:
			return &SyntaxError{
				Message: fmt.Sprintf("extra data after value: %s", tk.String()),
				Pos:     tk.pos,
			}
		}
	}
}

// valueEnd returns the offset just past the value parsed last and any
// spaces or tabs after it: the offset of the lookahead token if one has
// been scanned, else the scanner's offset.
func (p *parser) valueEnd() int {
	if p.have {
		return p.tok.pos.Offset
	}
	for !p.sc.atEnd() && (p.sc.src[p.sc.pos] == ' ' || p.sc.src[p.sc.pos] == '\t') {
		p.sc.advance()
	}
	return p.sc.pos
}

// peek returns the lookahead token, scanning it if needed. At the end of
// the input it returns tokenEOF; after a scan error it keeps returning a
// tokenError, and p.err holds the error.
func (p *parser) peek() token {
	if !p.have {
		p.tok, p.err = p.sc.next()
		p.have = true
	}
	return p.tok
}

// bump advances past the lookahead token. It is a no-op at the end of the
// input and after a scan error.
func (p *parser) bump() {
	if k := p.peek().kind; k != tokenEOF && k != tokenError {
		p.have = false
	}
}

// enterDepth increments the container nesting counter and returns an
// error if the configured maxDepth is exceeded. Scalars do not count.
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
