// Package interp is the reflection interpreter: the zero-config executor
// that walks an [ir.Schema] against a Go value (docsi/MISSION.md
// ADR-VAL-003). It is the reference semantics — the codegen executor must
// agree with it on every fixture — and the quick-start path for users who
// have not adopted go generate.
//
// Inputs it accepts: a struct (by value or pointer), or a map[string]any
// decoded from JSON, or any nesting of those. Pass a pointer when pre-phase
// transforms (trim, lower, default) should be written back; with a value
// they apply to the validated view only.
//
// Presence, the one semantic that needs stating: a key is "present" when it
// is non-nil for nil-able kinds (pointer, interface, slice, map) and
// non-empty for strings; numbers and booleans are always present, because a
// zero int or a false bool is a legitimate value and nobody wants Active:
// false to fail "required". Map input is present when the key exists with a
// non-nil value. Required means present; Nullable means an explicit null is
// accepted; the two compose as in the IR.
//
// Honesty rules: anything the interpreter cannot execute — a CEL expression,
// an unknown transform kind, a custom rule with no registered body — is
// rejected by [New], never skipped at validation time. A schema
// that builds is a schema that runs completely.
package interp

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ubgo/validator/internal/rules"
	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/vb"
	"github.com/ubgo/validator/verr"
)

// Funcs resolves the bodies the IR only names. *vb.Schema satisfies it.
type Funcs interface {
	Func(name string) (vb.Func, bool)
}

// Option configures a [Validator].
type Option func(*Validator)

// FailFast stops at the first issue instead of accumulating.
func FailFast() Option { return func(v *Validator) { v.failFast = true } }

// WithFuncs supplies the custom/async rule bodies. Required when the schema
// references any; [New] errors on an unresolved name.
func WithFuncs(f Funcs) Option { return func(v *Validator) { v.lookup = f } }

// CoerceMode says how string values in map input meet non-string nodes.
// Struct input is never coerced: a typed field already has its type.
type CoerceMode int

const (
	// CoerceMarked coerces only fields carrying the coerce transform. The
	// default: what the schema says, nothing more.
	CoerceMarked CoerceMode = iota

	// CoerceLax coerces every int, float, bool, time and duration field
	// except those marked strict — the mode for form-encoded or query
	// input, where everything arrives as a string.
	CoerceLax

	// CoerceStrict coerces nothing that is not explicitly marked. Today it
	// behaves as CoerceMarked; it exists so a caller can name the intent
	// and so a future default of Lax has an opt-out. A field's own coerce
	// marker still wins, because field markers beat call options.
	CoerceStrict
)

// Lax runs the validator in [CoerceLax] mode.
func Lax() Option { return func(v *Validator) { v.coerce = CoerceLax } }

// Strict runs the validator in [CoerceStrict] mode.
func Strict() Option { return func(v *Validator) { v.coerce = CoerceStrict } }

// WithCoerce sets the coercion mode explicitly.
func WithCoerce(m CoerceMode) Option { return func(v *Validator) { v.coerce = m } }

// rootPath is the JSONPath of the value under validation.
const rootPath = "$"

// Validator is a compiled interpreter for one schema. Safe for concurrent
// use; all mutable state lives in the per-call run.
type Validator struct {
	schema   *ir.Schema
	failFast bool
	coerce   CoerceMode
	lookup   Funcs
	funcs    map[string]vb.Func
	regex    map[string]*regexp.Regexp

	// exprCompiler is the engine for CEL conditions, supplied by WithExpr;
	// exprs holds one compiled program per distinct expression source.
	exprCompiler ExprCompiler
	exprs        map[string]ExprProgram
}

// New compiles a schema: checks its structure, compiles every pattern,
// resolves every function name, and refuses anything it cannot execute.
func New(s *ir.Schema, opts ...Option) (*Validator, error) {
	if err := ir.Check(s); err != nil {
		return nil, err
	}
	v := &Validator{schema: s, funcs: map[string]vb.Func{}, regex: map[string]*regexp.Regexp{}, exprs: map[string]ExprProgram{}}
	for _, o := range opts {
		o(v)
	}
	c := &compiler{v: v}
	c.node(s.Root)
	for _, name := range sortedKeys(s.Defs) {
		c.node(s.Defs[name])
	}
	if err := errors.Join(c.errs...); err != nil {
		return nil, err
	}
	return v, nil
}

// FromSchema compiles a builder schema, using it as the function registry.
func FromSchema[T any](s *vb.Schema[T], opts ...Option) (*Validator, error) {
	return New(s.IR(), append([]Option{WithFuncs(s)}, opts...)...)
}

