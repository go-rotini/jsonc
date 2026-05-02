package jsonc

import "fmt"

// nodeKind is the internal AST node kind. The exported [NodeKind] mirrors
// these values via [exportKind] / [importKind].
type nodeKind int

const (
	nodeObject   nodeKind = iota // {...}, children are nodeKeyValue
	nodeArray                    // [...], children are value nodes
	nodeKeyValue                 // "key": value, exactly one child (the value)
	nodeString
	nodeNumber
	nodeBool
	nodeNull
	nodeComment // orphan comment (used only when a comment cannot be attached to any host node)
)

// commentStyle records whether a comment was a // line comment or a /* */
// block comment in the original source.
type commentStyle int

const (
	styleLineComment  commentStyle = iota // // ...
	styleBlockComment                     // /* ... */
)

// node is the internal AST node. Public consumers see [Node]; the bridge
// functions [exportNode] / [importNode] convert between the two.
//
// Field semantics:
//   - kind: see nodeKind constants.
//   - key: for nodeKeyValue, the unescaped JSON-string member name. Empty
//     for other kinds.
//   - value: for scalar kinds (nodeString, nodeNumber, nodeBool, nodeNull),
//     the unescaped string value (numbers preserved as their source text;
//     strings unescaped on demand). Empty for containers.
//   - rawValue: for scalar kinds, the source-form bytes including any
//     surrounding quotes and escape sequences as written. Used by the
//     encoder for byte-equivalent round-trips.
//   - style: for nodeComment, the comment style. Unused for other kinds.
//   - children: for nodeObject (members), nodeArray (elements), or
//     nodeKeyValue (single value).
//   - pos: position of the node's first token.
//   - comment / headComment / footComment: comment-association annotations
//     (see §12.6 #5 of the requirements doc).
type node struct {
	kind        nodeKind
	key         string
	value       string
	rawValue    string
	style       commentStyle
	children    []*node
	pos         Position
	comment     string
	headComment string
	footComment string
}

// NodeKind identifies the type of a JSONC [Node] in the AST.
type NodeKind int

const (
	ObjectNode   NodeKind = iota // a JSON object {...}
	ArrayNode                    // a JSON array [...]
	KeyValueNode                 // a member of an object: "key": value
	StringNode                   // a string scalar
	NumberNode                   // a number scalar
	BooleanNode                  // a boolean scalar
	NullNode                     // a null scalar
	CommentNode                  // an orphan comment
)

// CommentStyle indicates whether a comment was written as a // line comment
// or a /* */ block comment in the original source.
type CommentStyle int

const (
	LineCommentStyle  CommentStyle = iota // // ...
	BlockCommentStyle                     // /* ... */
)

// Node is a JSONC AST node.
type Node struct {
	Kind         NodeKind
	Key          string       // for KeyValueNode: the unescaped member name (empty otherwise)
	Value        string       // for scalar nodes: the unescaped string value (empty for containers)
	RawValue     string       // for scalar nodes: the source-form bytes (preserves quoting style)
	CommentStyle CommentStyle // for CommentNode: which comment style
	Children     []*Node      // for ObjectNode: KeyValueNode children. For ArrayNode: value children. For KeyValueNode: a single value child.
	Pos          Position
	Comment      string // line comment (on the same line, after the value)
	HeadComment  string // comment block before this node
	FootComment  string // comment block after the last child but before the closing delimiter (containers only)
}

// String returns a concise human-readable representation. For scalars, the
// raw value text. For containers, a kind + child-count summary. For
// KeyValueNode, the member name.
func (n *Node) String() string {
	switch n.Kind {
	case KeyValueNode:
		return n.Key
	case ObjectNode:
		return fmt.Sprintf("{object: %d members}", len(n.Children))
	case ArrayNode:
		return fmt.Sprintf("[array: %d elements]", len(n.Children))
	case CommentNode:
		switch n.CommentStyle {
		case LineCommentStyle:
			return "// " + n.Value
		case BlockCommentStyle:
			return "/* " + n.Value + " */"
		default:
			return n.Value
		}
	default:
		if n.RawValue != "" {
			return n.RawValue
		}
		return n.Value
	}
}

// Validate checks structural invariants of the node tree: object key
// uniqueness, well-formed scalar nodes, KeyValueNode with exactly one
// child. Returns nil if the tree is valid, else the first violation
// encountered.
func (n *Node) Validate() error {
	var firstErr error
	Walk(n, func(node *Node) bool {
		if firstErr != nil {
			return false
		}
		switch node.Kind {
		case ObjectNode:
			seen := make(map[string]bool, len(node.Children))
			for _, child := range node.Children {
				if child.Kind != KeyValueNode {
					continue
				}
				if seen[child.Key] {
					firstErr = &DuplicateKeyError{Key: child.Key, Pos: child.Pos}
					return false
				}
				seen[child.Key] = true
			}
		case KeyValueNode:
			if len(node.Children) != 1 {
				firstErr = &SyntaxError{
					Message: fmt.Sprintf("KeyValueNode %q has %d children, want 1", node.Key, len(node.Children)),
					Pos:     node.Pos,
				}
				return false
			}
		}
		return true
	})
	return firstErr
}

