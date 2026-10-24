package ir

// Kind discriminates the [TypeNode] union on the wire: every serialized node
// carries {"kind": "<Kind>"} and the decoder dispatches on it.
//
// Closed set. Adding a kind is additive API; renaming one breaks every stored
// IR document and every consumer, so the strings are frozen.
type Kind string

const (
	KindObject   Kind = "object"
	KindString   Kind = "string"
	KindInt      Kind = "int"
	KindFloat    Kind = "float"
	KindBool     Kind = "bool"
	KindBytes    Kind = "bytes"
	KindTime     Kind = "time"
	KindDuration Kind = "duration"
	KindArray    Kind = "array"
	KindMap      Kind = "map"
	KindUnion    Kind = "union"
	KindOneOf    Kind = "oneof"
	KindRef      Kind = "ref"
	KindAny      Kind = "any"
)

// TypeNode is the sealed union of every type in the IR (docsi/IR.md §2).
//
// Sealed on purpose (resolves IR open question Q6): the JSON decoder must
// know every concrete shape to dispatch on "kind", and the back-end emitters
// must exhaustively handle every node. A third-party node would be invisible
// to both — so the interface has an unexported method, and the set of
// implementations lives entirely in this file.
type TypeNode interface {
	// Kind returns the wire discriminator.
	Kind() Kind

	// base exposes the shared attributes for [Check] and the emitters
	// without a type switch; also what seals the interface.
	base() *Base
}

// ExtraPolicy says what an object does with keys that no field declares.
//
// Closed set; maps to JSON Schema additionalProperties (and to
// strict/passthrough object modes in TypeScript schema libraries). The zero value is treated as [ExtraIgnore] — the
// safe default that neither leaks unknown input into the value nor rejects
// a payload for a harmless extra key.
type ExtraPolicy string

const (
	// ExtraIgnore drops unknown keys silently. The default.
	ExtraIgnore ExtraPolicy = "ignore"

	// ExtraForbid fails validation on any unknown key.
	ExtraForbid ExtraPolicy = "forbid"

	// ExtraAllow keeps unknown keys on the value.
	ExtraAllow ExtraPolicy = "allow"
)

// extraPolicies is the closed set [Check] accepts (empty means the default).
var extraPolicies = map[ExtraPolicy]struct{}{
	ExtraIgnore: {},
	ExtraForbid: {},
	ExtraAllow:  {},
}

// ObjectNode is a struct-shaped value: named fields plus object-level
// conditional logic. Cross-field rules and discriminated variants live in
// Gated, not on a single field, because they read more than one field.
type ObjectNode struct {
	Base

	// Rules apply to the object as a whole rather than to any one field:
	// "this booking is internally consistent", "this combination is not
	// already taken". Without them a check that reads several fields at
	// once has nowhere to live — a gate can only decide whether other
	// rules apply, it cannot report a failure of its own.
	//
	// In practice these are custom and async rules, because those are the
	// kinds whose subject is a whole value. Nothing forbids others; the IR
	// does not police which kinds suit which node, here or anywhere else.
	Rules []Rule `json:"rules,omitempty"`

	Fields []FieldNode  `json:"fields"`
	Extra  ExtraPolicy  `json:"extra,omitempty"`
	Gated  []GatedRules `json:"gated,omitempty"`
}

// FieldNode is one named slot in an [ObjectNode].
//
// Resolves IR open question Q1: rules live on the field's TYPE node (so
// array.elem.string.rules works uniformly at any depth) and presence is the
// only thing the field itself records. "required" is simply Optional=false.
type FieldNode struct {
	// Name is the Go field name — what the builder and generated accessors
	// refer to.
	Name string `json:"name"`

	// JSONName is the wire name from the json tag; empty means Name.
	JSONName string `json:"json,omitempty"`

	Type TypeNode `json:"type"`

	// Optional means the key may be ABSENT. Distinct from the type being
	// nullable (see [Base]).
	Optional bool `json:"optional,omitempty"`

	// Transforms run around validation: pre-phase coercion/sanitization,
	// post-phase normalization (docsi/IR.md §7).
	Transforms []Transform `json:"transforms,omitempty"`

	Meta NodeMeta `json:"meta,omitzero"`
}

