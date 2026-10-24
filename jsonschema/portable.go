package jsonschema

import "github.com/ubgo/validator/ir"

// The portable tail: rules JSON Schema cannot express, described well
// enough that a runtime can still execute them.
//
// JSON Schema has no way to compare two properties, so every cross-field
// rule (after, before, fieldcmp) and every gate keyed on one used to reach
// the browser as nothing but a warning, leaving the check to the server.
// That is a real hole: the frontend cannot tell the user "end must be
// after start" without a round trip.
//
// The conditions are already data, so the fix is to carry them. An entry
// whose condition could not become schema now also carries that condition
// with wire names, plus the JSON Schema fragment its branches would have
// produced. A runtime evaluates the condition itself (a comparison, no
// engine required) and hands the fragment to its ordinary validator.
// Nothing here is a closure, so the founding rule still holds.

// executableCond reports whether a runtime can evaluate this condition
// from data alone. Comparisons and their combinations can be; a Go
// predicate cannot, and CEL cannot without the CEL module, so those stay
// server-only and their entries carry no condition.
func executableCond(k ir.Condition) bool {
	switch k.Kind {
	case ir.CondCmp, ir.CondFieldCmp, ir.CondPresence, ir.CondIn:
		return true
	case ir.CondAnd, ir.CondOr, ir.CondNot:
		for _, sub := range k.Sub {
			if !executableCond(sub) {
				return false
			}
		}
		return len(k.Sub) > 0
	}
	return false // expr, custom
}

// lastSeg is the final property name of a JSONPath, the wire name of the
// field a rule sits on.
func lastSeg(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return path[i+1:]
		}
	}
	return path
}

// wireCond rewrites a condition's Go field names to wire names, so a
// consumer that only ever sees JSON can resolve them. The tree is copied;
// the schema's own condition is never modified.
func wireCond(o *ir.ObjectNode, k ir.Condition) ir.Condition {
	out := k
	if k.Field != "" {
		out.Field = wireOf(o, k.Field)
	}
	if k.FieldB != "" {
		out.FieldB = wireOf(o, k.FieldB)
	}
	if len(k.Sub) > 0 {
		out.Sub = make([]ir.Condition, len(k.Sub))
		for i, sub := range k.Sub {
			out.Sub[i] = wireCond(o, sub)
		}
	}
	return out
}

// targetsSchema renders gated rules as a JSON Schema fragment: the same
// {properties, required} shape an expressible gate would have emitted, so
// a runtime validates it with the validator it already has. Returns nil
// when nothing in the branch could be expressed, which leaves the entry
// carrying only its condition.
func (e *emitter) targetsSchema(path string, o *ir.ObjectNode, targets []ir.TargetRule) *Object {
	if len(targets) == 0 {
		return nil
	}
	frag := e.targets(path, o, targets)
	if frag == nil {
		return nil
	}
	// A fragment is compiled as a schema in its own right, and Ajv runs
	// with strictTypes on, which refuses object keywords without a stated
	// type. The same reason lifted rules carry one.
	return objectFragment(frag)
}

// objectFragment states the type an independently compiled fragment needs.
func objectFragment(frag *Object) *Object {
	out := NewObject().Set(kwType, tyObject)
	return out.Merge(frag)
}

// crossFieldCond expresses an after/before rule as the comparison it is,
// so every cross-field element reaches a runtime in one shape.
func crossFieldCond(o *ir.ObjectNode, selfWire string, r ir.Rule) *ir.Condition {
	other, ok := r.Params[ir.ParamField].(string)
	if !ok || other == "" {
		return nil
	}
	op := ir.OpGt
	if r.Kind == ir.RuleBefore {
		op = ir.OpLt
	}
	c := ir.FieldCmp(selfWire, op, wireOf(o, other))
	return &c
}
