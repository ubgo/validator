package vb

import "github.com/ubgo/validator/ir"

// Rename-safe condition and gate sugar. The [ir] constructors take field
// names as strings; these take [FieldRef]s so a renamed struct field is a
// compile error at the schema, not a silent no-match at runtime. Every
// helper lowers to the same [ir.Condition] / [ir.GatedRules] data — nothing
// here stores a closure.

// FieldName is anything that names a struct field; every [FieldRef]
// satisfies it, which lets variadic helpers mix refs of different value
// types.
type FieldName interface{ Name() string }

// Cmp builds field op value.
func Cmp[T any, V any](ref FieldRef[T, V], op ir.CmpOp, value V) ir.Condition {
	return ir.Cmp(ref.name, op, value)
}

// Eq is Cmp with equality — the common discriminator test.
func Eq[T any, V any](ref FieldRef[T, V], value V) ir.Condition {
	return ir.Cmp(ref.name, ir.OpEq, value)
}

// FieldCmp builds a op b between two fields of the same type.
func FieldCmp[T any, V any](a FieldRef[T, V], op ir.CmpOp, b FieldRef[T, V]) ir.Condition {
	return ir.FieldCmp(a.name, op, b.name)
}

// Has tests that the field is present.
func Has(ref FieldName) ir.Condition { return ir.HasField(ref.Name(), ir.Present) }

// Missing tests that the field is absent.
func Missing(ref FieldName) ir.Condition { return ir.HasField(ref.Name(), ir.Absent) }

// In tests membership.
func In[T any, V any](ref FieldRef[T, V], values ...V) ir.Condition {
	anys := make([]any, len(values))
	for i, v := range values {
		anys[i] = v
	}
	return ir.In(ref.name, anys...)
}

// On binds a rule to a field for use in [When].
func On(ref FieldName, rule ir.Rule) ir.TargetRule { return ir.On(ref.Name(), rule) }

// When applies then-rules when cond holds, else-rules otherwise.
func When(cond ir.Condition, then []ir.TargetRule, els []ir.TargetRule) ir.GatedRules {
	return ir.When(cond, then, els)
}

// Requires makes fields required when cond holds.
func Requires(cond ir.Condition, fields ...FieldName) ir.GatedRules {
	return ir.Requires(cond, names(fields)...)
}

// Group constrains how many of fields may be present.
func Group(mode ir.GroupMode, fields ...FieldName) ir.GatedRules {
	return ir.Group(mode, names(fields)...)
}

func names(fields []FieldName) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = f.Name()
	}
	return out
}
