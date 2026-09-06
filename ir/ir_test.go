package ir

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// userSchema exercises every node kind, every gate kind, transforms, and
// both non-portable rule tiers — the fixture the round-trip and Check tests
// share, and the source of testdata/user.json.
func userSchema() *Schema {
	return &Schema{
		Version: Version,
		Meta:    SchemaMeta{Title: "User", Source: "github.com/acme/app/models.User", Generator: "test"},
		Defs: map[string]TypeNode{
			"Address": &ObjectNode{Fields: []FieldNode{
				{Name: "Street", JSONName: "street", Type: &StringNode{Rules: []Rule{MinLength(1)}}},
				{Name: "Zip", JSONName: "zip", Type: &StringNode{Rules: []Rule{Pattern(`^[0-9]{5}$`)}}},
			}},
		},
		Root: &ObjectNode{
			Extra: ExtraForbid,
			Fields: []FieldNode{
				{Name: "Email", JSONName: "email", Type: &StringNode{Rules: []Rule{
					Email(),
					Custom("no_temp_domains"),
					Async("unique_email", "/v/unique_email"),
				}}, Transforms: []Transform{{Kind: TransformTrim, Phase: PhasePre}, {Kind: TransformLower, Phase: PhasePre}}},
				{Name: "Age", JSONName: "age", Type: &IntNode{Bits: 32, Rules: []Rule{Min(18), Max(120)}}},
				{Name: "Score", JSONName: "score", Type: &FloatNode{Bits: 64, Rules: []Rule{Gte(0), Lte(1)}}},
				{Name: "Active", JSONName: "active", Type: &BoolNode{}},
				{Name: "Avatar", JSONName: "avatar", Optional: true, Type: &BytesNode{Base: Base{Nullable: true}}},
				{Name: "Start", JSONName: "start", Type: &TimeNode{}},
				{Name: "End", JSONName: "end", Type: &TimeNode{Rules: []Rule{After("Start")}}},
				{Name: "TTL", JSONName: "ttl", Type: &DurationNode{Rules: []Rule{Gt(0)}}},
				{Name: "Tags", JSONName: "tags", Type: &ArrayNode{
					Elem:  &StringNode{Rules: []Rule{MinLength(1), Enum("a", "b")}},
					Rules: []Rule{MinItems(1), MaxItems(10), Unique()},
				}},
				{Name: "Labels", JSONName: "labels", Type: &MapNode{Key: &StringNode{}, Val: &IntNode{Rules: []Rule{MultipleOf(2)}}}},
				{Name: "Home", JSONName: "home", Type: &RefNode{Ref: "Address"}},
				{Name: "Extra", JSONName: "extra", Type: &AnyNode{}},
				{Name: "IDOrName", JSONName: "idOrName", Type: &UnionNode{Variants: []TypeNode{&IntNode{}, &StringNode{Rules: []Rule{UUID(), ULID(), URL(), Const("x")}}}}},
				{Name: "Type", JSONName: "type", Type: &StringNode{Rules: []Rule{Enum("online", "offline")}}},
				{Name: "URL", JSONName: "url", Optional: true, Type: &StringNode{Rules: []Rule{URL()}}},
				{Name: "Venue", JSONName: "venue", Optional: true, Type: &StringNode{}},
				{Name: "Coupon", JSONName: "coupon", Optional: true, Type: &StringNode{}},
				{Name: "Event", JSONName: "event", Type: &OneOfNode{
					Discriminator: "kind",
					Cases: map[string]TypeNode{
						"online":  &ObjectNode{Fields: []FieldNode{{Name: "kind", Type: &StringNode{Rules: []Rule{Const("online")}}}, {Name: "url", Type: &StringNode{Rules: []Rule{URL()}}}}},
						"offline": &ObjectNode{Fields: []FieldNode{{Name: "kind", Type: &StringNode{Rules: []Rule{Const("offline")}}}, {Name: "address", Type: &RefNode{Ref: "Address"}}}},
					},
					Default: &AnyNode{},
				}},
			},
			Gated: []GatedRules{
				When(Cmp("Type", OpEq, "online"), []TargetRule{On("URL", MinLength(1))}, []TargetRule{On("Venue", MinLength(1))}),
				Requires(And(HasField("Coupon", Present), Not(In("Type", "offline"))), "Venue"),
				Group(ExactlyOne, "URL", "Venue"),
				When(Or(FieldCmp("End", OpGt, "Start"), Expr("end > start"), CustomCond("weird")), []TargetRule{On("TTL", Lt(10))}, nil),
			},
		},
	}
}

