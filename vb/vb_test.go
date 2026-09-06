package vb

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ubgo/validator/ir"
)

// The Go types the golden fixture describes. Only field names and types
// matter; the fixture is ir/testdata/user.json.
type Address struct {
	Street string
	Zip    string
}

type Event struct {
	Kind    string
	URL     string
	Address Address
}

type User struct {
	Email    string
	Age      int32
	Score    float64
	Active   bool
	Avatar   []byte
	Start    time.Time
	End      time.Time
	TTL      time.Duration
	Tags     []string
	Labels   map[string]int
	Home     Address
	Extra    any
	IDOrName any
	Type     string
	URL      string
	Venue    string
	Coupon   string
	Event    Event
}

// Hand-written accessors in the shape Phase 5 will generate.
var addressFields = struct {
	Street FieldRef[Address, string]
	Zip    FieldRef[Address, string]
}{
	Street: NewField("Street", func(a Address) string { return a.Street }).JSON("street"),
	Zip:    NewField("Zip", func(a Address) string { return a.Zip }).JSON("zip"),
}

var eventFields = struct {
	Kind    FieldRef[Event, string]
	URL     FieldRef[Event, string]
	Address FieldRef[Event, Address]
}{
	Kind:    NewField("kind", func(e Event) string { return e.Kind }),
	URL:     NewField("url", func(e Event) string { return e.URL }),
	Address: NewField("address", func(e Event) Address { return e.Address }),
}

var userFields = struct {
	Email    FieldRef[User, string]
	Age      FieldRef[User, int32]
	Score    FieldRef[User, float64]
	Active   FieldRef[User, bool]
	Avatar   FieldRef[User, []byte]
	Start    FieldRef[User, time.Time]
	End      FieldRef[User, time.Time]
	TTL      FieldRef[User, time.Duration]
	Tags     FieldRef[User, []string]
	Labels   FieldRef[User, map[string]int]
	Home     FieldRef[User, Address]
	Extra    FieldRef[User, any]
	IDOrName FieldRef[User, any]
	Type     FieldRef[User, string]
	URL      FieldRef[User, string]
	Venue    FieldRef[User, string]
	Coupon   FieldRef[User, string]
	Event    FieldRef[User, Event]
}{
	Email:    NewField("Email", func(u User) string { return u.Email }).JSON("email"),
	Age:      NewField("Age", func(u User) int32 { return u.Age }).JSON("age"),
	Score:    NewField("Score", func(u User) float64 { return u.Score }).JSON("score"),
	Active:   NewField("Active", func(u User) bool { return u.Active }).JSON("active"),
	Avatar:   NewField("Avatar", func(u User) []byte { return u.Avatar }).JSON("avatar"),
	Start:    NewField("Start", func(u User) time.Time { return u.Start }).JSON("start"),
	End:      NewField("End", func(u User) time.Time { return u.End }).JSON("end"),
	TTL:      NewField("TTL", func(u User) time.Duration { return u.TTL }).JSON("ttl"),
	Tags:     NewField("Tags", func(u User) []string { return u.Tags }).JSON("tags"),
	Labels:   NewField("Labels", func(u User) map[string]int { return u.Labels }).JSON("labels"),
	Home:     NewField("Home", func(u User) Address { return u.Home }).JSON("home"),
	Extra:    NewField("Extra", func(u User) any { return u.Extra }).JSON("extra"),
	IDOrName: NewField("IDOrName", func(u User) any { return u.IDOrName }).JSON("idOrName"),
	Type:     NewField("Type", func(u User) string { return u.Type }).JSON("type"),
	URL:      NewField("URL", func(u User) string { return u.URL }).JSON("url"),
	Venue:    NewField("Venue", func(u User) string { return u.Venue }).JSON("venue"),
	Coupon:   NewField("Coupon", func(u User) string { return u.Coupon }).JSON("coupon"),
	Event:    NewField("Event", func(u User) Event { return u.Event }).JSON("event"),
}

func noTempDomains(_ context.Context, s string) error {
	if strings.HasSuffix(s, "@temp.com") {
		return errors.New("no temp emails")
	}
	return nil
}

func uniqueEmail(_ context.Context, _ string) error { return nil }

