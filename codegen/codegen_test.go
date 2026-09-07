package codegen

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubgo/validator/codegen/internal/example"
	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/vb"
)

const exampleDir = "internal/example"

// exampleSchema is the fixture's IR. Importing the example package here is
// cycle-free: its generated file depends on codegen/rt, not on codegen.
func exampleSchema(t *testing.T) *ir.Schema {
	t.Helper()
	return example.OrderSchema.IR()
}

// TestGoldenIsCurrent regenerates validate.gen.go from the example IR and
// compares it with the checked-in file the example package compiles.
func TestGoldenIsCurrent(t *testing.T) {
	res, err := Generate(Options{Schema: exampleSchema(t), Dir: exampleDir})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(exampleDir, DefaultOutput)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, res.Source, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run with UPDATE_GOLDEN=1 to create)", err)
	}
	if !bytes.Equal(want, res.Source) {
		t.Fatalf("validate.gen.go is stale; run UPDATE_GOLDEN=1 go test ./codegen\n%s", res.Source)
	}
	if strings.Join(res.Funcs, ",") != "ValidateOrder,ValidateAddress" {
		t.Errorf("funcs %v", res.Funcs)
	}
	src := string(res.Source)
	for _, want := range []string{
		Header,
		"func ValidateOrder(ctx context.Context, v *Order, reg rt.Registry) *verr.Errors",
		"func ValidateOrderInto(ctx context.Context, v *Order, reg rt.Registry, errs *verr.Errors)",
		"if errs.Stopped() {",
		"var probe",
		"func validateAddress(ctx context.Context, v *Address, reg rt.Registry, path rt.Path, errs *verr.Errors)",
		`rt.Email(v.Email)`,
		`rt.CallT(ctx, reg, "no_temp", v.Email)`,
		`float64(v.Qty) >= float64(float64(1))`,
		`"whole units only"`,
		`(*v.Note)`,
		`v.Placed.IsZero()`,
		`for i`,
		`.Index(i`,
		`rt.UniqueOf(v.Tags)`,
		`rt.UniqueOf(v.Scores)`,
		`(v.Type == "online" || v.Type == "offline")`,
		`v.Sku == "SKU"`,
		`path.Field(".email")`,
		`.String(), "must be a valid email address"`,
		`validateAddress(ctx, &r`,
		`rt.Group(errs, path.String(), "at-least-one"`,
		`rt.CondT(ctx, reg, "vip", *v)`,
		`float64(v.Qty) >= float64(int(50))`,
		`v.Nick = rt.Trim(v.Nick)`,
		`v.Contact = rt.NormalizeEmail(v.Contact)`,
		`// coerce: no-op on typed input`,
		`v.Nick = "anon"`,
		`v.Pref = rt.Ptr("a")`,
		`(*v.Pref) = rt.Upper((*v.Pref))`,
		`rt.AsIntegral(v.Ref)`,
		`rt.AsStruct[Address](v.Ref)`,
		`rt.Union(errs, p`,
		`switch o`,
		`case "offline":`,
		`rt.Variant(errs, p`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestWriteIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "m.go"), []byte("package m\n\ntype M struct {\n\tName string `json:\"name\"`\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fName := vb.NewField("Name", func(m struct{ Name string }) string { return m.Name }).JSON("name")
	s := vb.Struct("M", vb.F(fName, vb.String().MinLength(1))).MustBuild()
	res, err := Write(Options{Schema: s.IR(), Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := os.Stat(res.Path)
	if _, err := Write(Options{Schema: s.IR(), Dir: dir}); err != nil {
		t.Fatal(err)
	}
	second, _ := os.Stat(res.Path)
	if !second.ModTime().Equal(first.ModTime()) {
		t.Error("unchanged content must not be rewritten")
	}
	if _, err := Write(Options{Schema: s.IR(), Dir: dir, Output: filepath.Join("nope", "x.go")}); err == nil {
		t.Error("unwritable output must fail")
	}
}

func TestRefusals(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "m.go"), []byte("package m\n\ntype M struct {\n\tName string\n\tN int\n\tA any\n\tKids []Kid\n}\n\ntype Kid struct{ Age int }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	type M struct {
		Name string
		N    int
		A    any
	}
	fName := vb.NewField("Name", func(m M) string { return m.Name })
	fN := vb.NewField("N", func(m M) int { return m.N })
	fAny := vb.NewField("A", func(m M) any { return m.A })
	_, _ = fN, fAny
	cases := []struct {
		name   string
		schema *ir.Schema
		want   string
	}{
		{"nil schema", nil, "Schema is required"},
		{"no title", &ir.Schema{Version: ir.Version, Root: &ir.ObjectNode{}}, "no title"},
		{"non-object root", &ir.Schema{Version: ir.Version, Root: &ir.StringNode{}, Meta: ir.SchemaMeta{Title: "M"}}, "root must be an object"},
		{"missing struct", vb.Struct("Nope", vb.F(fName, vb.String())).MustBuild().IR(), "struct Nope not found"},
		{"missing field", &ir.Schema{Version: ir.Version, Root: &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "Ghost", Type: &ir.StringNode{}}}}, Meta: ir.SchemaMeta{Title: "M"}}, "field Ghost is not declared"},
		{"default type", vb.Struct("M", vb.F(fName, vb.String()).Default(5)).MustBuild().IR(), "default 5 cannot be assigned to string"},
		{"oneof bad discriminator", &ir.Schema{Version: ir.Version, Meta: ir.SchemaMeta{Title: "M"}, Root: &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "Kids", Type: &ir.ArrayNode{Elem: &ir.OneOfNode{Discriminator: "nope", Cases: map[string]ir.TypeNode{"a": &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "nope", Type: &ir.StringNode{}}}}}}}}}}}, `discriminator "nope" is not a field of Kid`},
		{"oneof non-string discriminator", &ir.Schema{Version: ir.Version, Meta: ir.SchemaMeta{Title: "M"}, Root: &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "Kids", Type: &ir.ArrayNode{Elem: &ir.OneOfNode{Discriminator: "Age", Cases: map[string]ir.TypeNode{"a": &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "Age", Type: &ir.IntNode{}}}}}}}}}}}, "must be a string field"},
		{"non-object def", &ir.Schema{Version: ir.Version, Root: &ir.ObjectNode{}, Defs: map[string]ir.TypeNode{"S": &ir.StringNode{}}, Meta: ir.SchemaMeta{Title: "M"}}, "def S is not an object"},
		{"array on scalar", &ir.Schema{Version: ir.Version, Root: &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "N", Type: &ir.ArrayNode{Elem: &ir.AnyNode{}}}}}, Meta: ir.SchemaMeta{Title: "M"}}, "array node on non-slice"},
		{"map on scalar", &ir.Schema{Version: ir.Version, Root: &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "N", Type: &ir.MapNode{Key: &ir.AnyNode{}, Val: &ir.AnyNode{}}}}}, Meta: ir.SchemaMeta{Title: "M"}}, "map node on non-map"},
		{"ref type mismatch", &ir.Schema{Version: ir.Version, Root: &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "N", Type: &ir.RefNode{Ref: "Kid"}}}}, Defs: map[string]ir.TypeNode{"Kid": &ir.ObjectNode{}}, Meta: ir.SchemaMeta{Title: "M"}}, "ref Kid used on Go type int"},
		{"inline object on scalar", &ir.Schema{Version: ir.Version, Root: &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "N", Type: &ir.ObjectNode{}}}}, Meta: ir.SchemaMeta{Title: "M"}}, "inline object on Go type int"},
		{"unknown sibling", vb.Struct("M", vb.F(fN, vb.Int[int]())).Gate(vb.Requires(ir.Cmp("Ghost", ir.OpEq, 1), fN)).MustBuild().IR(), "unknown field Ghost"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Generate(Options{Schema: c.schema, Dir: dir})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want %q, got %v", c.want, err)
			}
		})
	}
	if _, err := Generate(Options{Schema: &ir.Schema{Version: ir.Version, Root: &ir.ObjectNode{}, Meta: ir.SchemaMeta{Title: "M"}}}); err == nil {
		t.Error("Dir required")
	}
	if _, err := Generate(Options{Schema: &ir.Schema{Version: "v0", Root: &ir.ObjectNode{}}, Dir: dir}); err == nil {
		t.Error("bad IR must fail Check")
	}
	if _, err := Generate(Options{Schema: &ir.Schema{Version: ir.Version, Root: &ir.ObjectNode{}, Meta: ir.SchemaMeta{Title: "M"}}, Dir: filepath.Join(dir, "missing")}); err == nil {
		t.Error("missing dir")
	}
}