// Validate walks value against the schema. It returns nil when valid and a
// *verr.Errors otherwise.
func (v *Validator) Validate(ctx context.Context, value any) error {
	return v.Run(ctx, value).OrNil()
}

// Run is Validate returning the concrete issue list (never nil).
func (v *Validator) Run(ctx context.Context, value any) *verr.Errors {
	errs := &verr.Errors{FailFast: v.failFast}
	v.RunInto(ctx, value, errs)
	return errs
}

// RunInto validates into a caller-owned issue list, so several schemas can
// report into one result and a caller can request fail-fast per call by
// setting errs.FailFast. The generated executors expose the same
// Validate<Type>Into shape.
func (v *Validator) RunInto(ctx context.Context, value any, errs *verr.Errors) {
	v.RunIntoAt(ctx, value, rootPath, errs)
}

// RunIntoAt is RunInto with the root reported at path instead of "$", for
// callers validating a subtree of a larger document — generated code uses
// it to hand an interpreter-only shape back to this executor.
func (v *Validator) RunIntoAt(ctx context.Context, value any, path string, errs *verr.Errors) {
	r := &run{ctx: ctx, v: v, errs: errs}
	r.node(path, v.schema.Root, reflect.ValueOf(value), nil)
}

// compiler walks the IR once at New time collecting everything that must
// be resolved or rejected up front.
type compiler struct {
	v    *Validator
	errs []error

	// obj is the object whose subtree is being compiled, so an expression
	// condition can be told which variables it may reference.
	obj *ir.ObjectNode
}

func (c *compiler) errf(format string, args ...any) {
	c.errs = append(c.errs, fmt.Errorf("interp: "+format, args...))
}

func (c *compiler) node(n ir.TypeNode) {
	// Track the enclosing object so an expression condition, whether on a
	// gate or on a field, knows which variables it may name.
	prev := c.obj
	if o, ok := n.(*ir.ObjectNode); ok {
		c.obj = o
	}
	defer func() { c.obj = prev }()
	for _, r := range ir.Rules(n) {
		c.rule(r)
	}
	if o, ok := n.(*ir.ObjectNode); ok {
		for _, f := range o.Fields {
			for _, t := range f.Transforms {
				if _, known := transformKinds[t.Kind]; !known {
					c.errf("transform %q is not supported by the interpreter (field %q)", t.Kind, f.Name)
				}
			}
		}
		for _, g := range o.Gated {
			if g.Cond != nil {
				c.cond(*g.Cond)
			}
			for _, tr := range g.Then {
				c.rule(tr.Rule)
			}
			for _, tr := range g.Else {
				c.rule(tr.Rule)
			}
		}
	}
	for _, child := range ir.Children(n) {
		c.node(child)
	}
}

func (c *compiler) rule(r ir.Rule) {
	if r.Cond != nil {
		c.cond(*r.Cond)
	}
	switch r.Kind {
	case ir.RulePattern:
		pat, _ := r.Params[ir.ParamRegex].(string)
		if _, done := c.v.regex[pat]; done {
			return
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			c.errf("pattern %q: %v", pat, err)
			return
		}
		c.v.regex[pat] = re
	case ir.RuleCustom, ir.RuleAsync:
		name, _ := r.Params[ir.ParamRef].(string)
		c.resolve(name)
	}
}

func (c *compiler) cond(k ir.Condition) {
	switch k.Kind {
	case ir.CondExpr:
		// Compile once, here, so a bad expression fails at startup rather
		// than on the request that first reaches the gate.
		if c.v.exprCompiler == nil {
			c.errf("CEL condition %q needs an expression engine: pass interp.WithExpr (see github.com/ubgo/validator/cel)", k.CEL)
			break
		}
		if _, done := c.v.exprs[k.CEL]; !done {
			prog, err := c.v.exprCompiler.CompileExpr(k.CEL, objectVars(c.obj))
			if err != nil {
				c.errf("CEL condition %q: %v", k.CEL, err)
			} else {
				c.v.exprs[k.CEL] = prog
			}
		}
	case ir.CondCustom:
		c.resolve(k.Ref)
	}
	for _, sub := range k.Sub {
		c.cond(sub)
	}
}

// objectVars lists an object's fields by wire name, the variables an
// expression may reference.
func objectVars(o *ir.ObjectNode) []string {
	if o == nil {
		return nil
	}
	out := make([]string, 0, len(o.Fields))
	for i := range o.Fields {
		out = append(out, wireName(&o.Fields[i]))
	}
	return out
}