// userSchema rebuilds ir/testdata/user.json through the builder.
func userSchema(t *testing.T) *Schema[User] {
	t.Helper()
	address := Struct("Address",
		F(addressFields.Street, String().MinLength(1)),
		F(addressFields.Zip, String().Pattern(`^[0-9]{5}$`)),
	).MustBuild()

	s, err := Struct("User",
		F(userFields.Email, String().Email().
			Custom("no_temp_domains", noTempDomains).
			Async("unique_email", "/v/unique_email", uniqueEmail)).Trim().Lower(),
		F(userFields.Age, Int[int32]().Min(18).Max(120)),
		F(userFields.Score, Float[float64]().Gte(0).Lte(1)),
		F(userFields.Active, Bool()),
		F(userFields.Avatar, Bytes().Nullable()).Optional(),
		F(userFields.Start, Time()),
		F(userFields.End, Time().With(After(userFields.Start))),
		F(userFields.TTL, Duration().Gt(0)),
		F(userFields.Tags, Array(String().MinLength(1).Enum("a", "b")).MinItems(1).MaxItems(10).Unique()),
		F(userFields.Labels, Map(String(), Int[int]().MultipleOf(2))),
		F(userFields.Home, Ref(address)),
		F(userFields.Extra, Any[any]()),
		F(userFields.IDOrName, Union[any](Int[int](), String().UUID().ULID().URL().Const("x"))),
		F(userFields.Type, String().Enum("online", "offline")),
		F(userFields.URL, String().URL()).Optional(),
		F(userFields.Venue, String()).Optional(),
		F(userFields.Coupon, String()).Optional(),
		F(userFields.Event, OneOf[Event]("kind").
			Case("online", Object(
				F(eventFields.Kind, String().Const("online")),
				F(eventFields.URL, String().URL()),
			)).
			Case("offline", Object(
				F(eventFields.Kind, String().Const("offline")),
				F(eventFields.Address, Ref(address)),
			)).
			Default(Any[Event]())),
	).
		Source("github.com/acme/app/models.User").
		Generator("test").
		Extra(ir.ExtraForbid).
		Gate(
			When(Eq(userFields.Type, "online"), []ir.TargetRule{On(userFields.URL, ir.MinLength(1))}, []ir.TargetRule{On(userFields.Venue, ir.MinLength(1))}),
			Requires(ir.And(Has(userFields.Coupon), ir.Not(In(userFields.Type, "offline"))), userFields.Venue),
			Group(ir.ExactlyOne, userFields.URL, userFields.Venue),
			When(ir.Or(FieldCmp(userFields.End, ir.OpGt, userFields.Start), ir.Expr("end > start"), ir.CustomCond("weird")), []ir.TargetRule{On(userFields.TTL, ir.Lt(10))}, nil),
		).
		Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return s
}

// TestMatchesGolden is the conformance test between the builder and the IR
// package: the builder must reproduce ir/testdata/user.json byte for byte.
func TestMatchesGolden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "ir", "testdata", "user.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(userSchema(t).IR(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if string(got) != string(want) {
		t.Fatalf("builder output differs from golden\n--- got\n%s", got)
	}
}

func TestRegistryAndGetters(t *testing.T) {
	s := userSchema(t)
	if s.Name() != "User" {
		t.Errorf("name %q", s.Name())
	}
	funcs := s.Funcs()
	if len(funcs) != 2 || funcs[0].Name != "no_temp_domains" || funcs[1].Name != "unique_email" {
		t.Fatalf("funcs %+v", funcs)
	}
	fn, ok := s.Func("no_temp_domains")
	if !ok {
		t.Fatal("missing func")
	}
	if err := fn(context.Background(), "a@temp.com"); err == nil {
		t.Error("custom body not called")
	}
	err := fn(context.Background(), 42)
	var te *TypeError
	if !errors.As(err, &te) || te.Rule != "no_temp_domains" || te.Want != "string" || te.Got != "int" || !strings.Contains(err.Error(), "expected string, got int") {
		t.Errorf("typed wrapper must reject the wrong type with a TypeError: %v", err)
	}
	// Endpoints lists async rules only, at their declared or default path.
	eps := s.Endpoints()
	if len(eps) != 1 || eps[0].Rule != "unique_email" || eps[0].Path != "/v/unique_email" || eps[0].Func == nil {
		t.Errorf("endpoints %+v", eps)
	}
	if _, ok := s.Func("nope"); ok {
		t.Error("unknown func reported present")
	}
	get, ok := s.Getter("Age")
	if !ok || get(User{Age: 7}) != int32(7) {
		t.Error("getter wrong")
	}
	if _, ok := s.Getter("Nope"); ok {
		t.Error("unknown getter reported present")
	}
	if userFields.Age.Get(User{Age: 3}) != 3 || userFields.Age.Name() != "Age" {
		t.Error("FieldRef accessors wrong")
	}
}

