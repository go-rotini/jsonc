package jsonc

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// PatchOp is a single operation in an RFC 6902 JSON Patch document.
type PatchOp struct {
	// Op names the operation: "add", "remove", "replace", "move", "copy",
	// or "test".
	Op string
	// Path is the RFC 6901 JSON Pointer identifying the operation's target.
	Path string
	// From is the source pointer for "move" / "copy" operations.
	From string
	// Value is the operand for "add" / "replace" / "test" operations.
	Value *Node
}

// Patch is an RFC 6902 JSON Patch document — an ordered sequence of
// operations applied as a single transformation.
type Patch []PatchOp

// ParsePatch decodes data, a JSONC array of operation objects, into a
// [Patch]. Comments and trailing commas are accepted, and comments inside
// an operation's value are kept on its AST. Every operation must have a
// "path", and "move" and "copy" a "from"; other operation names are not
// checked until [Patch.Apply]. Errors wrap [ErrPatchSyntax].
func ParsePatch(data []byte) (Patch, error) {
	type rawOp struct {
		Op    string   `json:"op"`
		Path  *string  `json:"path"`
		From  *string  `json:"from,omitempty"`
		Value RawValue `json:"value,omitempty"`
	}
	var ops []rawOp
	if err := Unmarshal(data, &ops); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPatchSyntax, err)
	}
	out := make(Patch, len(ops))
	for i, op := range ops {
		if op.Path == nil {
			return nil, fmt.Errorf("%w: op[%d] has no path", ErrPatchSyntax, i)
		}
		out[i] = PatchOp{Op: op.Op, Path: *op.Path}
		if op.From != nil {
			out[i].From = *op.From
		} else if op.Op == "move" || op.Op == "copy" {
			return nil, fmt.Errorf("%w: op[%d] %s has no from", ErrPatchSyntax, i, op.Op)
		}
		if len(op.Value) > 0 {
			f, err := Parse(op.Value)
			if err != nil {
				return nil, fmt.Errorf("%w: op[%d].value: %w", ErrPatchSyntax, i, err)
			}
			out[i].Value = f.Root
		}
	}
	return out, nil
}

// Apply applies the operations of p in order to a deep copy of root and
// returns the result; root itself is not modified. If any operation fails,
// Apply returns a nil node and an error naming the operation. A failed
// "test" wraps [ErrPatchTestFailed].
func (p Patch) Apply(root *Node) (*Node, error) {
	out := cloneNode(root)
	for i, op := range p {
		var err error
		out, err = applyOp(out, op)
		if err != nil {
			return nil, fmt.Errorf("op[%d] %s %q: %w", i, op.Op, op.Path, err)
		}
	}
	return out, nil
}

// applyOp dispatches a single operation. Returns the (possibly new) root.
func applyOp(root *Node, op PatchOp) (*Node, error) {
	switch op.Op {
	case "add":
		return opAdd(root, op.Path, op.Value)
	case "remove":
		return opRemove(root, op.Path)
	case "replace":
		return opReplace(root, op.Path, op.Value)
	case "move":
		return opMove(root, op.From, op.Path)
	case "copy":
		return opCopy(root, op.From, op.Path)
	case "test":
		return opTest(root, op.Path, op.Value)
	default:
		return nil, fmt.Errorf("%w: unknown op %q", ErrPatchSyntax, op.Op)
	}
}

// pointerTokens splits a JSON Pointer into its decoded tokens. The empty
// pointer returns nil; a non-empty pointer must start with "/".
func pointerTokens(ptr string) ([]string, error) {
	if ptr == "" {
		return nil, nil
	}
	if ptr[0] != '/' {
		return nil, fmt.Errorf("%w: pointer must start with /", ErrPointerSyntax)
	}
	raw := strings.Split(ptr[1:], "/")
	out := make([]string, len(raw))
	for i, t := range raw {
		tok, err := decodePointerToken(t)
		if err != nil {
			return nil, err
		}
		out[i] = tok
	}
	return out, nil
}

