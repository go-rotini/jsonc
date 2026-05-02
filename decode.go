package jsonc

import (
	"context"
	"encoding"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// errKeyOverflow / errUnsupportedMapKey are sentinel errors used to satisfy
// the err113 lint rule for map-key construction failures.
var (
	errKeyOverflow       = errors.New("map key overflows target type")
	errUnsupportedMapKey = errors.New("unsupported map key type")
)

// Unmarshaler is implemented by types that need structured access to their
// JSONC data during decoding. The unmarshal function decodes the JSONC value
// into the provided Go value, similar to [Unmarshal].
type Unmarshaler interface {
	UnmarshalJSONC(unmarshal func(any) error) error
}

// BytesUnmarshaler is implemented by types that can decode themselves
// directly from raw JSONC bytes.
type BytesUnmarshaler interface {
	UnmarshalJSONC(data []byte) error
}

// UnmarshalerContext is like [Unmarshaler] but receives a context, set via
// [Decoder.DecodeContext].
type UnmarshalerContext interface {
	UnmarshalJSONC(ctx context.Context, unmarshal func(any) error) error
}

type decoder struct {
	opts       *decoderOptions
	ctx        context.Context //nolint:containedctx // by design
	typeErrors []string
}

func newDecoder(opts *decoderOptions) *decoder {
	return &decoder{opts: opts, ctx: context.Background()}
}

// decode is the top-level entry point. v must be a non-nil pointer.
func (d *decoder) decode(root *node, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return ErrNilPointer
	}
	rv = rv.Elem()
	if err := d.decodeValue(root, rv); err != nil {
		return err
	}
	if len(d.typeErrors) > 0 {
		return &TypeError{Errors: d.typeErrors}
	}
	return nil
}

