package vb

import (
	"context"
	"reflect"
	"time"

	"github.com/ubgo/validator/ir"
)

// Scalar type builders. Each is an immutable value: every rule method
// returns a copy with the rule appended, so a shared base builder
// (emailish := vb.String().Email()) can be extended in two places without
// the two schemas leaking rules into each other.

// appendRule copies before appending so builders stay value-immutable.
func appendRule(rules []ir.Rule, r ir.Rule) []ir.Rule {
	out := make([]ir.Rule, 0, len(rules)+1)
	out = append(out, rules...)
	return append(out, r)
}

// setMsg stamps an error code and message on the LAST rule — the zod
// convention of .min(3, "too short") expressed as a trailing call.
// Panics when there is no rule to annotate: that is a programming error at
// schema-declaration time, not a runtime condition.
func setMsg(rules []ir.Rule, code, message string) []ir.Rule {
	if len(rules) == 0 {
		panic("vb: Msg called before any rule")
	}
	out := append([]ir.Rule{}, rules...)
	last := &out[len(out)-1]
	last.Meta.Code = code
	last.Meta.Message = message
	return out
}

// register adds a function body under name; a nil registry is created on
// demand so zero-value builders work.
func register[V any](reg *registry, name string, fn func(context.Context, V) error) *registry {
	next := newRegistry()
	if reg != nil {
		_ = next.merge(reg)
	}
	next.funcs[name] = funcEntry{fn: typed(name, fn), raw: fn, id: nextFuncID.Add(1)}
	return next
}

// ---------------------------------------------------------------- string

// StringType builds a string value.
type StringType struct {
	node ir.StringNode
	reg  *registry
}

// String starts a string builder.
func String() *StringType { return &StringType{reg: newRegistry()} }

func (t *StringType) rule(r ir.Rule) *StringType {
	c := *t
	c.node.Rules = appendRule(t.node.Rules, r)
	return &c
}

// Node implements [Builder].
func (t *StringType) Node() ir.TypeNode   { n := t.node; return &n }
func (t *StringType) registry() *registry { return t.reg }
func (t *StringType) phantom(string)      {}

// Nullable allows null.
func (t *StringType) Nullable() *StringType { c := *t; c.node.Nullable = true; return &c }

// Describe sets the JSON Schema description.
func (t *StringType) Describe(s string) *StringType { c := *t; c.node.Meta.Description = s; return &c }

// Msg sets the error code and message of the last rule.
func (t *StringType) Msg(code, message string) *StringType {
	c := *t
	c.node.Rules = setMsg(t.node.Rules, code, message)
	return &c
}

// MinLength requires len >= n.
func (t *StringType) MinLength(n int) *StringType { return t.rule(ir.MinLength(n)) }

// MaxLength requires len <= n.
func (t *StringType) MaxLength(n int) *StringType { return t.rule(ir.MaxLength(n)) }

// Pattern requires a regex match (RE2 ∩ ECMA-262 subset for portability).
func (t *StringType) Pattern(re string) *StringType { return t.rule(ir.Pattern(re)) }

// Email requires an email address.
func (t *StringType) Email() *StringType { return t.rule(ir.Email()) }

// URL requires an absolute URL.
func (t *StringType) URL() *StringType { return t.rule(ir.URL()) }

// UUID requires a UUID.
func (t *StringType) UUID() *StringType { return t.rule(ir.UUID()) }

// ULID requires a ULID.
func (t *StringType) ULID() *StringType { return t.rule(ir.ULID()) }

// IPv4 requires a dotted-quad IPv4 address.
func (t *StringType) IPv4() *StringType { return t.rule(ir.IPv4()) }

// IPv6 requires an IPv6 address.
func (t *StringType) IPv6() *StringType { return t.rule(ir.IPv6()) }

// IP requires an IPv4 or IPv6 address.
func (t *StringType) IP() *StringType { return t.rule(ir.IP()) }

// CIDR requires a CIDR block.
func (t *StringType) CIDR() *StringType { return t.rule(ir.CIDR()) }

// MAC requires a 48-bit hardware address.
func (t *StringType) MAC() *StringType { return t.rule(ir.MAC()) }

// Hostname requires an RFC 1123 hostname.
func (t *StringType) Hostname() *StringType { return t.rule(ir.Hostname()) }

// Base64 requires padded standard base64.
func (t *StringType) Base64() *StringType { return t.rule(ir.Base64()) }