// resolve looks a function name up once and caches it, erroring when no
// registry or no body exists — the skeleton-honesty rule applied to rules.
func (c *compiler) resolve(name string) {
	if _, done := c.v.funcs[name]; done {
		return
	}
	if c.v.lookup == nil {
		c.errf("rule %q needs a function registry (use WithFuncs)", name)
		return
	}
	fn, ok := c.v.lookup.Func(name)
	if !ok {
		c.errf("function %q is referenced but not registered", name)
		return
	}
	c.v.funcs[name] = fn
}

// run is the per-call state of one validation walk.
type run struct {
	ctx     context.Context
	v       *Validator
	errs    *verr.Errors
	stopped bool
}

// scope is the enclosing object, needed by cross-field rules, conditions
// and gates. nil at the root of a non-object schema.
type scope struct {
	obj  *ir.ObjectNode
	val  reflect.Value
	path string
}

// field reads a scoped field by Go name, returning the value and whether
// it is present.
func (s *scope) field(name string) (reflect.Value, bool) {
	if s == nil {
		return reflect.Value{}, false
	}
	for i := range s.obj.Fields {
		if s.obj.Fields[i].Name == name {
			return fieldValue(s.val, s.obj.Fields[i])
		}
	}
	return reflect.Value{}, false
}

func (r *run) issue(is verr.Issue) {
	if r.stopped {
		return
	}
	r.errs.Add(is)
	if r.errs.Stopped() {
		r.stopped = true
	}
}

// structural reports a non-rule failure.
func (r *run) structural(path, code, message string, params map[string]any) {
	r.issue(verr.Issue{Code: code, Path: path, Message: message, Params: params})
}

// node validates val against n.
func (r *run) node(path string, n ir.TypeNode, val reflect.Value, sc *scope) {
	if r.stopped {
		return
	}
	val, isNil := deref(val)
	if isNil {
		if !nullable(n) {
			r.structural(path, verr.CodeRequired, "value is required", nil)
		}
		return
	}
	// JSON has no time type: an RFC 3339 string is a time's wire form, so a
	// string arriving for a time node is parsed rather than rejected. It is
	// written back when the container is a map[string]any, matching what
	// transforms do, so callers see the typed value afterwards.
	if _, isTime := n.(*ir.TimeNode); isTime && val.Kind() == reflect.String {
		if parsed, err := time.Parse(time.RFC3339, val.String()); err == nil {
			val = reflect.ValueOf(parsed)
			if sc != nil && isStringMap(sc.val) && sc.val.Type() == anyMapType {
				sc.val.SetMapIndex(reflect.ValueOf(lastSegment(path)), val)
			}
		}
	}
	if want := kindMismatch(n, val); want != "" {
		r.typeIssue(path, want, val)
		return
	}
	switch t := n.(type) {
	case *ir.ObjectNode:
		// Object-level rules run before the fields, so a whole-value check
		// reports first and fail-fast stops on it rather than on a field.
		r.rules(path, t.Rules, val, sc)
		r.object(path, t, val)
	case *ir.StringNode:
		r.rules(path, t.Rules, val, sc)
	case *ir.IntNode:
		r.rules(path, t.Rules, val, sc)
	case *ir.FloatNode:
		r.rules(path, t.Rules, val, sc)
	case *ir.BoolNode:
		r.rules(path, t.Rules, val, sc)
	case *ir.BytesNode:
		r.rules(path, t.Rules, val, sc)
	case *ir.TimeNode:
		r.rules(path, t.Rules, val, sc)
	case *ir.DurationNode:
		r.rules(path, t.Rules, val, sc)
	case *ir.ArrayNode:
		r.rules(path, t.Rules, val, sc)
		for i := 0; i < val.Len() && !r.stopped; i++ {
			r.node(path+"["+strconv.Itoa(i)+"]", t.Elem, val.Index(i), nil)
		}
	case *ir.MapNode:
		r.rules(path, t.Rules, val, sc)
		for _, key := range sortedMapKeys(val) {
			if r.stopped {
				return
			}
			kp := path + "." + fmt.Sprint(key.Interface())
			r.node(kp, t.Key, key, nil)
			r.node(kp, t.Val, val.MapIndex(key), nil)
		}
	case *ir.UnionNode:
		r.union(path, t, val)
	case *ir.OneOfNode:
		r.oneOf(path, t, val, sc)
	case *ir.RefNode:
		r.node(path, r.v.schema.Defs[t.Ref], val, sc)
	case *ir.AnyNode:
		// Nothing to check.
	}
}