func TestFallbacksAndConcreteUnions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "m.go"), []byte("package m\n\ntype M struct {\n\tName string\n\tA any\n\tN int\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	type M struct {
		Name string
		A    any
		N    int
	}
	fName := vb.NewField("Name", func(m M) string { return m.Name })
	fAny := vb.NewField("A", func(m M) any { return m.A })
	fN := vb.NewField("N", func(m M) int { return m.N })
	s := vb.Struct("M",
		vb.F(fName, vb.Union[string](vb.String().MinLength(1), vb.Int[int]())),
		vb.F(fAny, vb.Union[any](vb.Map(vb.String(), vb.String()), vb.String())),
		vb.F(fN, vb.Union[int](vb.String())),
	).MustBuild()
	res, err := Generate(Options{Schema: s.IR(), Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	src := string(res.Source)
	for _, want := range []string{
		"rt.MustSchema(",
		"union with a map variant",
		"rt.Interpret(ctx, reg, fallback",
		"no variant can match Go type int",
		":= v.Name",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q", want)
		}
	}
	// oneof on an interface field also delegates.
	one := &ir.Schema{Version: ir.Version, Meta: ir.SchemaMeta{Title: "M"}, Root: &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "A", Type: &ir.OneOfNode{Discriminator: "k", Cases: map[string]ir.TypeNode{"a": &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "k", Type: &ir.StringNode{}}}}}}}}}}
	res, err = Generate(Options{Schema: one, Dir: dir})
	if err != nil || !strings.Contains(string(res.Source), "discriminated union on Go type any") {
		t.Errorf("oneof fallback: %v", err)
	}
	for _, c := range []struct {
		v    ir.TypeNode
		typ  string
		want bool
	}{
		{&ir.StringNode{}, "string", true}, {&ir.IntNode{}, "float64", false}, {&ir.IntNode{}, "time.Duration", true},
		{&ir.FloatNode{}, "int8", true}, {&ir.BoolNode{}, "bool", true}, {&ir.BytesNode{}, "[]byte", true},
		{&ir.TimeNode{}, "time.Time", true}, {&ir.RefNode{Ref: "K"}, "*K", true}, {&ir.RefNode{Ref: "K"}, "J", false},
		{&ir.AnyNode{}, "whatever", true}, {&ir.ArrayNode{}, "[]int", false},
	} {
		if got := variantFits(c.v, c.typ); got != c.want {
			t.Errorf("variantFits(%s, %s) = %v", c.v.Kind(), c.typ, got)
		}
	}
}

