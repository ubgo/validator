// Package vb is the typed builder: the canonical hand-written way to author
// validation rules, compiling to the [ir] tree (docsi/MISSION.md ADR-VAL-006).
//
// The builder records data, never behavior. Calling String().MinLength(3)
// appends ir.MinLength(3) to a node; it does not capture a closure. The only
// functions the builder accepts are Custom and Async rule bodies, and even
// those go into a side registry keyed by name: the IR stores the name, the
// executor looks the function up. That is what keeps the tree serializable
// and the TypeScript generator honest (ADR-VAL-001, ADR-VAL-005).
//
// Two layers, so generated accessors and hand-written schemas share one
// vocabulary:
//
//   - a type builder ([Builder]) describes a VALUE: vb.String().Email(),
//     vb.Int[int]().Min(18), vb.Array(vb.String()). It carries the rules.
//   - a [FieldRef] names a struct FIELD and how to read it; [F] binds a
//     ref to a type builder to make a [Field], and [Struct] collects fields
//     into a [Schema].
//
// The compile-time contract: F(ref, builder) only accepts a builder whose
// value type matches the field's Go type, so vb.F(Fields.Age, vb.String())
// is a compile error, not a runtime surprise.
package vb

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"sync/atomic"

	"github.com/ubgo/validator/ir"
)

// Node is any type builder, regardless of the Go type it describes. Sealed
// to this package. Used where variants of DIFFERENT Go types meet — the
// members of a [Union] or the cases of a [OneOf] — and the value type is
// therefore an interface the caller chooses.
type Node interface {
	// Node returns the IR type node this builder describes. Safe to call
	// repeatedly; builders are immutable after construction and return a
	// fresh node each call.
	Node() ir.TypeNode

	// registry returns the custom/async functions referenced by this node
	// and its children, the named schemas it references for Defs, and any
	// build errors collected on the way.
	registry() *registry
}

// Builder is a type builder for Go values of type V. The phantom method
// pins V so that mismatched field/builder pairs fail to compile.
type Builder[V any] interface {
	Node

	// phantom pins V. Never called.
	phantom(V)
}

// Func is a registered rule body: the executor calls it with the field's
// value. Typed wrappers in the builders narrow value to the field type
// before calling the author's function.
type Func func(ctx context.Context, value any) error

// registry is the side table of everything the IR only names, plus the
// build errors a builder cannot express through types (duplicate case tags,
// conflicting function names). It flows up through [Builder.registry] so
// [Struct] can report every problem at once.
type registry struct {
	funcs map[string]funcEntry
	defs  map[string]ir.TypeNode
	err   error
}

// funcEntry pairs a body with a registration identity. Func values are not
// comparable, so the identity is what lets the same registration reached
// through two paths (a shared sub-schema referenced twice) merge silently
// while two different bodies under one name are reported.
type funcEntry struct {
	fn Func
	// raw is the author's function with its own signature
	// (func(context.Context, V) error), kept so an executor that knows V
	// statically can call it without boxing the value through any.
	raw any
	id  uint64
}

// nextFuncID hands out registration identities. A package-level counter is
// the one piece of process state here; it carries no meaning beyond
// "distinct from every other registration in this process".
var nextFuncID atomic.Uint64

func newRegistry() *registry {
	return &registry{funcs: map[string]funcEntry{}, defs: map[string]ir.TypeNode{}}
}

// merge folds other into r, reporting a name registered twice with
// different bodies — the one way a name-keyed side table can lie.
func (r *registry) merge(other *registry) error {
	if other == nil {
		return nil
	}
	var errs []error
	if other.err != nil {
		errs = append(errs, other.err)
	}
	for name, entry := range other.funcs {
		if have, dup := r.funcs[name]; dup {
			if have.id != entry.id {
				errs = append(errs, fmt.Errorf("vb: function %q registered twice with different bodies", name))
			}
			continue
		}
		r.funcs[name] = entry
	}
	for name, node := range other.defs {
		if _, dup := r.defs[name]; dup {
			// The same schema reached through two paths is expected; the
			// nodes are the same value, so keep the first.
			continue
		}
		r.defs[name] = node
	}
	r.err = errors.Join(append([]error{r.err}, errs...)...)
	return r.err
}

// FieldRef names one field of T and knows how to read it. Generated
// accessors (Phase 5) emit one per struct field; hand-written schemas use
// [NewField]. The getter is an access path for executors, not a rule, and
// is never part of the IR.
type FieldRef[T any, V any] struct {
	name     string
	jsonName string
	get      func(T) V
}

