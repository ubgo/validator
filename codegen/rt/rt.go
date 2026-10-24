// Package rt is the runtime generated validators import. It is
// deliberately tiny: generated code inlines every type check and comparison
// it can, and calls here only for the semantics that must stay identical
// to the interpreter (formats, uniqueness, normalized equality, ordered
// comparison, issue rendering) and for the one thing it cannot inline —
// looking a custom or async rule body up by name.
//
// Everything here delegates to the same internal package the interpreter
// uses, so the conformance suite compares two front-ends of one
// implementation, not two implementations.
package rt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ubgo/validator/internal/rules"
	"github.com/ubgo/validator/interp"
	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/vb"
	"github.com/ubgo/validator/verr"
)

// Registry resolves custom and async rule bodies by name. *vb.Schema
// satisfies it.
type Registry interface {
	Func(name string) (vb.Func, bool)
}

// ErrNoRegistry is returned when a generated validator meets a custom rule
// but was given a nil registry.
var ErrNoRegistry = errors.New("rt: custom rule needs a registry")

// ErrUnregistered wraps the rule name when no body is registered under it.
type ErrUnregistered struct{ Name string }

func (e *ErrUnregistered) Error() string { return "rt: rule " + e.Name + " is not registered" }

// Fail appends a rendered issue.
func Fail(errs *verr.Errors, code, path, tmpl, rule string, params map[string]any) {
	errs.Add(verr.Issue{Code: code, Path: path, Rule: rule, Message: verr.Render(tmpl, params), Params: params})
}

// Required appends the structural "field is required" issue.
func Required(errs *verr.Errors, path string) {
	errs.Add(verr.Issue{Code: verr.CodeRequired, Path: path, Message: "field is required"})
}

// Null appends the structural issue for a nil where null is not allowed.
func Null(errs *verr.Errors, path string) {
	errs.Add(verr.Issue{Code: verr.CodeRequired, Path: path, Message: "value is required"})
}

// Group appends a group-gate failure with the interpreter's exact wording.
func Group(errs *verr.Errors, path string, mode ir.GroupMode, fields []string, present int) {
	errs.Add(verr.Issue{
		Code: verr.CodeGroup, Path: path,
		Message: string(mode) + " of " + sprintFields(fields) + " required, " + itoa(present) + " present",
		Params:  map[string]any{"mode": string(mode), "fields": fields, "present": present},
	})
}

// Call runs a registered rule body. A missing registry or name is an
// error the caller reports as a custom-rule failure, never a pass.
func Call(ctx context.Context, reg Registry, name string, value any) error {
	if reg == nil {
		return ErrNoRegistry
	}
	fn, ok := reg.Func(name)
	if !ok {
		return &ErrUnregistered{Name: name}
	}
	return fn(ctx, value)
}

// Cond runs a registered predicate as a condition: true when it returns
// nil, false on any error (including a missing body).
func Cond(ctx context.Context, reg Registry, name string, value any) bool {
	return Call(ctx, reg, name, value) == nil
}

// ExprRegistry is the optional upgrade a registry offers when the schema
// it serves contains CEL expressions. Generated code hands CEL gates to
// the interpreter (one implementation of the language, not two), and the
// interpreter needs the engine; this is how it gets there without the
// generated package importing an expression library it may not use.
//
//	type Registry struct{ *vb.Schema[Order] }
//	func (Registry) ExprCompiler() interp.ExprCompiler { return cel.New() }
type ExprRegistry interface {
	ExprCompiler() interp.ExprCompiler
}

// TypedRegistry is the optional upgrade a registry offers when it kept the
// author's function with its real signature. *vb.Schema does.
type TypedRegistry interface {
	TypedFunc(name string) (any, bool)
}

