package vb

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"

	"github.com/ubgo/validator/ir"
)

// Composite type builders: inline objects, arrays, maps, pointers, refs to
// named schemas, unions, and the escape hatch Any.

// ---------------------------------------------------------------- object

// ObjectType is an inline object for Go struct S, built by [Object].
type ObjectType[S any] struct {
	node *ir.ObjectNode
	reg  *registry
}

// Node implements [Builder].
func (t *ObjectType[S]) Node() ir.TypeNode   { return t.node }
func (t *ObjectType[S]) registry() *registry { return t.reg }
func (t *ObjectType[S]) phantom(S)           {}

// Nullable allows null.
func (t *ObjectType[S]) Nullable() *ObjectType[S] {
	c := *t
	n := *t.node
	n.Nullable = true
	c.node = &n
	return &c
}

// Extra sets the unknown-key policy.
func (t *ObjectType[S]) Extra(p ir.ExtraPolicy) *ObjectType[S] {
	c := *t
	n := *t.node
	n.Extra = p
	c.node = &n
	return &c
}

// Gate appends object-level conditional logic.
func (t *ObjectType[S]) Gate(gates ...ir.GatedRules) *ObjectType[S] {
	c := *t
	n := *t.node
	n.Gated = append(append([]ir.GatedRules{}, t.node.Gated...), gates...)
	c.node = &n
	return &c
}

// ---------------------------------------------------------------- ref

// RefType references a named [Schema] by name. The referenced schema is
// hoisted into Defs of whichever root includes it, so recursion and sharing
// export as JSON Schema $ref.
type RefType[S any] struct {
	node ir.RefNode
	reg  *registry
}

// Ref references a named schema. The schema's own registry (functions and
// transitive defs) travels with the reference.
func Ref[S any](s *Schema[S]) *RefType[S] {
	reg := newRegistry()
	_ = reg.merge(s.reg)
	reg.defs[s.name] = s.root
	return &RefType[S]{node: ir.RefNode{Ref: s.name}, reg: reg}
}

// Node implements [Builder].
func (t *RefType[S]) Node() ir.TypeNode   { n := t.node; return &n }
func (t *RefType[S]) registry() *registry { return t.reg }
func (t *RefType[S]) phantom(S)           {}

// Nullable allows null.
func (t *RefType[S]) Nullable() *RefType[S] { c := *t; c.node.Nullable = true; return &c }

// ---------------------------------------------------------------- ptr

// PtrType wraps a builder for V so it describes *V: a pointer field is
// nullable at the type level. Optional-ness (key absent) is set on the
// [Field], not here — the two are distinct in the IR.
type PtrType[V any] struct {
	inner Builder[V]
}

// Ptr marks the value nullable and lifts the builder to *V.
func Ptr[V any](b Builder[V]) *PtrType[V] { return &PtrType[V]{inner: b} }

// Node implements [Builder].
func (t *PtrType[V]) Node() ir.TypeNode {
	n := t.inner.Node()
	// Every node kind embeds Base; clone through JSON-free means by kind
	// switch would be verbose, so we mutate a fresh node from the inner
	// builder — inner.Node() returns a fresh value each call by contract.
	setNullable(n)
	return n
}
func (t *PtrType[V]) registry() *registry { return t.inner.registry() }
func (t *PtrType[V]) phantom(*V)          {}

// setNullable flips Nullable on any node kind through the sealed interface.
func setNullable(n ir.TypeNode) {
	switch x := n.(type) {
	case *ir.ObjectNode:
		x.Nullable = true
	case *ir.StringNode:
		x.Nullable = true
	case *ir.IntNode:
		x.Nullable = true
	case *ir.FloatNode:
		x.Nullable = true
	case *ir.BoolNode:
		x.Nullable = true
	case *ir.BytesNode:
		x.Nullable = true
	case *ir.TimeNode:
		x.Nullable = true
	case *ir.DurationNode:
		x.Nullable = true
	case *ir.ArrayNode:
		x.Nullable = true
	case *ir.MapNode:
		x.Nullable = true
	case *ir.UnionNode:
		x.Nullable = true
	case *ir.OneOfNode:
		x.Nullable = true
	case *ir.RefNode:
		x.Nullable = true
	case *ir.AnyNode:
		x.Nullable = true
	}
}