// StringNode validates a string value.
type StringNode struct {
	Base
	Rules []Rule `json:"rules,omitempty"`
}

// IntNode validates an integer. Bits and Unsigned are range hints for the
// emitters (0 bits means the platform int).
type IntNode struct {
	Base
	Bits     int    `json:"bits,omitempty"`
	Unsigned bool   `json:"unsigned,omitempty"`
	Rules    []Rule `json:"rules,omitempty"`
}

// FloatNode validates a floating-point number. Bits is 32 or 64 (0 means 64).
type FloatNode struct {
	Base
	Bits  int    `json:"bits,omitempty"`
	Rules []Rule `json:"rules,omitempty"`
}

// BoolNode validates a boolean.
type BoolNode struct {
	Base
	Rules []Rule `json:"rules,omitempty"`
}

// BytesNode validates a byte slice; on the wire it is a base64 string.
type BytesNode struct {
	Base
	Rules []Rule `json:"rules,omitempty"`
}

// TimeNode validates a time.Time; on the wire it is an RFC 3339 string.
type TimeNode struct {
	Base
	Rules []Rule `json:"rules,omitempty"`
}

// DurationNode validates a time.Duration. The wire encoding (ISO-8601 string
// vs nanosecond integer) is open question Q3 in docsi/IR.md and is decided
// by the emitters, not here.
type DurationNode struct {
	Base
	Rules []Rule `json:"rules,omitempty"`
}

// ArrayNode validates a slice or array: per-element schema plus
// collection-level rules (minItems, maxItems, unique). Elem is the "dive".
type ArrayNode struct {
	Base
	Elem  TypeNode `json:"elem"`
	Rules []Rule   `json:"rules,omitempty"`
}

// MapNode validates a map with independent key and value schemas.
type MapNode struct {
	Base
	Key   TypeNode `json:"key"`
	Val   TypeNode `json:"val"`
	Rules []Rule   `json:"rules,omitempty"`
}

// UnionNode is an undiscriminated union: the value matches one of N shapes.
// Exports to anyOf, whose errors are poor ("matched 0 of 3") — prefer
// [OneOfNode] whenever a discriminator field exists (ADR-VAL-007).
type UnionNode struct {
	Base
	Variants []TypeNode `json:"variants"`
}

// OneOfNode is a discriminated union: the named field's value selects the
// variant. Always emitted as discriminator-keyed if/then/else rather than a
// raw oneOf so an error points at the right branch.
type OneOfNode struct {
	Base
	Discriminator string              `json:"discriminator"`
	Cases         map[string]TypeNode `json:"cases"`
	Default       TypeNode            `json:"default,omitempty"`
}

// RefNode points into [Schema.Defs] by name, for recursion and sharing.
type RefNode struct {
	Base
	Ref string `json:"ref"`
}

// AnyNode accepts anything. Exports to the empty schema.
type AnyNode struct {
	Base
}

// Kind implements [TypeNode].
func (*ObjectNode) Kind() Kind { return KindObject }

// Kind implements [TypeNode].
func (*StringNode) Kind() Kind { return KindString }

// Kind implements [TypeNode].
func (*IntNode) Kind() Kind { return KindInt }

// Kind implements [TypeNode].
func (*FloatNode) Kind() Kind { return KindFloat }

// Kind implements [TypeNode].
func (*BoolNode) Kind() Kind { return KindBool }

// Kind implements [TypeNode].
func (*BytesNode) Kind() Kind { return KindBytes }

// Kind implements [TypeNode].
func (*TimeNode) Kind() Kind { return KindTime }

// Kind implements [TypeNode].
func (*DurationNode) Kind() Kind { return KindDuration }

// Kind implements [TypeNode].
func (*ArrayNode) Kind() Kind { return KindArray }

// Kind implements [TypeNode].
func (*MapNode) Kind() Kind { return KindMap }

