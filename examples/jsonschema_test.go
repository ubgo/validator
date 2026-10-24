package examples

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/ubgo/validator/interp"
	"github.com/ubgo/validator/ir"
	vjs "github.com/ubgo/validator/jsonschema"
	"github.com/ubgo/validator/vb"
)

// This file is the external proof for the JSON Schema emitter: an
// independent, spec-conformant validator (santhosh-tekuri/jsonschema) must
// compile the documents we emit under BOTH dialects and agree with our own
// executor on which values are valid. Our emitter's tests only check the
// bytes we produce; this checks that another implementation reads them the
// way we mean.

func loadGolden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "jsonschema", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// compile parses a schema document with the third-party validator.
func compile(t *testing.T, doc []byte, url string) *jsonschema.Schema {
	t.Helper()
	parsed, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err := c.AddResource(url, parsed); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(url)
	if err != nil {
		t.Fatalf("independent validator rejected the emitted schema: %v", err)
	}
	return s
}

func validUser() map[string]any {
	return map[string]any{
		"email": "ada@example.com", "age": 30, "score": 0.5, "active": true,
		"start": "2026-01-01T00:00:00Z", "end": "2026-01-01T01:00:00Z", "ttl": 600000000000,
		"tags": []any{"a", "b"}, "labels": map[string]any{"k": 2},
		"home":  map[string]any{"street": "Main", "zip": "12345"},
		"extra": nil, "idOrName": 7, "type": "online", "url": "https://x.io",
		"event": map[string]any{"kind": "online", "url": "https://e.io"},
	}
}

func TestGoldenSchemasCompileAndValidate(t *testing.T) {
	for _, dialect := range []struct{ file, url string }{
		{"user.2020.schema.json", "https://acme.dev/schemas/user.json"},
		{"user.07.schema.json", "https://acme.dev/schemas/user07.json"},
	} {
		t.Run(dialect.file, func(t *testing.T) {
			s := compile(t, loadGolden(t, dialect.file), dialect.url)
			if err := s.Validate(validUser()); err != nil {
				t.Fatalf("valid user rejected by independent validator: %v", err)
			}
			mut := func(f func(map[string]any)) map[string]any { u := validUser(); f(u); return u }
			bad := map[string]map[string]any{
				"email":                    mut(func(u map[string]any) { u["email"] = "nope" }),
				"age":                      mut(func(u map[string]any) { u["age"] = 17 }),
				"score":                    mut(func(u map[string]any) { u["score"] = 2 }),
				"tags unique":              mut(func(u map[string]any) { u["tags"] = []any{"a", "a"} }),
				"tags enum":                mut(func(u map[string]any) { u["tags"] = []any{"zzz"} }),
				"zip pattern":              mut(func(u map[string]any) { u["home"] = map[string]any{"street": "s", "zip": "x"} }),
				"extra key":                mut(func(u map[string]any) { u["bogus"] = 1 }),
				"missing type":             mut(func(u map[string]any) { delete(u, "type") }),
				"both url venue":           mut(func(u map[string]any) { u["venue"] = "Hall" }),
				"offline no venue":         mut(func(u map[string]any) { u["type"] = "offline"; delete(u, "url") }),
				"event bad url":            mut(func(u map[string]any) { u["event"] = map[string]any{"kind": "online", "url": "x"} }),
				"event offline no address": mut(func(u map[string]any) { u["event"] = map[string]any{"kind": "offline"} }),
				"ttl":                      mut(func(u map[string]any) { u["ttl"] = 0 }),
			}
			for name, doc := range bad {
				if err := s.Validate(doc); err == nil {
					t.Errorf("%s: independent validator accepted an invalid document", name)
				}
			}
			ok := map[string]map[string]any{
				"offline with venue": mut(func(u map[string]any) { u["type"] = "offline"; delete(u, "url"); u["venue"] = "Hall" }),
				"event default":      mut(func(u map[string]any) { u["event"] = map[string]any{"kind": "mystery"} }),
				"event offline": mut(func(u map[string]any) {
					u["event"] = map[string]any{"kind": "offline", "address": map[string]any{"street": "s", "zip": "99999"}}
				}),
				"null avatar": mut(func(u map[string]any) { u["avatar"] = nil }),
			}
			for name, doc := range ok {
				if err := s.Validate(doc); err != nil {
					t.Errorf("%s: independent validator rejected a valid document: %v", name, err)
				}
			}
		})
	}
}