// JSON requires the string to parse as JSON.
func (t *StringType) JSON() *StringType { return t.rule(ir.JSON()) }

// Semver requires a Semantic Versioning 2.0.0 version.
func (t *StringType) Semver() *StringType { return t.rule(ir.Semver()) }

// Date requires YYYY-MM-DD.
func (t *StringType) Date() *StringType { return t.rule(ir.Date()) }

// DateTime requires an RFC 3339 date-time string.
func (t *StringType) DateTime() *StringType { return t.rule(ir.DateTime()) }

// NotBlank requires a non-whitespace character.
func (t *StringType) NotBlank() *StringType { return t.rule(ir.NotBlank()) }

// CreditCard requires a Luhn-valid card number.
func (t *StringType) CreditCard() *StringType { return t.rule(ir.CreditCard()) }

// JWT requires the three-segment JWT shape.
func (t *StringType) JWT() *StringType { return t.rule(ir.JWT()) }

// CountryCode requires an ISO 3166-1 alpha-2 shaped code.
func (t *StringType) CountryCode() *StringType { return t.rule(ir.CountryCode()) }

// CurrencyCode requires an ISO 4217 shaped code.
func (t *StringType) CurrencyCode() *StringType { return t.rule(ir.CurrencyCode()) }

// HexColor requires #RGB, #RRGGBB or #RRGGBBAA.
func (t *StringType) HexColor() *StringType { return t.rule(ir.HexColor()) }

// E164 requires an E.164 phone number.
func (t *StringType) E164() *StringType { return t.rule(ir.E164()) }

// Enum requires one of values.
func (t *StringType) Enum(values ...string) *StringType {
	anys := make([]any, len(values))
	for i, v := range values {
		anys[i] = v
	}
	return t.rule(ir.Enum(anys...))
}

// Const requires exactly v.
func (t *StringType) Const(v string) *StringType { return t.rule(ir.Const(v)) }

// Custom registers fn under name and records an opaque, non-portable rule.
func (t *StringType) Custom(name string, fn func(context.Context, string) error) *StringType {
	c := t.rule(ir.Custom(name))
	c.reg = register(t.reg, name, fn)
	return c
}

// Async registers a server-side rule; endpoint is where the frontend
// round-trips (empty lets the generator derive it).
func (t *StringType) Async(name, endpoint string, fn func(context.Context, string) error) *StringType {
	c := t.rule(ir.Async(name, endpoint))
	c.reg = register(t.reg, name, fn)
	return c
}

// ---------------------------------------------------------------- int

// Integer is the set of Go integer types an [IntType] can describe.
type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// IntType builds an integer value of Go type N. Bits and signedness are
// derived from N so the emitter can pick the right JSON Schema range.
type IntType[N Integer] struct {
	node ir.IntNode
	reg  *registry
}

// Int starts an integer builder for N.
func Int[N Integer]() *IntType[N] {
	t := &IntType[N]{reg: newRegistry()}
	rt := reflect.TypeOf(*new(N))
	switch rt.Kind() {
	case reflect.Int, reflect.Uint:
		// Platform-sized: Bits 0 means "the platform int" in the IR.
	default:
		t.node.Bits = rt.Bits()
	}
	switch rt.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		t.node.Unsigned = true
	}
	return t
}

func (t *IntType[N]) rule(r ir.Rule) *IntType[N] {
	c := *t
	c.node.Rules = appendRule(t.node.Rules, r)
	return &c
}

// Node implements [Builder].
func (t *IntType[N]) Node() ir.TypeNode   { n := t.node; return &n }
func (t *IntType[N]) registry() *registry { return t.reg }
func (t *IntType[N]) phantom(N)           {}

// Nullable allows null.
func (t *IntType[N]) Nullable() *IntType[N] { c := *t; c.node.Nullable = true; return &c }

// Describe sets the JSON Schema description.
func (t *IntType[N]) Describe(s string) *IntType[N] {
	c := *t
	c.node.Meta.Description = s
	return &c
}

// Msg sets the error code and message of the last rule.
func (t *IntType[N]) Msg(code, message string) *IntType[N] {
	c := *t
	c.node.Rules = setMsg(t.node.Rules, code, message)
	return &c
}

// Min requires v >= n.
func (t *IntType[N]) Min(n N) *IntType[N] { return t.rule(ir.Min(float64(n))) }