// Kind implements [TypeNode].
func (*UnionNode) Kind() Kind { return KindUnion }

// Kind implements [TypeNode].
func (*OneOfNode) Kind() Kind { return KindOneOf }

// Kind implements [TypeNode].
func (*RefNode) Kind() Kind { return KindRef }

// Kind implements [TypeNode].
func (*AnyNode) Kind() Kind { return KindAny }

func (n *ObjectNode) base() *Base   { return &n.Base }
func (n *StringNode) base() *Base   { return &n.Base }
func (n *IntNode) base() *Base      { return &n.Base }
func (n *FloatNode) base() *Base    { return &n.Base }
func (n *BoolNode) base() *Base     { return &n.Base }
func (n *BytesNode) base() *Base    { return &n.Base }
func (n *TimeNode) base() *Base     { return &n.Base }
func (n *DurationNode) base() *Base { return &n.Base }
func (n *ArrayNode) base() *Base    { return &n.Base }
func (n *MapNode) base() *Base      { return &n.Base }
func (n *UnionNode) base() *Base    { return &n.Base }
func (n *OneOfNode) base() *Base    { return &n.Base }
func (n *RefNode) base() *Base      { return &n.Base }
func (n *AnyNode) base() *Base      { return &n.Base }

// newNode returns an empty node of the given kind for the decoder to fill,
// or false for an unknown kind. This is the single registry of concrete
// types — the decoder and [Check] both key off it, so an unknown kind fails
// in exactly one place.
func newNode(k Kind) (TypeNode, bool) {
	switch k {
	case KindObject:
		return &ObjectNode{}, true
	case KindString:
		return &StringNode{}, true
	case KindInt:
		return &IntNode{}, true
	case KindFloat:
		return &FloatNode{}, true
	case KindBool:
		return &BoolNode{}, true
	case KindBytes:
		return &BytesNode{}, true
	case KindTime:
		return &TimeNode{}, true
	case KindDuration:
		return &DurationNode{}, true
	case KindArray:
		return &ArrayNode{}, true
	case KindMap:
		return &MapNode{}, true
	case KindUnion:
		return &UnionNode{}, true
	case KindOneOf:
		return &OneOfNode{}, true
	case KindRef:
		return &RefNode{}, true
	case KindAny:
		return &AnyNode{}, true
	}
	return nil, false
}

// Rules returns the rule list attached to a node, or nil for node kinds that
// carry none (object, union, oneof, ref, any). Lets [Check] and the emitters
// walk rules uniformly without a fourteen-arm type switch at every call site.
func Rules(n TypeNode) []Rule {
	switch t := n.(type) {
	case *ObjectNode:
		return t.Rules
	case *StringNode:
		return t.Rules
	case *IntNode:
		return t.Rules
	case *FloatNode:
		return t.Rules
	case *BoolNode:
		return t.Rules
	case *BytesNode:
		return t.Rules
	case *TimeNode:
		return t.Rules
	case *DurationNode:
		return t.Rules
	case *ArrayNode:
		return t.Rules
	case *MapNode:
		return t.Rules
	}
	return nil
}

// Children returns the type nodes directly nested in n, in a stable order,
// so walkers (Check, ref resolution, emitters) share one traversal.
func Children(n TypeNode) []TypeNode {
	switch t := n.(type) {
	case *ObjectNode:
		out := make([]TypeNode, 0, len(t.Fields))
		for i := range t.Fields {
			out = append(out, t.Fields[i].Type)
		}
		return out
	case *ArrayNode:
		return []TypeNode{t.Elem}
	case *MapNode:
		return []TypeNode{t.Key, t.Val}
	case *UnionNode:
		return t.Variants
	case *OneOfNode:
		out := make([]TypeNode, 0, len(t.Cases)+1)
		for _, key := range sortedKeys(t.Cases) {
			out = append(out, t.Cases[key])
		}
		if t.Default != nil {
			out = append(out, t.Default)
		}
		return out
	}
	return nil
}