func TestImmutability(t *testing.T) {
	base := String().MinLength(1)
	a := base.Email()
	b := base.URL()
	if n := len(ir.Rules(base.Node())); n != 1 {
		t.Errorf("base mutated: %d rules", n)
	}
	if ir.Rules(a.Node())[1].Kind != ir.RuleEmail || ir.Rules(b.Node())[1].Kind != ir.RuleURL {
		t.Error("derived builders share state")
	}
	arr := Array(String()).MinItems(1)
	if len(ir.Rules(arr.Unique().Node())) != 2 || len(ir.Rules(arr.Node())) != 1 {
		t.Error("array builder mutated")
	}
	obj := Object[Address]()
	if obj.Extra(ir.ExtraForbid).node.Extra != ir.ExtraForbid || obj.node.Extra != "" {
		t.Error("object builder mutated")
	}
	if !obj.Nullable().node.Nullable || obj.node.Nullable {
		t.Error("object nullable mutated")
	}
	one := OneOf[any]("k").Case("a", Any[any]())
	if len(one.Case("b", Any[any]()).cases) != 2 || len(one.cases) != 1 {
		t.Error("oneof builder mutated")
	}
	if one.Default(Any[any]()).def == nil || one.def != nil {
		t.Error("oneof default mutated")
	}
	f := F(addressFields.Street, String())
	if len(f.Trim().transforms) != 1 || len(f.transforms) != 0 {
		t.Error("field builder mutated")
	}
}

func TestMsg(t *testing.T) {
	n := String().MinLength(3).Msg("too_short", "at least 3").Node()
	r := ir.Rules(n)[0]
	if r.Meta.Code != "too_short" || r.Meta.Message != "at least 3" {
		t.Errorf("msg not applied: %+v", r.Meta)
	}
	defer func() {
		if recover() == nil {
			t.Error("Msg before any rule must panic")
		}
	}()
	String().Msg("x", "y")
}

func TestBuildErrors(t *testing.T) {
	// Two different bodies under one name conflict.
	_, err := Struct("A",
		F(addressFields.Street, String().Custom("f", func(context.Context, string) error { return nil })),
		F(addressFields.Zip, String().Custom("f", func(context.Context, string) error { return errors.New("x") })),
	).Build()
	if err == nil || !strings.Contains(err.Error(), `"f" registered twice`) {
		t.Errorf("conflict not reported: %v", err)
	}
	// The same registration reached twice is fine.
	shared := String().Custom("g", func(context.Context, string) error { return nil })
	if _, err := Struct("B", F(addressFields.Street, shared), F(addressFields.Zip, shared)).Build(); err != nil {
		t.Errorf("shared builder must not conflict: %v", err)
	}
	// A duplicate oneof case surfaces through the enclosing Build.
	_, err = Struct("C", F(userFields.Extra, OneOf[any]("k").Case("a", Any[any]()).Case("a", Any[any]()))).Build()
	if err == nil || !strings.Contains(err.Error(), `case "a" added twice`) {
		t.Errorf("duplicate case not reported: %v", err)
	}
	// A gate naming a field the struct lacks fails ir.Check.
	_, err = Struct("D", F(addressFields.Street, String())).Gate(Group(ir.ExactlyOne, addressFields.Street, userFields.Venue)).Build()
	if err == nil || !strings.Contains(err.Error(), `unknown field "Venue"`) {
		t.Errorf("bad gate not reported: %v", err)
	}
	// Ref to a schema that itself carries an error propagates.
	defer func() {
		if recover() == nil {
			t.Error("MustBuild must panic on error")
		}
	}()
	Struct("E", F(userFields.Extra, OneOf[any]("k"))).MustBuild()
}

