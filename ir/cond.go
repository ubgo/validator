package ir

// CondKind discriminates the condition expression sub-IR (ADR-VAL-007).
//
// Conditions are data, not closures, for the same reason rules are: a
// condition that is a Go predicate cannot cross to the browser. The one
// escape hatch, [CondCustom], is explicitly non-portable.
type CondKind string

const (
	// CondCmp compares a field against a literal: field op value.
	CondCmp CondKind = "cmp"

	// CondFieldCmp compares two fields: field op fieldB.
	CondFieldCmp CondKind = "fieldcmp"

	// CondPresence tests whether a field is present or absent.
	CondPresence CondKind = "presence"

	// CondIn tests membership: field in values.
	CondIn CondKind = "in"

	// CondAnd, CondOr, CondNot compose sub-conditions.
	CondAnd CondKind = "and"
	CondOr  CondKind = "or"
	CondNot CondKind = "not"

	// CondExpr is a CEL expression — the portable escape hatch that runs on
	// both sides with the same engine.
	CondExpr CondKind = "expr"

	// CondCustom references a registered Go predicate. Opaque and
	// non-portable by definition.
	CondCustom CondKind = "custom"
)

// condKinds is the closed set [Check] accepts.
var condKinds = map[CondKind]struct{}{
	CondCmp: {}, CondFieldCmp: {}, CondPresence: {}, CondIn: {},
	CondAnd: {}, CondOr: {}, CondNot: {}, CondExpr: {}, CondCustom: {},
}

// CmpOp is a comparison operator for [CondCmp] and [CondFieldCmp].
type CmpOp string

const (
	OpEq  CmpOp = "eq"
	OpNe  CmpOp = "ne"
	OpGt  CmpOp = "gt"
	OpGte CmpOp = "gte"
	OpLt  CmpOp = "lt"
	OpLte CmpOp = "lte"
)

// cmpOps is the closed set [Check] accepts.
var cmpOps = map[CmpOp]struct{}{
	OpEq: {}, OpNe: {}, OpGt: {}, OpGte: {}, OpLt: {}, OpLte: {},
}

// PresenceOp says which way a [CondPresence] test points.
type PresenceOp string

const (
	Present PresenceOp = "present"
	Absent  PresenceOp = "absent"
)

// presenceOps is the closed set [Check] accepts.
var presenceOps = map[PresenceOp]struct{}{Present: {}, Absent: {}}

// Condition is one node of the expression sub-IR. A single struct with a
// kind discriminator (rather than a sealed interface like [TypeNode])
// because every variant is small and flat, and a fat struct keeps the JSON
// decoder trivial.
type Condition struct {
	Kind CondKind `json:"kind"`

	// Field is the subject for cmp, fieldcmp, presence, in.
	Field string `json:"field,omitempty"`

	// Op is the comparison for cmp/fieldcmp, or the presence direction for
	// presence (see [PresenceOp]).
	Op string `json:"op,omitempty"`

	// Value is the literal for cmp.
	Value any `json:"value,omitempty"`

	// FieldB is the right-hand field for fieldcmp.
	FieldB string `json:"fieldB,omitempty"`

	// Values is the set for in.
	Values []any `json:"values,omitempty"`

	// Sub holds operands for and/or (any count) and not (exactly one).
	Sub []Condition `json:"sub,omitempty"`

	// CEL is the expression source for expr.
	CEL string `json:"cel,omitempty"`

	// Ref names the registered predicate for custom.
	Ref string `json:"ref,omitempty"`
}

// Portability returns the class inherent to the condition: custom is opaque,
// everything else runs on both sides.
func (c Condition) Portability() Portability {
	if c.Kind == CondCustom {
		return Opaque
	}
	return Portable
}

// Condition constructors — the sanctioned way to build the sub-IR so the
// kind/op strings never appear at call sites.

// Cmp builds field op value.
func Cmp(field string, op CmpOp, value any) Condition {
	return Condition{Kind: CondCmp, Field: field, Op: string(op), Value: value}
}