// TestEveryEmittedKeywordIsUnderstood emits one schema per rule kind and
// node shape through our emitter and asks the independent validator to
// compile each, in both dialects. A keyword we emit that the spec does not
// define would surface here.
func TestEveryEmittedKeywordIsUnderstood(t *testing.T) {
	nodes := map[string]ir.TypeNode{
		"string": &ir.StringNode{Rules: []ir.Rule{ir.MinLength(1), ir.MaxLength(9), ir.Pattern("^a"), ir.Enum("a", "b"), ir.Email()}},
		"formats": &ir.ObjectNode{Fields: []ir.FieldNode{
			{Name: "u", Type: &ir.StringNode{Rules: []ir.Rule{ir.URL()}}},
			{Name: "i", Type: &ir.StringNode{Rules: []ir.Rule{ir.UUID()}}},
			{Name: "l", Type: &ir.StringNode{Rules: []ir.Rule{ir.ULID()}}},
			{Name: "c", Type: &ir.StringNode{Rules: []ir.Rule{ir.Const("x")}}},
			{Name: "n1", Type: &ir.StringNode{Rules: []ir.Rule{ir.IPv4(), ir.IPv6(), ir.IP()}}},
			{Name: "n2", Type: &ir.StringNode{Rules: []ir.Rule{ir.CIDR(), ir.MAC(), ir.Hostname()}}},
			{Name: "n3", Type: &ir.StringNode{Rules: []ir.Rule{ir.Base64(), ir.Semver(), ir.Date(), ir.DateTime(), ir.NotBlank(), ir.JSON()}}},
			{Name: "n4", Type: &ir.StringNode{Rules: []ir.Rule{ir.CreditCard(), ir.JWT(), ir.CountryCode(), ir.CurrencyCode(), ir.HexColor(), ir.E164()}}},
			{Name: "n5", Type: &ir.FloatNode{Rules: []ir.Rule{ir.Latitude(), ir.Longitude()}}},
		}},
		"int":      &ir.IntNode{Bits: 8, Unsigned: true, Rules: []ir.Rule{ir.MultipleOf(2)}},
		"float":    &ir.FloatNode{Rules: []ir.Rule{ir.Min(1), ir.Max(2), ir.Gt(0), ir.Lt(9)}},
		"bool":     &ir.BoolNode{Rules: []ir.Rule{ir.Const(true)}},
		"bytes":    &ir.BytesNode{},
		"time":     &ir.TimeNode{},
		"duration": &ir.DurationNode{Rules: []ir.Rule{ir.Gte(0)}},
		"nullable": &ir.StringNode{Base: ir.Base{Nullable: true, Meta: ir.NodeMeta{Description: "d", Examples: []any{"x"}}}},
		"array":    &ir.ArrayNode{Elem: &ir.AnyNode{}, Rules: []ir.Rule{ir.MinItems(1), ir.MaxItems(3), ir.Unique()}},
		"map":      &ir.MapNode{Key: &ir.StringNode{}, Val: &ir.IntNode{}, Rules: []ir.Rule{ir.MinItems(1), ir.MaxItems(3)}},
		"union":    &ir.UnionNode{Variants: []ir.TypeNode{&ir.IntNode{}, &ir.StringNode{}}},
		"gates": &ir.ObjectNode{
			Extra: ir.ExtraForbid,
			Fields: []ir.FieldNode{
				{Name: "t", Type: &ir.StringNode{}}, {Name: "n", Type: &ir.IntNode{}},
				{Name: "a", Type: &ir.StringNode{}, Optional: true}, {Name: "b", Type: &ir.StringNode{}, Optional: true,
					Transforms: []ir.Transform{{Kind: ir.TransformDefault, Phase: ir.PhasePre, Params: map[string]any{ir.ParamValue: "x"}}}},
			},
			Gated: []ir.GatedRules{
				ir.When(ir.And(ir.Cmp("t", ir.OpEq, "x"), ir.Cmp("n", ir.OpGte, 3), ir.Not(ir.HasField("a", ir.Present)), ir.In("t", "x", "y")),
					[]ir.TargetRule{ir.On("a", ir.MinLength(1))}, []ir.TargetRule{ir.On("b", ir.MaxLength(5))}),
				ir.Requires(ir.Or(ir.Cmp("n", ir.OpLt, 1), ir.Cmp("n", ir.OpLte, 2), ir.Cmp("n", ir.OpGt, 9), ir.Cmp("t", ir.OpNe, "q")), "a"),
				ir.Group(ir.ExactlyOne, "a", "b"), ir.Group(ir.AtLeastOne, "a", "b"), ir.Group(ir.AllOrNone, "a", "b"),
			},
		},
		"oneof": &ir.OneOfNode{Discriminator: "k", Cases: map[string]ir.TypeNode{
			"a": &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "k", Type: &ir.StringNode{Rules: []ir.Rule{ir.Const("a")}}}}},
		}, Default: &ir.AnyNode{}},
		"refs": &ir.RefNode{Ref: "Leaf"},
	}
	for name, node := range nodes {
		for _, draft := range []vjs.Draft{vjs.Draft2020, vjs.Draft07} {
			t.Run(name+"/"+string(draft[len(draft)-12:]), func(t *testing.T) {
				s := &ir.Schema{Version: ir.Version, Root: node, Defs: map[string]ir.TypeNode{"Leaf": &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "x", Type: &ir.StringNode{}}}}}, Meta: ir.SchemaMeta{Title: name}}
				res, err := vjs.Emit(s, vjs.WithDraft(draft))
				if err != nil {
					t.Fatal(err)
				}
				doc, _ := res.JSON()
				compile(t, doc, "https://acme.dev/"+name+".json")
			})
		}
	}
}

