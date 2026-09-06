package interp

import (
	"reflect"

	"github.com/ubgo/validator/internal/rules"
	"github.com/ubgo/validator/ir"
)

// cond evaluates a condition against the enclosing object scope. Every
// arm is data-driven; the only function call is the custom predicate,
// resolved at New. A condition with no scope (a rule on a non-object root)
// evaluates false for field tests, so gated rules never fire spuriously.
func (r *run) cond(sc *scope, k ir.Condition) bool {
	switch k.Kind {
	case ir.CondCmp:
		v, ok := sc.field(k.Field)
		if !ok {
			return false
		}
		return compareAny(v, reflect.ValueOf(k.Value), ir.CmpOp(k.Op))
	case ir.CondFieldCmp:
		a, okA := sc.field(k.Field)
		b, okB := sc.field(k.FieldB)
		if !okA || !okB {
			return false
		}
		return compareAny(a, b, ir.CmpOp(k.Op))
	case ir.CondPresence:
		_, ok := sc.field(k.Field)
		if ir.PresenceOp(k.Op) == ir.Absent {
			return !ok
		}
		return ok
	case ir.CondIn:
		v, ok := sc.field(k.Field)
		if !ok {
			return false
		}
		want := norm(v.Interface())
		for _, candidate := range k.Values {
			if norm(candidate) == want {
				return true
			}
		}
		return false
	case ir.CondAnd:
		for _, sub := range k.Sub {
			if !r.cond(sc, sub) {
				return false
			}
		}
		return true
	case ir.CondOr:
		for _, sub := range k.Sub {
			if r.cond(sc, sub) {
				return true
			}
		}
		return false
	case ir.CondNot:
		return !r.cond(sc, k.Sub[0])
	case ir.CondCustom:
		if sc == nil {
			return false
		}
		return r.v.funcs[k.Ref](r.ctx, sc.val.Interface()) == nil
	}
	// CondExpr is rejected at New; unknown kinds by ir.Check.
	return false
}

// compareAny delegates to the shared rule package so conditions decide
// identically in every executor.
func compareAny(a, b reflect.Value, op ir.CmpOp) bool { return rules.Compare(a, b, op) }

// orderOp is kept for the interpreter's tests; the semantics live in rules.
func orderOp(op ir.CmpOp, eq, lt, gt bool) bool { return rules.OrderOp(op, eq, lt, gt) }