// errResolveRoot is returned when callers ask for the parent of the
// document root — only the operations that explicitly handle root paths
// (add/replace at "") should reach this state.
var errResolveRoot = errors.New("cannot resolve parent of root")

// resolveParent walks tokens[:len-1] and returns (parent, lastToken, error).
// The parent is guaranteed to be ObjectNode or ArrayNode on success.
func resolveParent(root *Node, tokens []string) (*Node, string, error) {
	if len(tokens) == 0 {
		return nil, "", errResolveRoot
	}
	cur, err := resolveTarget(root, tokens[:len(tokens)-1])
	if err != nil {
		return nil, "", err
	}
	if cur == nil {
		return nil, "", fmt.Errorf("%w: no document", ErrPathNotFound)
	}
	return cur, tokens[len(tokens)-1], nil
}

// descend follows one token from n.
func descend(n *Node, tok string) (*Node, error) {
	if n == nil {
		return nil, fmt.Errorf("%w: no document", ErrPathNotFound)
	}
	switch n.Kind {
	case ObjectNode:
		for _, c := range n.Children {
			if c.Kind == KeyValueNode && c.Key == tok && len(c.Children) > 0 {
				return c.Children[0], nil
			}
		}
		return nil, fmt.Errorf("%w: member %q", ErrPathNotFound, tok)
	case ArrayNode:
		i := parseArrayIndex(tok)
		if i < 0 || i >= len(n.Children) {
			return nil, fmt.Errorf("%w: index %q", ErrPathNotFound, tok)
		}
		return n.Children[i], nil
	default:
		return nil, fmt.Errorf("%w: cannot descend into %s", ErrPathNotFound, n.Kind)
	}
}

// resolveTarget returns the node addressed by tokens, or an error.
func resolveTarget(root *Node, tokens []string) (*Node, error) {
	cur := root
	for _, t := range tokens {
		next, err := descend(cur, t)
		if err != nil {
			return nil, err
		}
		cur = next
	}
	return cur, nil
}

// opAdd implements "add": insert value at path. Per RFC 6902:
//   - empty path → replace root with value
//   - path ending at object member → set/insert that member
//   - path ending at array index → insert at that position (existing
//     elements shift right)
//   - path ending at array "-" → append
func opAdd(root *Node, path string, value *Node) (*Node, error) {
	if value == nil {
		return nil, fmt.Errorf("%w: add requires value", ErrPatchSyntax)
	}
	tokens, err := pointerTokens(path)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return cloneNode(value), nil
	}
	parent, last, err := resolveParent(root, tokens)
	if err != nil {
		return nil, err
	}
	switch parent.Kind {
	case ObjectNode:
		for _, c := range parent.Children {
			if c.Kind == KeyValueNode && c.Key == last {
				if len(c.Children) > 0 {
					c.Children[0] = cloneNode(value)
				} else {
					c.Children = []*Node{cloneNode(value)}
				}
				return root, nil
			}
		}
		parent.Children = append(parent.Children, &Node{
			Kind:     KeyValueNode,
			Key:      last,
			Children: []*Node{cloneNode(value)},
		})
		return root, nil
	case ArrayNode:
		if last == "-" {
			parent.Children = append(parent.Children, cloneNode(value))
			return root, nil
		}
		i := parseArrayIndex(last)
		if i < 0 || i > len(parent.Children) {
			return nil, fmt.Errorf("%w: array index %q", ErrPointerSyntax, last)
		}
		parent.Children = append(parent.Children, nil)
		copy(parent.Children[i+1:], parent.Children[i:])
		parent.Children[i] = cloneNode(value)
		return root, nil
	default:
		return nil, fmt.Errorf("%w: add into %s", ErrPathNotFound, parent.Kind)
	}
}

