package interp

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ubgo/validator/internal/rules"
	"github.com/ubgo/validator/ir"
)

// Reflect helpers: the one place that knows how Go values map onto IR
// kinds, so the walker in interp.go reads as IR semantics, not reflection.

var (
	timeType     = reflect.TypeFor[time.Time]()
	durationType = reflect.TypeFor[time.Duration]()
	anyMapType   = reflect.TypeFor[map[string]any]()
)

// deref unwraps interfaces and pointers. isNil reports a nil at any level
// (a nil pointer, a nil interface, or an invalid Value).
func deref(v reflect.Value) (out reflect.Value, isNil bool) {
	for {
		if !v.IsValid() {
			return v, true
		}
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface:
			if v.IsNil() {
				return v, true
			}
			v = v.Elem()
		default:
			return v, false
		}
	}
}

// nullable reads the Nullable flag through the sealed interface.
func nullable(n ir.TypeNode) bool {
	switch t := n.(type) {
	case *ir.ObjectNode:
		return t.Nullable
	case *ir.StringNode:
		return t.Nullable
	case *ir.IntNode:
		return t.Nullable
	case *ir.FloatNode:
		return t.Nullable
	case *ir.BoolNode:
		return t.Nullable
	case *ir.BytesNode:
		return t.Nullable
	case *ir.TimeNode:
		return t.Nullable
	case *ir.DurationNode:
		return t.Nullable
	case *ir.ArrayNode:
		return t.Nullable
	case *ir.MapNode:
		return t.Nullable
	case *ir.UnionNode:
		return t.Nullable
	case *ir.OneOfNode:
		return t.Nullable
	case *ir.RefNode:
		return t.Nullable
	case *ir.AnyNode:
		return t.Nullable
	}
	return false
}

func isInt(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	}
	return false
}

func isFloat(v reflect.Value) bool {
	return v.Kind() == reflect.Float32 || v.Kind() == reflect.Float64
}

func isBytes(v reflect.Value) bool {
	return v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8
}

// isStringMap reports a map with string keys — JSON-decoded input.
func isStringMap(v reflect.Value) bool {
	return v.Kind() == reflect.Map && v.Type().Key().Kind() == reflect.String
}

// toFloat converts any numeric value (ints, uints, floats, durations) to
// float64 for rule comparison.
func toFloat(v reflect.Value) (float64, bool) {
	switch {
	case isInt(v):
		if v.CanInt() {
			return float64(v.Int()), true
		}
		return float64(v.Uint()), true
	case isFloat(v):
		return v.Float(), true
	}
	return 0, false
}

// paramFloat reads a numeric rule parameter; JSON decoding yields float64,
// Go construction yields the native type.
func paramFloat(p any) (float64, bool) {
	if p == nil {
		return 0, false
	}
	return toFloat(reflect.ValueOf(p))
}

// paramInt reads an integer rule parameter.
func paramInt(p any) (int, bool) {
	f, ok := paramFloat(p)
	if !ok {
		return 0, false
	}
	return int(f), true
}

// wireName is the key a field appears under in JSON.
func wireName(f *ir.FieldNode) string {
	if f.JSONName != "" {
		return f.JSONName
	}
	return f.Name
}

// wireOf resolves a Go field name to its wire name within an object.
func wireOf(o *ir.ObjectNode, goName string) string {
	for i := range o.Fields {
		if o.Fields[i].Name == goName {
			return wireName(&o.Fields[i])
		}
	}
	return goName
}

// present decides whether a value counts as supplied. See the package doc
// for the rule: nil-able kinds must be non-nil, strings non-empty, and
// numbers/bools always count.
func present(v reflect.Value) bool {
	if !v.IsValid() {
		return false
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map:
		return !v.IsNil()
	case reflect.String:
		return v.Len() > 0
	}
	// A zero time.Time is "no time given", the same way "" is "no string
	// given"; every other struct counts as present.
	if v.Type() == timeType {
		return !v.Interface().(time.Time).IsZero()
	}
	return true
}

// kindMismatch reports the shape a node expects when val does not have it,
// or "" when the value fits. Shared by the walker and gated rules so a
// value that is the wrong shape is reported once, as a type issue, and
// never also fails every rule on the node.
func kindMismatch(n ir.TypeNode, val reflect.Value) string {
	switch n.(type) {
	case *ir.ObjectNode, *ir.OneOfNode:
		if val.Kind() != reflect.Struct && !isStringMap(val) {
			return "object"
		}
	case *ir.StringNode:
		if val.Kind() != reflect.String {
			return "string"
		}
	case *ir.IntNode:
		if !isIntegral(val) {
			return "integer"
		}
	case *ir.FloatNode:
		if !isFloat(val) && !isInt(val) {
			return "number"
		}
	case *ir.BoolNode:
		if val.Kind() != reflect.Bool {
			return "boolean"
		}
	case *ir.BytesNode:
		if !isBytes(val) {
			return "bytes"
		}
	case *ir.TimeNode:
		if val.Type() != timeType {
			return "time"
		}
	case *ir.DurationNode:
		if val.Type() != durationType && !isIntegral(val) {
			return "duration"
		}
	case *ir.ArrayNode:
		if (val.Kind() != reflect.Slice && val.Kind() != reflect.Array) || isBytes(val) {
			return "array"
		}
	case *ir.MapNode:
		if val.Kind() != reflect.Map {
			return "map"
		}
	}
	return ""
}