// Max requires v <= n.
func (t *IntType[N]) Max(n N) *IntType[N] { return t.rule(ir.Max(float64(n))) }

// Gt requires v > n.
func (t *IntType[N]) Gt(n N) *IntType[N] { return t.rule(ir.Gt(float64(n))) }

// Gte requires v >= n.
func (t *IntType[N]) Gte(n N) *IntType[N] { return t.rule(ir.Gte(float64(n))) }

// Lt requires v < n.
func (t *IntType[N]) Lt(n N) *IntType[N] { return t.rule(ir.Lt(float64(n))) }

// Lte requires v <= n.
func (t *IntType[N]) Lte(n N) *IntType[N] { return t.rule(ir.Lte(float64(n))) }

// MultipleOf requires v to be a multiple of n.
func (t *IntType[N]) MultipleOf(n N) *IntType[N] { return t.rule(ir.MultipleOf(float64(n))) }

// Enum requires one of values.
func (t *IntType[N]) Enum(values ...N) *IntType[N] {
	anys := make([]any, len(values))
	for i, v := range values {
		anys[i] = v
	}
	return t.rule(ir.Enum(anys...))
}

// Const requires exactly v.
func (t *IntType[N]) Const(v N) *IntType[N] { return t.rule(ir.Const(v)) }

// Custom registers fn under name and records an opaque, non-portable rule.
func (t *IntType[N]) Custom(name string, fn func(context.Context, N) error) *IntType[N] {
	c := t.rule(ir.Custom(name))
	c.reg = register(t.reg, name, fn)
	return c
}

// Async registers a server-side rule.
func (t *IntType[N]) Async(name, endpoint string, fn func(context.Context, N) error) *IntType[N] {
	c := t.rule(ir.Async(name, endpoint))
	c.reg = register(t.reg, name, fn)
	return c
}

// ---------------------------------------------------------------- float

// Floating is the set of Go float types a [FloatType] can describe.
type Floating interface{ ~float32 | ~float64 }

// FloatType builds a floating-point value of Go type F.
type FloatType[F Floating] struct {
	node ir.FloatNode
	reg  *registry
}

// Float starts a float builder for F.
func Float[F Floating]() *FloatType[F] {
	return &FloatType[F]{reg: newRegistry(), node: ir.FloatNode{Bits: reflect.TypeOf(*new(F)).Bits()}}
}

func (t *FloatType[F]) rule(r ir.Rule) *FloatType[F] {
	c := *t
	c.node.Rules = appendRule(t.node.Rules, r)
	return &c
}

// Node implements [Builder].
func (t *FloatType[F]) Node() ir.TypeNode   { n := t.node; return &n }
func (t *FloatType[F]) registry() *registry { return t.reg }
func (t *FloatType[F]) phantom(F)           {}

// Nullable allows null.
func (t *FloatType[F]) Nullable() *FloatType[F] { c := *t; c.node.Nullable = true; return &c }

// Describe sets the JSON Schema description.
func (t *FloatType[F]) Describe(s string) *FloatType[F] {
	c := *t
	c.node.Meta.Description = s
	return &c
}

// Msg sets the error code and message of the last rule.
func (t *FloatType[F]) Msg(code, message string) *FloatType[F] {
	c := *t
	c.node.Rules = setMsg(t.node.Rules, code, message)
	return &c
}

// Min requires v >= n.
func (t *FloatType[F]) Min(n F) *FloatType[F] { return t.rule(ir.Min(float64(n))) }

// Max requires v <= n.
func (t *FloatType[F]) Max(n F) *FloatType[F] { return t.rule(ir.Max(float64(n))) }

// Gt requires v > n.
func (t *FloatType[F]) Gt(n F) *FloatType[F] { return t.rule(ir.Gt(float64(n))) }

// Gte requires v >= n.
func (t *FloatType[F]) Gte(n F) *FloatType[F] { return t.rule(ir.Gte(float64(n))) }

// Lt requires v < n.
func (t *FloatType[F]) Lt(n F) *FloatType[F] { return t.rule(ir.Lt(float64(n))) }

// Lte requires v <= n.
func (t *FloatType[F]) Lte(n F) *FloatType[F] { return t.rule(ir.Lte(float64(n))) }

// MultipleOf requires v to be a multiple of n.
func (t *FloatType[F]) MultipleOf(n F) *FloatType[F] { return t.rule(ir.MultipleOf(float64(n))) }