// CallT runs a registered rule body with a statically typed value. When the
// registry kept the typed function and its parameter type is exactly T, it
// is called directly — no boxing of value, no assertion inside the body —
// which is what keeps a custom rule off the allocation count on the
// generated success path. Anything else falls back to [Call], so a type
// mismatch is still reported as a TypeError, never silently passed.
func CallT[T any](ctx context.Context, reg Registry, name string, value T) error {
	if tr, ok := reg.(TypedRegistry); ok {
		if raw, found := tr.TypedFunc(name); found {
			if fn, matches := raw.(func(context.Context, T) error); matches {
				return fn(ctx, value)
			}
		}
	}
	return Call(ctx, reg, name, value)
}

// CondT is [Cond] over the typed path.
func CondT[T any](ctx context.Context, reg Registry, name string, value T) bool {
	return CallT(ctx, reg, name, value) == nil
}

// Present is the interpreter's presence rule for values whose static type
// is an interface: nil-able kinds non-nil, strings non-empty, zero time
// absent, everything else present.
func Present(v any) bool {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return false
	}
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map:
		return !rv.IsNil()
	case reflect.String:
		return rv.Len() > 0
	}
	if t, ok := v.(time.Time); ok {
		return !t.IsZero()
	}
	return true
}

// Email, URL, UUID and ULID are the shared format checks.
func Email(s string) bool { return rules.Email(s) }
func URL(s string) bool   { return rules.URL(s) }
func UUID(s string) bool  { return rules.UUID(s) }
func ULID(s string) bool  { return rules.ULID(s) }

// Additional string formats, shared with the interpreter.
func IPv4(s string) bool         { return rules.IPv4(s) }
func IPv6(s string) bool         { return rules.IPv6(s) }
func IP(s string) bool           { return rules.IP(s) }
func CIDR(s string) bool         { return rules.CIDR(s) }
func MAC(s string) bool          { return rules.MAC(s) }
func Hostname(s string) bool     { return rules.Hostname(s) }
func Base64(s string) bool       { return rules.Base64(s) }
func JSON(s string) bool         { return rules.JSON(s) }
func Semver(s string) bool       { return rules.Semver(s) }
func Date(s string) bool         { return rules.Date(s) }
func DateTime(s string) bool     { return rules.DateTime(s) }
func NotBlank(s string) bool     { return rules.NotBlank(s) }
func CreditCard(s string) bool   { return rules.CreditCard(s) }
func JWT(s string) bool          { return rules.JWT(s) }
func CountryCode(s string) bool  { return rules.CountryCode(s) }
func CurrencyCode(s string) bool { return rules.CurrencyCode(s) }
func HexColor(s string) bool     { return rules.HexColor(s) }
func E164(s string) bool         { return rules.E164(s) }
func Timezone(s string) bool     { return rules.Timezone(s) }
func Base64URL(s string) bool    { return rules.Base64URL(s) }
func ISBN(s string) bool         { return rules.ISBN(s) }
func BIC(s string) bool          { return rules.BIC(s) }
func MongoID(s string) bool      { return rules.MongoID(s) }
func Latitude(f float64) bool    { return rules.Latitude(f) }
func Longitude(f float64) bool   { return rules.Longitude(f) }

// MultipleOf is the shared epsilon-tolerant multiple check.
func MultipleOf(a, b float64) bool { return rules.MultipleOf(a, b) }

// RuneLen is the shared string length measure.
func RuneLen(s string) int { return rules.RuneLen(s) }

// Unique is the shared distinctness check over any slice or array.
func Unique(v any) bool { return rules.Unique(reflect.ValueOf(v)) }

// In is the shared normalized membership test.
func In(v any, values []any) bool { return rules.In(v, values) }

// Eq is the shared normalized equality.
func Eq(a, b any) bool { return rules.Norm(a) == rules.Norm(b) }

// Compare is the shared ordered comparison for conditions.
func Compare(a, b any, op ir.CmpOp) bool {
	return rules.Compare(reflect.ValueOf(a), reflect.ValueOf(b), op)
}