func TestRoundTrip(t *testing.T) {
	want := userSchema()
	if err := Check(want); err != nil {
		t.Fatalf("fixture must pass Check: %v", err)
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Schema
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, data)
	}
	// JSON numbers decode as float64 and typed slices as []any, so compare
	// through a second marshal: the stable contract is the bytes.
	again, err := json.Marshal(&got)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(data) {
		t.Fatalf("round-trip changed the document\nfirst:  %s\nsecond: %s", data, again)
	}
	if err := Check(&got); err != nil {
		t.Fatalf("decoded document must pass Check: %v", err)
	}
}

// TestGolden pins the serialized shape: any change to the wire format shows
// up as a diff against testdata/user.json. Regenerate deliberately with
// UPDATE_GOLDEN=1 when the change is intended (and bump Version if it is
// breaking).
func TestGolden(t *testing.T) {
	path := filepath.Join("testdata", "user.json")
	data, err := json.MarshalIndent(userSchema(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run with UPDATE_GOLDEN=1 to create)", err)
	}
	if string(want) != string(data) {
		t.Fatalf("golden mismatch; run UPDATE_GOLDEN=1 go test ./ir if intended\n%s", data)
	}
	var s Schema
	if err := json.Unmarshal(want, &s); err != nil {
		t.Fatalf("golden does not decode: %v", err)
	}
	if err := Check(&s); err != nil {
		t.Fatalf("golden does not pass Check: %v", err)
	}
}

func TestKindOnWire(t *testing.T) {
	// Every node kind must carry its discriminator first so the document
	// reads naturally and the decoder's probe finds it.
	nodes := []TypeNode{
		&ObjectNode{}, &StringNode{}, &IntNode{}, &FloatNode{}, &BoolNode{}, &BytesNode{},
		&TimeNode{}, &DurationNode{}, &ArrayNode{Elem: &AnyNode{}}, &MapNode{Key: &StringNode{}, Val: &AnyNode{}},
		&UnionNode{}, &OneOfNode{}, &RefNode{Ref: "x"}, &AnyNode{},
	}
	for _, n := range nodes {
		data, err := json.Marshal(n)
		if err != nil {
			t.Fatalf("%s: %v", n.Kind(), err)
		}
		prefix := `{"kind":"` + string(n.Kind()) + `"`
		if !strings.HasPrefix(string(data), prefix) {
			t.Errorf("%s: want prefix %s, got %s", n.Kind(), prefix, data)
		}
		back, err := DecodeType(data)
		if err != nil {
			t.Fatalf("%s: decode: %v", n.Kind(), err)
		}
		if back.Kind() != n.Kind() {
			t.Errorf("%s: decoded as %s", n.Kind(), back.Kind())
		}
		if reflect.TypeOf(back) != reflect.TypeOf(n) {
			t.Errorf("%s: decoded to %T", n.Kind(), back)
		}
	}
	if _, ok := newNode(Kind("nope")); ok {
		t.Error("newNode accepted an unknown kind")
	}
}