// TestExecutorsAgreeWithIndependentValidator runs the same documents
// through our interpreter and the third-party validator on the golden
// schema. Where the two models differ by design (empty-string presence,
// custom rules) the inputs avoid the difference; everything else must
// agree on valid/invalid.
func TestExecutorsAgreeWithIndependentValidator(t *testing.T) {
	type Address struct {
		Street string `json:"street"`
		Zip    string `json:"zip"`
	}
	type User struct {
		Email  string   `json:"email"`
		Age    int32    `json:"age"`
		Score  float64  `json:"score"`
		Active bool     `json:"active"`
		Tags   []string `json:"tags"`
		Home   Address  `json:"home"`
	}
	fStreet := vb.NewField("Street", func(a Address) string { return a.Street }).JSON("street")
	fZip := vb.NewField("Zip", func(a Address) string { return a.Zip }).JSON("zip")
	address := vb.Struct("Address", vb.F(fStreet, vb.String().MinLength(1)), vb.F(fZip, vb.String().Pattern(`^[0-9]{5}$`))).MustBuild()
	fEmail := vb.NewField("Email", func(u User) string { return u.Email }).JSON("email")
	fAge := vb.NewField("Age", func(u User) int32 { return u.Age }).JSON("age")
	fScore := vb.NewField("Score", func(u User) float64 { return u.Score }).JSON("score")
	fActive := vb.NewField("Active", func(u User) bool { return u.Active }).JSON("active")
	fTags := vb.NewField("Tags", func(u User) []string { return u.Tags }).JSON("tags")
	fHome := vb.NewField("Home", func(u User) Address { return u.Home }).JSON("home")
	schema := vb.Struct("User",
		vb.F(fEmail, vb.String().Email()),
		vb.F(fAge, vb.Int[int32]().Min(18).Max(120)),
		vb.F(fScore, vb.Float[float64]().Gte(0).Lte(1).MultipleOf(0.25)),
		vb.F(fActive, vb.Bool()),
		vb.F(fTags, vb.Array(vb.String().Enum("a", "b")).MinItems(1).Unique()),
		vb.F(fHome, vb.Ref(address)),
	).Extra(ir.ExtraForbid).MustBuild()

	res, err := vjs.Emit(schema.IR())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Manifest.Portable() {
		t.Fatalf("this schema must be fully portable: %+v", res.Manifest)
	}
	doc, _ := res.JSON()
	external := compile(t, doc, "https://acme.dev/agree.json")
	ours, err := interp.FromSchema(schema)
	if err != nil {
		t.Fatal(err)
	}
	valid := User{Email: "a@b.io", Age: 30, Score: 0.5, Active: true, Tags: []string{"a"}, Home: Address{Street: "s", Zip: "12345"}}
	mut := func(f func(*User)) User { u := valid; f(&u); return u }
	cases := map[string]User{
		"valid":      valid,
		"email":      mut(func(u *User) { u.Email = "x" }),
		"age low":    mut(func(u *User) { u.Age = 1 }),
		"age high":   mut(func(u *User) { u.Age = 200 }),
		"score":      mut(func(u *User) { u.Score = 0.3 }),
		"tags dup":   mut(func(u *User) { u.Tags = []string{"a", "a"} }),
		"tags enum":  mut(func(u *User) { u.Tags = []string{"z"} }),
		"zip":        mut(func(u *User) { u.Home.Zip = "1" }),
		"street":     mut(func(u *User) { u.Home.Street = "" }),
		"active off": mut(func(u *User) { u.Active = false }),
	}
	for name, u := range cases {
		t.Run(name, func(t *testing.T) {
			// Send the JSON encoding through the external validator: that is
			// the wire the frontend would see.
			raw, _ := json.Marshal(u)
			var generic any
			_ = json.Unmarshal(raw, &generic)
			extErr := external.Validate(generic)
			ourErr := ours.Validate(context.Background(), u)
			if (extErr == nil) != (ourErr == nil) {
				t.Fatalf("disagreement: external=%v ours=%v", extErr, ourErr)
			}
		})
	}
	_ = strings.TrimSpace
}

