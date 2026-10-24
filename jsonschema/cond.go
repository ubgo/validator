package jsonschema

import (
	"github.com/ubgo/validator/ir"
)

// Conditions and gates. JSON Schema can express "if this property has
// that value/is present, then these constraints" through if/then/else and
// property-level required/const/enum; it cannot compare two properties or
// run code. Everything expressible is emitted as an allOf clause on the
// object; everything else lands in the manifest with the class that says
// how the TypeScript side should handle it.

// cond translates a condition into a schema that matches when the
// condition holds. ok is false when the condition needs CEL or a custom
// predicate; the caller records the manifest entry.
func (e *emitter) cond(o *ir.ObjectNode, k ir.Condition) (schema *Object, ok bool) {
	switch k.Kind {
	case ir.CondCmp:
		wire := wireOf(o, k.Field)
		var prop *Object
		switch ir.CmpOp(k.Op) {
		case ir.OpEq:
			prop = NewObject().Set(kwConst, k.Value)
		case ir.OpNe:
			prop = NewObject().Set(kwNot, NewObject().Set(kwConst, k.Value))
		case ir.OpGt:
			prop = NewObject().Set(kwExclusiveMinimum, k.Value)
		case ir.OpGte:
			prop = NewObject().Set(kwMinimum, k.Value)
		case ir.OpLt:
			prop = NewObject().Set(kwExclusiveMaximum, k.Value)
		case ir.OpLte:
			prop = NewObject().Set(kwMaximum, k.Value)
		}
		if prop == nil {
			return nil, false
		}
		if ir.CmpOp(k.Op) != ir.OpEq && ir.CmpOp(k.Op) != ir.OpNe && !isNumber(k.Value) {
			// Ordered comparison against a non-number (string ordering) has
			// no keyword.
			return nil, false
		}
		return propertyIs(wire, prop), true
	case ir.CondPresence:
		wire := wireOf(o, k.Field)
		req := NewObject().Set(kwRequired, []any{wire})
		if ir.PresenceOp(k.Op) == ir.Absent {
			return NewObject().Set(kwNot, req), true
		}
		return req, true
	case ir.CondIn:
		return propertyIs(wireOf(o, k.Field), NewObject().Set(kwEnum, k.Values)), true
	case ir.CondAnd, ir.CondOr:
		subs := make([]any, 0, len(k.Sub))
		for _, sub := range k.Sub {
			s, ok := e.cond(o, sub)
			if !ok {
				return nil, false
			}
			subs = append(subs, s)
		}
		key := kwAllOf
		if k.Kind == ir.CondOr {
			key = kwAnyOf
		}
		return NewObject().Set(key, subs), true
	case ir.CondNot:
		s, ok := e.cond(o, k.Sub[0])
		if !ok {
			return nil, false
		}
		return NewObject().Set(kwNot, s), true
	}
	// fieldcmp, expr, custom.
	return nil, false
}

// condClass says why a condition could not be emitted.
func condClass(k ir.Condition) (Class, string) {
	switch k.Kind {
	case ir.CondCustom:
		return ClassOpaque, "custom Go predicate runs server-side only"
	case ir.CondExpr:
		return ClassCEL, "CEL expression; portable through the CEL layer only"
	case ir.CondFieldCmp:
		return ClassCEL, "comparing two properties needs CEL"
	case ir.CondCmp:
		return ClassCEL, "ordered comparison against a non-numeric literal needs CEL"
	}
	// Composite: the class of the first inexpressible member.
	for _, sub := range k.Sub {
		if c, reason := condClass(sub); c != "" {
			return c, reason
		}
	}
	return "", ""
}

// propertyIs builds {"properties":{wire: prop},"required":[wire]} — the
// idiom for "this property exists and satisfies prop".
func propertyIs(wire string, prop *Object) *Object {
	return NewObject().
		Set(kwProperties, NewObject().Set(wire, prop)).
		Set(kwRequired, []any{wire})
}

// isNumber reports a numeric literal of any Go or JSON-decoded type.
func isNumber(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	}
	return false
}

// liftedRule turns a field-level conditional rule into an object-level
// if/then clause. Returns nil (after recording the manifest entry) when
// the condition is inexpressible.
func (e *emitter) liftedRule(path string, o *ir.ObjectNode, fieldPath, wire string, r ir.Rule) *Object {
	condSchema, ok := e.cond(o, *r.Cond)
	if !ok {
		class, reason := condClass(*r.Cond)
		entry := Entry{Path: fieldPath, Kind: KindCondition, Name: string(r.Cond.Kind), Class: class, Ref: r.Cond.Ref, Reason: reason}
		// A condition built only from comparisons is executable by a
		// runtime even though the schema cannot state it; one that reaches
		// into Go or CEL is not.
		if executableCond(*r.Cond) {
			c := wireCond(o, *r.Cond)
			entry.Cond = &c
			prop := NewObject()
			e.rule(fieldPath, prop, r)
			if prop.Len() > 0 {
				entry.Then = objectFragment(NewObject().Set(kwProperties, NewObject().Set(wire, withType(prop, fieldTypeByWire(o, wire)))))
			}
		}
		e.manifest.Add(entry)
		return nil
	}
	prop := NewObject()
	e.rule(fieldPath, prop, r)
	if prop.Len() == 0 {
		// The rule itself was non-portable and is already in the manifest.
		return nil
	}
	_ = path
	return NewObject().Set(kwIf, condSchema).Set(kwThen, NewObject().Set(kwProperties, NewObject().Set(wire, withType(prop, fieldTypeByWire(o, wire)))))
}