func TestDecodeErrors(t *testing.T) {
	cases := map[string]string{
		"missing kind":   `{"rules":[]}`,
		"unknown kind":   `{"kind":"decimal"}`,
		"null":           `null`,
		"empty":          ``,
		"not an object":  `[1,2]`,
		"bad array elem": `{"kind":"array","elem":{"kind":"nope"}}`,
		"bad map key":    `{"kind":"map","key":{"kind":"nope"},"val":{"kind":"any"}}`,
		"bad map val":    `{"kind":"map","key":{"kind":"string"},"val":{"kind":"nope"}}`,
		"bad variant":    `{"kind":"union","variants":[{"kind":"nope"}]}`,
		"bad case":       `{"kind":"oneof","discriminator":"k","cases":{"a":{"kind":"nope"}}}`,
		"bad default":    `{"kind":"oneof","discriminator":"k","cases":{},"default":{"kind":"nope"}}`,
		"bad field":      `{"kind":"object","fields":[{"name":"a","type":{"kind":"nope"}}]}`,
		"field no type":  `{"kind":"object","fields":[{"name":"a"}]}`,
		"malformed":      `{"kind":"string",`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeType(json.RawMessage(raw)); err == nil {
				t.Fatalf("expected error for %s", raw)
			}
		})
	}
	schemaCases := map[string]string{
		"bad root":  `{"version":"` + Version + `","root":{"kind":"nope"}}`,
		"bad def":   `{"version":"` + Version + `","root":{"kind":"any"},"defs":{"A":{"kind":"nope"}}}`,
		"malformed": `{"version":`,
	}
	for name, raw := range schemaCases {
		t.Run("schema "+name, func(t *testing.T) {
			var s Schema
			if err := json.Unmarshal([]byte(raw), &s); err == nil {
				t.Fatalf("expected error for %s", raw)
			}
		})
	}
	// Container decoders reject malformed JSON before reaching child slots.
	for name, target := range map[string]json.Unmarshaler{
		"field": &FieldNode{}, "array": &ArrayNode{}, "map": &MapNode{}, "union": &UnionNode{}, "oneof": &OneOfNode{},
	} {
		if err := target.UnmarshalJSON([]byte(`{`)); err == nil {
			t.Errorf("%s: accepted malformed JSON", name)
		}
	}
	// OneOf with an explicit null default decodes with no default.
	var o OneOfNode
	if err := json.Unmarshal([]byte(`{"kind":"oneof","discriminator":"k","cases":{"a":{"kind":"any"}},"default":null}`), &o); err != nil {
		t.Fatal(err)
	}
	if o.Default != nil {
		t.Error("null default should decode to nil")
	}
}