// NewField declares a field by Go name and getter.
func NewField[T any, V any](name string, get func(T) V) FieldRef[T, V] {
	return FieldRef[T, V]{name: name, get: get}
}

// JSON sets the wire name (the json tag). Empty means the Go name.
func (f FieldRef[T, V]) JSON(name string) FieldRef[T, V] {
	f.jsonName = name
	return f
}

// Name returns the Go field name.
func (f FieldRef[T, V]) Name() string { return f.name }

// Get reads the field from a value of T.
func (f FieldRef[T, V]) Get(v T) V { return f.get(v) }

// Field is a named slot in a [Schema]: a ref bound to a type builder plus
// slot-level attributes (presence, transforms, description).
type Field[T any] struct {
	name       string
	jsonName   string
	node       ir.TypeNode
	reg        *registry
	optional   bool
	transforms []ir.Transform
	meta       ir.NodeMeta
	get        func(T) any
}

// F binds a field ref to a type builder. The builder's value type must
// equal the field's type — enforced by the compiler through [Builder]'s
// phantom parameter.
func F[T any, V any](ref FieldRef[T, V], b Builder[V]) Field[T] {
	return Field[T]{
		name:     ref.name,
		jsonName: ref.jsonName,
		node:     b.Node(),
		reg:      b.registry(),
		get:      func(v T) any { return ref.get(v) },
	}
}

// Optional marks the key as allowed to be absent. Fields are required by
// default, matching JSON Schema's model where presence must be opted out of.
func (f Field[T]) Optional() Field[T] {
	f.optional = true
	return f
}

// Describe sets the JSON Schema description for the slot.
func (f Field[T]) Describe(s string) Field[T] {
	f.meta.Description = s
	return f
}

// transform appends one pipeline step; the public methods name the kinds so
// callers never write the strings.
func (f Field[T]) transform(kind ir.TransformKind, phase ir.Phase, params map[string]any) Field[T] {
	f.transforms = append(append([]ir.Transform{}, f.transforms...), ir.Transform{Kind: kind, Phase: phase, Params: params})
	return f
}

// Trim strips surrounding whitespace before validation.
func (f Field[T]) Trim() Field[T] { return f.transform(ir.TransformTrim, ir.PhasePre, nil) }

// Lower lower-cases before validation.
func (f Field[T]) Lower() Field[T] { return f.transform(ir.TransformLower, ir.PhasePre, nil) }

// Upper upper-cases before validation.
func (f Field[T]) Upper() Field[T] { return f.transform(ir.TransformUpper, ir.PhasePre, nil) }

// Coerce enables lax string→typed conversion before validation on map
// input, whatever coercion mode the executor runs in.
func (f Field[T]) Coerce() Field[T] { return f.transform(ir.TransformCoerce, ir.PhasePre, nil) }

// NoCoerce pins the field to its declared type even when the executor runs
// in lax mode: a string where an int is expected is a type mismatch, never
// converted. Field markers beat call options in both directions.
func (f Field[T]) NoCoerce() Field[T] { return f.transform(ir.TransformStrict, ir.PhasePre, nil) }

// NormalizeEmail canonicalizes an address before validation.
func (f Field[T]) NormalizeEmail() Field[T] {
	return f.transform(ir.TransformNormalizeEmail, ir.PhasePre, nil)
}

// Default substitutes v when the key is absent, before validation.
func (f Field[T]) Default(v any) Field[T] {
	return f.transform(ir.TransformDefault, ir.PhasePre, map[string]any{ir.ParamValue: v})
}

// Schema is a compiled struct schema: the IR plus the Go-side side tables an
// executor needs (function registry, field getters). Build one with
// [Struct] → [StructBuilder.Build] and hand it to an executor or an emitter;
// the IR alone is what leaves Go.
type Schema[T any] struct {
	name    string
	meta    ir.SchemaMeta
	root    *ir.ObjectNode
	reg     *registry
	getters map[string]func(T) any
}

// Object builds an inline object type for a Go struct S — used for nested
// values, oneof variants, and array elements. Unlike [Struct] it has no name
// and is not hoisted into Defs.
func Object[S any](fields ...Field[S]) *ObjectType[S] {
	o := &ObjectType[S]{reg: newRegistry(), node: &ir.ObjectNode{}}
	for _, f := range fields {
		o.node.Fields = append(o.node.Fields, ir.FieldNode{
			Name: f.name, JSONName: f.jsonName, Type: f.node, Optional: f.optional,
			Transforms: f.transforms, Meta: f.meta,
		})
		_ = o.reg.merge(f.reg)
	}
	return o
}