// TestIRDocumentsConformToTheContract validates real IR documents against
// the contract schema with the independent validator: the golden user.json
// must pass, and documents with an unknown kind or a missing version must
// fail. This is the proof that the contract describes the wire format.
func TestIRDocumentsConformToTheContract(t *testing.T) {
	contractDoc, err := json.Marshal(vjs.Contract())
	if err != nil {
		t.Fatal(err)
	}
	contract := compile(t, contractDoc, vjs.ContractID)
	golden, err := os.ReadFile(filepath.Join("..", "ir", "testdata", "user.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := json.Unmarshal(golden, &doc); err != nil {
		t.Fatal(err)
	}
	if err := contract.Validate(doc); err != nil {
		t.Fatalf("golden IR does not conform to the contract: %v", err)
	}
	// Every example IR the repo ships must conform too.
	for _, extra := range []string{filepath.Join("..", "tags", "internal", "example", "signup.ir.json")} {
		raw, err := os.ReadFile(extra)
		if err != nil {
			t.Fatal(err)
		}
		var d any
		_ = json.Unmarshal(raw, &d)
		if err := contract.Validate(d); err != nil {
			t.Errorf("%s does not conform: %v", extra, err)
		}
	}
	for name, bad := range map[string]string{
		"unknown kind":    `{"version":"ubgo.validator.ir/v1","root":{"kind":"decimal"}}`,
		"missing version": `{"root":{"kind":"any"}}`,
		"wrong version":   `{"version":"v0","root":{"kind":"any"}}`,
		"extra node key":  `{"version":"ubgo.validator.ir/v1","root":{"kind":"string","bogus":1}}`,
		"bad rule shape":  `{"version":"ubgo.validator.ir/v1","root":{"kind":"string","rules":[{"params":{}}]}}`,
	} {
		var d any
		_ = json.Unmarshal([]byte(bad), &d)
		if err := contract.Validate(d); err == nil {
			t.Errorf("%s: contract accepted an invalid document", name)
		}
	}
}

// TestErrorBodiesMatchTheirContract is the proof that matters for the
// error schema: real issues, from real validation failures, checked
// against the document by an independent validator.
//
// A schema of the error model is only worth having if a client typed from
// it can parse what the library actually sends. Reflection guarantees the
// shape matches the struct; this guarantees the struct matches reality.
func TestErrorBodiesMatchTheirContract(t *testing.T) {
	contractDoc, err := json.Marshal(vjs.ErrorContract())
	if err != nil {
		t.Fatal(err)
	}
	contract := compile(t, contractDoc, vjs.ErrorContractID)

	v, err := interp.FromSchema(bookingSchemaForErrors())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Failures chosen to produce every SHAPE of issue: a rule with params,
	// a structural one with none, a cross-field one naming a sibling, and
	// an object rule reported at the root.
	inputs := []any{
		map[string]any{}, // required, no params
		map[string]any{"email": "nope", "guests": 99},          // rule codes with params
		map[string]any{"email": "a@b.io", "guests": "not-int"}, // a type issue
		map[string]any{"email": "a@b.io", "guests": 1, "check_in": "2026-03-10T00:00:00Z", "check_out": "2026-03-01T00:00:00Z"},
	}
	var seen int
	for i, in := range inputs {
		errs := v.Run(ctx, in)
		if errs.Len() == 0 {
			continue
		}
		seen += errs.Len()
		body, err := json.Marshal(errs)
		if err != nil {
			t.Fatal(err)
		}
		var decoded any
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatal(err)
		}
		if err := contract.Validate(decoded); err != nil {
			t.Errorf("input %d: the error body does not match its own contract: %v\n%s", i, err, body)
		}
	}
	if seen < 4 {
		t.Fatalf("only %d issues produced; the contract was barely exercised", seen)
	}

	// And the negative: a body missing a required field must be rejected,
	// or the contract is not constraining anything.
	for name, bad := range map[string]string{
		"issue without a code": `{"issues":[{"path":"$","message":"x"}]}`,
		"issue without a path": `{"issues":[{"code":"x","message":"x"}]}`,
		"issues not an array":  `{"issues":{"code":"x"}}`,
		"code of wrong type":   `{"issues":[{"code":1,"path":"$","message":"x"}]}`,
	} {
		var d any
		_ = json.Unmarshal([]byte(bad), &d)
		if err := contract.Validate(d); err == nil {
			t.Errorf("%s: the contract accepted a malformed body", name)
		}
	}
}