// opRemove implements "remove".
func opRemove(root *Node, path string) (*Node, error) {
	tokens, err := pointerTokens(path)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("%w: cannot remove root", ErrPatchSyntax)
	}
	parent, last, err := resolveParent(root, tokens)
	if err != nil {
		return nil, err
	}
	switch parent.Kind {
	case ObjectNode:
		for i, c := range parent.Children {
			if c.Kind == KeyValueNode && c.Key == last {
				parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
				return root, nil
			}
		}
		return nil, fmt.Errorf("%w: member %q", ErrPathNotFound, last)
	case ArrayNode:
		i := parseArrayIndex(last)
		if i < 0 || i >= len(parent.Children) {
			return nil, fmt.Errorf("%w: array index %q", ErrPointerSyntax, last)
		}
		parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
		return root, nil
	default:
		return nil, fmt.Errorf("%w: remove from %s", ErrPathNotFound, parent.Kind)
	}
}

// opReplace implements "replace": the target must exist, and its value is
// overwritten in place. A replaced array element keeps its comments when
// value has none.
func opReplace(root *Node, path string, value *Node) (*Node, error) {
	if value == nil {
		return nil, fmt.Errorf("%w: replace requires value", ErrPatchSyntax)
	}
	tokens, err := pointerTokens(path)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return cloneNode(value), nil
	}
	parent, last, err := resolveParent(root, tokens)
	if err != nil {
		return nil, err
	}
	switch parent.Kind {
	case ObjectNode:
		if kv := memberNode(parent, last); kv != nil {
			kv.Children = []*Node{cloneNode(value)}
			return root, nil
		}
		return nil, fmt.Errorf("%w: member %q", ErrPathNotFound, last)
	case ArrayNode:
		i := parseArrayIndex(last)
		if i < 0 || i >= len(parent.Children) {
			return nil, fmt.Errorf("%w: index %q", ErrPathNotFound, last)
		}
		parent.Children[i] = replacementNode(parent.Children[i], value)
		return root, nil
	default:
		return nil, fmt.Errorf("%w: cannot descend into %s", ErrPathNotFound, parent.Kind)
	}
}

// memberNode returns obj's KeyValueNode named key, or nil.
func memberNode(obj *Node, key string) *Node {
	for _, c := range obj.Children {
		if c.Kind == KeyValueNode && c.Key == key {
			return c
		}
	}
	return nil
}

// opMove removes from `from` and adds at `path`. A member moved to another
// member keeps its comments.
func opMove(root *Node, from, path string) (*Node, error) {
	tokens, err := pointerTokens(from)
	if err != nil {
		return nil, err
	}
	val, err := resolveTarget(root, tokens)
	if err != nil {
		return nil, err
	}
	dstTokens, err := pointerTokens(path)
	if err != nil {
		return nil, err
	}
	if from == path {
		return root, nil
	}
	if strings.HasPrefix(path, from+"/") {
		return nil, fmt.Errorf("%w: cannot move into descendant", ErrPatchSyntax)
	}
	var srcMember *Node
	if parent, last, err := resolveParent(root, tokens); err == nil && parent.Kind == ObjectNode {
		srcMember = memberNode(parent, last)
	}
	moved := cloneNode(val)
	root, err = opRemove(root, from)
	if err != nil {
		return nil, err
	}
	root, err = opAdd(root, path, moved)
	if err != nil || srcMember == nil {
		return root, err
	}
	if parent, last, err := resolveParent(root, dstTokens); err == nil && parent.Kind == ObjectNode {
		if dst := memberNode(parent, last); dst != nil && !hasComments(dst) {
			dst.HeadComment, dst.Comment, dst.FootComment = srcMember.HeadComment, srcMember.Comment, srcMember.FootComment
		}
	}
	return root, nil
}

// opCopy clones the node at `from` and adds it at `path`.
func opCopy(root *Node, from, path string) (*Node, error) {
	tokens, err := pointerTokens(from)
	if err != nil {
		return nil, err
	}
	val, err := resolveTarget(root, tokens)
	if err != nil {
		return nil, err
	}
	return opAdd(root, path, cloneNode(val))
}

