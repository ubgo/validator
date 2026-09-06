package tags

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubgo/validator/accessors"
	"github.com/ubgo/validator/ir"
)

const exampleDir = "internal/example"

func migrate(t *testing.T) *Result {
	t.Helper()
	res, err := Migrate(Options{Dir: exampleDir, Type: "Signup", Generator: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// field finds a root field by Go name.
func field(t *testing.T, s *ir.Schema, name string) *ir.FieldNode {
	t.Helper()
	root := s.Root.(*ir.ObjectNode)
	for i := range root.Fields {
		if root.Fields[i].Name == name {
			return &root.Fields[i]
		}
	}
	t.Fatalf("field %s missing", name)
	return nil
}

func kinds(rs []ir.Rule) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = string(r.Kind)
	}
	return strings.Join(parts, ",")
}

func TestMigrateMapping(t *testing.T) {
	res := migrate(t)
	s := res.Schema
	if strings.Join(res.Types, ",") != "Signup,Address" {
		t.Errorf("types %v", res.Types)
	}
	if s.Meta.Title != "Signup" || s.Meta.Generator != "test" || s.Defs["Address"] == nil {
		t.Errorf("meta/defs %+v", s.Meta)
	}
	cases := []struct {
		name     string
		optional bool
		kind     ir.Kind
		rules    string
	}{
		{"Email", false, ir.KindString, "email"},
		{"Name", false, ir.KindString, "minLength,maxLength,pattern"},
		{"Age", false, ir.KindInt, "gte,lte"},
		{"Score", true, ir.KindFloat, "gt,lt"},
		{"Plan", false, ir.KindString, "enum"},
		{"Level", true, ir.KindInt, "enum"},
		{"Accept", true, ir.KindBool, "const"},
		{"Site", true, ir.KindString, "url"},
		{"ID", false, ir.KindString, "uuid"},
		{"Ref", true, ir.KindString, "ulid"},
		{"Code", true, ir.KindString, "pattern,pattern,pattern"},
		{"Tags", false, ir.KindArray, "minItems,maxItems,unique"},
		{"Scores", true, ir.KindArray, "minItems,maxItems"},
		{"Labels", true, ir.KindMap, "maxItems"},
		{"Home", false, ir.KindRef, ""},
		{"Work", true, ir.KindRef, ""},
		{"Start", false, ir.KindTime, ""},
		{"End", false, ir.KindTime, "after"},
		{"Raw", true, ir.KindBytes, "maxLength"},
		{"Coupon", true, ir.KindString, ""},
		{"Venue", true, ir.KindString, ""},
		{"Phone", true, ir.KindString, "e164"},
		{"Extra", true, ir.KindAny, ""},
		{"Internal", false, ir.KindString, ""},
		{"Weird", false, ir.KindString, ""},
		{"Hex", true, ir.KindString, "pattern,pattern"},
		{"Nick", true, ir.KindString, "minLength,maxLength"},
	}
	for _, c := range cases {
		f := field(t, s, c.name)
		if f.Optional != c.optional || f.Type.Kind() != c.kind || kinds(ir.Rules(f.Type)) != c.rules {
			t.Errorf("%s: optional=%v kind=%s rules=%s", c.name, f.Optional, f.Type.Kind(), kinds(ir.Rules(f.Type)))
		}
	}
	// Details: enum values with a quoted member, numeric enum, pointer nullability,
	// element rules after dive, gt/lt on strings as inclusive lengths, len.
	plan := ir.Rules(field(t, s, "Plan").Type)[0].Params[ir.ParamValues].([]any)
	if len(plan) != 3 || plan[2] != "team plus" {
		t.Errorf("plan enum %v", plan)
	}
	level := ir.Rules(field(t, s, "Level").Type)[0].Params[ir.ParamValues].([]any)
	if len(level) != 3 || level[0] != 1.0 {
		t.Errorf("level enum %v", level)
	}
	if !field(t, s, "Site").Type.(*ir.StringNode).Nullable || !field(t, s, "Work").Type.(*ir.RefNode).Nullable {
		t.Error("pointer fields must be nullable")
	}
	tags := field(t, s, "Tags").Type.(*ir.ArrayNode)
	if kinds(ir.Rules(tags.Elem)) != "minLength,pattern" {
		t.Errorf("dive element rules %s", kinds(ir.Rules(tags.Elem)))
	}
	nick := ir.Rules(field(t, s, "Nick").Type)
	if nick[0].Params[ir.ParamValue] != 2 || nick[1].Params[ir.ParamValue] != 9 {
		t.Errorf("gt/lt on strings must become inclusive lengths: %v", nick)
	}
	zip := ir.Rules(s.Defs["Address"].(*ir.ObjectNode).Fields[1].Type)
	if kinds(zip) != "minLength,maxLength,pattern" || zip[0].Params[ir.ParamValue] != 5 {
		t.Errorf("len: %s %v", kinds(zip), zip)
	}
	labels := field(t, s, "Labels").Type.(*ir.MapNode)
	if kinds(ir.Rules(labels.Val)) != "maxLength" {
		t.Errorf("map dive: %s", kinds(ir.Rules(labels.Val)))
	}
	if field(t, s, "Internal").JSONName != "" || field(t, s, "Email").JSONName != "email" {
		t.Error("json names")
	}
	// Ignored (`validate:"-"`) and unexported fields are absent.
	for _, f := range s.Root.(*ir.ObjectNode).Fields {
		if f.Name == "Ignored" || f.Name == "unexport" {
			t.Errorf("%s must not be migrated", f.Name)
		}
	}
}

func TestMigrateGates(t *testing.T) {
	res := migrate(t)
	gates := res.Schema.Root.(*ir.ObjectNode).Gated
	got := make([]string, len(gates))
	for i, g := range gates {
		b, _ := json.Marshal(g)
		got[i] = string(b)
	}
	want := []string{
		`{"kind":"requires","cond":{"kind":"cmp","field":"Plan","op":"eq","value":"pro"},"fields":["Coupon"]}`,
		`{"kind":"requires","cond":{"kind":"presence","field":"Ref","op":"present"},"fields":["Coupon"]}`,
		`{"kind":"requires","cond":{"kind":"not","sub":[{"kind":"cmp","field":"Plan","op":"eq","value":"free"}]},"fields":["Venue"]}`,
		`{"kind":"requires","cond":{"kind":"presence","field":"Email","op":"absent"},"fields":["Phone"]}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("gates\n%s", strings.Join(got, "\n"))
	}
	// Multi-pair and _all variants.
	if g, ok := gateFor(tagItem{name: "required_if", value: "A x B y"}, "F"); !ok || g.Cond.Kind != ir.CondAnd || len(g.Cond.Sub) != 2 {
		t.Errorf("required_if pairs %+v", g)
	}
	if g, ok := gateFor(tagItem{name: "required_with_all", value: "A B"}, "F"); !ok || g.Cond.Kind != ir.CondAnd {
		t.Errorf("required_with_all %+v", g)
	}
	if g, ok := gateFor(tagItem{name: "required_without", value: "A B"}, "F"); !ok || g.Cond.Kind != ir.CondOr || g.Cond.Sub[0].Op != string(ir.Absent) {
		t.Errorf("required_without %+v", g)
	}
	if _, ok := gateFor(tagItem{name: "required_if", value: "A"}, "F"); ok {
		t.Error("odd pairs must fail")
	}
	if _, ok := gateFor(tagItem{name: "required_with", value: ""}, "F"); ok {
		t.Error("empty with must fail")
	}
	if _, ok := gateFor(tagItem{name: "bogus"}, "F"); ok {
		t.Error("unknown gate")
	}
}

func TestUnsupportedIsReported(t *testing.T) {
	res := migrate(t)
	got := make([]string, len(res.Unsupported))
	for i, u := range res.Unsupported {
		got[i] = u.String()
	}
	want := []string{
		`Signup.Weird: "eqfield=Name" — cross-field comparison on a non-time field needs the CEL layer (v2); express it as a Requires gate or a custom rule`,
		`Signup.Weird: "isdefault" — isdefault has no portable meaning (frontend cannot know Go zero values); use a const rule or a custom rule`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("unsupported:\n%s", strings.Join(got, "\n"))
	}
}

func TestMigrateErrors(t *testing.T) {
	if _, err := Migrate(Options{}); err == nil {
		t.Error("Dir/Type required")
	}
	if _, err := Migrate(Options{Dir: exampleDir, Type: "Nope"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing type: %v", err)
	}
	if _, err := Migrate(Options{Dir: filepath.Join(t.TempDir(), "missing"), Type: "X"}); err == nil {
		t.Error("missing dir")
	}
}

func TestEdgeTags(t *testing.T) {
	dir := t.TempDir()
	src := "package e\n\nimport \"time\"\n\ntype Inner struct{ A int `validate:\"gte=1\"` }\n\n" +
		"type E struct {\n" +
		"\tInner\n" +
		"\tS string `validate:\"oneof=,min=x,unknown,gtfield=,contains=a.b\"`\n" +
		"\tN int `validate:\"email,len=2,oneof=a,eq=x,gt=zz\"`\n" +
		"\tF float32 `validate:\"eq=1.5,oneof=1 2\"`\n" +
		"\tB bool `validate:\"eq=maybe\"`\n" +
		"\tT time.Time `validate:\"gtfield=S,ltfield=S\"`\n" +
		"\tD time.Duration `validate:\"gt=1\"`\n" +
		"\tU uint16 `validate:\"max=3\"`\n" +
		"\tM map[int]Inner `validate:\"min=1,dive,structonly\"`\n" +
		"\tL []*Inner `validate:\"dive,required\"`\n" +
		"\tX chan int `validate:\"required,min=1\"`\n" +
		"\tNs []string `validate:\"gt=2,lt=4,dive,gte=1,lte=3\"`\n" +
		"\tI Inner `validate:\"min=1\"`\n" +
		"\tNet string `validate:\"ip,ipv4,ipv6,cidr,mac,hostname,fqdn,base64,json,semver,notblank,datetime=2006-01-02,datetime=2006-01-02T15:04:05Z07:00,datetime=Jan 2\"`\n" +
		"\tNotStr int `validate:\"ip,json\"`\n" +
		"\tMore string `validate:\"credit_card,jwt,iso3166_1_alpha2,iso4217,hexcolor,e164\"`\n" +
		"\tGeo float64 `validate:\"latitude,longitude\"`\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(dir, "e.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Migrate(Options{Dir: dir, Type: "E"})
	if err != nil {
		t.Fatal(err)
	}
	s := res.Schema
	if kinds(ir.Rules(field(t, s, "S").Type)) != "pattern" {
		t.Errorf("S: %s", kinds(ir.Rules(field(t, s, "S").Type)))
	}
	if kinds(ir.Rules(field(t, s, "N").Type)) != "" {
		t.Errorf("N: %s", kinds(ir.Rules(field(t, s, "N").Type)))
	}
	if kinds(ir.Rules(field(t, s, "F").Type)) != "const,enum" || field(t, s, "F").Type.(*ir.FloatNode).Bits != 32 {
		t.Errorf("F: %s", kinds(ir.Rules(field(t, s, "F").Type)))
	}
	if kinds(ir.Rules(field(t, s, "B").Type)) != "" {
		t.Error("B: bad bool const must be reported")
	}
	if kinds(ir.Rules(field(t, s, "T").Type)) != "after,before" {
		t.Errorf("T: %s", kinds(ir.Rules(field(t, s, "T").Type)))
	}
	if kinds(ir.Rules(field(t, s, "D").Type)) != "gt" {
		t.Errorf("D: %s", kinds(ir.Rules(field(t, s, "D").Type)))
	}
	u := field(t, s, "U").Type.(*ir.IntNode)
	if !u.Unsigned || u.Bits != 16 || kinds(u.Rules) != "max" {
		t.Errorf("U: %+v", u)
	}
	m := field(t, s, "M").Type.(*ir.MapNode)
	if kinds(m.Rules) != "minItems" || m.Val.Kind() != ir.KindRef || m.Key.Kind() != ir.KindInt {
		t.Errorf("M: %s %s %s", kinds(m.Rules), m.Val.Kind(), m.Key.Kind())
	}
	l := field(t, s, "L").Type.(*ir.ArrayNode)
	if l.Elem.Kind() != ir.KindRef || !l.Elem.(*ir.RefNode).Nullable {
		t.Errorf("L: %+v", l.Elem)
	}
	if field(t, s, "X").Type.Kind() != ir.KindAny || field(t, s, "X").Optional {
		t.Error("X: unknown type is any but required still applies")
	}
	ns := field(t, s, "Ns").Type.(*ir.ArrayNode)
	if kinds(ns.Rules) != "minItems,maxItems" || ns.Rules[0].Params[ir.ParamValue] != 3 || ns.Rules[1].Params[ir.ParamValue] != 3 || kinds(ir.Rules(ns.Elem)) != "minLength,maxLength" {
		t.Errorf("Ns: %s %v %s", kinds(ns.Rules), ns.Rules, kinds(ir.Rules(ns.Elem)))
	}
	if kinds(ir.Rules(field(t, s, "Net").Type)) != "ip,ipv4,ipv6,cidr,mac,hostname,hostname,base64,json,semver,notBlank,date,datetime" {
		t.Errorf("Net: %s", kinds(ir.Rules(field(t, s, "Net").Type)))
	}
	if kinds(ir.Rules(field(t, s, "More").Type)) != "creditCard,jwt,countryCode,currencyCode,hexColor,e164" {
		t.Errorf("More: %s", kinds(ir.Rules(field(t, s, "More").Type)))
	}
	if kinds(ir.Rules(field(t, s, "Geo").Type)) != "latitude,longitude" {
		t.Errorf("Geo: %s", kinds(ir.Rules(field(t, s, "Geo").Type)))
	}
	if kinds(ir.Rules(field(t, s, "NotStr").Type)) != "" {
		t.Error("format tags on an int must be reported")
	}
	reported := strings.Join(func() []string {
		out := make([]string, len(res.Unsupported))
		for i, u := range res.Unsupported {
			out[i] = u.Field + ":" + u.Tag
		}
		return out
	}(), " ")
	for _, want := range []string{"Inner:", "S:oneof=", "S:min=x", "S:unknown", "S:gtfield=", "N:email", "N:len=2", "N:oneof=a", "N:eq=x", "N:gt=zz", "B:eq=maybe", "X:min=1", "I:min=1", "L:required", "Net:datetime=Jan 2", "NotStr:ip", "NotStr:json"} {
		if !strings.Contains(reported, want) {
			t.Errorf("missing report %q in %s", want, reported)
		}
	}
	if strings.Contains(reported, "structonly") {
		t.Error("structonly is a no-op, not unsupported")
	}
	if _, err := Migrate(Options{Dir: dir, Type: "Inner"}); err != nil {
		t.Errorf("leaf struct migrates alone: %v", err)
	}
}

func TestHelpers(t *testing.T) {
	if got := splitOneOf("a 'b c' d"); strings.Join(got, "|") != "a|b c|d" {
		t.Errorf("splitOneOf %v", got)
	}
	if got := parseTags(" required , min=3,,"); len(got) != 2 || got[1].name != "min" || got[1].value != "3" || got[1].raw != "min=3" {
		t.Errorf("parseTags %+v", got)
	}
	b, a := splitDive(parseTags("min=1,dive,max=2"))
	if len(b) != 1 || len(a) != 1 || a[0].name != "max" {
		t.Error("splitDive")
	}
	if tagOf(nil) != "" || jsonName(nil, "X") != "" {
		t.Error("nil tags")
	}
	if _, _, ok := splitMapType("map[string"); ok {
		t.Error("unterminated map")
	}
	if _, _, ok := intType("string"); ok {
		t.Error("intType string")
	}
	for _, n := range []ir.TypeNode{&ir.ObjectNode{}, &ir.StringNode{}, &ir.IntNode{}, &ir.FloatNode{}, &ir.BoolNode{}, &ir.BytesNode{}, &ir.TimeNode{}, &ir.DurationNode{}, &ir.ArrayNode{}, &ir.MapNode{}, &ir.RefNode{}, &ir.AnyNode{}} {
		setNullable(n)
		if !nullableOf(n) {
			t.Errorf("%s nullable", n.Kind())
		}
		appendRule(n, ir.Email()) // no-op on non-rule nodes, must not panic
	}
	if u := (Unsupported{Type: "T", Field: "F", Tag: "x", Reason: "r"}); u.String() != `T.F: "x" — r` {
		t.Error(u.String())
	}
}

// TestBuilderGolden pins schema.gen.go in the example package; the example's
// own test proves the emitted source builds the same IR.
func TestBuilderGolden(t *testing.T) {
	res := migrate(t)
	src, err := Builder(res.Schema, res.Types, BuilderOptions{Package: "example"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(exampleDir, "schema.gen.go")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, src, 0o644); err != nil {
			t.Fatal(err)
		}
		// The builder source names <Type>Fields accessors; generate them the
		// way a user would with `validatorctl fields`.
		if _, err := accessors.Write(accessors.Options{Dir: exampleDir}); err != nil {
			t.Fatal(err)
		}
		irDoc, _ := json.MarshalIndent(res.Schema, "", "  ")
		if err := os.WriteFile(filepath.Join(exampleDir, "signup.ir.json"), append(irDoc, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run with UPDATE_GOLDEN=1 to create)", err)
	}
	if !bytes.Equal(want, src) {
		t.Fatalf("schema.gen.go is stale; run UPDATE_GOLDEN=1 go test ./tags\n%s", src)
	}
	s := string(src)
	for _, want := range []string{
		BuilderHeader,
		"var AddressSchema = vb.Struct(\"Address\",",
		"var SignupSchema = vb.Struct(\"Signup\",",
		`vb.F(SignupFields.Email, vb.String().Email()),`,
		`vb.F(SignupFields.Plan, vb.String().Enum("free", "pro", "team plus")),`,
		`vb.F(SignupFields.Level, vb.Int[int8]().Enum(1, 2, 3)).Optional(),`,
		`vb.F(SignupFields.Site, vb.Ptr(vb.String().URL())).Optional(),`,
		`vb.F(SignupFields.Work, vb.Ptr(vb.Ref(AddressSchema))).Optional(),`,
		`vb.Array(vb.String().MinLength(2).Pattern("^[a-zA-Z0-9]+$")).MinItems(1).MaxItems(5).Unique()`,
		`vb.F(SignupFields.End, vb.Time().With(vb.AfterField("Start"))),`,
		`vb.Requires(ir.Cmp("Plan", ir.OpEq, "pro"), SignupFields.Coupon),`,
		`vb.Requires(ir.Not(ir.Cmp("Plan", ir.OpEq, "free")), SignupFields.Venue),`,
		`vb.Requires(ir.HasField("Email", ir.Absent), SignupFields.Phone),`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	if _, err := Builder(res.Schema, res.Types, BuilderOptions{}); err == nil {
		t.Error("package required")
	}
	if _, err := Builder(&ir.Schema{Version: ir.Version, Root: &ir.StringNode{}, Meta: ir.SchemaMeta{Title: "X"}}, []string{"X"}, BuilderOptions{Package: "p"}); err == nil {
		t.Error("non-object root")
	}
	if _, err := Builder(&ir.Schema{Version: ir.Version, Root: &ir.ObjectNode{}, Defs: map[string]ir.TypeNode{"D": &ir.StringNode{}}, Meta: ir.SchemaMeta{Title: "X"}}, []string{"X", "D"}, BuilderOptions{Package: "p"}); err == nil {
		t.Error("non-object def")
	}
}

func TestBuilderRendering(t *testing.T) {
	e := &emitter{suffix: "Fields"}
	cases := map[string]ir.TypeNode{
		"vb.Float[float32]().Min(1).Max(2).Gt(0).Gte(0).Lt(9).Lte(9).MultipleOf(0.5)": &ir.FloatNode{Bits: 32, Rules: []ir.Rule{ir.Min(1), ir.Max(2), ir.Gt(0), ir.Gte(0), ir.Lt(9), ir.Lte(9), ir.MultipleOf(0.5)}},
		"vb.Int[uint64]().Const(3)":                           &ir.IntNode{Bits: 64, Unsigned: true, Rules: []ir.Rule{ir.Const(3.0)}},
		"vb.Bool().Const(true)":                               &ir.BoolNode{Rules: []ir.Rule{ir.Const(true)}},
		"vb.Bytes().MaxLength(4)":                             &ir.BytesNode{Rules: []ir.Rule{ir.MaxLength(4)}},
		"vb.Duration().Gt(1)":                                 &ir.DurationNode{Rules: []ir.Rule{ir.Gt(1)}},
		"vb.Map(vb.String(), vb.Any[any]()).MinItems(1)":      &ir.MapNode{Key: &ir.StringNode{}, Val: &ir.AnyNode{}, Rules: []ir.Rule{ir.MinItems(1)}},
		"vb.Time().With(vb.BeforeField(\"E\"))":               &ir.TimeNode{Rules: []ir.Rule{ir.Before("E")}},
		"vb.String().UUID().ULID()":                           &ir.StringNode{Rules: []ir.Rule{ir.UUID(), ir.ULID()}},
		"vb.Any[any]() /* union is not emitted by migrate */": &ir.UnionNode{},
	}
	for want, n := range cases {
		if got := e.node(n); got != want {
			t.Errorf("node = %s want %s", got, want)
		}
	}
	// Custom/async rules are not migrated and render nothing.
	if got := e.rules([]ir.Rule{ir.Custom("f")}); got != "" {
		t.Error(got)
	}
	if e.gate("T", ir.Group(ir.ExactlyOne, "A", "B")) != "vb.Group(ir.ExactlyOne, TFields.A, TFields.B)" {
		t.Error("group gate")
	}
	if !strings.Contains(e.gate("T", ir.When(ir.Expr("x"), nil, nil)), "not produced") {
		t.Error("when gate placeholder")
	}
	if e.cond(nil) != "ir.Condition{}" || e.cond(&ir.Condition{Kind: ir.CondExpr}) != "ir.Condition{}" {
		t.Error("cond fallbacks")
	}
	if e.cond(&ir.Condition{Kind: ir.CondOr, Sub: []ir.Condition{ir.Cmp("A", ir.OpNe, 1.0), ir.Cmp("B", ir.OpLte, "x")}}) != `ir.Or(ir.Cmp("A", ir.OpNe, 1), ir.Cmp("B", ir.OpLte, "x"))` {
		t.Error("or cond")
	}
	for op, want := range map[ir.CmpOp]string{ir.OpGt: "OpGt", ir.OpGte: "OpGte", ir.OpLt: "OpLt"} {
		if opConst(op) != want {
			t.Error(op)
		}
	}
	if groupConst(ir.AtLeastOne) != "ir.AtLeastOne" || groupConst(ir.AllOrNone) != "ir.AllOrNone" || extraConst(ir.ExtraAllow) != "ir.ExtraAllow" || extraConst(ir.ExtraForbid) != "ir.ExtraForbid" || extraConst("") != "ir.ExtraIgnore" {
		t.Error("consts")
	}
	if intLit("x") != "x" || numLit(2) != "2" || anyLit(2) != "2" || len(sortedKeys(map[string]int{"b": 1, "a": 2})) != 2 {
		t.Error("literal fallbacks")
	}
}

func TestUnsupportedReasons(t *testing.T) {
	str := &ir.StringNode{}
	num := &ir.IntNode{}
	cases := []struct {
		tag  tagItem
		node ir.TypeNode
		want string
	}{
		{tagItem{name: "required_if", raw: "required_if=A|B"}, str, "OR-groups"},
		{tagItem{name: "gtfield", value: "X"}, &ir.TimeNode{}, "only gtfield/ltfield on time"},
		{tagItem{name: "nefield", value: "X"}, num, "CEL layer"},
		{tagItem{name: "excludes", value: "x"}, str, "lookahead"},
		{tagItem{name: "isdefault"}, str, "no portable meaning"},
		{tagItem{name: "unique", value: "ID"}, &ir.ArrayNode{}, "slices of structs"},
		{tagItem{name: "unique"}, str, "slices only"},
		{tagItem{name: "datetime", value: "Jan 2"}, str, "RFC 3339"},
		{tagItem{name: "dive"}, str, "slices and maps"},
		{tagItem{name: "required_with"}, str, "malformed"},
		{tagItem{name: "email"}, num, "non-string field"},
		{tagItem{name: "alpha"}, num, "non-string field"},
		{tagItem{name: "whatever"}, str, "no IR equivalent"},
	}
	for _, c := range cases {
		if got := unsupportedReason(c.tag, c.node); !strings.Contains(got, c.want) {
			t.Errorf("%s: %q lacks %q", c.tag.name, got, c.want)
		}
	}
}