func TestHelpers(t *testing.T) {
	cases := map[any]string{
		nil:          "nil",
		"s":          `"s"`,
		true:         "true",
		int(3):       "int(3)",
		int32(3):     "int32(3)",
		int64(3):     "int64(3)",
		float64(1.5): "float64(1.5)",
		uint8(2):     "0x2",
	}
	for in, want := range cases {
		if got := goLiteral(in); got != want {
			t.Errorf("goLiteral(%v) = %s want %s", in, got, want)
		}
	}
	if got := goLiteral([]any{1, "a"}); got != `[]any{int(1), "a"}` {
		t.Error(got)
	}
	if got := goLiteral([]string{"a"}); got != `[]string{"a"}` {
		t.Error(got)
	}
	if got := goLiteral(map[string]any{"b": 1, "a": "x"}); got != `map[string]any{"a": "x", "b": int(1)}` {
		t.Error(got)
	}
	if k, v, ok := splitMapType("map[string]map[int][]string"); !ok || k != "string" || v != "map[int][]string" {
		t.Errorf("splitMapType %s %s %v", k, v, ok)
	}
	if _, _, ok := splitMapType("map[string"); ok {
		t.Error("unterminated map type")
	}
	if _, _, ok := splitMapType("[]string"); ok {
		t.Error("not a map")
	}
	for _, c := range []struct{ typ, want string }{
		{"*T", "x != nil"}, {"[]T", "x != nil"}, {"map[k]v", "x != nil"}, {"string", `x != ""`},
		{"time.Time", "!x.IsZero()"}, {"any", "rt.Present(x)"}, {"interface{}", "rt.Present(x)"}, {"int", "true"},
	} {
		if got := presenceExpr("x", c.typ); got != c.want {
			t.Errorf("presenceExpr(%s) = %s", c.typ, got)
		}
	}
	if lenExpr("x", "string") != "rt.RuneLen(x)" || lenExpr("x", "[]byte") != "len(x)" {
		t.Error("lenExpr")
	}
	g := &gen{hoisted: map[string]string{}}
	if got := g.membership("x", "string", []any{"a", "b"}); got != `(x == "a" || x == "b")` {
		t.Error(got)
	}
	if got := g.membership("x", "int32", []any{1, 2.5}); got != `(float64(x) == float64(int(1)) || float64(x) == float64(float64(2.5)))` {
		t.Error(got)
	}
	if got := g.membership("x", "bool", []any{true}); got != `(x == true)` {
		t.Error(got)
	}
	if got := g.membership("x", "any", []any{"a", 1}); got != `rt.In(x, lit1)` || g.membership("y", "any", []any{"a", 1}) != `rt.In(y, lit1)` {
		t.Error("fallback must hoist once:", got)
	}
	if got := g.compare("x", "int", 3, ir.OpGte); got != "float64(x) >= float64(int(3))" {
		t.Error(got)
	}
	if got := g.compare("x", "string", "m", ir.OpLt); got != `x < "m"` {
		t.Error(got)
	}
	if got := g.compare("x", "time.Time", "m", ir.OpLt); got != `rt.Compare(x, "m", "lt")` {
		t.Error(got)
	}
	if got := g.compareFields("a", "int", "b", "float64", ir.OpGt); got != "float64(a) > float64(b)" {
		t.Error(got)
	}
	if got := g.compareFields("a", "string", "b", "string", ir.OpNe); got != "a != b" {
		t.Error(got)
	}
	if got := g.compareFields("a", "time.Time", "b", "time.Time", ir.OpLte); got != `rt.Compare(a, b, "lte")` {
		t.Error(got)
	}
	if !comparableScalar("*T") || comparableScalar("[]T") || !numericGoType("time.Duration") || numericGoType("string") {
		t.Error("type predicates")
	}
	if e, ok := sliceElem("[]int"); !ok || e != "int" {
		t.Error("sliceElem")
	}
	if allStrings(nil) || allNumbers(nil) || allStrings([]any{"a", 1}) || !allNumbers([]any{1, 2.0}) {
		t.Error("all predicates")
	}
	for _, n := range []ir.TypeNode{&ir.ObjectNode{}, &ir.StringNode{}, &ir.IntNode{}, &ir.FloatNode{}, &ir.BoolNode{}, &ir.BytesNode{}, &ir.TimeNode{}, &ir.DurationNode{}, &ir.ArrayNode{}, &ir.MapNode{}, &ir.RefNode{}, &ir.AnyNode{}, &ir.UnionNode{}} {
		if nullableOf(n) {
			t.Errorf("%s nullable by default", n.Kind())
		}
	}
	if wireOfGo(&ir.ObjectNode{}, "X") != "X" {
		t.Error("wireOfGo fallback")
	}
	_ = context.Background()
}