func TestCheckFindings(t *testing.T) {
	str := func(rules ...Rule) TypeNode { return &StringNode{Rules: rules} }
	obj := func(fields ...FieldNode) *ObjectNode { return &ObjectNode{Fields: fields} }
	field := func(name string, t TypeNode) FieldNode { return FieldNode{Name: name, Type: t} }
	schema := func(root TypeNode) *Schema { return &Schema{Version: Version, Root: root} }

	cases := []struct {
		name string
		s    *Schema
		want string
	}{
		{"nil schema", nil, "nil schema"},
		{"wrong version", &Schema{Version: "v0", Root: &AnyNode{}}, "version"},
		{"missing root", &Schema{Version: Version}, "root: missing"},
		{"nil field type", schema(obj(FieldNode{Name: "a"})), "nil node"},
		{"unresolved ref", schema(&RefNode{Ref: "Nope"}), `ref "Nope" does not resolve`},
		{"unknown rule", schema(str(Rule{Kind: "luhn"})), `unknown rule kind "luhn"`},
		{"missing param", schema(str(Rule{Kind: RuleMinLength})), "missing params: value"},
		{"unknown portability", schema(str(Rule{Kind: RuleEmail, Portable: "maybe"})), `unknown portability "maybe"`},
		{"widened portability", schema(str(Rule{Kind: RuleCustom, Params: map[string]any{ParamRef: "f"}, Portable: Portable})), `cannot be "portable"`},
		{"bad extra", schema(&ObjectNode{Extra: "drop"}), `unknown extra policy "drop"`},
		{"unnamed field", schema(obj(FieldNode{Type: &AnyNode{}})), "field has no name"},
		{"duplicate field", schema(obj(field("a", &AnyNode{}), field("a", &AnyNode{}))), `duplicate field name "a"`},
		{"empty union", schema(&UnionNode{}), "union has no variants"},
		{"bad variant", schema(&UnionNode{Variants: []TypeNode{&RefNode{Ref: "x"}}}), "variants[0]"},
		{"oneof no discriminator", schema(&OneOfNode{Cases: map[string]TypeNode{"a": &AnyNode{}}}), "no discriminator"},
		{"oneof no cases", schema(&OneOfNode{Discriminator: "k"}), "no cases"},
		{"oneof variant lacks discriminator", schema(&OneOfNode{Discriminator: "k", Cases: map[string]TypeNode{"a": obj(field("x", &AnyNode{}))}}), `does not declare discriminator field "k"`},
		{"oneof discriminator is a wire name", schema(&OneOfNode{Discriminator: "k", Cases: map[string]TypeNode{"a": obj(FieldNode{Name: "K", JSONName: "other", Type: &AnyNode{}})}}), `does not declare discriminator field "k"`},
		{"oneof bad default", schema(&OneOfNode{Discriminator: "k", Cases: map[string]TypeNode{"a": obj(field("k", &AnyNode{}))}, Default: &RefNode{Ref: "x"}}), "default"},
		{"bad array elem", schema(&ArrayNode{Elem: &RefNode{Ref: "x"}}), "elem"},
		{"bad map key", schema(&MapNode{Key: &RefNode{Ref: "x"}, Val: &AnyNode{}}), "key"},
		{"bad map val", schema(&MapNode{Key: &AnyNode{}, Val: &RefNode{Ref: "x"}}), "val"},
		{"bad transform kind", schema(obj(FieldNode{Name: "a", Type: &AnyNode{}, Transforms: []Transform{{Kind: "shout", Phase: PhasePre}}})), `unknown transform kind "shout"`},
		{"bad transform phase", schema(obj(FieldNode{Name: "a", Type: &AnyNode{}, Transforms: []Transform{{Kind: TransformTrim, Phase: "mid"}}})), `unknown transform phase "mid"`},
		{"unknown cond kind", schema(str(Rule{Kind: RuleEmail, Cond: &Condition{Kind: "if"}})), `unknown condition kind "if"`},
		{"cmp no field", schema(str(Rule{Kind: RuleEmail, Cond: &Condition{Kind: CondCmp, Op: string(OpEq)}})), "cmp needs field"},
		{"cmp bad op", schema(str(Rule{Kind: RuleEmail, Cond: &Condition{Kind: CondCmp, Field: "a", Op: "like"}})), `unknown comparison op "like"`},
		{"fieldcmp no fieldB", schema(str(Rule{Kind: RuleEmail, Cond: &Condition{Kind: CondFieldCmp, Field: "a", Op: string(OpEq)}})), "needs fieldB"},
		{"presence no field", schema(str(Rule{Kind: RuleEmail, Cond: &Condition{Kind: CondPresence, Op: string(Present)}})), "presence needs field"},
		{"presence bad op", schema(str(Rule{Kind: RuleEmail, Cond: &Condition{Kind: CondPresence, Field: "a", Op: "maybe"}})), `unknown presence op "maybe"`},
		{"in no field", schema(str(Rule{Kind: RuleEmail, Cond: &Condition{Kind: CondIn, Values: []any{1}}})), "in needs field"},
		{"in no values", schema(str(Rule{Kind: RuleEmail, Cond: &Condition{Kind: CondIn, Field: "a"}})), "in needs values"},
		{"and empty", schema(str(Rule{Kind: RuleEmail, Cond: &Condition{Kind: CondAnd}})), "and needs sub-conditions"},
		{"not two", schema(str(Rule{Kind: RuleEmail, Cond: &Condition{Kind: CondNot, Sub: []Condition{Expr("a"), Expr("b")}}})), "exactly one"},
		{"expr empty", schema(str(Rule{Kind: RuleEmail, Cond: &Condition{Kind: CondExpr}})), "expr needs cel"},
		{"custom cond empty", schema(str(Rule{Kind: RuleEmail, Cond: &Condition{Kind: CondCustom}})), "custom needs ref"},
		{"nested cond", schema(str(Rule{Kind: RuleEmail, Cond: &Condition{Kind: CondOr, Sub: []Condition{{Kind: "zzz"}}}})), "sub[0]"},
		{"unknown gate", schema(&ObjectNode{Gated: []GatedRules{{Kind: "unless"}}}), `unknown gate kind "unless"`},
		{"when no cond", schema(&ObjectNode{Gated: []GatedRules{{Kind: GateWhen, Then: []TargetRule{}}}}), "when needs cond"},
		{"when empty", schema(&ObjectNode{Gated: []GatedRules{{Kind: GateWhen, Cond: ptr(Expr("x"))}}}), "neither then nor else"},
		{"requires no cond", schema(&ObjectNode{Gated: []GatedRules{{Kind: GateRequires, Fields: []string{"a"}}}}), "requires needs cond"},
		{"requires no fields", schema(&ObjectNode{Gated: []GatedRules{{Kind: GateRequires, Cond: ptr(Expr("x"))}}}), "requires needs fields"},
		{"group bad mode", schema(&ObjectNode{Gated: []GatedRules{{Kind: GateGroup, Mode: "some"}}}), `unknown group mode "some"`},
		{"group one field", schema(obj(field("a", &AnyNode{}))).withGate(Group(ExactlyOne, "a")), "at least two fields"},
		{"gate unknown field", schema(obj()).withGate(Group(ExactlyOne, "a", "b")), `gate names unknown field "a"`},
		{"target unknown field", schema(obj()).withGate(When(Expr("x"), []TargetRule{On("zz", Email())}, nil)), `targets unknown field "zz"`},
		{"target bad rule", schema(obj(field("a", &AnyNode{}))).withGate(When(Expr("x"), nil, []TargetRule{On("a", Rule{Kind: "nope"})})), `unknown rule kind "nope"`},
		{"gate bad cond", schema(obj()).withGate(Requires(Condition{Kind: "zzz"}, "a")), `unknown condition kind "zzz"`},
		{"unknown def", &Schema{Version: Version, Root: &AnyNode{}, Defs: map[string]TypeNode{"A": &RefNode{Ref: "B"}}}, "defs.A"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Check(tc.s)
			if err == nil {
				t.Fatalf("expected a finding containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want finding containing %q, got:\n%v", tc.want, err)
			}
		})
	}
}

