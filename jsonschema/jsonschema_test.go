package jsonschema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/vb"
)

func golden(t *testing.T) *ir.Schema {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "ir", "testdata", "user.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s ir.Schema
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	return &s
}

// compareGolden pins an emitted document to testdata/<name>; regenerate
// with UPDATE_GOLDEN=1 when a change is intended.
func compareGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run with UPDATE_GOLDEN=1 to create)", err)
	}
	if string(want) != string(got) {
		t.Fatalf("golden %s mismatch; run UPDATE_GOLDEN=1 go test ./jsonschema if intended\n%s", name, got)
	}
}

func TestGolden2020(t *testing.T) {
	res, err := Emit(golden(t), WithID("https://acme.dev/schemas/user.json"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := res.JSON()
	if err != nil {
		t.Fatal(err)
	}
	compareGolden(t, "user.2020.schema.json", out)
	man, _ := json.MarshalIndent(res.Manifest, "", "  ")
	compareGolden(t, "user.manifest.json", append(man, '\n'))

	// The emission is deterministic.
	again, _ := Emit(golden(t), WithID("https://acme.dev/schemas/user.json"))
	out2, _ := again.JSON()
	if string(out) != string(out2) {
		t.Fatal("emission is not deterministic")
	}
	// And the document round-trips through the ordered decoder unchanged.
	var back Object
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	re, _ := json.MarshalIndent(&back, "", "  ")
	if string(append(re, '\n')) != string(out) {
		t.Fatalf("ordered round trip changed the document\n%s", re)
	}
}

func TestGolden07(t *testing.T) {
	res, err := Emit(golden(t), WithDraft(Draft07))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := res.JSON()
	compareGolden(t, "user.07.schema.json", out)
	s := string(out)
	if !strings.Contains(s, `"definitions"`) || strings.Contains(s, `"$defs"`) || !strings.Contains(s, `"#/definitions/Address"`) {
		t.Error("draft-07 must use definitions")
	}
	if !strings.Contains(s, Draft07.refPrefix()) {
		t.Error("ref prefix")
	}
}

func TestManifestOfGolden(t *testing.T) {
	res, err := Emit(golden(t))
	if err != nil {
		t.Fatal(err)
	}
	m := res.Manifest
	if m.Portable() {
		t.Fatal("golden has non-portable rules; manifest must not be empty")
	}
	type key struct {
		path, name string
		class      Class
	}
	want := map[key]bool{
		{"$.email", "custom", ClassOpaque}: false,
		{"$.email", "async", ClassServer}:  false,
		{"$.email", "trim", ClassLossy}:    false,
		{"$.email", "lower", ClassLossy}:   false,
		{"$.end", "after", ClassCEL}:       false,
		{"$", "when", ClassCEL}:            false, // the Or(fieldcmp, expr, custom) gate
	}
	for _, e := range m.Entries {
		k := key{e.path(), e.Name, e.Class}
		if _, ok := want[k]; ok {
			want[k] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("manifest missing %+v; got %+v", k, m.Entries)
		}
	}
	by := m.ByClass()
	if len(by[ClassOpaque]) != 1 || len(by[ClassServer]) != 1 {
		t.Errorf("ByClass %v", by)
	}
	for _, e := range m.Entries {
		if e.Name == "async" && e.Endpoint != "/v/unique_email" {
			t.Errorf("async endpoint lost: %+v", e)
		}
		if e.Reason == "" {
			t.Errorf("entry without reason: %+v", e)
		}
	}
	if m.Len() != len(m.Entries) {
		t.Error("Len")
	}
}

func (e Entry) path() string { return e.Path }

func emit(t *testing.T, n ir.TypeNode, defs ...map[string]ir.TypeNode) *Object {
	t.Helper()
	s := &ir.Schema{Version: ir.Version, Root: n}
	if len(defs) > 0 {
		s.Defs = defs[0]
	}
	res, err := Emit(s)
	if err != nil {
		t.Fatal(err)
	}
	// Drop the dialect marker so shape assertions read as bare nodes.
	body := NewObject()
	for _, k := range res.Schema.Keys() {
		if k != "$schema" {
			v, _ := res.Schema.Get(k)
			body.Set(k, v)
		}
	}
	return body
}

func text(t *testing.T, o *Object) string {
	t.Helper()
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestEveryRuleKeyword(t *testing.T) {
	str := func(rules ...ir.Rule) ir.TypeNode { return &ir.StringNode{Rules: rules} }
	num := func(rules ...ir.Rule) ir.TypeNode { return &ir.FloatNode{Rules: rules} }
	arr := func(rules ...ir.Rule) ir.TypeNode { return &ir.ArrayNode{Elem: &ir.AnyNode{}, Rules: rules} }
	mp := func(rules ...ir.Rule) ir.TypeNode {
		return &ir.MapNode{Key: &ir.StringNode{}, Val: &ir.AnyNode{}, Rules: rules}
	}
	cases := []struct {
		node ir.TypeNode
		want string
	}{
		{str(ir.MinLength(1)), `"minLength":1`},
		{str(ir.MaxLength(9)), `"maxLength":9`},
		{str(ir.Pattern("^a")), `"pattern":"^a"`},
		{str(ir.Enum("a", "b")), `"enum":["a","b"]`},
		{str(ir.Const("a")), `"const":"a"`},
		{str(ir.Email()), `"format":"email"`},
		{str(ir.URL()), `"format":"uri"`},
		{str(ir.UUID()), `"format":"uuid"`},
		{str(ir.ULID()), `"pattern":"` + strings.ReplaceAll(ulidPattern, `\`, `\\`)},
		{str(ir.IPv4()), `"format":"ipv4"`},
		{str(ir.IPv6()), `"format":"ipv6"`},
		{str(ir.Hostname()), `"format":"hostname"`},
		{str(ir.Date()), `"format":"date"`},
		{str(ir.DateTime()), `"format":"date-time"`},
		{str(ir.IP()), `"anyOf":[{"format":"ipv4"},{"format":"ipv6"}]`},
		{str(ir.CIDR()), `"pattern":"^(?:(?:25[0-5]`},
		{str(ir.MAC()), `"pattern":"^(?:[0-9A-Fa-f]{2}[:-]){5}`},
		{str(ir.Base64()), `"pattern":"^(?:[A-Za-z0-9+/]{4})*`},
		{str(ir.Semver()), `"pattern":"^(0|[1-9]`},
		{str(ir.NotBlank()), `"pattern":"\\S"`},
		{str(ir.JWT()), `"pattern":"^[A-Za-z0-9_-]+`},
		{str(ir.CountryCode()), `"pattern":"^[A-Z]{2}$"`},
		{str(ir.CurrencyCode()), `"pattern":"^[A-Z]{3}$"`},
		{str(ir.HexColor()), `"pattern":"^#(?:`},
		{str(ir.E164()), `"pattern":"^\\+[1-9]`},
		{str(ir.CreditCard()), `"pattern":"^[0-9]{12,19}$"`},
		{num(ir.Latitude()), `"minimum":-90,"maximum":90`},
		{num(ir.Longitude()), `"minimum":-180,"maximum":180`},
		{num(ir.Min(1)), `"minimum":1`},
		{num(ir.Gte(1)), `"minimum":1`},
		{num(ir.Max(1)), `"maximum":1`},
		{num(ir.Lte(1)), `"maximum":1`},
		{num(ir.Gt(1)), `"exclusiveMinimum":1`},
		{num(ir.Lt(1)), `"exclusiveMaximum":1`},
		{num(ir.MultipleOf(2)), `"multipleOf":2`},
		{arr(ir.MinItems(1)), `"minItems":1`},
		{arr(ir.MaxItems(1)), `"maxItems":1`},
		{arr(ir.Unique()), `"uniqueItems":true`},
		{mp(ir.MinItems(1)), `"minProperties":1`},
		{mp(ir.MaxItems(1)), `"maxProperties":1`},
		{&ir.StringNode{Rules: []ir.Rule{{Kind: ir.RuleMinLength, Params: map[string]any{ir.ParamValue: 1}, Meta: ir.NodeMeta{Code: "short", Message: "too short"}}}}, `"x-code":"short","x-message":"too short"`},
	}
	for _, c := range cases {
		if got := text(t, emit(t, c.node)); !strings.Contains(got, c.want) {
			t.Errorf("want %s in %s", c.want, got)
		}
	}
	// Non-portable rule kinds produce no keyword and one manifest entry.
	for _, r := range []ir.Rule{ir.After("x"), ir.Before("x"), ir.Custom("f"), ir.Async("g", "/v/g")} {
		res, _ := Emit(&ir.Schema{Version: ir.Version, Root: &ir.TimeNode{Rules: []ir.Rule{r}}})
		if res.Manifest.Len() != 1 || res.Manifest.Entries[0].Name != string(r.Kind) {
			t.Errorf("%s: manifest %+v", r.Kind, res.Manifest)
		}
	}
	// creditCard exports its shape AND reports the checksum as lossy.
	resCC, _ := Emit(&ir.Schema{Version: ir.Version, Root: &ir.StringNode{Rules: []ir.Rule{ir.CreditCard()}}})
	if resCC.Manifest.Len() != 1 || resCC.Manifest.Entries[0].Class != ClassLossy || !strings.Contains(text(t, resCC.Schema), "pattern") {
		t.Errorf("creditCard: %+v", resCC.Manifest)
	}
	// json cannot be expressed: reported, not emitted.
	res0, _ := Emit(&ir.Schema{Version: ir.Version, Root: &ir.StringNode{Rules: []ir.Rule{ir.JSON()}}})
	if res0.Manifest.Len() != 1 || res0.Manifest.Entries[0].Name != "json" || res0.Manifest.Entries[0].Class != ClassLossy {
		t.Errorf("json rule: %+v", res0.Manifest)
	}
	// Byte-length rules on base64 strings are lossy.
	res, _ := Emit(&ir.Schema{Version: ir.Version, Root: &ir.BytesNode{Rules: []ir.Rule{ir.MinLength(1), ir.MaxLength(2)}}})
	if res.Manifest.Len() != 2 || res.Manifest.Entries[0].Class != ClassLossy {
		t.Errorf("bytes length: %+v", res.Manifest)
	}
	if got := text(t, res.Schema); strings.Contains(got, "minLength") {
		t.Errorf("bytes length must not be emitted: %s", got)
	}
}

func TestNodeShapes(t *testing.T) {
	cases := []struct {
		name string
		node ir.TypeNode
		want string
	}{
		{"int", &ir.IntNode{}, `{"type":"integer"}`},
		{"int32", &ir.IntNode{Bits: 32}, `"minimum":-2147483648,"maximum":2147483647`},
		{"uint8", &ir.IntNode{Bits: 8, Unsigned: true}, `"minimum":0,"maximum":255`},
		{"uint64", &ir.IntNode{Bits: 64, Unsigned: true}, `{"type":"integer","minimum":0}`},
		{"int64", &ir.IntNode{Bits: 64}, `{"type":"integer"}`},
		{"bool", &ir.BoolNode{Rules: []ir.Rule{ir.Const(true)}}, `{"type":"boolean","const":true}`},
		{"bytes", &ir.BytesNode{}, `"contentEncoding":"base64"`},
		{"time", &ir.TimeNode{}, `"format":"date-time"`},
		{"duration", &ir.DurationNode{Rules: []ir.Rule{ir.Gt(0)}}, `"exclusiveMinimum":0`},
		{"any", &ir.AnyNode{}, `{}`},
		{"nullable scalar", &ir.StringNode{Base: ir.Base{Nullable: true}}, `{"type":["string","null"]}`},
		{"nullable with rules", &ir.StringNode{Base: ir.Base{Nullable: true}, Rules: []ir.Rule{ir.Email()}}, `{"anyOf":[{"type":"string","format":"email"},{"type":"null"}]}`},
		{"nullable any", &ir.AnyNode{Base: ir.Base{Nullable: true}}, `{"anyOf":[{},{"type":"null"}]}`},
		{"description", &ir.StringNode{Base: ir.Base{Meta: ir.NodeMeta{Description: "d", Examples: []any{"x"}}}}, `"description":"d","examples":["x"]`},
		{"array", &ir.ArrayNode{Elem: &ir.IntNode{}}, `{"type":"array","items":{"type":"integer"}}`},
		{"map", &ir.MapNode{Key: &ir.StringNode{}, Val: &ir.IntNode{}}, `{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"type":"integer"}}`},
		{"union", &ir.UnionNode{Variants: []ir.TypeNode{&ir.IntNode{}, &ir.StringNode{}}}, `{"anyOf":[{"type":"integer"},{"type":"string"}]}`},
		{"object forbid", &ir.ObjectNode{Extra: ir.ExtraForbid, Fields: []ir.FieldNode{{Name: "A", JSONName: "a", Type: &ir.AnyNode{}}, {Name: "B", Type: &ir.AnyNode{}, Optional: true}}}, `{"type":"object","properties":{"a":{},"B":{}},"required":["a"],"additionalProperties":false}`},
		{"object allow", &ir.ObjectNode{Extra: ir.ExtraAllow}, `"additionalProperties":true`},
		{"field description + default", &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "a", Type: &ir.StringNode{}, Meta: ir.NodeMeta{Description: "fd"}, Transforms: []ir.Transform{{Kind: ir.TransformDefault, Phase: ir.PhasePre, Params: map[string]any{ir.ParamValue: "x"}}}}}}, `"a":{"type":"string","description":"fd","default":"x"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := text(t, emit(t, c.node)); !strings.Contains(got, c.want) {
				t.Errorf("want %s in %s", c.want, got)
			}
		})
	}
	// Ref uses the dialect prefix.
	got := text(t, emit(t, &ir.RefNode{Ref: "A"}, map[string]ir.TypeNode{"A": &ir.AnyNode{}}))
	if !strings.Contains(got, `"$ref":"#/$defs/A"`) || !strings.Contains(got, `"$defs":{"A":{}}`) {
		t.Errorf("ref: %s", got)
	}
}

func TestOneOf(t *testing.T) {
	obj := func(tag string) ir.TypeNode {
		return &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "kind", Type: &ir.StringNode{Rules: []ir.Rule{ir.Const(tag)}}}}}
	}
	// Without default: discriminator restricted to known tags.
	got := text(t, emit(t, &ir.OneOfNode{Discriminator: "kind", Cases: map[string]ir.TypeNode{"b": obj("b"), "a": obj("a")}}))
	for _, want := range []string{
		`"properties":{"kind":{"enum":["a","b"]}},"required":["kind"]`,
		`"if":{"properties":{"kind":{"const":"a"}},"required":["kind"]},"then":{"type":"object"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %s in %s", want, got)
		}
	}
	if strings.Contains(got, `"oneOf"`) {
		t.Error("discriminated unions must never emit raw oneOf")
	}
	// With default: discriminator is any string and unknown tags route to
	// the default.
	got = text(t, emit(t, &ir.OneOfNode{Discriminator: "kind", Cases: map[string]ir.TypeNode{"a": obj("a")}, Default: &ir.AnyNode{}}))
	if !strings.Contains(got, `"kind":{"type":"string"}`) || !strings.Contains(got, `"if":{"not":{"properties":{"kind":{"enum":["a"]}},"required":["kind"]}},"then":{}`) {
		t.Errorf("default routing: %s", got)
	}
}

func TestConditionsAndGates(t *testing.T) {
	fields := []ir.FieldNode{
		{Name: "Type", JSONName: "type", Type: &ir.StringNode{}},
		{Name: "N", JSONName: "n", Type: &ir.IntNode{}},
		{Name: "URL", JSONName: "url", Type: &ir.StringNode{}, Optional: true},
		{Name: "Venue", JSONName: "venue", Type: &ir.StringNode{}, Optional: true},
	}
	mk := func(gates ...ir.GatedRules) string {
		return text(t, emit(t, &ir.ObjectNode{Fields: fields, Gated: gates}))
	}
	then := []ir.TargetRule{ir.On("URL", ir.MinLength(1))}
	els := []ir.TargetRule{ir.On("Venue", ir.MinLength(1))}
	cases := []struct {
		name string
		gate ir.GatedRules
		want string
	}{
		{"when eq", ir.When(ir.Cmp("Type", ir.OpEq, "online"), then, els), `{"if":{"properties":{"type":{"const":"online"}},"required":["type"]},"then":{"properties":{"url":{"type":"string","minLength":1}}},"else":{"properties":{"venue":{"type":"string","minLength":1}}}}`},
		{"when ne", ir.When(ir.Cmp("Type", ir.OpNe, "x"), then, nil), `"if":{"properties":{"type":{"not":{"const":"x"}}},"required":["type"]}`},
		{"when gt", ir.When(ir.Cmp("N", ir.OpGt, 3), then, nil), `"n":{"exclusiveMinimum":3}`},
		{"when gte", ir.When(ir.Cmp("N", ir.OpGte, 3), then, nil), `"n":{"minimum":3}`},
		{"when lt", ir.When(ir.Cmp("N", ir.OpLt, 3), then, nil), `"n":{"exclusiveMaximum":3}`},
		{"when lte", ir.When(ir.Cmp("N", ir.OpLte, 3), then, nil), `"n":{"maximum":3}`},
		{"when present", ir.When(ir.HasField("URL", ir.Present), then, nil), `"if":{"required":["url"]}`},
		{"when absent", ir.When(ir.HasField("URL", ir.Absent), then, nil), `"if":{"not":{"required":["url"]}}`},
		{"when in", ir.When(ir.In("Type", "a", "b"), then, nil), `"if":{"properties":{"type":{"enum":["a","b"]}},"required":["type"]}`},
		{"when and", ir.When(ir.And(ir.HasField("URL", ir.Present), ir.In("Type", "a")), then, nil), `"if":{"allOf":[{"required":["url"]},{"properties":{"type":{"enum":["a"]}},"required":["type"]}]}`},
		{"when or", ir.When(ir.Or(ir.HasField("URL", ir.Present), ir.HasField("Venue", ir.Present)), then, nil), `"if":{"anyOf":[{"required":["url"]},{"required":["venue"]}]}`},
		{"when not", ir.When(ir.Not(ir.HasField("URL", ir.Present)), then, nil), `"if":{"not":{"required":["url"]}}`},
		{"requires", ir.Requires(ir.Cmp("Type", ir.OpEq, "online"), "URL", "Venue"), `"then":{"required":["url","venue"]}`},
		{"group exactly-one", ir.Group(ir.ExactlyOne, "URL", "Venue"), `"oneOf":[{"required":["url"]},{"required":["venue"]}]`},
		{"group at-least-one", ir.Group(ir.AtLeastOne, "URL", "Venue"), `"anyOf":[{"required":["url"]},{"required":["venue"]}]`},
		{"group all-or-none", ir.Group(ir.AllOrNone, "URL", "Venue"), `"oneOf":[{"required":["url","venue"]},{"not":{"anyOf":[{"required":["url"]},{"required":["venue"]}]}}]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mk(c.gate); !strings.Contains(got, c.want) {
				t.Errorf("want %s in %s", c.want, got)
			}
		})
	}
	// Inexpressible conditions go to the manifest, one entry per gate.
	for _, c := range []struct {
		cond  ir.Condition
		class Class
	}{
		{ir.FieldCmp("N", ir.OpGt, "N"), ClassCEL},
		{ir.Expr("n > 1"), ClassCEL},
		{ir.CustomCond("f"), ClassOpaque},
		{ir.Cmp("Type", ir.OpGt, "m"), ClassCEL},
		{ir.And(ir.HasField("URL", ir.Present), ir.Expr("x")), ClassCEL},
		{ir.Not(ir.CustomCond("g")), ClassOpaque},
	} {
		res, err := Emit(&ir.Schema{Version: ir.Version, Root: &ir.ObjectNode{Fields: fields, Gated: []ir.GatedRules{ir.When(c.cond, then, nil), ir.Requires(c.cond, "URL")}}})
		if err != nil {
			t.Fatal(err)
		}
		if res.Manifest.Len() != 2 || res.Manifest.Entries[0].Class != c.class || res.Manifest.Entries[0].Kind != KindGate {
			t.Errorf("%s: manifest %+v", c.cond.Kind, res.Manifest.Entries)
		}
		if strings.Contains(text(t, res.Schema), `"allOf"`) {
			t.Errorf("%s: inexpressible gate must emit nothing", c.cond.Kind)
		}
	}
	// A gate whose then/else only carry non-portable rules emits nothing.
	res, _ := Emit(&ir.Schema{Version: ir.Version, Root: &ir.ObjectNode{Fields: fields, Gated: []ir.GatedRules{ir.When(ir.HasField("URL", ir.Present), []ir.TargetRule{ir.On("URL", ir.Custom("f"))}, nil)}}})
	if strings.Contains(text(t, res.Schema), `"allOf"`) || res.Manifest.Len() != 1 {
		t.Errorf("non-portable target: %s %+v", text(t, res.Schema), res.Manifest)
	}
	// Two target rules on the same field merge into one property schema.
	got := mk(ir.When(ir.HasField("URL", ir.Present), []ir.TargetRule{ir.On("Venue", ir.MinLength(1)), ir.On("Venue", ir.MaxLength(5))}, nil))
	if !strings.Contains(got, `"venue":{"type":"string","minLength":1,"maxLength":5}`) {
		t.Errorf("merge: %s", got)
	}
}

func TestLiftedFieldConditions(t *testing.T) {
	// A conditional rule on a field becomes an if/then at object level.
	cond := ir.Cmp("Type", ir.OpEq, "online")
	root := &ir.ObjectNode{Fields: []ir.FieldNode{
		{Name: "Type", JSONName: "type", Type: &ir.StringNode{}},
		{Name: "URL", JSONName: "url", Type: &ir.StringNode{Rules: []ir.Rule{
			{Kind: ir.RuleMinLength, Params: map[string]any{ir.ParamValue: 1}, Cond: &cond},
			ir.Email(),
		}}},
	}}
	got := text(t, emit(t, root))
	if !strings.Contains(got, `"url":{"type":"string","format":"email"}`) {
		t.Errorf("unconditional rule must stay on the property: %s", got)
	}
	if !strings.Contains(got, `"allOf":[{"if":{"properties":{"type":{"const":"online"}},"required":["type"]},"then":{"properties":{"url":{"type":"string","minLength":1}}}}]`) {
		t.Errorf("lifted rule: %s", got)
	}
	// Inexpressible condition on a field rule → manifest, nothing emitted.
	bad := ir.Expr("x")
	root.Fields[1].Type.(*ir.StringNode).Rules[0].Cond = &bad
	res, _ := Emit(&ir.Schema{Version: ir.Version, Root: root})
	if res.Manifest.Len() != 1 || res.Manifest.Entries[0].Kind != KindCondition || strings.Contains(text(t, res.Schema), "allOf") {
		t.Errorf("lifted inexpressible: %s %+v", text(t, res.Schema), res.Manifest)
	}
	// Conditional non-portable rule: manifest for the rule, no clause.
	custom := ir.Custom("f")
	custom.Cond = &cond
	root.Fields[1].Type.(*ir.StringNode).Rules[0] = custom
	res, _ = Emit(&ir.Schema{Version: ir.Version, Root: root})
	if strings.Contains(text(t, res.Schema), "allOf") || res.Manifest.Len() != 1 || res.Manifest.Entries[0].Name != "custom" {
		t.Errorf("conditional custom: %s %+v", text(t, res.Schema), res.Manifest)
	}
}

func TestEmitRefusesBadIR(t *testing.T) {
	if _, err := Emit(&ir.Schema{Version: "v0", Root: &ir.AnyNode{}}); err == nil {
		t.Error("bad version accepted")
	}
	if _, err := Emit(nil); err == nil {
		t.Error("nil accepted")
	}
}

func TestObject(t *testing.T) {
	o := NewObject().Set("b", 1).Set("a", 2).Set("b", 3)
	if got := text(t, o); got != `{"b":3,"a":2}` {
		t.Errorf("order/replace: %s", got)
	}
	if keys := o.Keys(); len(keys) != 2 || keys[0] != "b" || o.Len() != 2 {
		t.Errorf("keys %v", keys)
	}
	if v, ok := o.Get("a"); !ok || v != 2 {
		t.Error("Get")
	}
	m := NewObject().Set("z", 0).Merge(o)
	if got := text(t, m); got != `{"z":0,"b":3,"a":2}` {
		t.Errorf("merge: %s", got)
	}
	// Decode errors.
	var bad Object
	for _, raw := range []string{`[1]`, `{"a":`, `{`, `42x`} {
		if err := json.Unmarshal([]byte(raw), &bad); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	var nested Object
	if err := json.Unmarshal([]byte(`{"a":[{"y":1,"x":2}],"b":null}`), &nested); err != nil {
		t.Fatal(err)
	}
	if got := text(t, &nested); got != `{"a":[{"y":1,"x":2}],"b":null}` {
		t.Errorf("nested order: %s", got)
	}
	// A value that cannot marshal surfaces the key.
	if _, err := json.Marshal(NewObject().Set("f", func() {})); err == nil || !strings.Contains(err.Error(), `key "f"`) {
		t.Errorf("marshal error: %v", err)
	}
	if _, err := decodeValue(json.RawMessage(`[{`)); err == nil {
		t.Error("bad nested array accepted")
	}
	if _, err := decodeValue(json.RawMessage(`{"a":1`)); err == nil {
		t.Error("bad nested object accepted")
	}
	if _, err := decodeValue(json.RawMessage(`[1,}`)); err == nil {
		t.Error("bad array accepted")
	}
}

func TestHelpers(t *testing.T) {
	if wireOf(&ir.ObjectNode{}, "X") != "X" {
		t.Error("wireOf fallback")
	}
	if isNumber("x") || !isNumber(int8(1)) || !isNumber(2.5) {
		t.Error("isNumber")
	}
	if c, _ := condClass(ir.And(ir.HasField("a", ir.Present))); c != "" {
		t.Error("expressible composite has no class")
	}
	for _, n := range []ir.TypeNode{&ir.ObjectNode{}, &ir.StringNode{}, &ir.IntNode{}, &ir.FloatNode{}, &ir.BoolNode{}, &ir.BytesNode{}, &ir.TimeNode{}, &ir.DurationNode{}, &ir.ArrayNode{}, &ir.MapNode{}, &ir.UnionNode{}, &ir.OneOfNode{}, &ir.RefNode{}, &ir.AnyNode{}} {
		if nullableOf(n) {
			t.Errorf("%s nullable by default", n.Kind())
		}
	}
	if Draft2020.defsKey() != "$defs" || Draft07.defsKey() != "definitions" {
		t.Error("defsKey")
	}
}

func TestTSModule(t *testing.T) {
	res, err := Emit(golden(t))
	if err != nil {
		t.Fatal(err)
	}
	out, err := res.TSModule("user")
	if err != nil {
		t.Fatal(err)
	}
	src := string(out)
	for _, want := range []string{"export const userSchema = {", "} as const;", "export const userManifest = {", `"entries": [`, "DO NOT EDIT"} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q", want)
		}
	}
	compareGolden(t, "user.schema.ts", out)
	if _, err := res.TSModule(""); err == nil {
		t.Error("name required")
	}
}

func TestInline(t *testing.T) {
	res, _ := Emit(golden(t))
	inl := Inline(res.Schema)
	src := text(t, inl)
	if strings.Contains(src, `"$ref"`) || strings.Contains(src, `"$defs"`) {
		t.Errorf("golden refs are non-recursive and must all inline: %s", src)
	}
	if !strings.Contains(src, `"home":{"type":"object","properties":{"street":`) {
		t.Errorf("home not inlined: %s", src)
	}
	// The original is untouched.
	if !strings.Contains(text(t, res.Schema), `"$ref":"#/$defs/Address"`) {
		t.Error("Inline must copy")
	}
	// A recursive def keeps its $ref and the container.
	rec := &ir.Schema{Version: ir.Version, Meta: ir.SchemaMeta{Title: "Node"},
		Root: &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "next", Type: &ir.RefNode{Ref: "Node"}, Optional: true}}},
		Defs: map[string]ir.TypeNode{"Node": &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "next", Type: &ir.RefNode{Ref: "Node"}, Optional: true}}}}}
	rres, err := Emit(rec)
	if err != nil {
		t.Fatal(err)
	}
	rsrc := text(t, Inline(rres.Schema))
	if !strings.Contains(rsrc, `"$ref":"#/$defs/Node"`) || !strings.Contains(rsrc, `"$defs"`) {
		t.Errorf("recursive ref must survive: %s", rsrc)
	}
	// Draft-07 container name and no-defs documents.
	r07, _ := Emit(golden(t), WithDraft(Draft07))
	if strings.Contains(text(t, Inline(r07.Schema)), "definitions") {
		t.Error("draft-07 defs not inlined")
	}
	plain := NewObject().Set("type", "string")
	if text(t, Inline(plain)) != `{"type":"string"}` {
		t.Error("no defs")
	}
}

func TestContract(t *testing.T) {
	doc := Contract()
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	compareGolden(t, "ir.contract.schema.json", append(out, '\n'))
	src := string(out)
	for _, want := range []string{
		`"$id": "` + ContractID + `"`,
		`"const": "` + ir.Version + `"`,
		`"TypeNode": {`,
		`"$ref": "#/$defs/ObjectNode"`,
		`"$ref": "#/$defs/AnyNode"`,
		`"kind": {`,
		`"const": "oneof"`,
		`"Condition": {`,
		`"GatedRules": {`,
		`"Rule": {`,
		`"Transform": {`,
		`"NodeMeta": {`,
		`"SchemaMeta": {`,
		`"additionalProperties": false`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q", want)
		}
	}
	// Every node kind has a def and appears in the union.
	defs, _ := doc.Get("$defs")
	union, _ := defs.(*Object).Get("TypeNode")
	variants, _ := union.(*Object).Get(kwOneOf)
	if len(variants.([]any)) != len(nodeKinds) {
		t.Errorf("union has %d variants, want %d", len(variants.([]any)), len(nodeKinds))
	}
	for _, nk := range nodeKinds {
		if _, ok := defs.(*Object).Get(nk.typ.Name()); !ok {
			t.Errorf("no def for %s", nk.typ.Name())
		}
	}
	// The root is inlined, never also a def.
	if _, dup := defs.(*Object).Get("Schema"); dup {
		t.Error("root Schema emitted as a def too")
	}
	// The wire names the contract hardcodes are the Go json tags.
	if tag, _, _ := strings.Cut(reflect.TypeFor[ir.Schema]().Field(0).Tag.Get("json"), ","); tag != wireVersion {
		t.Errorf("ir.Schema version tag %q != %q", tag, wireVersion)
	}
	if raw, _ := json.Marshal(&ir.AnyNode{}); !strings.Contains(string(raw), `"`+wireKind+`":"any"`) {
		t.Errorf("node discriminator is not %q: %s", wireKind, raw)
	}
	// Deterministic.
	again, _ := json.Marshal(Contract())
	first, _ := json.Marshal(doc)
	if string(again) != string(first) {
		t.Error("contract is not deterministic")
	}
}

func TestStrictTransformManifest(t *testing.T) {
	type S struct{ N int }
	f := vb.NewField("N", func(s S) int { return s.N }).JSON("n")
	s := vb.Struct("S", vb.F(f, vb.Int[int]()).NoCoerce()).MustBuild()
	res, err := Emit(s.IR())
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range res.Manifest.Entries {
		if e.Kind == KindTransform && e.Name == "strict" && e.Class == ClassLossy && e.Path == "$.n" && strings.Contains(e.Reason, "pinned strict") {
			found = true
		}
	}
	if !found {
		t.Errorf("strict entry missing: %+v", res.Manifest.Entries)
	}
	// It must not enable coercion on its own.
	for _, e := range res.Manifest.Entries {
		if e.Name == "coerce" {
			t.Error("strict produced a coerce entry")
		}
	}
}
