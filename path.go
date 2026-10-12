package jsonc

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Path is a compiled query that selects zero or more [Node] values from a
// JSONC AST. Build one from a JSONPath-like expression with [PathString] or
// from an RFC 6901 JSON Pointer with [PathPointer]. A Path is immutable and
// can be reused across documents.
//
// A member segment selects the member's value node, not its
// [KeyValueNode].
type Path struct {
	expr     string // original expression, for error messages
	segments []pathSegment
}

// pathSegment is one component of a Path.
type pathSegment struct {
	kind  segmentKind
	name  string // for child / recursive segments — unescaped member name
	index int    // for index segments — 0-based array index
}

// segmentKind identifies the kind of a path segment.
type segmentKind int

const (
	segChild     segmentKind = iota // .name or ["name"] — exact child by name
	segIndex                        // [N] — array element by 0-based index
	segWildcard                     // [*] — every child of an object or array
	segRecursive                    // ..name — every descendant matching name
)

// String returns the original expression used to build the path.
func (p Path) String() string { return p.expr }

// PathString compiles a path expression into a [Path]. The grammar is a
// small JSONPath-like subset:
//
//	$           — the root (optional; an expression may start with . or [)
//	.name       — object member; name runs to the next . or [
//	["name"]    — object member; name may contain . and [ but not ], and
//	              has no escape sequences
//	['name']    — same, with single quotes
//	[N]         — 0-based array index (negative indices are not supported)
//	[*]         — every member value or element of the current container
//	..name      — every descendant member named name, at any depth
//
// Whitespace inside brackets is trimmed; elsewhere it is part of the name.
//
// Malformed expressions return an error wrapping [ErrPathSyntax].
func PathString(expr string) (Path, error) {
	p := Path{expr: expr}
	src := expr
	if src == "" {
		return p, fmt.Errorf("%w: empty expression", ErrPathSyntax)
	}
	if src[0] == '$' {
		src = src[1:]
	}
	for src != "" {
		seg, rest, err := parsePathSegment(src)
		if err != nil {
			return Path{}, err
		}
		p.segments = append(p.segments, seg)
		src = rest
	}
	return p, nil
}

// parsePathSegment consumes one segment from the front of s and returns
// (segment, remainder, error).
func parsePathSegment(s string) (pathSegment, string, error) {
	switch s[0] {
	case '.':
		return parseDotSegment(s)
	case '[':
		return parseBracketSegment(s)
	default:
		return pathSegment{}, "", fmt.Errorf("%w: unexpected %q", ErrPathSyntax, s[0])
	}
}

// parseDotSegment parses ".name" or "..name". Caller has confirmed s[0] == '.'.
func parseDotSegment(s string) (pathSegment, string, error) {
	recursive := false
	if len(s) >= 2 && s[1] == '.' {
		recursive = true
		s = s[2:]
	} else {
		s = s[1:]
	}
	end := len(s)
	for i := range len(s) {
		if s[i] == '.' || s[i] == '[' {
			end = i
			break
		}
	}
	name := s[:end]
	if name == "" {
		return pathSegment{}, "", fmt.Errorf("%w: empty member name after dot", ErrPathSyntax)
	}
	kind := segChild
	if recursive {
		kind = segRecursive
	}
	return pathSegment{kind: kind, name: name}, s[end:], nil
}

// parseBracketSegment parses "[...]" forms: [N], [*], ["name"], ['name'].
// Caller has confirmed s[0] == '['.
func parseBracketSegment(s string) (pathSegment, string, error) {
	closeIdx := strings.IndexByte(s, ']')
	if closeIdx < 0 {
		return pathSegment{}, "", fmt.Errorf("%w: missing ]", ErrPathSyntax)
	}
	inner := strings.TrimSpace(s[1:closeIdx])
	rest := s[closeIdx+1:]

	if inner == "*" {
		return pathSegment{kind: segWildcard}, rest, nil
	}
	if len(inner) >= 2 && (inner[0] == '"' || inner[0] == '\'') && inner[len(inner)-1] == inner[0] {
		return pathSegment{kind: segChild, name: inner[1 : len(inner)-1]}, rest, nil
	}
	idx, err := strconv.Atoi(inner)
	if err != nil || idx < 0 {
		return pathSegment{}, "", fmt.Errorf("%w: invalid bracket %q", ErrPathSyntax, inner)
	}
	return pathSegment{kind: segIndex, index: idx}, rest, nil
}