// withGate appends a gate to a schema whose root is an object.
func (s *Schema) withGate(g GatedRules) *Schema {
	s.Root.(*ObjectNode).Gated = append(s.Root.(*ObjectNode).Gated, g)
	return s
}

func ptr[T any](v T) *T { return &v }

func TestOneOfDiscriminatorMatchesWireName(t *testing.T) {
	// The discriminator names the JSON key; a Go field "Kind" tagged json
	// "kind" satisfies discriminator "kind".
	s := &Schema{Version: Version, Root: &OneOfNode{Discriminator: "kind", Cases: map[string]TypeNode{
		"a": &ObjectNode{Fields: []FieldNode{{Name: "Kind", JSONName: "kind", Type: &StringNode{}}}},
	}}}
	if err := Check(s); err != nil {
		t.Fatalf("wire-named discriminator rejected: %v", err)
	}
}

func TestCheckReportsAllFindings(t *testing.T) {
	s := &Schema{Version: "old", Root: &StringNode{Rules: []Rule{{Kind: "a"}, {Kind: "b"}}}}
	err := Check(s)
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("expected a joined error, got %T", err)
	}
	if n := len(joined.Unwrap()); n != 3 {
		t.Fatalf("want 3 findings (version + 2 rules), got %d: %v", n, err)
	}
}