// gate emits one object-level gate as an allOf clause, or records it.
func (e *emitter) gate(path string, o *ir.ObjectNode, g ir.GatedRules) *Object {
	switch g.Kind {
	case ir.GateWhen:
		condSchema, ok := e.cond(o, *g.Cond)
		if !ok {
			class, reason := condClass(*g.Cond)
			entry := Entry{Path: path, Kind: KindGate, Name: string(g.Kind), Class: class, Ref: g.Cond.Ref, Reason: reason}
			if executableCond(*g.Cond) {
				c := wireCond(o, *g.Cond)
				entry.Cond, entry.Then, entry.Else = &c, e.targetsSchema(path, o, g.Then), e.targetsSchema(path, o, g.Else)
			}
			e.manifest.Add(entry)
			return nil
		}
		clause := NewObject().Set(kwIf, condSchema)
		if then := e.targets(path, o, g.Then); then != nil {
			clause.Set(kwThen, then)
		}
		if els := e.targets(path, o, g.Else); els != nil {
			clause.Set(kwElse, els)
		}
		if clause.Len() == 1 {
			return nil
		}
		return clause
	case ir.GateRequires:
		condSchema, ok := e.cond(o, *g.Cond)
		if !ok {
			class, reason := condClass(*g.Cond)
			entry := Entry{Path: path, Kind: KindGate, Name: string(g.Kind), Class: class, Ref: g.Cond.Ref, Reason: reason}
			if executableCond(*g.Cond) {
				c := wireCond(o, *g.Cond)
				entry.Cond = &c
				entry.Then = objectFragment(NewObject().Set(kwRequired, wires(o, g.Fields)))
			}
			e.manifest.Add(entry)
			return nil
		}
		return NewObject().Set(kwIf, condSchema).Set(kwThen, NewObject().Set(kwRequired, wires(o, g.Fields)))
	case ir.GateGroup:
		fields := wires(o, g.Fields)
		each := make([]any, 0, len(fields))
		for _, f := range fields {
			each = append(each, NewObject().Set(kwRequired, []any{f}))
		}
		switch g.Mode {
		case ir.ExactlyOne:
			// oneOf over single-required clauses: exactly one may match, so
			// two present properties fail as intended.
			return NewObject().Set(kwOneOf, each)
		case ir.AtLeastOne:
			return NewObject().Set(kwAnyOf, each)
		case ir.AllOrNone:
			return NewObject().Set(kwOneOf, []any{
				NewObject().Set(kwRequired, fields),
				NewObject().Set(kwNot, NewObject().Set(kwAnyOf, each)),
			})
		}
	}
	return nil
}

// targets emits a set of gated target rules as {"properties":{…}}.
func (e *emitter) targets(path string, o *ir.ObjectNode, ts []ir.TargetRule) *Object {
	if len(ts) == 0 {
		return nil
	}
	props := NewObject()
	for _, t := range ts {
		wire := wireOf(o, t.Field)
		prop, _ := props.Get(wire)
		po, _ := prop.(*Object)
		if po == nil {
			po = NewObject()
		}
		before := po.Len()
		e.rule(path+"."+wire, po, t.Rule)
		if po.Len() > before || po.Len() > 0 {
			props.Set(wire, withType(po, fieldType(o, t.Field)))
		}
	}
	if props.Len() == 0 {
		return nil
	}
	return NewObject().Set(kwProperties, props)
}

// withType puts "type" first on a property schema when the field has a
// single JSON type, leaving typeless schemas (refs, unions) untouched.
func withType(prop *Object, ty string) *Object {
	if ty == "" || prop.Len() == 0 {
		return prop
	}
	if _, has := prop.Get(kwType); has {
		return prop
	}
	return NewObject().Set(kwType, ty).Merge(prop)
}

// fieldTypeByWire is fieldType keyed by wire name.
func fieldTypeByWire(o *ir.ObjectNode, wire string) string {
	for i := range o.Fields {
		if wireName(&o.Fields[i]) == wire {
			return typeKeyword(o.Fields[i].Type)
		}
	}
	return ""
}

// wires maps Go field names to wire names.
func wires(o *ir.ObjectNode, goNames []string) []any {
	out := make([]any, 0, len(goNames))
	for _, n := range goNames {
		out = append(out, wireOf(o, n))
	}
	return out
}