// isIntegral reports a value that an integer node accepts: any Go integer,
// or a float carrying a whole number — JSON has no integer type, so a
// decoded 30 arrives as float64(30) and must not be rejected.
func isIntegral(v reflect.Value) bool {
	if isInt(v) {
		return true
	}
	if isFloat(v) {
		f := v.Float()
		return f == math.Trunc(f) && !math.IsInf(f, 0)
	}
	return false
}

// fieldValue reads a field from a struct (by Go name) or a string-keyed
// map (by wire name), reporting presence. Interface-typed struct fields
// and map values are unwrapped one level so presence sees through them.
func fieldValue(parent reflect.Value, f ir.FieldNode) (reflect.Value, bool) {
	parent, isNil := deref(parent)
	if isNil {
		return reflect.Value{}, false
	}
	var v reflect.Value
	switch {
	case parent.Kind() == reflect.Struct:
		v = parent.FieldByName(f.Name)
	case isStringMap(parent):
		v = parent.MapIndex(reflect.ValueOf(wireName(&f)).Convert(parent.Type().Key()))
	default:
		return reflect.Value{}, false
	}
	if !v.IsValid() {
		return v, false
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return v, false
		}
		return v, present(v.Elem())
	}
	return v, present(v)
}

// lookupByWireName finds a field by its JSON name in a struct (matching the
// json tag, then the Go name) or a map. Used for oneof discriminators, which
// the IR names by wire key.
func lookupByWireName(parent reflect.Value, wire string) (reflect.Value, bool) {
	parent, isNil := deref(parent)
	if isNil {
		return reflect.Value{}, false
	}
	if isStringMap(parent) {
		v := parent.MapIndex(reflect.ValueOf(wire).Convert(parent.Type().Key()))
		return v, v.IsValid()
	}
	if parent.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	t := parent.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		if name, _, _ := strings.Cut(sf.Tag.Get("json"), ","); name == wire {
			return parent.Field(i), true
		}
	}
	if v := parent.FieldByName(wire); v.IsValid() {
		return v, true
	}
	// Case-insensitive fallback mirrors encoding/json's matching.
	for i := 0; i < t.NumField(); i++ {
		if strings.EqualFold(t.Field(i).Name, wire) && t.Field(i).IsExported() {
			return parent.Field(i), true
		}
	}
	return reflect.Value{}, false
}

// defaultOf returns the field's default transform value, if declared.
func defaultOf(f *ir.FieldNode) (any, bool) {
	for _, t := range f.Transforms {
		if t.Kind == ir.TransformDefault {
			v, ok := t.Params[ir.ParamValue]
			return v, ok
		}
	}
	return nil, false
}

// applyDefault writes def into the absent field when the parent is
// settable, and returns the value to validate either way. A default whose
// type cannot be assigned is returned as-is for validation, where the type
// check will report it — better than a panic from reflect.Set.
func applyDefault(parent reflect.Value, f ir.FieldNode, def any) reflect.Value {
	dv := reflect.ValueOf(def)
	if !dv.IsValid() {
		return dv
	}
	parent, isNil := deref(parent)
	if isNil {
		return dv
	}
	switch {
	case parent.Kind() == reflect.Struct:
		target := parent.FieldByName(f.Name)
		if target.IsValid() && target.CanSet() && dv.Type().ConvertibleTo(target.Type()) {
			target.Set(dv.Convert(target.Type()))
			return target
		}
	case isStringMap(parent):
		if parent.Type() == anyMapType {
			parent.SetMapIndex(reflect.ValueOf(wireName(&f)), dv)
		}
	}
	return dv
}

// zeroFor produces the zero Go value a node's kind maps to, used when a
// gated rule targets an absent field.
func zeroFor(n ir.TypeNode) reflect.Value {
	switch n.(type) {
	case *ir.StringNode:
		return reflect.ValueOf("")
	case *ir.IntNode:
		return reflect.ValueOf(0)
	case *ir.FloatNode:
		return reflect.ValueOf(0.0)
	case *ir.BoolNode:
		return reflect.ValueOf(false)
	case *ir.BytesNode:
		return reflect.ValueOf([]byte{})
	case *ir.TimeNode:
		return reflect.ValueOf(time.Time{})
	case *ir.DurationNode:
		return reflect.ValueOf(time.Duration(0))
	case *ir.ArrayNode:
		return reflect.ValueOf([]any{})
	case *ir.MapNode:
		return reflect.ValueOf(map[string]any{})
	}
	return reflect.ValueOf(map[string]any{})
}

// applyStringTransform runs one pre-phase string transform. Unsupported
// kinds were rejected at New, so the default arm is unreachable by
// construction.
func applyStringTransform(kind ir.TransformKind, s string) string {
	switch kind {
	case ir.TransformTrim:
		return strings.TrimSpace(s)
	case ir.TransformLower:
		return strings.ToLower(s)
	case ir.TransformUpper:
		return strings.ToUpper(s)
	case ir.TransformNormalizeEmail:
		return rules.NormalizeEmail(s)
	}
	return s
}

// norm delegates to the shared rule package so equality semantics cannot
// diverge between executors.
func norm(v any) any { return rules.Norm(v) }

// sortedKeys returns map keys in lexical order for deterministic walks.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedMapKeys returns a reflect map's keys ordered by their printed form
// so issue order is stable across runs.
func sortedMapKeys(m reflect.Value) []reflect.Value {
	keys := m.MapKeys()
	sort.Slice(keys, func(i, j int) bool {
		return fmt.Sprint(keys[i].Interface()) < fmt.Sprint(keys[j].Interface())
	})
	return keys
}