func TestPortability(t *testing.T) {
	for _, p := range []Portability{Portable, Opaque, Server, Wasm} {
		if !p.Valid() {
			t.Errorf("%s should be valid", p)
		}
	}
	if Portability("").Valid() || Portability("x").Valid() {
		t.Error("empty and unknown must be invalid")
	}
	// Defaults come from the kind; explicit narrowing wins; unknown kinds
	// with no explicit class report false rather than guessing.
	if p, _ := Custom("f").EffectivePortability(); p != Opaque {
		t.Errorf("custom default %s", p)
	}
	if p, _ := Async("f", "").EffectivePortability(); p != Server {
		t.Errorf("async default %s", p)
	}
	if p, _ := Email().EffectivePortability(); p != Portable {
		t.Errorf("email default %s", p)
	}
	if p, _ := (Rule{Kind: RuleCustom, Portable: Wasm}).EffectivePortability(); p != Wasm {
		t.Errorf("explicit wasm lost: %s", p)
	}
	if _, ok := (Rule{Kind: "nope"}).EffectivePortability(); ok {
		t.Error("unknown kind must not report a portability")
	}
	if _, ok := RuleKind("nope").DefaultPortability(); ok {
		t.Error("unknown kind must not have a default")
	}
	if !RuleEmail.Known() || RuleKind("nope").Known() {
		t.Error("Known is wrong")
	}
	if CustomCond("x").Portability() != Opaque || Expr("x").Portability() != Portable {
		t.Error("condition portability is wrong")
	}
	// A custom rule marked portable is rejected — the silent-drop guard.
	if err := Check(&Schema{Version: Version, Root: &StringNode{Rules: []Rule{{Kind: RuleCustom, Params: map[string]any{ParamRef: "f"}, Portable: Portable}}}}); err == nil {
		t.Error("custom rule claimed portable must fail Check")
	}
	// Narrowing a portable rule to wasm/opaque/server is allowed.
	if err := Check(&Schema{Version: Version, Root: &StringNode{Rules: []Rule{{Kind: RuleEmail, Portable: Server}}}}); err != nil {
		t.Errorf("narrowing must be allowed: %v", err)
	}
}

func TestConstructors(t *testing.T) {
	// Every constructor must produce a rule its own kind's spec accepts —
	// pins the param keys to the registry.
	rules := []Rule{
		MinLength(1), MaxLength(1), MinItems(1), MaxItems(1), Unique(),
		Min(1), Max(1), Gt(1), Gte(1), Lt(1), Lte(1), MultipleOf(2),
		Pattern("x"), Enum(1, 2), Const(1), Email(), URL(), UUID(), ULID(),
		IPv4(), IPv6(), IP(), CIDR(), MAC(), Hostname(), Base64(), JSON(), Semver(), Date(), DateTime(), NotBlank(),
		CreditCard(), JWT(), CountryCode(), CurrencyCode(), HexColor(), E164(), Latitude(), Longitude(),
		After("a"), Before("a"), Custom("f"), Async("f", "/v/f"),
	}
	seen := map[RuleKind]bool{}
	for _, r := range rules {
		seen[r.Kind] = true
		if err := Check(&Schema{Version: Version, Root: &StringNode{Rules: []Rule{r}}}); err != nil {
			t.Errorf("%s: %v", r.Kind, err)
		}
	}
	for k := range ruleSpecs {
		if !seen[k] {
			t.Errorf("rule kind %s has no constructor under test", k)
		}
	}
	if v, ok := Async("f", "/v/f").Param(ParamEndpoint); !ok || v != "/v/f" {
		t.Error("Async must record the endpoint")
	}
	if _, ok := Async("f", "").Param(ParamEndpoint); ok {
		t.Error("empty endpoint must be omitted so the generator derives it")
	}
	// Condition constructors cover every kind.
	conds := []Condition{Cmp("a", OpEq, 1), FieldCmp("a", OpGt, "b"), HasField("a", Absent), In("a", 1), And(Expr("x")), Or(Expr("x")), Not(Expr("x")), Expr("x"), CustomCond("f")}
	seenC := map[CondKind]bool{}
	for _, c := range conds {
		seenC[c.Kind] = true
		if err := Check(&Schema{Version: Version, Root: &StringNode{Rules: []Rule{{Kind: RuleEmail, Cond: ptr(c)}}}}); err != nil {
			t.Errorf("%s: %v", c.Kind, err)
		}
	}
	for k := range condKinds {
		if !seenC[k] {
			t.Errorf("condition kind %s has no constructor under test", k)
		}
	}
}