// decodeValue routes a single AST node to the right type-specific decoder
// based on the target reflect.Value's kind and any unmarshaler interfaces.
func (d *decoder) decodeValue(n *node, rv reflect.Value) error {
	// Walk through pointers, allocating as needed, except for null which
	// terminates at the pointer (sets it to nil).
	for rv.Kind() == reflect.Pointer {
		if n.kind == nodeNull {
			if !rv.IsNil() {
				rv.Set(reflect.Zero(rv.Type()))
			}
			return nil
		}
		if rv.IsNil() {
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		rv = rv.Elem()
	}

	// Special-cased types (RawValue, json.RawMessage, json.Number, time.Time)
	// take priority before unmarshaler interfaces — they would also satisfy
	// some interfaces, but the special-case path preserves the value verbatim.
	if d.decodeSpecialType(n, rv) {
		return nil
	}

	// Custom unmarshalers (registered via WithCustomUnmarshaler).
	if d.opts.customUnmarshalers != nil {
		if fn, ok := d.opts.customUnmarshalers[rv.Type()]; ok {
			raw := nodeRawBytes(n)
			ptr := reflect.New(rv.Type())
			result := reflect.ValueOf(fn).Call([]reflect.Value{ptr, reflect.ValueOf(raw)})
			if !result[0].IsNil() {
				if err, ok := result[0].Interface().(error); ok {
					return err
				}
			}
			rv.Set(ptr.Elem())
			return nil
		}
	}

	// Unmarshaler interfaces (priority order).
	if rv.CanAddr() && rv.Addr().CanInterface() {
		if handled, err := d.decodeViaInterface(n, rv); handled {
			return err
		}
	}

	// Default reflection-based dispatch.
	switch n.kind {
	case nodeNull:
		return d.decodeNull(rv)
	case nodeBool:
		return d.decodeBool(n, rv)
	case nodeNumber:
		return d.decodeNumber(n, rv)
	case nodeString:
		return d.decodeString(n, rv)
	case nodeArray:
		return d.decodeArray(n, rv)
	case nodeObject:
		return d.decodeObject(n, rv)
	}
	return nil
}

// decodeSpecialType handles types that need to bypass interface dispatch:
// RawValue, json.RawMessage (raw bytes), json.Number (preserved text), and
// time.Time (RFC 3339, matches stdlib). All errors are accumulated as type
// errors via typeErrorf — no error is returned directly.
func (d *decoder) decodeSpecialType(n *node, rv reflect.Value) bool {
	switch rv.Type() {
	case reflect.TypeFor[RawValue]():
		rv.SetBytes(nodeRawBytes(n))
		return true
	case reflect.TypeFor[json.RawMessage]():
		rv.SetBytes(nodeRawBytes(n))
		return true
	case reflect.TypeFor[json.Number]():
		// json.Number can absorb any number; for non-numbers, accumulate type error.
		if n.kind != nodeNumber {
			d.typeErrorf(n, "cannot decode %s into json.Number", nodeKindName(n.kind))
			return true
		}
		rv.SetString(n.value)
		return true
	case reflect.TypeFor[time.Time]():
		// time.Time uses its own UnmarshalJSON (RFC 3339).
		if n.kind != nodeString {
			d.typeErrorf(n, "cannot decode %s into time.Time", nodeKindName(n.kind))
			return true
		}
		var t time.Time
		if err := t.UnmarshalJSON([]byte(n.rawValue)); err != nil {
			d.typeErrorf(n, "invalid time.Time: %v", err)
			return true
		}
		rv.Set(reflect.ValueOf(t))
		return true
	}

	// time.Duration via int64 path is handled in decodeNumber/decodeString.
	return false
}

// decodeViaInterface checks the various unmarshaler interfaces on rv.Addr().
// Returns (handled, err) — handled=true means the interface was used (whether
// successfully or with an error).
func (d *decoder) decodeViaInterface(n *node, rv reflect.Value) (bool, error) {
	iface := rv.Addr().Interface()

	if u, ok := iface.(UnmarshalerContext); ok {
		return true, u.UnmarshalJSONC(d.ctx, d.makeUnmarshalCallback(n))
	}
	if u, ok := iface.(Unmarshaler); ok {
		return true, u.UnmarshalJSONC(d.makeUnmarshalCallback(n))
	}
	if u, ok := iface.(BytesUnmarshaler); ok {
		return true, u.UnmarshalJSONC(nodeRawBytes(n))
	}
	if u, ok := iface.(json.Unmarshaler); ok {
		// Stdlib json.Unmarshaler: feed it standardized JSON bytes (comments
		// stripped) so existing implementations work unchanged.
		std, err := standardizeNodeBytes(n)
		if err != nil {
			return true, err
		}
		return true, u.UnmarshalJSON(std)
	}
	if u, ok := iface.(encoding.TextUnmarshaler); ok && n.kind == nodeString {
		return true, u.UnmarshalText([]byte(n.value))
	}
	return false, nil
}

// makeUnmarshalCallback returns the closure passed to Unmarshaler /
// UnmarshalerContext methods. The user code calls it with a Go value; we
// decode the same node into that value.
func (d *decoder) makeUnmarshalCallback(n *node) func(any) error {
	return func(v any) error {
		return d.decode(n, v)
	}
}

func (d *decoder) decodeNull(rv reflect.Value) error {
	switch rv.Kind() {
	case reflect.Interface, reflect.Map, reflect.Slice:
		rv.Set(reflect.Zero(rv.Type()))
	default:
		// For other kinds, leave at zero value (matches stdlib).
		rv.Set(reflect.Zero(rv.Type()))
	}
	return nil
}

func (d *decoder) decodeBool(n *node, rv reflect.Value) error {
	b := n.value == "true"
	switch rv.Kind() {
	case reflect.Bool:
		rv.SetBool(b)
	case reflect.Interface:
		rv.Set(reflect.ValueOf(b))
	default:
		d.typeErrorf(n, "cannot decode boolean into %s", rv.Type())
	}
	return nil
}

func (d *decoder) decodeNumber(n *node, rv reflect.Value) error {
	s := n.value
	isFloatShaped := strings.ContainsAny(s, ".eE")

	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if isFloatShaped {
			d.typeErrorf(n, "cannot decode float-shaped number %q into %s", s, rv.Type())
			return nil
		}
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			d.typeErrorf(n, "invalid integer %q: %v", s, err)
			return nil
		}
		if rv.OverflowInt(i) {
			return &OverflowError{Value: s, Type: rv.Type().String(), Pos: n.pos}
		}
		rv.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if isFloatShaped {
			d.typeErrorf(n, "cannot decode float-shaped number %q into %s", s, rv.Type())
			return nil
		}
		u, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			d.typeErrorf(n, "invalid unsigned integer %q: %v", s, err)
			return nil
		}
		if rv.OverflowUint(u) {
			return &OverflowError{Value: s, Type: rv.Type().String(), Pos: n.pos}
		}
		rv.SetUint(u)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			d.typeErrorf(n, "invalid float %q: %v", s, err)
			return nil
		}
		rv.SetFloat(f)
	case reflect.Interface:
		d.numberToInterface(n, rv, s, isFloatShaped)
	default:
		d.numberToBigOrError(n, rv, s, isFloatShaped)
	}
	return nil
}