func sprintFields(fields []string) string {
	return "[" + strings.Join(fields, " ") + "]"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// Path is a lazily rendered JSONPath. Generated code builds one per
// nesting level on the stack and only calls String on failure, so a valid
// value costs no path allocations at all — the interpreter's `path + "."`
// concatenation per field was where the success path spent its allocs.
//
// A zero Path is the root and renders as "$".
type Path struct {
	parent *Path
	kind   pathKind
	lit    string
	idx    int
}

type pathKind uint8

const (
	pathRoot pathKind = iota
	pathField
	pathIndex
	pathKey
	pathKeyInt
)

// Field returns the child path for a struct field; lit includes the dot
// (".email") so rendering is a plain append.
func (p *Path) Field(lit string) Path { return Path{parent: p, kind: pathField, lit: lit} }

// Index returns the child path for an array element.
func (p *Path) Index(i int) Path { return Path{parent: p, kind: pathIndex, idx: i} }

// KeyStr returns the child path for a map entry with a string key. No
// allocation: the key's string header is copied, not rendered.
func (p *Path) KeyStr(k string) Path { return Path{parent: p, kind: pathKey, lit: k} }

// KeyInt returns the child path for a map entry with an integer key.
func (p *Path) KeyInt(k int) Path { return Path{parent: p, kind: pathKeyInt, idx: k} }

// Key returns the child path for a map entry of any other key type. The
// key is rendered eagerly, which allocates; generated code prefers KeyStr
// and KeyInt and only reaches for this on exotic key types.
func (p *Path) Key(k any) Path { return Path{parent: p, kind: pathKey, lit: fmt.Sprint(k)} }

// String renders the full path. Called only when an issue is produced.
// It recurses instead of collecting the chain into a slice: storing the
// parent pointers would make every Path escape to the heap even on the
// success path, which is the allocation this type exists to avoid.
func (p *Path) String() string {
	var b strings.Builder
	p.write(&b)
	return b.String()
}

func (p *Path) write(b *strings.Builder) {
	if p == nil {
		b.WriteString("$")
		return
	}
	if p.parent == nil {
		b.WriteString("$")
	} else {
		p.parent.write(b)
	}
	switch p.kind {
	case pathField:
		b.WriteString(p.lit)
	case pathIndex:
		b.WriteString("[")
		b.WriteString(itoa(p.idx))
		b.WriteString("]")
	case pathKey:
		b.WriteString(".")
		b.WriteString(p.lit)
	case pathKeyInt:
		b.WriteString(".")
		b.WriteString(itoa(p.idx))
	}
}

// smallUnique is the slice length up to which UniqueOf compares pairwise
// instead of building a set: for a handful of tags the quadratic scan is
// faster than a map allocation and keeps the success path allocation-free.
const smallUnique = 16

// UniqueOf is the distinctness check for slices of comparable elements
// without boxing; generated code uses it whenever the element type allows.
func UniqueOf[T comparable](s []T) bool {
	if len(s) < 2 {
		return true
	}
	if len(s) <= smallUnique {
		for i := range s {
			for j := i + 1; j < len(s); j++ {
				if s[i] == s[j] {
					return false
				}
			}
		}
		return true
	}
	seen := make(map[T]struct{}, len(s))
	for _, v := range s {
		if _, dup := seen[v]; dup {
			return false
		}
		seen[v] = struct{}{}
	}
	return true
}

// Probes for union variants. A union value is statically `any`; each
// variant is tried in order and the first one whose Go type matches AND
// whose rules pass accepts the value, mirroring the interpreter's probe.

// AsString reports a string value.
func AsString(v any) (string, bool) { s, ok := v.(string); return s, ok }

// AsIntegral reports a value an integer node accepts: any Go integer, or a
// float carrying a whole number (JSON has no integer type).
func AsIntegral(v any) (float64, bool) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		if f == math.Trunc(f) && !math.IsInf(f, 0) {
			return f, true
		}
	}
	return 0, false
}

// AsNumber reports any numeric value as float64.
func AsNumber(v any) (float64, bool) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return 0, false
}