// FieldCmp builds fieldA op fieldB.
func FieldCmp(fieldA string, op CmpOp, fieldB string) Condition {
	return Condition{Kind: CondFieldCmp, Field: fieldA, Op: string(op), FieldB: fieldB}
}

// HasField builds a presence test.
func HasField(field string, op PresenceOp) Condition {
	return Condition{Kind: CondPresence, Field: field, Op: string(op)}
}

// In builds field ∈ values.
func In(field string, values ...any) Condition {
	return Condition{Kind: CondIn, Field: field, Values: values}
}

// And composes conditions conjunctively.
func And(sub ...Condition) Condition { return Condition{Kind: CondAnd, Sub: sub} }

// Or composes conditions disjunctively.
func Or(sub ...Condition) Condition { return Condition{Kind: CondOr, Sub: sub} }

// Not negates one condition.
func Not(sub Condition) Condition { return Condition{Kind: CondNot, Sub: []Condition{sub}} }

// Expr wraps a CEL expression.
func Expr(cel string) Condition { return Condition{Kind: CondExpr, CEL: cel} }

// CustomCond references a registered Go predicate (non-portable).
func CustomCond(ref string) Condition { return Condition{Kind: CondCustom, Ref: ref} }

// GateKind discriminates object-level gated logic — the sugar that lowers to
// a [Condition] plus rules so back-ends only implement the primitives.
type GateKind string

const (
	// GateWhen applies Then rules if Cond holds, else the Else rules.
	GateWhen GateKind = "when"

	// GateRequires makes Fields required when Cond holds.
	GateRequires GateKind = "requires"

	// GateGroup constrains how many of Fields may be present (see Mode).
	GateGroup GateKind = "group"
)

// gateKinds is the closed set [Check] accepts.
var gateKinds = map[GateKind]struct{}{GateWhen: {}, GateRequires: {}, GateGroup: {}}

// GroupMode says how a [GateGroup] counts presence across its fields.
type GroupMode string

const (
	ExactlyOne GroupMode = "exactly-one"
	AtLeastOne GroupMode = "at-least-one"
	AllOrNone  GroupMode = "all-or-none"
)

// groupModes is the closed set [Check] accepts.
var groupModes = map[GroupMode]struct{}{ExactlyOne: {}, AtLeastOne: {}, AllOrNone: {}}

// GatedRules is one object-level conditional (docsi/IR.md §5).
type GatedRules struct {
	Kind GateKind `json:"kind"`

	// Cond drives when and requires; unused by group.
	Cond *Condition `json:"cond,omitempty"`

	// Then and Else are the rule sets for when.
	Then []TargetRule `json:"then,omitempty"`
	Else []TargetRule `json:"else,omitempty"`

	// Fields lists the subject fields for requires and group.
	Fields []string `json:"fields,omitempty"`

	// Mode is the counting mode for group.
	Mode GroupMode `json:"mode,omitempty"`
}

// TargetRule binds a rule to a named field of the enclosing object — used
// inside [GatedRules] because the rule lives at object level, not on the
// field's own type node.
type TargetRule struct {
	Field string `json:"field"`
	Rule  Rule   `json:"rule"`
}

// When builds a when-gate.
func When(cond Condition, then []TargetRule, els []TargetRule) GatedRules {
	c := cond
	return GatedRules{Kind: GateWhen, Cond: &c, Then: then, Else: els}
}

// Requires builds a requires-gate.
func Requires(cond Condition, fields ...string) GatedRules {
	c := cond
	return GatedRules{Kind: GateRequires, Cond: &c, Fields: fields}
}

// Group builds a group-gate.
func Group(mode GroupMode, fields ...string) GatedRules {
	return GatedRules{Kind: GateGroup, Mode: mode, Fields: fields}
}

// On binds a rule to a field for use in [When].
func On(field string, rule Rule) TargetRule { return TargetRule{Field: field, Rule: rule} }
