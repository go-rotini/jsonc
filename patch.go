package jsonc

import (
	"errors"
	"fmt"
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

// ParsePatch decodes data (a JSONC array of operation objects) into a
// [Patch]. Comments and trailing commas in the input are tolerated; the
// operation values themselves preserve their JSONC source form.
func ParsePatch(data []byte) (Patch, error) {
	type rawOp struct {
		Op    string   `json:"op"`
		Path  string   `json:"path"`
		From  string   `json:"from,omitempty"`
		Value RawValue `json:"value,omitempty"`
	}
	var ops []rawOp
	if err := Unmarshal(data, &ops); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPatchSyntax, err)
	}
	out := make(Patch, len(ops))
	for i, op := range ops {
		out[i] = PatchOp{Op: op.Op, Path: op.Path, From: op.From}
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

// Apply applies p to root in order, returning the resulting [Node]. The
// input root is not modified — Apply works on a deep clone. Any failure
// returns an error and leaves the (cloned) intermediate state unobservable
// to the caller.
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
		out[i] = decodePointerToken(t)
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
	cur := root
	for _, t := range tokens[:len(tokens)-1] {
		next, err := descend(cur, t)
		if err != nil {
			return nil, "", err
		}
		cur = next
	}
	return cur, tokens[len(tokens)-1], nil
}

// descend follows one token from n.
func descend(n *Node, tok string) (*Node, error) {
	switch n.Kind {
	case ObjectNode:
		for _, c := range n.Children {
			if c.Kind == KeyValueNode && c.Key == tok && len(c.Children) > 0 {
				return c.Children[0], nil
			}
		}
		return nil, fmt.Errorf("%w: member %q", ErrPathNotFound, tok)
	case ArrayNode:
		i, err := strconv.Atoi(tok)
		if err != nil || i < 0 || i >= len(n.Children) {
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
		// If the member already exists, replace its value; otherwise append.
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
		i, err := strconv.Atoi(last)
		if err != nil || i < 0 || i > len(parent.Children) {
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
		i, err := strconv.Atoi(last)
		if err != nil || i < 0 || i >= len(parent.Children) {
			return nil, fmt.Errorf("%w: array index %q", ErrPointerSyntax, last)
		}
		parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
		return root, nil
	default:
		return nil, fmt.Errorf("%w: remove from %s", ErrPathNotFound, parent.Kind)
	}
}

// opReplace implements "replace" — equivalent to remove+add but the target
// must exist.
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
	// Confirm target exists, then call add (which handles both insert and overwrite).
	if _, err := resolveTarget(root, tokens); err != nil {
		return nil, err
	}
	return opAdd(root, path, value)
}

// opMove removes from `from` and adds at `path`.
func opMove(root *Node, from, path string) (*Node, error) {
	if from == path {
		return root, nil
	}
	if strings.HasPrefix(path, from+"/") {
		return nil, fmt.Errorf("%w: cannot move into descendant", ErrPatchSyntax)
	}
	tokens, err := pointerTokens(from)
	if err != nil {
		return nil, err
	}
	val, err := resolveTarget(root, tokens)
	if err != nil {
		return nil, err
	}
	moved := cloneNode(val)
	root, err = opRemove(root, from)
	if err != nil {
		return nil, err
	}
	return opAdd(root, path, moved)
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
		return nil, fmt.Errorf("%w: test failed at %q", ErrPatchSyntax, path)
	}
	return root, nil
}

// nodeEqual reports whether two nodes are equal as JSON values, ignoring
// comments and source form. Used by the "test" operation.
func nodeEqual(a, b *Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case StringNode, NumberNode, BooleanNode, NullNode:
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