// StructBuilder is the unchecked schema under construction: object-level
// attributes (gates, extra policy) are added here, then [StructBuilder.Build]
// validates the whole tree at once.
type StructBuilder[T any] struct {
	name    string
	meta    ir.SchemaMeta
	obj     *ObjectType[T]
	getters map[string]func(T) any
}

// Struct starts a named schema for T. The name becomes the IR title and,
// when another schema references this one through [Ref], the Defs key.
func Struct[T any](name string, fields ...Field[T]) *StructBuilder[T] {
	b := &StructBuilder[T]{name: name, meta: ir.SchemaMeta{Title: name}, obj: Object(fields...), getters: map[string]func(T) any{}}
	for _, f := range fields {
		b.getters[f.name] = f.get
	}
	return b
}

// Source records the fully qualified Go type the schema describes, for
// tracing generated artifacts back to their origin.
func (b *StructBuilder[T]) Source(src string) *StructBuilder[T] {
	c := b.clone()
	c.meta.Source = src
	return c
}

// Generator records which tool produced the schema.
func (b *StructBuilder[T]) Generator(g string) *StructBuilder[T] {
	c := b.clone()
	c.meta.Generator = g
	return c
}

// clone copies the builder so every method below is a value operation,
// the same contract the type builders keep. Without it two schemas
// derived from one base would share state, and the second would silently
// inherit the first's gates.
func (b *StructBuilder[T]) clone() *StructBuilder[T] {
	c := *b
	return &c
}

// Extra sets the unknown-key policy on the root object.
func (b *StructBuilder[T]) Extra(p ir.ExtraPolicy) *StructBuilder[T] {
	c := b.clone()
	c.obj = b.obj.Extra(p)
	return c
}

// Gate appends object-level conditional logic (see [When], [Requires],
// [Group]). Fields are named through refs so a rename is a compile error;
// Build still verifies every target exists.
func (b *StructBuilder[T]) Gate(gates ...ir.GatedRules) *StructBuilder[T] {
	c := b.clone()
	c.obj = b.obj.Gate(gates...)
	return c
}

// Custom attaches a rule to the whole struct: the function receives the
// entire value. Use it when the check has no single owning field, which a
// gate cannot express because a gate only decides whether other rules
// apply.
//
//	vb.Struct("Booking", fields...).
//		Custom("dates_consistent", func(_ context.Context, b Booking) error {
//			if b.End.Sub(b.Start) > 30*24*time.Hour {
//				return errors.New("a booking may not exceed 30 days")
//			}
//			return nil
//		})
func (b *StructBuilder[T]) Custom(name string, fn func(context.Context, T) error) *StructBuilder[T] {
	c := b.clone()
	c.obj = b.obj.Custom(name, fn)
	return c
}

// Async attaches a server-backed rule to the whole struct, for a check
// that needs the database and reads more than one field.
func (b *StructBuilder[T]) Async(name, endpoint string, fn func(context.Context, T) error) *StructBuilder[T] {
	c := b.clone()
	c.obj = b.obj.Async(name, endpoint, fn)
	return c
}

// Build finalizes the schema. It returns an error for the mistakes a builder
// cannot prevent by type: a function name registered twice with different
// bodies, a duplicate oneof case, or a tree that fails [ir.Check].
func (b *StructBuilder[T]) Build() (*Schema[T], error) {
	s := &Schema[T]{name: b.name, meta: b.meta, root: b.obj.node, reg: b.obj.reg, getters: b.getters}
	if b.obj.reg.err != nil {
		return nil, b.obj.reg.err
	}
	if err := ir.Check(s.IR()); err != nil {
		return nil, err
	}
	return s, nil
}

// MustBuild is [StructBuilder.Build] for package-level declarations; it
// panics on error because a schema that fails to build is a programming
// error, not input.
func (b *StructBuilder[T]) MustBuild() *Schema[T] {
	s, err := b.Build()
	if err != nil {
		panic(err)
	}
	return s
}

// Name returns the schema's name.
func (s *Schema[T]) Name() string { return s.name }

// IR materializes the serializable schema. Defs are every named schema
// reached through [Ref]; the root is inlined. The result is a fresh Schema
// value sharing nodes with the builder — treat it as read-only.
func (s *Schema[T]) IR() *ir.Schema {
	out := &ir.Schema{Version: ir.Version, Root: s.root, Meta: s.meta}
	if len(s.reg.defs) > 0 {
		out.Defs = make(map[string]ir.TypeNode, len(s.reg.defs))
		maps.Copy(out.Defs, s.reg.defs)
	}
	return out
}