// opTest verifies the value at `path` equals `value`. Returns the unchanged
// root on success, or an error on mismatch (per RFC 6902 the entire patch
// fails).
func opTest(root *Node, path string, value *Node) (*Node, error) {
	if value == nil {
		return nil, fmt.Errorf("%w: test requires value", ErrPatchSyntax)
	}
	tokens, err := pointerTokens(path)
	if err != nil {
		return nil, err
	}
	target := root
	if len(tokens) > 0 {
		target, err = resolveTarget(root, tokens)
		if err != nil {
			return nil, err
		}
	}
	if !nodeEqual(target, value) {
		return nil, fmt.Errorf("%w at %q", ErrPatchTestFailed, path)
	}
	return root, nil
}

// nodeEqual reports whether two nodes are equal as JSON values (RFC 6902
// section 4.6), ignoring comments and source form: numbers compare by
// numeric value, so 1, 1.0 and 1e0 are equal. Used by the "test" operation.
func nodeEqual(a, b *Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case NumberNode:
		return numbersEqual(a.Value, b.Value)
	case StringNode, BooleanNode, NullNode:
		return a.Value == b.Value
	case ArrayNode:
		av := nonCommentChildren(a)
		bv := nonCommentChildren(b)
		if len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !nodeEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case ObjectNode:
		am := objectMembers(a)
		bm := objectMembers(b)
		if len(am) != len(bm) {
			return false
		}
		for k, v := range am {
			bv, ok := bm[k]
			if !ok || !nodeEqual(v, bv) {
				return false
			}
		}
		return true
	}
	return false
}

// nonCommentChildren returns the children of n excluding CommentNode siblings.
func nonCommentChildren(n *Node) []*Node {
	var out []*Node
	for _, c := range n.Children {
		if c.Kind != CommentNode {
			out = append(out, c)
		}
	}
	return out
}

// objectMembers maps an ObjectNode's KeyValueNode children to their value
// nodes by key.
func objectMembers(n *Node) map[string]*Node {
	out := make(map[string]*Node)
	for _, c := range n.Children {
		if c.Kind != KeyValueNode || len(c.Children) == 0 {
			continue
		}
		out[c.Key] = c.Children[0]
	}
	return out
}

// numbersEqual reports whether two JSON number texts denote the same value.
// Text that is not a JSON number compares as text.
func numbersEqual(a, b string) bool {
	if a == b {
		return true
	}
	ca, okA := canonicalNumber(a)
	cb, okB := canonicalNumber(b)
	if !okA || !okB {
		return false
	}
	return ca == cb
}

// canonicalNumber rewrites a JSON number as sign, significant digits and a
// decimal exponent (e.g. "-1.50e2" becomes "-15e1"), so equal values have
// equal text. Zero of either sign becomes "0". It reports false for text
// that is not a JSON number or whose exponent overflows an int.
func canonicalNumber(s string) (string, bool) {
	neg := false
	if s != "" && s[0] == '-' {
		neg = true
		s = s[1:]
	}
	mant, expText, hasExp := strings.Cut(strings.ToLower(s), "e")
	intPart, frac, _ := strings.Cut(mant, ".")
	if intPart == "" {
		return "", false
	}
	digits := intPart + frac
	for i := range len(digits) {
		if digits[i] < '0' || digits[i] > '9' {
			return "", false
		}
	}
	exp := 0
	if hasExp {
		e, err := strconv.Atoi(expText)
		if err != nil {
			return "", false
		}
		exp = e
	}
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		return "0", true
	}
	trimmed := strings.TrimRight(digits, "0")
	shift := len(digits) - len(trimmed) - len(frac)
	if (shift > 0 && exp > math.MaxInt-shift) || (shift < 0 && exp < math.MinInt-shift) {
		return "", false
	}
	exp += shift
	sign := ""
	if neg {
		sign = "-"
	}
	return sign + trimmed + "e" + strconv.Itoa(exp), true
}