// PathPointer compiles an RFC 6901 JSON Pointer into a [Path]. The empty
// string refers to the root; other pointers must start with "/". Tokens use
// the escapes ~0 for ~ and ~1 for /. A numeric token applied to an array
// selects an element by index. The "-" token (one past the end of an array)
// matches nothing; it is meaningful only in a [Patch] add operation.
//
// A pointer that does not start with "/" returns an error wrapping
// [ErrPointerSyntax].
func PathPointer(ptr string) (Path, error) {
	p := Path{expr: ptr}
	if ptr == "" {
		return p, nil
	}
	if ptr[0] != '/' {
		return Path{}, fmt.Errorf("%w: pointer must start with /", ErrPointerSyntax)
	}
	for tok := range strings.SplitSeq(ptr[1:], "/") {
		decoded := decodePointerToken(tok)
		// In a JSON Pointer the same token can refer to either an object
		// member or an array index — the structure of the document
		// determines which. We default to a child segment; the index is
		// resolved at lookup time when a numeric token is applied to an
		// array.
		seg := pathSegment{kind: segChild, name: decoded}
		if i, err := strconv.Atoi(decoded); err == nil && i >= 0 {
			seg.index = i
		}
		p.segments = append(p.segments, seg)
	}
	return p, nil
}

// decodePointerToken applies RFC 6901 escape rules: ~1 → /, ~0 → ~. The
// order matters (do ~1 before ~0).
func decodePointerToken(tok string) string {
	tok = strings.ReplaceAll(tok, "~1", "/")
	tok = strings.ReplaceAll(tok, "~0", "~")
	return tok
}

// Read returns the nodes matched by p under root, or nil when none match.
func (p Path) Read(root *Node) []*Node {
	if root == nil {
		return nil
	}
	cur := []*Node{root}
	for _, seg := range p.segments {
		cur = applySegment(cur, seg)
		if len(cur) == 0 {
			return nil
		}
	}
	return cur
}

// ReadFirst returns the first [Node] reachable by p, or [ErrPathNotFound].
func (p Path) ReadFirst(root *Node) (*Node, error) {
	matches := p.Read(root)
	if len(matches) == 0 {
		return nil, ErrPathNotFound
	}
	return matches[0], nil
}

// ReadString returns the value of the first node matched by p, or
// [ErrPathNotFound]. A string yields its unescaped value, another scalar its
// source text, and a container the summary from [Node.String].
func (p Path) ReadString(root *Node) (string, error) {
	n, err := p.ReadFirst(root)
	if err != nil {
		return "", err
	}
	switch n.Kind {
	case StringNode, NumberNode, BooleanNode, NullNode:
		return n.Value, nil
	default:
		return n.String(), nil
	}
}

// ReadPositions returns the source [Position] of every node matched by p.
func (p Path) ReadPositions(root *Node) []Position {
	matches := p.Read(root)
	out := make([]Position, len(matches))
	for i, n := range matches {
		out[i] = n.Pos
	}
	return out
}

// Replace overwrites every node matched by p, in place, with a deep copy of
// value. It returns [ErrPathNotFound] if p matches no nodes.
func (p Path) Replace(root, value *Node) error {
	matches := p.Read(root)
	if len(matches) == 0 {
		return ErrPathNotFound
	}
	for _, n := range matches {
		switch n.Kind {
		case KeyValueNode:
			if len(n.Children) > 0 {
				n.Children[0] = cloneNode(value)
			} else {
				n.Children = []*Node{cloneNode(value)}
			}
		default:
			*n = *cloneNode(value)
		}
	}
	return nil
}

// Append appends a deep copy of value to every container matched by p. For
// an object, value must be a [KeyValueNode]; for an array, it is appended
// as an element. Append does not check for duplicate keys.
//
// It returns [ErrPathNotFound] if p matches no nodes, or a [SyntaxError] if
// a match is not a container or value does not fit it. Matches before the
// failing one have already been modified.
func (p Path) Append(root, value *Node) error {
	matches := p.Read(root)
	if len(matches) == 0 {
		return ErrPathNotFound
	}
	for _, n := range matches {
		switch n.Kind {
		case ObjectNode:
			if value.Kind != KeyValueNode {
				return &SyntaxError{Message: "Append into ObjectNode requires KeyValueNode value"}
			}
			n.Children = append(n.Children, cloneNode(value))
		case ArrayNode:
			n.Children = append(n.Children, cloneNode(value))
		default:
			return &SyntaxError{Message: fmt.Sprintf("Append target must be object or array, got %s", n.Kind)}
		}
	}
	return nil
}

// Delete removes every node matched by p from its parent container: an
// object member together with its key, or an array element. It returns
// [ErrPathNotFound] if p matches no nodes.
func (p Path) Delete(root *Node) error {
	matches := p.Read(root)
	if len(matches) == 0 {
		return ErrPathNotFound
	}
	parents := mapNodeParents(root)
	// Reverse iteration so index shifts from earlier deletes don't invalidate
	// later ones when multiple matches share a parent.
	for _, target := range slices.Backward(matches) {
		// A member value's parent is its KeyValueNode; remove the whole
		// member from the enclosing object.
		if kv := parents[target]; kv != nil && kv.Kind == KeyValueNode {
			target = kv
		}
		parent, idx := findChild(parents, target)
		if parent == nil {
			continue
		}
		parent.Children = append(parent.Children[:idx], parent.Children[idx+1:]...)
	}
	return nil
}