func (d *decoder) numberToInterface(n *node, rv reflect.Value, s string, isFloatShaped bool) {
	if d.opts.useNumber {
		rv.Set(reflect.ValueOf(json.Number(s)))
		return
	}
	if isFloatShaped {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			d.typeErrorf(n, "invalid float %q: %v", s, err)
			return
		}
		rv.Set(reflect.ValueOf(f))
		return
	}
	// Try int64 first; on overflow, fall back to float64 (matches stdlib's
	// behavior of using float64 for any number when UseNumber is off).
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		// Stdlib produces float64 for all numbers in interface{} mode (without
		// UseNumber). We follow that convention.
		rv.Set(reflect.ValueOf(float64(i)))
		return
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		rv.Set(reflect.ValueOf(f))
		return
	}
	d.typeErrorf(n, "invalid number %q", s)
}

func (d *decoder) numberToBigOrError(n *node, rv reflect.Value, s string, isFloatShaped bool) {
	switch rv.Type() {
	case reflect.TypeFor[big.Int]():
		if isFloatShaped {
			d.typeErrorf(n, "cannot decode float-shaped number %q into big.Int", s)
			return
		}
		bi := new(big.Int)
		if _, ok := bi.SetString(s, 10); !ok {
			d.typeErrorf(n, "invalid big.Int %q", s)
			return
		}
		rv.Set(reflect.ValueOf(*bi))
	case reflect.TypeFor[big.Float]():
		bf, _, err := big.ParseFloat(s, 10, 0, big.ToNearestEven)
		if err != nil {
			d.typeErrorf(n, "invalid big.Float %q: %v", s, err)
			return
		}
		rv.Set(reflect.ValueOf(*bf))
	default:
		d.typeErrorf(n, "cannot decode number into %s", rv.Type())
	}
}

func (d *decoder) decodeString(n *node, rv reflect.Value) error {
	s := n.value
	switch rv.Kind() {
	case reflect.String:
		rv.SetString(s)
	case reflect.Slice:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			data, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				d.typeErrorf(n, "invalid base64: %v", err)
				return nil
			}
			rv.SetBytes(data)
		} else {
			d.typeErrorf(n, "cannot decode string into %s", rv.Type())
		}
	case reflect.Int64:
		// time.Duration extension — accept "1h30m"-style strings.
		if rv.Type() == reflect.TypeFor[time.Duration]() {
			dur, err := time.ParseDuration(s)
			if err != nil {
				d.typeErrorf(n, "cannot parse duration %q: %v", s, err)
				return nil
			}
			rv.SetInt(int64(dur))
		} else {
			d.typeErrorf(n, "cannot decode string into %s", rv.Type())
		}
	case reflect.Interface:
		rv.Set(reflect.ValueOf(s))
	default:
		d.typeErrorf(n, "cannot decode string into %s", rv.Type())
	}
	return nil
}