// File is the result of parsing a JSONC byte stream. JSONC documents
// represent a single JSON value at the top level (typically an object,
// but JSON permits any value).
type File struct {
	Root     *Node
	Warnings []string
}

// WalkFunc is the callback for [Walk]. Return true to recurse into the
// node's children, or false to skip the subtree.
type WalkFunc func(n *Node) bool

// Walk traverses the AST rooted at n in depth-first pre-order, calling fn
// for each node. If fn returns false, the node's children are not visited.
// Walk(nil, fn) is a no-op.
func Walk(n *Node, fn WalkFunc) {
	if n == nil {
		return
	}
	if !fn(n) {
		return
	}
	for _, child := range n.Children {
		Walk(child, fn)
	}
}

// Filter walks the AST rooted at n and returns all nodes for which fn
// returns true.
func Filter(n *Node, fn func(*Node) bool) []*Node {
	var result []*Node
	Walk(n, func(node *Node) bool {
		if fn(node) {
			result = append(result, node)
		}
		return true
	})
	return result
}

// Parse tokenizes and parses data into an AST. It accepts JSONC by default
// (line and block comments, optional trailing commas). Pass [WithStrictJSON]
// to reject those extensions and require RFC 8259 conformance. Limits like
// [WithMaxDepth] / [WithMaxKeys] / [WithMaxNodes] / [WithMaxDocumentSize]
// are honored at parse time.
func Parse(data []byte, opts ...DecodeOption) (*File, error) {
	o := defaultDecodeOptions()
	for _, opt := range opts {
		opt(o)
	}
	if o.maxDocumentSize > 0 && len(data) > o.maxDocumentSize {
		return nil, ErrDocumentSize
	}

	s, err := newScanner(data)
	if err != nil {
		return nil, err
	}
	s.strictJSON = o.strictJSON
	tokens, err := s.scan()
	if err != nil {
		return nil, err
	}

	p := newParser(tokens, o)
	root, err := p.parse()
	if err != nil {
		return nil, err
	}

	return &File{
		Root:     exportNode(root),
		Warnings: p.warnings,
	}, nil
}

// NodeToBytes serializes a [Node] tree back into JSONC bytes using default
// encoding options. Comments and structure are preserved.
func NodeToBytes(n *Node) ([]byte, error) {
	internal := importNode(n)
	enc := newNodeEncoder(defaultEncodeOptions())
	return enc.encodeNode(internal)
}

// NodeToBytesWithOptions serializes a [Node] tree back into JSONC bytes
// using the provided encoding options.
func NodeToBytesWithOptions(n *Node, opts ...EncodeOption) ([]byte, error) {
	o := defaultEncodeOptions()
	for _, opt := range opts {
		opt(o)
	}
	internal := importNode(n)
	enc := newNodeEncoder(o)
	return enc.encodeNode(internal)
}

// exportNode converts an internal *node tree into its public *Node mirror.
// Returns nil for a nil input.
func exportNode(n *node) *Node {
	if n == nil {
		return nil
	}
	out := &Node{
		Kind:         exportKind(n.kind),
		Key:          n.key,
		Value:        n.value,
		RawValue:     n.rawValue,
		CommentStyle: exportStyle(n.style),
		Pos:          n.pos,
		Comment:      n.comment,
		HeadComment:  n.headComment,
		FootComment:  n.footComment,
	}
	for _, child := range n.children {
		out.Children = append(out.Children, exportNode(child))
	}
	return out
}

// importNode converts a public *Node tree into the internal *node mirror.
// Returns nil for a nil input.
func importNode(n *Node) *node {
	if n == nil {
		return nil
	}
	out := &node{
		kind:        importKind(n.Kind),
		key:         n.Key,
		value:       n.Value,
		rawValue:    n.RawValue,
		style:       importStyle(n.CommentStyle),
		pos:         n.Pos,
		comment:     n.Comment,
		headComment: n.HeadComment,
		footComment: n.FootComment,
	}
	for _, child := range n.Children {
		out.children = append(out.children, importNode(child))
	}
	return out
}

func exportKind(k nodeKind) NodeKind {
	switch k {
	case nodeObject:
		return ObjectNode
	case nodeArray:
		return ArrayNode
	case nodeKeyValue:
		return KeyValueNode
	case nodeString:
		return StringNode
	case nodeNumber:
		return NumberNode
	case nodeBool:
		return BooleanNode
	case nodeNull:
		return NullNode
	case nodeComment:
		return CommentNode
	default:
		return StringNode
	}
}

func importKind(k NodeKind) nodeKind {
	switch k {
	case ObjectNode:
		return nodeObject
	case ArrayNode:
		return nodeArray
	case KeyValueNode:
		return nodeKeyValue
	case StringNode:
		return nodeString
	case NumberNode:
		return nodeNumber
	case BooleanNode:
		return nodeBool
	case NullNode:
		return nodeNull
	case CommentNode:
		return nodeComment
	default:
		return nodeString
	}
}

func exportStyle(s commentStyle) CommentStyle {
	if s == styleBlockComment {
		return BlockCommentStyle
	}
	return LineCommentStyle
}

func importStyle(s CommentStyle) commentStyle {
	if s == BlockCommentStyle {
		return styleBlockComment
	}
	return styleLineComment
}