func TestIntBitsAndSign(t *testing.T) {
	check := func(n ir.TypeNode, bits int, unsigned bool) {
		t.Helper()
		in := n.(*ir.IntNode)
		if in.Bits != bits || in.Unsigned != unsigned {
			t.Errorf("got bits=%d unsigned=%v want %d/%v", in.Bits, in.Unsigned, bits, unsigned)
		}
	}
	check(Int[int]().Node(), 0, false)
	check(Int[uint]().Node(), 0, true)
	check(Int[int8]().Node(), 8, false)
	check(Int[int16]().Node(), 16, false)
	check(Int[int32]().Node(), 32, false)
	check(Int[int64]().Node(), 64, false)
	check(Int[uint8]().Node(), 8, true)
	check(Int[uint16]().Node(), 16, true)
	check(Int[uint32]().Node(), 32, true)
	check(Int[uint64]().Node(), 64, true)
	if Float[float32]().Node().(*ir.FloatNode).Bits != 32 {
		t.Error("float32 bits")
	}
}

func TestPtrNullableEveryKind(t *testing.T) {
	address := Struct[Address]("Address").MustBuild()
	nodes := []ir.TypeNode{
		Ptr(String()).Node(), Ptr(Int[int]()).Node(), Ptr(Float[float64]()).Node(), Ptr(Bool()).Node(),
		Ptr(Bytes()).Node(), Ptr(Time()).Node(), Ptr(Duration()).Node(), Ptr(Array(String())).Node(),
		Ptr(Map(String(), String())).Node(), Ptr(Union[any](String())).Node(), Ptr(OneOf[any]("k")).Node(),
		Ptr(Ref(address)).Node(), Ptr(Any[any]()).Node(), Ptr(Object[Address]()).Node(),
	}
	for _, n := range nodes {
		data, _ := json.Marshal(n)
		if !strings.Contains(string(data), `"nullable":true`) {
			t.Errorf("%s: Ptr did not set nullable: %s", n.Kind(), data)
		}
	}
	// Ptr forwards the inner registry.
	p := Ptr(String().Custom("h", func(context.Context, string) error { return nil }))
	if _, ok := p.registry().funcs["h"]; !ok {
		t.Error("Ptr lost the registry")
	}
	// Builders' own Nullable too.
	for _, n := range []ir.TypeNode{
		String().Nullable().Node(), Int[int]().Nullable().Node(), Float[float64]().Nullable().Node(),
		Bool().Nullable().Node(), Bytes().Nullable().Node(), Time().Nullable().Node(), Duration().Nullable().Node(),
		Array(String()).Nullable().Node(), Map(String(), String()).Nullable().Node(), Union[any]().Nullable().Node(),
		OneOf[any]("k").Nullable().Node(), Ref(address).Nullable().Node(), Any[any]().Nullable().Node(),
	} {
		data, _ := json.Marshal(n)
		if !strings.Contains(string(data), `"nullable":true`) {
			t.Errorf("%s: Nullable not set: %s", n.Kind(), data)
		}
	}
}