func TestHelpers(t *testing.T) {
	s := userSchema()
	root := s.Root.(*ObjectNode)
	if got := len(Children(root)); got != len(root.Fields) {
		t.Errorf("object children %d, want %d", got, len(root.Fields))
	}
	if Rules(root) != nil || Children(&AnyNode{}) != nil {
		t.Error("objects carry no rules; any has no children")
	}
	arr := &ArrayNode{Elem: &AnyNode{}}
	if len(Children(arr)) != 1 || len(Children(&MapNode{Key: &AnyNode{}, Val: &AnyNode{}})) != 2 {
		t.Error("collection children are wrong")
	}
	one := &OneOfNode{Cases: map[string]TypeNode{"b": &AnyNode{}, "a": &StringNode{}}, Default: &BoolNode{}}
	kids := Children(one)
	if len(kids) != 3 || kids[0].Kind() != KindString || kids[2].Kind() != KindBool {
		t.Errorf("oneof children must be case-sorted then default: %v", kids)
	}
	if len(Children(&UnionNode{Variants: []TypeNode{&AnyNode{}}})) != 1 {
		t.Error("union children wrong")
	}
	for _, n := range []TypeNode{&StringNode{Rules: []Rule{Email()}}, &IntNode{Rules: []Rule{Min(1)}}, &FloatNode{Rules: []Rule{Min(1)}}, &BoolNode{Rules: []Rule{Const(true)}}, &BytesNode{Rules: []Rule{MinLength(1)}}, &TimeNode{Rules: []Rule{After("x")}}, &DurationNode{Rules: []Rule{Gt(1)}}, &ArrayNode{Elem: &AnyNode{}, Rules: []Rule{Unique()}}, &MapNode{Key: &AnyNode{}, Val: &AnyNode{}, Rules: []Rule{MinItems(1)}}} {
		if len(Rules(n)) != 1 {
			t.Errorf("%s: Rules lost the rule", n.Kind())
		}
		if n.base() == nil {
			t.Errorf("%s: nil base", n.Kind())
		}
	}
	for _, n := range []TypeNode{&ObjectNode{}, &UnionNode{}, &OneOfNode{}, &RefNode{}, &AnyNode{}} {
		if n.base() == nil {
			t.Errorf("%s: nil base", n.Kind())
		}
	}
}

func TestWithKindRejectsNonObject(t *testing.T) {
	if _, err := withKind(KindAny, 42); err == nil {
		t.Error("a non-object body must be rejected")
	}
}

func TestCheckPattern(t *testing.T) {
	ok := []string{`^[a-z]+$`, `\d{3}-\d{4}`, `(?:ab)+`, `^\S+@\S+$`, `[\x00-\x7F]*`, `^[-+]?[0-9]+(?:\.[0-9]+)?$`}
	for _, p := range ok {
		if err := CheckPattern(p); err != nil {
			t.Errorf("%q should pass: %v", p, err)
		}
	}
	bad := map[string]string{
		`(?i)abc`:      "inline flags",
		`(?P<n>a)`:     "named groups",
		`\Aabc`:        `\A`,
		`abc\z`:        `\z`,
		`\Qa.b\E`:      `\Q`,
		`[[:alpha:]]+`: "POSIX",
		`\p{L}+`:       "Unicode property",
		`(?=a)b`:       "not valid RE2",
		`(a)\1`:        "not valid RE2",
		`a{2,1}`:       "not valid RE2",
	}
	for p, want := range bad {
		err := CheckPattern(p)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: want error containing %q, got %v", p, want, err)
		}
	}
	// Check wires it in.
	if err := Check(&Schema{Version: Version, Root: &StringNode{Rules: []Rule{Pattern(`(?i)x`)}}}); err == nil || !strings.Contains(err.Error(), "inline flags") {
		t.Errorf("Check must lint patterns: %v", err)
	}
}

func TestStrictTransformAccepted(t *testing.T) {
	s := &Schema{Version: Version, Root: &ObjectNode{Fields: []FieldNode{{Name: "N", Type: &IntNode{}, Transforms: []Transform{{Kind: TransformStrict, Phase: PhasePre}}}}}}
	if err := Check(s); err != nil {
		t.Fatal(err)
	}
}