// Funcs returns the registered custom and async rule bodies by name, sorted
// for deterministic iteration.
func (s *Schema[T]) Funcs() []NamedFunc {
	names := make([]string, 0, len(s.reg.funcs))
	for n := range s.reg.funcs {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]NamedFunc, 0, len(names))
	for _, n := range names {
		out = append(out, NamedFunc{Name: n, Func: s.reg.funcs[n].fn})
	}
	return out
}

// Func looks up one registered body by name.
func (s *Schema[T]) Func(name string) (Func, bool) {
	entry, ok := s.reg.funcs[name]
	return entry.fn, ok
}

// TypedFunc returns the author's function as registered, before the [Func]
// wrapper: a func(context.Context, V) error for the builder's V. Generated
// code asserts it to the field's static type and calls it directly, which
// is the allocation-free path; [Func] stays the universal one.
func (s *Schema[T]) TypedFunc(name string) (any, bool) {
	entry, ok := s.reg.funcs[name]
	return entry.raw, ok
}

// Getter returns the access path for a root field by Go name.
func (s *Schema[T]) Getter(field string) (func(T) any, bool) {
	g, ok := s.getters[field]
	return g, ok
}

// NamedFunc pairs a registry name with its body.
type NamedFunc struct {
	Name string
	Func Func
}

// TypeError is returned by a registered rule body when the executor hands
// it a value of the wrong dynamic type. It is a distinct type so transports
// (the HTTP round-trip handler) can answer "bad request" instead of
// reporting a validation failure the client cannot fix by changing input.
type TypeError struct {
	Rule string
	Want string
	Got  string
}

// Error implements error.
func (e *TypeError) Error() string {
	return fmt.Sprintf("vb: rule %q expected %s, got %s", e.Rule, e.Want, e.Got)
}

// typed wraps an author's function of V as a [Func], narrowing at the call
// boundary. A value of the wrong dynamic type is reported as a [TypeError]
// rather than a panic so a misuse surfaces with a clear message.
func typed[V any](name string, fn func(context.Context, V) error) Func {
	return func(ctx context.Context, value any) error {
		v, ok := value.(V)
		if !ok {
			return &TypeError{Rule: name, Want: fmt.Sprintf("%T", *new(V)), Got: fmt.Sprintf("%T", value)}
		}
		return fn(ctx, v)
	}
}

// Endpoint is one async rule and the URL path the frontend round-trips to.
type Endpoint struct {
	// Rule is the registered function name.
	Rule string

	// Path is the declared endpoint, or DefaultEndpointPrefix+Rule when the
	// schema declared none — the same default the generator uses.
	Path string

	// Func is the registered body.
	Func Func
}

// DefaultEndpointPrefix is where async rules are served when a schema does
// not declare an endpoint: POST /v/<rule>.
const DefaultEndpointPrefix = "/v/"

// Endpoints lists every async rule in the schema with its endpoint, sorted
// by path. Custom (opaque) rules are deliberately excluded: they were never
// meant to be reachable over HTTP, and exposing them would turn every
// helper into an enumeration oracle.
func (s *Schema[T]) Endpoints() []Endpoint {
	seen := map[string]Endpoint{}
	var walk func(n ir.TypeNode)
	walk = func(n ir.TypeNode) {
		if n == nil {
			return
		}
		for _, r := range ir.Rules(n) {
			s.collectEndpoint(seen, r)
		}
		if o, ok := n.(*ir.ObjectNode); ok {
			for _, g := range o.Gated {
				for _, tr := range append(append([]ir.TargetRule{}, g.Then...), g.Else...) {
					s.collectEndpoint(seen, tr.Rule)
				}
			}
		}
		for _, c := range ir.Children(n) {
			walk(c)
		}
	}
	walk(s.root)
	for _, name := range sortedKeys(s.reg.defs) {
		walk(s.reg.defs[name])
	}
	out := make([]Endpoint, 0, len(seen))
	for _, name := range sortedKeys(seen) {
		out = append(out, seen[name])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func (s *Schema[T]) collectEndpoint(seen map[string]Endpoint, r ir.Rule) {
	if r.Kind != ir.RuleAsync {
		return
	}
	name, _ := r.Params[ir.ParamRef].(string)
	if _, dup := seen[name]; dup {
		return
	}
	path, _ := r.Params[ir.ParamEndpoint].(string)
	if path == "" {
		path = DefaultEndpointPrefix + name
	}
	entry, ok := s.reg.funcs[name]
	if !ok {
		return
	}
	seen[name] = Endpoint{Rule: name, Path: path, Func: entry.fn}
}

// sortedKeys returns map keys in lexical order for deterministic output.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