// ---------------------------------------------------------------- any

// AnyType accepts any value of Go type V without constraints.
type AnyType[V any] struct {
	node ir.AnyNode
}

// Any starts an unconstrained builder.
func Any[V any]() *AnyType[V] { return &AnyType[V]{} }

// Node implements [Builder].
func (t *AnyType[V]) Node() ir.TypeNode   { n := t.node; return &n }
func (t *AnyType[V]) registry() *registry { return newRegistry() }
func (t *AnyType[V]) phantom(V)           {}

// Nullable allows null.
func (t *AnyType[V]) Nullable() *AnyType[V] { c := *t; c.node.Nullable = true; return &c }

// ---------------------------------------------------------------- array

// ArrayType builds a []E value with a per-element schema.
type ArrayType[E any] struct {
	elem  Builder[E]
	rules []ir.Rule
	base  ir.Base
	reg   *registry
}

// Array starts a slice builder over elem.
func Array[E any](elem Builder[E]) *ArrayType[E] {
	return &ArrayType[E]{elem: elem, reg: elem.registry()}
}

func (t *ArrayType[E]) rule(r ir.Rule) *ArrayType[E] {
	c := *t
	c.rules = appendRule(t.rules, r)
	return &c
}

// Node implements [Builder].
func (t *ArrayType[E]) Node() ir.TypeNode {
	return &ir.ArrayNode{Base: t.base, Elem: t.elem.Node(), Rules: append([]ir.Rule{}, t.rules...)}
}
func (t *ArrayType[E]) registry() *registry { return t.reg }
func (t *ArrayType[E]) phantom([]E)         {}

// Nullable allows null.
func (t *ArrayType[E]) Nullable() *ArrayType[E] { c := *t; c.base.Nullable = true; return &c }

// Describe sets the JSON Schema description.
func (t *ArrayType[E]) Describe(s string) *ArrayType[E] {
	c := *t
	c.base.Meta.Description = s
	return &c
}

// Msg sets the error code and message of the last rule.
func (t *ArrayType[E]) Msg(code, message string) *ArrayType[E] {
	c := *t
	c.rules = setMsg(t.rules, code, message)
	return &c
}

// MinItems requires len >= n.
func (t *ArrayType[E]) MinItems(n int) *ArrayType[E] { return t.rule(ir.MinItems(n)) }

// MaxItems requires len <= n.
func (t *ArrayType[E]) MaxItems(n int) *ArrayType[E] { return t.rule(ir.MaxItems(n)) }

// Unique requires distinct elements.
func (t *ArrayType[E]) Unique() *ArrayType[E] { return t.rule(ir.Unique()) }

// Custom registers fn over the whole slice.
func (t *ArrayType[E]) Custom(name string, fn func(context.Context, []E) error) *ArrayType[E] {
	c := t.rule(ir.Custom(name))
	c.reg = register(t.reg, name, typed(name, fn))
	return c
}

// ---------------------------------------------------------------- map

// MapType builds a map[K]V value with key and value schemas.
type MapType[K comparable, V any] struct {
	key   Builder[K]
	val   Builder[V]
	rules []ir.Rule
	base  ir.Base
	reg   *registry
}

// Map starts a map builder.
func Map[K comparable, V any](key Builder[K], val Builder[V]) *MapType[K, V] {
	reg := newRegistry()
	_ = reg.merge(key.registry())
	_ = reg.merge(val.registry())
	return &MapType[K, V]{key: key, val: val, reg: reg}
}

func (t *MapType[K, V]) rule(r ir.Rule) *MapType[K, V] {
	c := *t
	c.rules = appendRule(t.rules, r)
	return &c
}

// Node implements [Builder].
func (t *MapType[K, V]) Node() ir.TypeNode {
	return &ir.MapNode{Base: t.base, Key: t.key.Node(), Val: t.val.Node(), Rules: append([]ir.Rule{}, t.rules...)}
}
func (t *MapType[K, V]) registry() *registry { return t.reg }
func (t *MapType[K, V]) phantom(map[K]V)     {}