// Union appends the interpreter's no-variant-matched issue.
func Union(errs *verr.Errors, path string, variants int) {
	errs.Add(verr.Issue{Code: verr.CodeUnion, Path: path, Message: "value matches no variant", Params: map[string]any{"variants": variants}})
}

// Variant appends the interpreter's unknown-discriminator issue.
func Variant(errs *verr.Errors, path, discriminator, tag string) {
	errs.Add(verr.Issue{Code: verr.CodeVariant, Path: path, Message: "unknown variant " + strconv.Quote(tag),
		Params: map[string]any{"discriminator": discriminator, "value": tag}})
}

// Transforms shared with the interpreter.

// Trim, Lower, Upper and NormalizeEmail are the pre-phase string transforms.
func Trim(s string) string           { return strings.TrimSpace(s) }
func Lower(s string) string          { return strings.ToLower(s) }
func Upper(s string) string          { return strings.ToUpper(s) }
func NormalizeEmail(s string) string { return rules.NormalizeEmail(s) }

// AsStruct matches a union value against a concrete struct type, by value
// or by pointer, returning a pointer for the variant's validator.
func AsStruct[T any](v any) (*T, bool) {
	switch x := v.(type) {
	case T:
		return &x, true
	case *T:
		if x == nil {
			return nil, false
		}
		return x, true
	}
	return nil, false
}

// Ptr returns a pointer to v; generated code uses it for pointer-typed
// defaults.
func Ptr[T any](v T) *T { return &v }

// Interpreter fallback. Generated code compiles every shape it knows how
// to; for the rest (a discriminated union on an interface-typed field, a
// union with collection variants) it embeds that subtree's IR and hands
// the value to the interpreter at that path. The delegation is explicit
// in the generated source, so a reader can see which fields run slower —
// nothing is dropped, nothing is silently reinterpreted.

// MustSchema parses an embedded IR document. It panics on failure because
// the document was written by the generator that also wrote the caller;
// a parse failure is a generator bug, not input.
func MustSchema(doc string) *ir.Schema {
	var s ir.Schema
	if err := json.Unmarshal([]byte(doc), &s); err != nil {
		panic("rt: embedded IR does not parse: " + err.Error())
	}
	if err := ir.Check(&s); err != nil {
		panic("rt: embedded IR fails Check: " + err.Error())
	}
	return &s
}

// interpreters caches compiled interpreters per (schema, registry) pair so
// a fallback compiles once per process, not once per validation. A
// registry whose dynamic type is not comparable (a map type) cannot key a
// cache; such callers pay a compile per call, which is correct and rare.
var interpreters sync.Map

type interpKey struct {
	schema *ir.Schema
	reg    Registry
}

// cacheable reports whether reg can be used in a map key.
func cacheable(reg Registry) bool {
	return reg == nil || reflect.TypeOf(reg).Comparable()
}

// Interpret validates value against an embedded subtree schema at path,
// through the interpreter, appending into errs. A schema the interpreter
// cannot run (it refuses the same things codegen refuses at generation
// time) is reported as one custom issue rather than swallowed.
func Interpret(ctx context.Context, reg Registry, s *ir.Schema, value any, path *Path, errs *verr.Errors) {
	key := interpKey{schema: s, reg: reg}
	var v any
	var ok bool
	if cacheable(reg) {
		v, ok = interpreters.Load(key)
	}
	if !ok {
		var opts []interp.Option
		if reg != nil {
			opts = append(opts, interp.WithFuncs(reg))
			if er, ok := reg.(ExprRegistry); ok {
				if c := er.ExprCompiler(); c != nil {
					opts = append(opts, interp.WithExpr(c))
				}
			}
		}
		compiled, err := interp.New(s, opts...)
		if err != nil {
			errs.Add(verr.Issue{Code: verr.CodeCustom, Path: path.String(), Message: "interpreter fallback unavailable: " + err.Error()})
			return
		}
		v = compiled
		if cacheable(reg) {
			v, _ = interpreters.LoadOrStore(key, compiled)
		}
	}
	v.(*interp.Validator).RunIntoAt(ctx, value, path.String(), errs)
}