func TestEveryRuleMethod(t *testing.T) {
	// Every rule method must produce a node that passes Check and the
	// expected kind, in order.
	kinds := func(n ir.TypeNode) []ir.RuleKind {
		var out []ir.RuleKind
		for _, r := range ir.Rules(n) {
			out = append(out, r.Kind)
		}
		return out
	}
	eq := func(name string, got []ir.RuleKind, want ...ir.RuleKind) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: got %v want %v", name, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%s: got %v want %v", name, got, want)
			}
		}
	}
	nop := func(context.Context, string) error { return nil }
	eq("string", kinds(String().MinLength(1).MaxLength(2).Pattern("x").Email().URL().UUID().ULID().Enum("a").Const("a").Custom("c1", nop).Async("a1", "", nop).Node()),
		ir.RuleMinLength, ir.RuleMaxLength, ir.RulePattern, ir.RuleEmail, ir.RuleURL, ir.RuleUUID, ir.RuleULID, ir.RuleEnum, ir.RuleConst, ir.RuleCustom, ir.RuleAsync)
	eq("formats", kinds(String().IPv4().IPv6().IP().CIDR().MAC().Hostname().Base64().JSON().Semver().Date().DateTime().NotBlank().Node()),
		ir.RuleIPv4, ir.RuleIPv6, ir.RuleIP, ir.RuleCIDR, ir.RuleMAC, ir.RuleHostname, ir.RuleBase64, ir.RuleJSON, ir.RuleSemver, ir.RuleDate, ir.RuleDateTime, ir.RuleNotBlank)
	eq("more formats", kinds(String().CreditCard().JWT().CountryCode().CurrencyCode().HexColor().E164().Node()),
		ir.RuleCreditCard, ir.RuleJWT, ir.RuleCountryCode, ir.RuleCurrencyCode, ir.RuleHexColor, ir.RuleE164)
	eq("coords", kinds(Float[float64]().Latitude().Longitude().Node()), ir.RuleLatitude, ir.RuleLongitude)
	eq("int", kinds(Int[int]().Min(1).Max(2).Gt(0).Gte(0).Lt(9).Lte(9).MultipleOf(2).Enum(1, 2).Const(1).
		Custom("c2", func(context.Context, int) error { return nil }).Async("a2", "/v/a2", func(context.Context, int) error { return nil }).Node()),
		ir.RuleMin, ir.RuleMax, ir.RuleGt, ir.RuleGte, ir.RuleLt, ir.RuleLte, ir.RuleMultipleOf, ir.RuleEnum, ir.RuleConst, ir.RuleCustom, ir.RuleAsync)
	eq("float", kinds(Float[float64]().Min(1).Max(2).Gt(0).Gte(0).Lt(9).Lte(9).MultipleOf(0.5).Custom("c3", func(context.Context, float64) error { return nil }).Node()),
		ir.RuleMin, ir.RuleMax, ir.RuleGt, ir.RuleGte, ir.RuleLt, ir.RuleLte, ir.RuleMultipleOf, ir.RuleCustom)
	eq("bool", kinds(Bool().Const(true).Custom("c4", func(context.Context, bool) error { return nil }).Node()), ir.RuleConst, ir.RuleCustom)
	eq("bytes", kinds(Bytes().MinLength(1).MaxLength(2).Custom("c5", func(context.Context, []byte) error { return nil }).Node()), ir.RuleMinLength, ir.RuleMaxLength, ir.RuleCustom)
	eq("time", kinds(Time().With(After(userFields.Start)).With(Before(userFields.End)).Custom("c6", func(context.Context, time.Time) error { return nil }).Node()), ir.RuleAfter, ir.RuleBefore, ir.RuleCustom)
	eq("time by name", kinds(Time().With(AfterField("Start")).With(BeforeField("End")).Node()), ir.RuleAfter, ir.RuleBefore)
	eq("duration", kinds(Duration().Min(1).Max(2).Gt(0).Lt(9).Custom("c7", func(context.Context, time.Duration) error { return nil }).Node()), ir.RuleMin, ir.RuleMax, ir.RuleGt, ir.RuleLt, ir.RuleCustom)
	eq("array", kinds(Array(String()).MinItems(1).MaxItems(2).Unique().Custom("c8", func(context.Context, []string) error { return nil }).Node()), ir.RuleMinItems, ir.RuleMaxItems, ir.RuleUnique, ir.RuleCustom)
	eq("map", kinds(Map(String(), Int[int]()).MinItems(1).MaxItems(2).Custom("c9", func(context.Context, map[string]int) error { return nil }).Node()), ir.RuleMinItems, ir.RuleMaxItems, ir.RuleCustom)

	// Every Custom/Async above lands in the registry of a schema that uses
	// the builder, and the finished schema passes Check.
	s := Struct("All",
		F(addressFields.Street, String().Custom("c1", nop).Async("a1", "", nop)),
	).MustBuild()
	if len(s.Funcs()) != 2 {
		t.Errorf("funcs %v", s.Funcs())
	}
}

func TestDescribeMsgOnEveryBuilder(t *testing.T) {
	desc := func(n ir.TypeNode) string {
		data, _ := json.Marshal(n)
		return string(data)
	}
	for _, n := range []ir.TypeNode{
		String().Describe("d").Node(), Int[int]().Describe("d").Node(), Float[float64]().Describe("d").Node(),
		Bool().Describe("d").Node(), Bytes().Describe("d").Node(), Time().Describe("d").Node(),
		Duration().Describe("d").Node(), Array(String()).Describe("d").Node(),
	} {
		if !strings.Contains(desc(n), `"description":"d"`) {
			t.Errorf("%s: Describe lost: %s", n.Kind(), desc(n))
		}
	}
	for _, n := range []ir.TypeNode{
		Int[int]().Min(1).Msg("c", "m").Node(), Float[float64]().Min(1).Msg("c", "m").Node(),
		Bool().Const(true).Msg("c", "m").Node(), Bytes().MinLength(1).Msg("c", "m").Node(),
		Time().With(After(userFields.Start)).Msg("c", "m").Node(), Duration().Gt(1).Msg("c", "m").Node(),
		Array(String()).Unique().Msg("c", "m").Node(),
	} {
		if !strings.Contains(desc(n), `"code":"c"`) {
			t.Errorf("%s: Msg lost: %s", n.Kind(), desc(n))
		}
	}
}