// Nullable allows null.
func (t *MapType[K, V]) Nullable() *MapType[K, V] { c := *t; c.base.Nullable = true; return &c }

// MinItems requires len >= n entries.
func (t *MapType[K, V]) MinItems(n int) *MapType[K, V] { return t.rule(ir.MinItems(n)) }

// MaxItems requires len <= n entries.
func (t *MapType[K, V]) MaxItems(n int) *MapType[K, V] { return t.rule(ir.MaxItems(n)) }

// Custom registers fn over the whole map.
func (t *MapType[K, V]) Custom(name string, fn func(context.Context, map[K]V) error) *MapType[K, V] {
	c := t.rule(ir.Custom(name))
	c.reg = register(t.reg, name, typed(name, fn))
	return c
}

// ---------------------------------------------------------------- union

// UnionType is an undiscriminated union over Go type V (typically an
// interface or any). Prefer [OneOf] when a discriminator field exists.
type UnionType[V any] struct {
	variants []Node
	base     ir.Base
	reg      *registry
}

// Union starts an undiscriminated union. V is the Go type of the field —
// an interface, usually any — and the variants are whatever shapes may
// appear there.
func Union[V any](variants ...Node) *UnionType[V] {
	reg := newRegistry()
	for _, v := range variants {
		_ = reg.merge(v.registry())
	}
	return &UnionType[V]{variants: variants, reg: reg}
}

// Node implements [Builder].
func (t *UnionType[V]) Node() ir.TypeNode {
	n := &ir.UnionNode{Base: t.base}
	for _, v := range t.variants {
		n.Variants = append(n.Variants, v.Node())
	}
	return n
}
func (t *UnionType[V]) registry() *registry { return t.reg }
func (t *UnionType[V]) phantom(V)           {}

// Nullable allows null.
func (t *UnionType[V]) Nullable() *UnionType[V] { c := *t; c.base.Nullable = true; return &c }

// ---------------------------------------------------------------- oneof

// OneOfType is a discriminated union over Go type V: the named field's
// value selects the case. Emits discriminator-keyed if/then/else, never a
// raw oneOf (ADR-VAL-007).
type OneOfType[V any] struct {
	discriminator string
	cases         map[string]Node
	def           Node
	base          ir.Base
	err           error
}

// OneOf starts a discriminated union keyed on the given field's wire name.
func OneOf[V any](discriminator string) *OneOfType[V] {
	return &OneOfType[V]{discriminator: discriminator, cases: map[string]Node{}}
}

// Case adds a variant for discriminator value tag. Adding the same tag
// twice is recorded as an error surfaced by the enclosing [Struct].
func (t *OneOfType[V]) Case(tag string, b Node) *OneOfType[V] {
	c := *t
	c.cases = make(map[string]Node, len(t.cases)+1)
	maps.Copy(c.cases, t.cases)
	if _, dup := c.cases[tag]; dup {
		c.err = errors.Join(t.err, fmt.Errorf("vb: oneof case %q added twice", tag))
	}
	c.cases[tag] = b
	return &c
}

// Default sets the variant used when the discriminator matches no case.
func (t *OneOfType[V]) Default(b Node) *OneOfType[V] {
	c := *t
	c.def = b
	return &c
}

// Nullable allows null.
func (t *OneOfType[V]) Nullable() *OneOfType[V] { c := *t; c.base.Nullable = true; return &c }

// Node implements [Builder].
func (t *OneOfType[V]) Node() ir.TypeNode {
	n := &ir.OneOfNode{Base: t.base, Discriminator: t.discriminator, Cases: make(map[string]ir.TypeNode, len(t.cases))}
	for tag, b := range t.cases {
		n.Cases[tag] = b.Node()
	}
	if t.def != nil {
		n.Default = t.def.Node()
	}
	return n
}

func (t *OneOfType[V]) registry() *registry {
	reg := newRegistry()
	reg.err = t.err
	tags := make([]string, 0, len(t.cases))
	for tag := range t.cases {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	for _, tag := range tags {
		_ = reg.merge(t.cases[tag].registry())
	}
	if t.def != nil {
		_ = reg.merge(t.def.registry())
	}
	return reg
}
func (t *OneOfType[V]) phantom(V) {}