// Latitude requires a value in [-90, 90].
func (t *FloatType[F]) Latitude() *FloatType[F] { return t.rule(ir.Latitude()) }

// Longitude requires a value in [-180, 180].
func (t *FloatType[F]) Longitude() *FloatType[F] { return t.rule(ir.Longitude()) }

// Custom registers fn under name and records an opaque, non-portable rule.
func (t *FloatType[F]) Custom(name string, fn func(context.Context, F) error) *FloatType[F] {
	c := t.rule(ir.Custom(name))
	c.reg = register(t.reg, name, fn)
	return c
}

// ---------------------------------------------------------------- bool

// BoolType builds a boolean value.
type BoolType struct {
	node ir.BoolNode
	reg  *registry
}

// Bool starts a boolean builder.
func Bool() *BoolType { return &BoolType{reg: newRegistry()} }

func (t *BoolType) rule(r ir.Rule) *BoolType {
	c := *t
	c.node.Rules = appendRule(t.node.Rules, r)
	return &c
}

// Node implements [Builder].
func (t *BoolType) Node() ir.TypeNode   { n := t.node; return &n }
func (t *BoolType) registry() *registry { return t.reg }
func (t *BoolType) phantom(bool)        {}

// Nullable allows null.
func (t *BoolType) Nullable() *BoolType { c := *t; c.node.Nullable = true; return &c }

// Describe sets the JSON Schema description.
func (t *BoolType) Describe(s string) *BoolType { c := *t; c.node.Meta.Description = s; return &c }

// Msg sets the error code and message of the last rule.
func (t *BoolType) Msg(code, message string) *BoolType {
	c := *t
	c.node.Rules = setMsg(t.node.Rules, code, message)
	return &c
}

// Const requires exactly v — the "must accept the terms" rule.
func (t *BoolType) Const(v bool) *BoolType { return t.rule(ir.Const(v)) }

// Custom registers fn under name and records an opaque, non-portable rule.
func (t *BoolType) Custom(name string, fn func(context.Context, bool) error) *BoolType {
	c := t.rule(ir.Custom(name))
	c.reg = register(t.reg, name, fn)
	return c
}

// ---------------------------------------------------------------- bytes

// BytesType builds a []byte value (base64 on the wire).
type BytesType struct {
	node ir.BytesNode
	reg  *registry
}

// Bytes starts a byte-slice builder.
func Bytes() *BytesType { return &BytesType{reg: newRegistry()} }

func (t *BytesType) rule(r ir.Rule) *BytesType {
	c := *t
	c.node.Rules = appendRule(t.node.Rules, r)
	return &c
}

// Node implements [Builder].
func (t *BytesType) Node() ir.TypeNode   { n := t.node; return &n }
func (t *BytesType) registry() *registry { return t.reg }
func (t *BytesType) phantom([]byte)      {}

// Nullable allows null.
func (t *BytesType) Nullable() *BytesType { c := *t; c.node.Nullable = true; return &c }

// Describe sets the JSON Schema description.
func (t *BytesType) Describe(s string) *BytesType { c := *t; c.node.Meta.Description = s; return &c }

// Msg sets the error code and message of the last rule.
func (t *BytesType) Msg(code, message string) *BytesType {
	c := *t
	c.node.Rules = setMsg(t.node.Rules, code, message)
	return &c
}

// MinLength requires len >= n bytes.
func (t *BytesType) MinLength(n int) *BytesType { return t.rule(ir.MinLength(n)) }

// MaxLength requires len <= n bytes.
func (t *BytesType) MaxLength(n int) *BytesType { return t.rule(ir.MaxLength(n)) }

// Custom registers fn under name and records an opaque, non-portable rule.
func (t *BytesType) Custom(name string, fn func(context.Context, []byte) error) *BytesType {
	c := t.rule(ir.Custom(name))
	c.reg = register(t.reg, name, fn)
	return c
}

// ---------------------------------------------------------------- time

// TimeType builds a time.Time value.
type TimeType struct {
	node ir.TimeNode
	reg  *registry
}

// Time starts a time builder.
func Time() *TimeType { return &TimeType{reg: newRegistry()} }

func (t *TimeType) rule(r ir.Rule) *TimeType {
	c := *t
	c.node.Rules = appendRule(t.node.Rules, r)
	return &c
}