func (d *decoder) decodeArray(n *node, rv reflect.Value) error {
	// Filter out comment children so length checks and elements are aligned.
	values := valueChildren(n)
	switch rv.Kind() {
	case reflect.Slice:
		slice := reflect.MakeSlice(rv.Type(), len(values), len(values))
		for i, child := range values {
			if err := d.decodeValue(child, slice.Index(i)); err != nil {
				return err
			}
		}
		rv.Set(slice)
	case reflect.Array:
		if rv.Len() != len(values) {
			d.typeErrorf(n, "array length %d does not match target [%d]%s",
				len(values), rv.Len(), rv.Type().Elem())
			return nil
		}
		for i, child := range values {
			if err := d.decodeValue(child, rv.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Interface:
		arr := make([]any, len(values))
		for i, child := range values {
			val, err := d.nodeToInterface(child)
			if err != nil {
				return err
			}
			arr[i] = val
		}
		rv.Set(reflect.ValueOf(arr))
	default:
		d.typeErrorf(n, "cannot decode array into %s", rv.Type())
	}
	return nil
}

func (d *decoder) decodeObject(n *node, rv reflect.Value) error {
	switch rv.Kind() {
	case reflect.Struct:
		return d.decodeObjectToStruct(n, rv)
	case reflect.Map:
		return d.decodeObjectToMap(n, rv)
	case reflect.Interface:
		if d.opts.useOrderedMap {
			ms, err := d.nodeToMapSlice(n)
			if err != nil {
				return err
			}
			rv.Set(reflect.ValueOf(ms))
			return nil
		}
		m := make(map[string]any)
		if err := d.fillGenericMap(n, m); err != nil {
			return err
		}
		rv.Set(reflect.ValueOf(m))
		return nil
	default:
		d.typeErrorf(n, "cannot decode object into %s", rv.Type())
		return nil
	}
}

func (d *decoder) decodeObjectToStruct(n *node, rv reflect.Value) error {
	sf := getStructFields(rv.Type())
	assigned := make(map[string]bool, len(n.children))

	for _, child := range n.children {
		if child.kind != nodeKeyValue {
			continue
		}
		key := child.key
		assigned[key] = true

		idx, ok := sf.byName[key]
		if !ok {
			idx = caseInsensitiveLookup(sf, key)
		}
		if idx < 0 {
			if d.opts.strict {
				return &UnknownFieldError{Field: key, Pos: child.pos}
			}
			continue
		}

		fi := sf.fields[idx]
		fv := fieldByIndexAlloc(rv, fi.index)

		// Note: a JSON null sets pointer/interface fields to nil; "key
		// present with null" satisfies `required` (the key exists).
		valueChild := child.children[0]
		if err := d.decodeValue(valueChild, fv); err != nil {
			return err
		}
	}

	// Required + default handling for unassigned fields.
	for _, fi := range sf.fields {
		if assigned[fi.name] {
			continue
		}
		if fi.required {
			return &SyntaxError{
				Message: fmt.Sprintf("required key %q is missing", fi.name),
				Pos:     n.pos,
			}
		}
		if fi.hasDefault && d.opts.applyDefaults {
			fv := fieldByIndexAlloc(rv, fi.index)
			if err := applyDefault(fi, fv); err != nil {
				return err
			}
		}
	}

	if d.opts.validator != nil {
		if rv.CanAddr() {
			if err := d.opts.validator.Struct(rv.Addr().Interface()); err != nil {
				return &ValidationError{Err: err, Pos: n.pos}
			}
		} else {
			if err := d.opts.validator.Struct(rv.Interface()); err != nil {
				return &ValidationError{Err: err, Pos: n.pos}
			}
		}
	}
	return nil
}

func (d *decoder) decodeObjectToMap(n *node, rv reflect.Value) error {
	if rv.IsNil() {
		rv.Set(reflect.MakeMap(rv.Type()))
	}
	keyType := rv.Type().Key()
	valType := rv.Type().Elem()

	if !canDecodeMapKey(keyType) {
		d.typeErrorf(n, "map key type %s is not supported", keyType)
		return nil
	}

	for _, child := range n.children {
		if child.kind != nodeKeyValue {
			continue
		}
		mapKey, err := buildMapKey(keyType, child.key)
		if err != nil {
			return &SyntaxError{
				Message: fmt.Sprintf("cannot decode key %q into %s: %v", child.key, keyType, err),
				Pos:     child.pos,
			}
		}
		mapVal := reflect.New(valType).Elem()
		if err := d.decodeValue(child.children[0], mapVal); err != nil {
			return err
		}
		rv.SetMapIndex(mapKey, mapVal)
	}
	return nil
}

// fillGenericMap populates a map[string]any with the contents of an object
// node. Used for untyped (any) decoding.
func (d *decoder) fillGenericMap(n *node, m map[string]any) error {
	for _, child := range n.children {
		if child.kind != nodeKeyValue {
			continue
		}
		val, err := d.nodeToInterface(child.children[0])
		if err != nil {
			return err
		}
		m[child.key] = val
	}
	return nil
}

// nodeToInterface materializes an AST node as a Go value following the
// untyped-interface conventions: float64 (or json.Number) for numbers,
// map[string]any (or MapSlice) for objects, []any for arrays.
func (d *decoder) nodeToInterface(n *node) (any, error) {
	switch n.kind {
	case nodeNull:
		return nil, nil
	case nodeBool:
		return n.value == "true", nil
	case nodeNumber:
		if d.opts.useNumber {
			return json.Number(n.value), nil
		}
		f, err := strconv.ParseFloat(n.value, 64)
		if err != nil {
			return nil, &SyntaxError{Message: fmt.Sprintf("invalid number %q: %v", n.value, err), Pos: n.pos}
		}
		return f, nil
	case nodeString:
		return n.value, nil
	case nodeArray:
		values := valueChildren(n)
		out := make([]any, len(values))
		for i, child := range values {
			v, err := d.nodeToInterface(child)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case nodeObject:
		if d.opts.useOrderedMap {
			return d.nodeToMapSlice(n)
		}
		m := make(map[string]any)
		if err := d.fillGenericMap(n, m); err != nil {
			return nil, err
		}
		return m, nil
	default:
		return nil, &SyntaxError{Message: fmt.Sprintf("unexpected node kind %d", n.kind), Pos: n.pos}
	}
}

// nodeToMapSlice materializes an object node as a MapSlice preserving
// member order.
func (d *decoder) nodeToMapSlice(n *node) (MapSlice, error) {
	var ms MapSlice
	for _, child := range n.children {
		if child.kind != nodeKeyValue {
			continue
		}
		val, err := d.nodeToInterface(child.children[0])
		if err != nil {
			return nil, err
		}
		ms = append(ms, MapItem{Key: child.key, Value: val})
	}
	return ms, nil
}

// typeErrorf accumulates a TypeError message rather than failing fast.
// Multiple errors are collected and returned as a single TypeError at the
// end of decoding.
func (d *decoder) typeErrorf(n *node, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if n.pos.Line > 0 {
		msg = fmt.Sprintf("line %d: %s", n.pos.Line, msg)
	}
	d.typeErrors = append(d.typeErrors, msg)
}

// applyDefault sets a struct field to the parsed default value from its tag.
// Only scalar types are supported.
func applyDefault(fi fieldInfo, fv reflect.Value) error {
	raw := fi.defaultValue
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return &DefaultError{Field: fi.name, Message: fmt.Sprintf("invalid bool default %q", raw)}
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if fv.Type() == reflect.TypeFor[time.Duration]() {
			dur, err := time.ParseDuration(raw)
			if err != nil {
				return &DefaultError{Field: fi.name, Message: fmt.Sprintf("invalid duration default %q", raw)}
			}
			fv.SetInt(int64(dur))
			return nil
		}
		i, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return &DefaultError{Field: fi.name, Message: fmt.Sprintf("invalid int default %q", raw)}
		}
		if fv.OverflowInt(i) {
			return &DefaultError{Field: fi.name, Message: fmt.Sprintf("default %q overflows %s", raw, fv.Type())}
		}
		fv.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return &DefaultError{Field: fi.name, Message: fmt.Sprintf("invalid uint default %q", raw)}
		}
		if fv.OverflowUint(u) {
			return &DefaultError{Field: fi.name, Message: fmt.Sprintf("default %q overflows %s", raw, fv.Type())}
		}
		fv.SetUint(u)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return &DefaultError{Field: fi.name, Message: fmt.Sprintf("invalid float default %q", raw)}
		}
		fv.SetFloat(f)
	default:
		return &DefaultError{Field: fi.name, Message: fmt.Sprintf("default tag not supported for type %s", fv.Type())}
	}
	return nil
}