func (r *run) typeIssue(path, want string, val reflect.Value) {
	r.structural(path, verr.CodeType, "expected "+want+", got "+val.Kind().String(),
		map[string]any{"expected": want, "actual": val.Kind().String()})
}

// lastSegment returns the final key of a dotted path ("$.a.b" → "b"), the
// map key a field value lives under. Field paths are built from wire
// names, which never contain dots for map input produced by JSON.
func lastSegment(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// Measured negative result, do not retry without new evidence: the
// per-field `path + "." + wire` below looks like the obvious thing to make
// lazy, the way codegen/rt.Path does it for generated code. It was tried
// (a shared jsonpath.Path threaded through node/object/union/oneOf/rules)
// and measured WORSE — 184 → 728 B/op on the success path — because every
// segment moved to the heap. Generated code is straight-line, so escape
// analysis proves its Path values never outlive the frame; this walker is
// recursive, dispatches on an interface, and stores the path in a *scope
// that is itself heap-allocated, so the whole chain escapes. Closing this
// gap needs the paths precomputed per static node position at New time,
// not a lazily built chain.
//
// object validates a struct or map[string]any against an object node; the
// caller has already checked the shape.
func (r *run) object(path string, o *ir.ObjectNode, val reflect.Value) {
	sc := &scope{obj: o, val: val, path: path}
	// The set of declared names is only consulted by the unknown-key check
	// below, which only applies to map input under ExtraForbid. Building it
	// unconditionally cost one map allocation per object on every run,
	// including the common struct-input case that can have no unknown keys.
	checkExtra := o.Extra == ir.ExtraForbid && isStringMap(val)
	var declared map[string]struct{}
	if checkExtra {
		declared = make(map[string]struct{}, len(o.Fields))
	}
	for i := range o.Fields {
		if r.stopped {
			return
		}
		f := &o.Fields[i]
		wire := wireName(f)
		if checkExtra {
			declared[wire] = struct{}{}
		}
		fp := path + "." + wire
		child, present := fieldValue(val, *f)
		if !present {
			if def, ok := defaultOf(f); ok {
				child = applyDefault(val, *f, def)
				present = child.IsValid()
			}
		}
		if !present {
			if !f.Optional {
				r.structural(fp, verr.CodeRequired, "field is required", nil)
			}
			continue
		}
		child = r.transform(val, *f, child)
		r.node(fp, f.Type, child, sc)
	}
	if checkExtra {
		for _, key := range sortedMapKeys(val) {
			if _, ok := declared[key.String()]; !ok {
				r.structural(path+"."+key.String(), verr.CodeUnknownKey, "unknown key", map[string]any{"key": key.String()})
			}
		}
	}
	for _, g := range o.Gated {
		if r.stopped {
			return
		}
		r.gate(sc, g)
	}
}

// union passes when any variant accepts the value.
func (r *run) union(path string, u *ir.UnionNode, val reflect.Value) {
	for _, variant := range u.Variants {
		probe := &run{ctx: r.ctx, v: &Validator{schema: r.v.schema, failFast: true, funcs: r.v.funcs, regex: r.v.regex}, errs: &verr.Errors{}}
		probe.node(path, variant, val, nil)
		if probe.errs.Len() == 0 {
			return
		}
	}
	r.structural(path, verr.CodeUnion, "value matches no variant", map[string]any{"variants": len(u.Variants)})
}

// oneOf selects the case by discriminator and validates against it.
func (r *run) oneOf(path string, o *ir.OneOfNode, val reflect.Value, sc *scope) {
	tagVal, ok := lookupByWireName(val, o.Discriminator)
	tag := ""
	if ok {
		tagVal, _ = deref(tagVal)
		if tagVal.IsValid() {
			tag = fmt.Sprint(tagVal.Interface())
		}
	}
	node, found := o.Cases[tag]
	if !found {
		node = o.Default
	}
	if node == nil {
		r.structural(path, verr.CodeVariant, "unknown variant "+strconv.Quote(tag),
			map[string]any{"discriminator": o.Discriminator, "value": tag})
		return
	}
	r.node(path, node, val, sc)
}

// gate applies one object-level conditional.
func (r *run) gate(sc *scope, g ir.GatedRules) {
	switch g.Kind {
	case ir.GateWhen:
		targets := g.Else
		if r.cond(sc, *g.Cond) {
			targets = g.Then
		}
		for _, tr := range targets {
			if r.stopped {
				return
			}
			r.targetRule(sc, tr)
		}
	case ir.GateRequires:
		if !r.cond(sc, *g.Cond) {
			return
		}
		for _, name := range g.Fields {
			if _, present := sc.field(name); !present {
				r.structural(sc.path+"."+wireOf(sc.obj, name), verr.CodeRequired, "field is required", nil)
			}
		}
	case ir.GateGroup:
		count := 0
		for _, name := range g.Fields {
			if _, present := sc.field(name); present {
				count++
			}
		}
		ok := true
		switch g.Mode {
		case ir.ExactlyOne:
			ok = count == 1
		case ir.AtLeastOne:
			ok = count >= 1
		case ir.AllOrNone:
			ok = count == 0 || count == len(g.Fields)
		}
		if !ok {
			r.structural(sc.path, verr.CodeGroup, fmt.Sprintf("%s of %v required, %d present", g.Mode, g.Fields, count),
				map[string]any{"mode": string(g.Mode), "fields": g.Fields, "present": count})
		}
	}
}

// targetRule applies a gated rule to a field; an absent field is checked as
// its zero value so "MinLength(1) when online" reads as "required when
// online".
func (r *run) targetRule(sc *scope, tr ir.TargetRule) {
	var f *ir.FieldNode
	for i := range sc.obj.Fields {
		if sc.obj.Fields[i].Name == tr.Field {
			f = &sc.obj.Fields[i]
		}
	}
	if f == nil {
		return
	}
	val, present := fieldValue(sc.val, *f)
	if !present {
		val = zeroFor(f.Type)
	}
	val, isNil := deref(val)
	if isNil || kindMismatch(f.Type, val) != "" {
		// The field's own walk reported the null or the wrong shape; a gated
		// rule on top of that would be noise.
		return
	}
	r.rules(sc.path+"."+wireName(f), []ir.Rule{tr.Rule}, val, sc)
}

// transformKinds is the closed set the interpreter executes; anything
// else is refused at New.
var transformKinds = map[ir.TransformKind]struct{}{
	ir.TransformTrim: {}, ir.TransformLower: {}, ir.TransformUpper: {}, ir.TransformDefault: {},
	ir.TransformCoerce: {}, ir.TransformStrict: {}, ir.TransformNormalizeEmail: {},
}

// shouldCoerce resolves the per-field markers against the validator's
// mode. Precedence: field marker (coerce / strict) > call option > the
// marked-only default. Both markers on one field is a schema bug; strict
// wins so the careless path is the correct one.
func (v *Validator) shouldCoerce(f *ir.FieldNode) bool {
	marked, strict := false, false
	for _, t := range f.Transforms {
		if t.Phase != ir.PhasePre {
			continue
		}
		switch t.Kind {
		case ir.TransformCoerce:
			marked = true
		case ir.TransformStrict:
			strict = true
		}
	}
	switch {
	case strict:
		return false
	case marked:
		return true
	default:
		return v.coerce == CoerceLax
	}
}

// transform applies pre-phase transforms, writing back when the target is
// settable and always returning the value to validate.
//
// String transforms rewrite a string in place. Coerce only has work to do
// on map input, where JSON or form decoding left a string for a numeric,
// boolean, time or duration node: the converted value replaces the entry.
// A struct field already has the right type, so coerce is a no-op there —
// the same reason generated code emits nothing for it.
func (r *run) transform(parent reflect.Value, f ir.FieldNode, child reflect.Value) reflect.Value {
	if len(f.Transforms) == 0 && r.v.coerce != CoerceLax {
		return child
	}
	inner, isNil := deref(child)
	if isNil {
		return child
	}
	if inner.Kind() == reflect.String && r.v.shouldCoerce(&f) && isStringMap(parent) && parent.Type() == anyMapType {
		if coerced, ok := rules.Coerce(f.Type.Kind(), inner.String()); ok {
			out := reflect.ValueOf(coerced)
			parent.SetMapIndex(reflect.ValueOf(wireName(&f)), out)
			return out
		}
	}
	if inner.Kind() != reflect.String {
		return child
	}
	s := inner.String()
	for _, t := range f.Transforms {
		if t.Phase != ir.PhasePre {
			continue
		}
		s = applyStringTransform(t.Kind, s)
	}
	if s == inner.String() {
		return child
	}
	out := reflect.ValueOf(s).Convert(inner.Type())
	if inner.CanSet() {
		inner.Set(out)
		return child
	}
	if isStringMap(parent) {
		parent.SetMapIndex(reflect.ValueOf(wireName(&f)), out)
	}
	return out
}