// bookingSchemaForErrors is a small schema whose failures span every issue
// shape the error contract must describe.
func bookingSchemaForErrors() *vb.Schema[errBooking] {
	return vb.Struct("ErrBooking",
		vb.F(ebEmail, vb.String().Email()),
		vb.F(ebGuests, vb.Int[int]().Min(1).Max(4)),
		vb.F(ebIn, vb.Time()).Optional(),
		vb.F(ebOut, vb.Time().With(vb.After(ebIn))).Optional(),
	).Custom("consistent", func(_ context.Context, b errBooking) error {
		if b.Guests > 2 && b.Email == "" {
			return errors.New("a large party needs a contact address")
		}
		return nil
	}).MustBuild()
}

type errBooking struct {
	Email    string    `json:"email"`
	Guests   int       `json:"guests"`
	CheckIn  time.Time `json:"check_in"`
	CheckOut time.Time `json:"check_out"`
}

var (
	ebEmail  = vb.NewField("Email", func(b errBooking) string { return b.Email }).JSON("email")
	ebGuests = vb.NewField("Guests", func(b errBooking) int { return b.Guests }).JSON("guests")
	ebIn     = vb.NewField("CheckIn", func(b errBooking) time.Time { return b.CheckIn }).JSON("check_in")
	ebOut    = vb.NewField("CheckOut", func(b errBooking) time.Time { return b.CheckOut }).JSON("check_out")
)