// caseInsensitiveLookup finds a struct field by case-insensitive name match.
// Returns -1 if no match.
func caseInsensitiveLookup(sf *structFields, key string) int {
	lower := strings.ToLower(key)
	for i, fi := range sf.fields {
		if strings.EqualFold(fi.name, lower) {
			return i
		}
	}
	return -1
}

// fieldByIndexAlloc returns the addressable reflect.Value of a struct field
// (resolving an index path), allocating intermediate pointer-to-struct fields
// on demand.
func fieldByIndexAlloc(rv reflect.Value, index []int) reflect.Value {
	for i, idx := range index {
		if i > 0 {
			if rv.Kind() == reflect.Pointer {
				if rv.IsNil() {
					rv.Set(reflect.New(rv.Type().Elem()))
				}
				rv = rv.Elem()
			}
		}
		rv = rv.Field(idx)
	}
	return rv
}

// canDecodeMapKey reports whether a Go map key type is decodable from a
// JSON object key (always a string). Supported: string-kind, integer-kinds,
// and types implementing TextUnmarshaler.
func canDecodeMapKey(t reflect.Type) bool {
	if t.Kind() == reflect.String {
		return true
	}
	if reflect.PointerTo(t).Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) {
		return true
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	}
	return false
}

// buildMapKey constructs a reflect.Value of type keyType from a JSON
// object-key string. Caller must have validated keyType is supported.
func buildMapKey(keyType reflect.Type, key string) (reflect.Value, error) {
	if keyType.Kind() == reflect.String {
		v := reflect.New(keyType).Elem()
		v.SetString(key)
		return v, nil
	}
	if reflect.PointerTo(keyType).Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) {
		v := reflect.New(keyType)
		tu, ok := v.Interface().(encoding.TextUnmarshaler)
		if !ok {
			return reflect.Value{}, fmt.Errorf("%w: %s", errUnsupportedMapKey, keyType)
		}
		if err := tu.UnmarshalText([]byte(key)); err != nil {
			return reflect.Value{}, err
		}
		return v.Elem(), nil
	}
	switch keyType.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			return reflect.Value{}, err
		}
		v := reflect.New(keyType).Elem()
		if v.OverflowInt(i) {
			return reflect.Value{}, fmt.Errorf("%w: key %q does not fit %s", errKeyOverflow, key, keyType)
		}
		v.SetInt(i)
		return v, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(key, 10, 64)
		if err != nil {
			return reflect.Value{}, err
		}
		v := reflect.New(keyType).Elem()
		if v.OverflowUint(u) {
			return reflect.Value{}, fmt.Errorf("%w: key %q does not fit %s", errKeyOverflow, key, keyType)
		}
		v.SetUint(u)
		return v, nil
	}
	return reflect.Value{}, fmt.Errorf("%w: %s", errUnsupportedMapKey, keyType)
}