// Node implements [Builder].
func (t *TimeType) Node() ir.TypeNode   { n := t.node; return &n }
func (t *TimeType) registry() *registry { return t.reg }
func (t *TimeType) phantom(time.Time)   {}

// Nullable allows null.
func (t *TimeType) Nullable() *TimeType { c := *t; c.node.Nullable = true; return &c }

// Describe sets the JSON Schema description.
func (t *TimeType) Describe(s string) *TimeType { c := *t; c.node.Meta.Description = s; return &c }

// Msg sets the error code and message of the last rule.
func (t *TimeType) Msg(code, message string) *TimeType {
	c := *t
	c.node.Rules = setMsg(t.node.Rules, code, message)
	return &c
}

// After requires this time to be later than the sibling field. Taking a
// [FieldRef] rather than a string keeps the reference rename-safe.
func After[T any](other FieldRef[T, time.Time]) func(*TimeType) *TimeType {
	return func(t *TimeType) *TimeType { return t.rule(ir.After(other.name)) }
}

// Before requires this time to be earlier than the sibling field.
func Before[T any](other FieldRef[T, time.Time]) func(*TimeType) *TimeType {
	return func(t *TimeType) *TimeType { return t.rule(ir.Before(other.name)) }
}

// AfterField is [After] keyed by Go field name, for generated or migrated
// code that has no typed FieldRef for the sibling. Prefer [After] in
// hand-written schemas: a renamed field is a compile error there and a
// runtime Check failure here.
func AfterField(name string) func(*TimeType) *TimeType {
	return func(t *TimeType) *TimeType { return t.rule(ir.After(name)) }
}

// BeforeField is [Before] keyed by Go field name.
func BeforeField(name string) func(*TimeType) *TimeType {
	return func(t *TimeType) *TimeType { return t.rule(ir.Before(name)) }
}

// With applies a cross-field rule produced by [After] or [Before]. Go
// methods cannot take type parameters, so the rule is a function value the
// builder applies to itself: vb.Time().With(vb.After(Fields.Start)).
func (t *TimeType) With(rule func(*TimeType) *TimeType) *TimeType { return rule(t) }

// Custom registers fn under name and records an opaque, non-portable rule.
func (t *TimeType) Custom(name string, fn func(context.Context, time.Time) error) *TimeType {
	c := t.rule(ir.Custom(name))
	c.reg = register(t.reg, name, fn)
	return c
}

// ---------------------------------------------------------------- duration

// DurationType builds a time.Duration value.
type DurationType struct {
	node ir.DurationNode
	reg  *registry
}

// Duration starts a duration builder.
func Duration() *DurationType { return &DurationType{reg: newRegistry()} }

func (t *DurationType) rule(r ir.Rule) *DurationType {
	c := *t
	c.node.Rules = appendRule(t.node.Rules, r)
	return &c
}

// Node implements [Builder].
func (t *DurationType) Node() ir.TypeNode     { n := t.node; return &n }
func (t *DurationType) registry() *registry   { return t.reg }
func (t *DurationType) phantom(time.Duration) {}

// Nullable allows null.
func (t *DurationType) Nullable() *DurationType { c := *t; c.node.Nullable = true; return &c }

// Describe sets the JSON Schema description.
func (t *DurationType) Describe(s string) *DurationType {
	c := *t
	c.node.Meta.Description = s
	return &c
}

// Msg sets the error code and message of the last rule.
func (t *DurationType) Msg(code, message string) *DurationType {
	c := *t
	c.node.Rules = setMsg(t.node.Rules, code, message)
	return &c
}

// Min requires d >= n. Recorded in nanoseconds — the IR's numeric form.
func (t *DurationType) Min(n time.Duration) *DurationType { return t.rule(ir.Min(float64(n))) }

// Max requires d <= n.
func (t *DurationType) Max(n time.Duration) *DurationType { return t.rule(ir.Max(float64(n))) }

// Gt requires d > n.
func (t *DurationType) Gt(n time.Duration) *DurationType { return t.rule(ir.Gt(float64(n))) }

// Lt requires d < n.
func (t *DurationType) Lt(n time.Duration) *DurationType { return t.rule(ir.Lt(float64(n))) }

// Custom registers fn under name and records an opaque, non-portable rule.
func (t *DurationType) Custom(name string, fn func(context.Context, time.Duration) error) *DurationType {
	c := t.rule(ir.Custom(name))
	c.reg = register(t.reg, name, fn)
	return c
}