// TestCELGatesAreDelegated pins the decision not to compile CEL into Go.
// Two implementations of a language's semantics drift; one does not. A
// gate whose condition is an expression is handed to the interpreter,
// which is the only place CEL is evaluated.
func TestCELGatesAreDelegated(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "m.go"),
		[]byte("package m\n\ntype M struct {\n\tName string\n\tN int\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	type M struct {
		Name string
		N    int
	}
	fName := vb.NewField("Name", func(m M) string { return m.Name })
	fN := vb.NewField("N", func(m M) int { return m.N })

	schema := vb.Struct("M",
		vb.F(fName, vb.String().MinLength(2)),
		vb.F(fN, vb.Int[int]()),
	).Gate(
		// One gate codegen can compile, one it cannot.
		vb.Requires(vb.Cmp(fN, ir.OpGt, 5), fName),
		vb.When(ir.Expr("n > 1"), []ir.TargetRule{vb.On(fName, ir.MinLength(4))}, nil),
	).MustBuild().IR()

	res, err := Generate(Options{Schema: schema, Dir: dir})
	if err != nil {
		t.Fatalf("CEL gates should generate, not refuse: %v", err)
	}
	src := string(res.Source)
	for _, want := range []string{
		"CEL gates: delegated to the interpreter",
		"rt.Interpret(",
		"rt.MustSchema(",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated source lacks %q", want)
		}
	}
	// The compilable gate is still compiled inline, not swept into the
	// fallback along with the CEL one.
	if !strings.Contains(src, "float64(v.N) > float64(int(5))") {
		t.Error("the non-CEL gate should still be inline Go")
	}
	// The embedded subtree carries the gates and no field rules, so the
	// fallback cannot re-report what the generated code already checked.
	if strings.Contains(src, "minLength") && strings.Contains(src, `\"value\":2`) {
		t.Error("field rules leaked into the delegated subtree")
	}
}