// valueChildren returns the value children of a container node, filtering
// out CommentNode siblings.
func valueChildren(n *node) []*node {
	out := n.children[:0:0]
	for _, child := range n.children {
		if child.kind != nodeComment {
			out = append(out, child)
		}
	}
	return out
}

// nodeRawBytes returns the raw JSONC source bytes for a node. For scalar
// nodes this is the rawValue (preserving quoting and source form). For
// containers we re-emit via the AST encoder.
func nodeRawBytes(n *node) []byte {
	switch n.kind {
	case nodeString, nodeNumber, nodeBool, nodeNull:
		if n.rawValue != "" {
			return []byte(n.rawValue)
		}
		return []byte(n.value)
	default:
		// Re-emit container as compact JSONC. The AST encoder is total over
		// well-formed nodes — encodeNode only ever returns nil error for
		// container kinds — so we don't propagate the error here.
		enc := newNodeEncoder(defaultEncodeOptions())
		out, err := enc.encodeNode(n)
		if err != nil {
			return nil
		}
		return out
	}
}

// standardizeNodeBytes returns RFC 8259 JSON bytes for a node — useful for
// passing to types that implement json.Unmarshaler. Strips comments and
// trailing commas.
func standardizeNodeBytes(n *node) ([]byte, error) {
	opts := defaultEncodeOptions()
	opts.strictJSONOutput = true
	enc := newNodeEncoder(opts)
	return enc.encodeNode(n)
}

// nodeKindName returns a human-readable name for a node kind, used in
// type-error messages.
func nodeKindName(k nodeKind) string {
	switch k {
	case nodeObject:
		return "object"
	case nodeArray:
		return "array"
	case nodeString:
		return "string"
	case nodeNumber:
		return "number"
	case nodeBool:
		return "boolean"
	case nodeNull:
		return "null"
	default:
		return "unknown"
	}
}

// avoidUnusedImport keeps the math import alive for future use (e.g.,
// detecting NaN/Inf during decode of typed numbers). The decoder itself
// doesn't currently need math at top level, but the file's tests do.
var _ = math.Inf