// applySegment maps the current selection through a single segment.
func applySegment(cur []*Node, seg pathSegment) []*Node {
	var next []*Node
	for _, n := range cur {
		switch seg.kind {
		case segChild:
			next = append(next, childByName(n, seg.name)...)
			// JSON Pointer leaves array-vs-object resolution to the document.
			// If the node is an array and the segment name parses as a
			// non-negative integer, treat it as an index.
			if len(next) == 0 && n.Kind == ArrayNode {
				if i := seg.indexFromName(); i >= 0 && i < len(n.Children) {
					next = append(next, n.Children[i])
				}
			}
		case segIndex:
			if n.Kind == ArrayNode && seg.index < len(n.Children) {
				next = append(next, n.Children[seg.index])
			}
		case segWildcard:
			next = append(next, valueChildrenPublic(n)...)
		case segRecursive:
			collectRecursive(n, seg.name, &next)
		}
	}
	return next
}

// indexFromName attempts to interpret a child segment's name as a numeric
// index (used when a JSON Pointer token addresses an array slot).
func (s pathSegment) indexFromName() int {
	i, err := strconv.Atoi(s.name)
	if err != nil || i < 0 {
		return -1
	}
	return i
}

// childByName returns the value child(ren) of n whose member name equals
// name. For an ObjectNode this is at most one match; the function returns
// the value (not the KeyValueNode wrapper) so subsequent segments operate
// on the actual member.
func childByName(n *Node, name string) []*Node {
	if n == nil || n.Kind != ObjectNode {
		return nil
	}
	for _, c := range n.Children {
		if c.Kind != KeyValueNode {
			continue
		}
		if c.Key == name && len(c.Children) > 0 {
			return []*Node{c.Children[0]}
		}
	}
	return nil
}

// valueChildrenPublic returns the value children of a container, filtering
// out CommentNode siblings and unwrapping KeyValueNode children of objects.
func valueChildrenPublic(n *Node) []*Node {
	if n == nil {
		return nil
	}
	var out []*Node
	for _, c := range n.Children {
		if c.Kind == CommentNode {
			continue
		}
		if c.Kind == KeyValueNode && len(c.Children) > 0 {
			out = append(out, c.Children[0])
			continue
		}
		out = append(out, c)
	}
	return out
}

// collectRecursive walks the AST under n collecting every value reachable
// from a member named name (at any depth).
func collectRecursive(n *Node, name string, out *[]*Node) {
	if n == nil {
		return
	}
	switch n.Kind {
	case ObjectNode:
		for _, c := range n.Children {
			if c.Kind != KeyValueNode {
				continue
			}
			if c.Key == name && len(c.Children) > 0 {
				*out = append(*out, c.Children[0])
			}
			for _, gc := range c.Children {
				collectRecursive(gc, name, out)
			}
		}
	case ArrayNode:
		for _, c := range n.Children {
			collectRecursive(c, name, out)
		}
	}
}

// mapNodeParents builds a map from child node to its parent for the AST
// rooted at n, allowing constant-time lookups during structural mutations.
func mapNodeParents(root *Node) map[*Node]*Node {
	parents := make(map[*Node]*Node)
	var visit func(n *Node)
	visit = func(n *Node) {
		if n == nil {
			return
		}
		for _, c := range n.Children {
			parents[c] = n
			visit(c)
		}
	}
	visit(root)
	return parents
}

// findChild returns the parent of target and target's index within
// parent.Children, or (nil, -1) if target is the root or unparented.
func findChild(parents map[*Node]*Node, target *Node) (*Node, int) {
	parent := parents[target]
	if parent == nil {
		return nil, -1
	}
	for i, c := range parent.Children {
		if c == target {
			return parent, i
		}
	}
	// Could be wrapped in a KeyValueNode whose only child is target.
	for i, c := range parent.Children {
		if c.Kind == KeyValueNode && len(c.Children) > 0 && c.Children[0] == target {
			return parent, i
		}
	}
	return nil, -1
}

// cloneNode returns a deep copy of n. Used by Replace/Append to avoid
// aliasing the caller's value into the AST.
func cloneNode(n *Node) *Node {
	if n == nil {
		return nil
	}
	out := *n
	if n.Children != nil {
		out.Children = make([]*Node, len(n.Children))
		for i, c := range n.Children {
			out.Children[i] = cloneNode(c)
		}
	}
	return &out
}
