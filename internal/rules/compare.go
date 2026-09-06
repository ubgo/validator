package rules

import (
	"reflect"
	"time"

	"github.com/ubgo/validator/ir"
)

var timeType = reflect.TypeFor[time.Time]()

// Compare orders two values: times chronologically, numbers numerically,
// strings lexically, and anything else by equality only. Mismatched or
// unorderable operands make every ordering false — a condition that cannot
// be decided must not fire. Shared by the interpreter and generated code.
func Compare(a, b reflect.Value, op ir.CmpOp) bool {
	a, nilA := deref(a)
	b, nilB := deref(b)
	if nilA || nilB {
		return op == ir.OpEq && nilA && nilB || op == ir.OpNe && nilA != nilB
	}
	if a.Type() == timeType && b.Type() == timeType {
		ta, tb := a.Interface().(time.Time), b.Interface().(time.Time)
		return OrderOp(op, ta.Equal(tb), ta.Before(tb), ta.After(tb))
	}
	if fa, ok := toFloat(a); ok {
		fb, ok := toFloat(b)
		if !ok {
			return false
		}
		return OrderOp(op, fa == fb, fa < fb, fa > fb)
	}
	if a.Kind() == reflect.String && b.Kind() == reflect.String {
		sa, sb := a.String(), b.String()
		return OrderOp(op, sa == sb, sa < sb, sa > sb)
	}
	eq := Norm(a.Interface()) == Norm(b.Interface())
	switch op {
	case ir.OpEq:
		return eq
	case ir.OpNe:
		return !eq
	}
	return false
}

// OrderOp maps a comparison operator onto precomputed eq/lt/gt facts.
func OrderOp(op ir.CmpOp, eq, lt, gt bool) bool {
	switch op {
	case ir.OpEq:
		return eq
	case ir.OpNe:
		return !eq
	case ir.OpLt:
		return lt
	case ir.OpLte:
		return lt || eq
	case ir.OpGt:
		return gt
	case ir.OpGte:
		return gt || eq
	}
	return false
}

// deref unwraps interfaces and pointers, reporting a nil at any level.
func deref(v reflect.Value) (reflect.Value, bool) {
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

// toFloat converts any numeric value to float64 for comparison.
func toFloat(v reflect.Value) (float64, bool) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint()), true
	case reflect.Float32, reflect.Float64:
		return v.Float(), true
	}
	return 0, false
}