func TestFieldTransforms(t *testing.T) {
	f := F(addressFields.Street, String()).Trim().Lower().Upper().Coerce().NormalizeEmail().Default("x").Describe("street")
	want := []ir.TransformKind{ir.TransformTrim, ir.TransformLower, ir.TransformUpper, ir.TransformCoerce, ir.TransformNormalizeEmail, ir.TransformDefault}
	if len(f.transforms) != len(want) {
		t.Fatalf("transforms %v", f.transforms)
	}
	for i, tr := range f.transforms {
		if tr.Kind != want[i] || tr.Phase != ir.PhasePre {
			t.Errorf("transform %d: %+v", i, tr)
		}
	}
	if f.transforms[5].Params[ir.ParamValue] != "x" || f.meta.Description != "street" {
		t.Error("default/describe lost")
	}
	s := Struct("A", f).MustBuild()
	if s.IR().Root.(*ir.ObjectNode).Fields[0].Meta.Description != "street" {
		t.Error("field meta not emitted")
	}
}

func TestCondSugar(t *testing.T) {
	c := Cmp(userFields.Age, ir.OpGte, 18)
	if c.Kind != ir.CondCmp || c.Field != "Age" || c.Op != string(ir.OpGte) || c.Value != int32(18) {
		t.Errorf("Cmp %+v", c)
	}
	if m := Missing(userFields.URL); m.Kind != ir.CondPresence || m.Op != string(ir.Absent) {
		t.Errorf("Missing %+v", m)
	}
	if r := Requires(ir.Expr("x"), userFields.URL, userFields.Age); len(r.Fields) != 2 || r.Fields[1] != "Age" {
		t.Errorf("Requires %+v", r)
	}
}

func TestEndpointsDefaultPathAndGates(t *testing.T) {
	nop := func(context.Context, string) error { return nil }
	inner := Struct("Inner", F(addressFields.Street, String().Async("in_ref", "", nop))).MustBuild()
	s := Struct("S",
		F(userFields.Email, String().Async("zeta", "/custom/zeta", nop).Custom("opaque", nop)),
		F(userFields.Home, Ref(inner)),
		F(userFields.Venue, String()),
	).Gate(When(ir.Expr("x"), []ir.TargetRule{On(userFields.Venue, ir.Async("gated", ""))}, nil)).MustBuild()
	eps := s.Endpoints()
	// "gated" has no registered body (declared through ir directly), so it is
	// skipped; opaque rules never appear; paths sort.
	if len(eps) != 2 || eps[0].Path != "/custom/zeta" || eps[1].Path != "/v/in_ref" {
		t.Errorf("endpoints %+v", eps)
	}
}

func TestDefsAndUnionRegistry(t *testing.T) {
	inner := Struct("Inner", F(addressFields.Street, String().Custom("k", func(context.Context, string) error { return nil }))).MustBuild()
	outer := Struct("Outer",
		F(userFields.Extra, Union[any](Ref(inner), Map(String(), Ref(inner)))),
		F(userFields.IDOrName, OneOf[any]("t").Case("a", Ref(inner)).Default(Ref(inner))),
	).MustBuild()
	irs := outer.IR()
	if len(irs.Defs) != 1 || irs.Defs["Inner"] == nil {
		t.Errorf("defs %v", irs.Defs)
	}
	if _, ok := outer.Func("k"); !ok {
		t.Error("function from a ref inside a union/oneof did not propagate")
	}
	if err := ir.Check(irs); err != nil {
		t.Error(err)
	}
	if Any[any]().registry().err != nil || len(Any[any]().registry().funcs) != 0 {
		t.Error("Any registry must be empty")
	}
}
