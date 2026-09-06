package examples

// This file is docs/GUIDE.md's proof. The guide's files live in guide/
// (compiled by the build); the behaviors the guide claims for them are
// asserted here, in the guide's order. When the guide changes, change this
// file; when this file changes, change the guide.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ubgo/validator/examples/guide/signup"
	"github.com/ubgo/validator/i18n"
	"github.com/ubgo/validator/interp"
	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/jsonschema"
	"github.com/ubgo/validator/rulehttp"
	"github.com/ubgo/validator/vb"
	"github.com/ubgo/validator/verr"
)

// --- GUIDE §2 Quickstart -----------------------------------------------------

func TestGuideQuickstart(t *testing.T) {
	v, err := interp.FromSchema(signup.Schema)
	if err != nil {
		t.Fatal(err)
	}
	in := signup.Signup{
		Email: "  Ada@Example.com ", Password: "short", Age: 17, Plan: "team",
		Starts: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Ends: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	err = v.Validate(context.Background(), &in)
	var errs *verr.Errors
	if !errors.As(err, &errs) {
		t.Fatalf("want *verr.Errors, got %T", err)
	}
	// The transforms wrote back: the email is trimmed and lowercased.
	if in.Email != "ada@example.com" {
		t.Errorf("email not normalized: %q", in.Email)
	}
	got := map[string]string{}
	for _, is := range errs.Issues {
		got[is.Path] = is.Code
	}
	want := map[string]string{"$.password": "min_length", "$.age": "min", "$.ends": "after", "$.company": "required"}
	for p, code := range want {
		if got[p] != code {
			t.Errorf("%s: got %q want %q (all: %v)", p, got[p], code, got)
		}
	}
	// The JSON shape of the error is what an API returns.
	body, _ := json.Marshal(errs)
	if !strings.Contains(string(body), `"code":"min_length"`) || !strings.Contains(string(body), `"path":"$.password"`) {
		t.Errorf("json: %s", body)
	}
	// A valid value returns a plain nil.
	ok := signup.Signup{Email: "ada@example.com", Password: "correct horse", Age: 30, Plan: "free",
		Starts: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Ends: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}
	if err := v.Validate(context.Background(), &ok); err != nil {
		t.Errorf("valid signup rejected: %v", err)
	}
}

// --- GUIDE §4.6 Map input, coercion, time strings ----------------------------

func TestGuideMapInput(t *testing.T) {
	v, _ := interp.FromSchema(signup.Schema, interp.Lax())
	form := map[string]any{
		"email": "ada@example.com", "password": "correct horse", "age": "30", "plan": "free",
		"starts": "2026-01-01T00:00:00Z", "ends": "2026-01-02T00:00:00Z",
	}
	if err := v.Validate(context.Background(), form); err != nil {
		t.Fatalf("lax map input rejected: %v", err)
	}
	if _, isTime := form["ends"].(time.Time); !isTime {
		t.Errorf("time string not written back: %T", form["ends"])
	}
	if _, isNum := form["age"].(float64); !isNum {
		t.Errorf("age not coerced: %T", form["age"])
	}
	// Default mode: "30" is a type mismatch for an int field.
	strict, _ := interp.FromSchema(signup.Schema)
	err := strict.Validate(context.Background(), map[string]any{
		"email": "ada@example.com", "password": "correct horse", "age": "30", "plan": "free",
		"starts": "2026-01-01T00:00:00Z", "ends": "2026-01-02T00:00:00Z",
	})
	var errs *verr.Errors
	if !errors.As(err, &errs) || errs.Issues[0].Path != "$.age" || errs.Issues[0].Code != "type" {
		t.Errorf("default mode: %v", err)
	}
}

// --- GUIDE §4.8 Fail-fast --------------------------------------------------------

func TestGuideFailFast(t *testing.T) {
	v, _ := interp.FromSchema(signup.Schema)
	first := &verr.Errors{FailFast: true}
	v.RunInto(context.Background(), &signup.Signup{}, first)
	if first.Len() != 1 {
		t.Errorf("fail-fast reported %d issues", first.Len())
	}
	all := v.Run(context.Background(), &signup.Signup{})
	if all.Len() < 3 {
		t.Errorf("accumulating run reported only %d issues", all.Len())
	}
}

// --- GUIDE §4.9 Custom and async rules ---------------------------------------------

type Profile struct {
	Handle string `json:"handle"`
}

var handleField = vb.NewField("Handle", func(a Profile) string { return a.Handle }).JSON("handle")

var reserved = map[string]bool{"admin": true, "root": true}

// ProfileSchema: a local custom rule (Go only) and an async rule (Go runs
// it; the browser round-trips to POST /v/handle_free).
var ProfileSchema = vb.Struct("Profile",
	vb.F(handleField, vb.String().MinLength(3).
		Custom("not_reserved", func(_ context.Context, h string) error {
			if reserved[h] {
				return errors.New("reserved name")
			}
			return nil
		}).
		Async("handle_free", "/v/handle_free", func(_ context.Context, h string) error {
			if h == "taken" {
				return errors.New("already taken")
			}
			return nil
		})),
).MustBuild()

func TestGuideCustomAndAsync(t *testing.T) {
	v, _ := interp.FromSchema(ProfileSchema)
	err := v.Validate(context.Background(), &Profile{Handle: "admin"})
	var errs *verr.Errors
	if !errors.As(err, &errs) || errs.Issues[0].Code != "custom" || errs.Issues[0].Rule != "custom" {
		t.Errorf("custom rule: %v", err)
	}
	// The manifest names both as non-portable, with their class.
	res, _ := jsonschema.Emit(ProfileSchema.IR())
	classes := map[string]jsonschema.Class{}
	for _, e := range res.Manifest.Entries {
		classes[e.Name] = e.Class
	}
	if classes["custom"] != jsonschema.ClassOpaque || classes["async"] != jsonschema.ClassServer {
		t.Errorf("manifest classes: %v", classes)
	}
	// Serving the async rule over HTTP.
	h, err := rulehttp.FromSchema(ProfileSchema)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/v/handle_free", "application/json", strings.NewReader(`{"rule":"handle_free","value":"taken"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		OK     bool         `json:"ok"`
		Issues []verr.Issue `json:"issues"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.OK || len(out.Issues) != 1 || out.Issues[0].Code != "custom" {
		t.Errorf("round-trip: %+v", out)
	}
}

// --- GUIDE §4.10 Messages and translations ---------------------------------------------

func TestGuideMessages(t *testing.T) {
	// Per-rule override: your own code and message.
	f := vb.NewField("Password", func(v signup.Signup) string { return v.Password }).JSON("password")
	s := vb.Struct("Pw", vb.F(f, vb.String().MinLength(8).Msg("weak_password", "use at least {value} characters"))).MustBuild()
	v, _ := interp.FromSchema(s)
	errs := v.Run(context.Background(), &signup.Signup{Password: "x"})
	if errs.Issues[0].Code != "weak_password" || errs.Issues[0].Message != "use at least 8 characters" {
		t.Errorf("override: %+v", errs.Issues[0])
	}
	// Translation: a partial catalog merged over the default, applied by code.
	de, err := i18n.Load(strings.NewReader(`{"locale":"de","messages":{"min_length":"mindestens {value} Zeichen"}}`))
	if err != nil {
		t.Fatal(err)
	}
	cat := i18n.Merge(i18n.Default(), de)
	v2, _ := interp.FromSchema(signup.Schema)
	localized := cat.Localize(v2.Run(context.Background(), &signup.Signup{Password: "x"}))
	for _, is := range localized.Issues {
		if is.Path == "$.password" && is.Message != "mindestens 8 Zeichen" {
			t.Errorf("localized: %q", is.Message)
		}
	}
}

// --- GUIDE §5 Export: JSON Schema, manifest, TypeScript module ----------------------------

func TestGuideExport(t *testing.T) {
	res, err := jsonschema.Emit(signup.Schema.IR(), jsonschema.WithID("https://example.com/signup.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := res.JSON()
	for _, want := range []string{`"$schema"`, `"format": "email"`, `"minLength": 8`, `"enum"`, `"if"`, `"then"`} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("schema lacks %s", want)
		}
	}
	// What the schema could NOT carry: the cross-field "after" and the
	// two string transforms. Never dropped silently.
	names := map[string]bool{}
	for _, e := range res.Manifest.Entries {
		names[e.Name] = true
	}
	for _, want := range []string{"after", "trim", "lower"} {
		if !names[want] {
			t.Errorf("manifest lacks %s: %+v", want, res.Manifest.Entries)
		}
	}
	// The TS module is the file the frontend imports.
	ts, err := res.TSModule("signup")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ts), "export const signupSchema =") || !strings.Contains(string(ts), "export const signupManifest =") {
		t.Errorf("ts module: %s", ts)
	}
	// The IR round-trips through JSON and is checked on the way back.
	raw, _ := json.Marshal(signup.Schema.IR())
	var back ir.Schema
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if err := ir.Check(&back); err != nil {
		t.Fatal(err)
	}
	if _, err := interp.New(&back, interp.WithFuncs(signup.Schema)); err != nil {
		t.Fatal(err)
	}
}

// --- GUIDE §4.5 Discriminated unions and unions -------------------------------------------

type Payment struct {
	Method string `json:"method"`
	Card   string `json:"card"`
	IBAN   string `json:"iban"`
}

var (
	pMethod = vb.NewField("Method", func(p Payment) string { return p.Method }).JSON("method")
	pCard   = vb.NewField("Card", func(p Payment) string { return p.Card }).JSON("card")
	pIBAN   = vb.NewField("IBAN", func(p Payment) string { return p.IBAN }).JSON("iban")
)

// PaymentSchema: one struct, two shapes, chosen by "method".
var PaymentSchema = vb.Struct("Payment",
	vb.F(pMethod, vb.String().Enum("card", "bank")),
	vb.F(pCard, vb.String()).Optional(),
	vb.F(pIBAN, vb.String()).Optional(),
).Gate(
	vb.When(vb.Eq(pMethod, "card"),
		[]ir.TargetRule{vb.On(pCard, ir.CreditCard())},
		[]ir.TargetRule{vb.On(pIBAN, ir.MinLength(15))}),
).MustBuild()

func TestGuideVariants(t *testing.T) {
	v, _ := interp.FromSchema(PaymentSchema)
	if err := v.Validate(context.Background(), &Payment{Method: "card", Card: "4242 4242 4242 4242"}); err != nil {
		t.Errorf("card: %v", err)
	}
	errs := v.Run(context.Background(), &Payment{Method: "bank", IBAN: "short"})
	if errs.Len() != 1 || errs.Issues[0].Path != "$.iban" {
		t.Errorf("bank: %v", errs)
	}
	// A union on an `any` field: first passing variant wins.
	type Doc struct {
		ID any `json:"id"`
	}
	idField := vb.NewField("ID", func(d Doc) any { return d.ID }).JSON("id")
	docSchema := vb.Struct("Doc", vb.F(idField, vb.Union[any](vb.Int[int]().Min(1), vb.String().UUID()))).MustBuild()
	dv, _ := interp.FromSchema(docSchema)
	if err := dv.Validate(context.Background(), &Doc{ID: 7}); err != nil {
		t.Errorf("int variant: %v", err)
	}
	if err := dv.Validate(context.Background(), &Doc{ID: "123e4567-e89b-12d3-a456-426614174000"}); err != nil {
		t.Errorf("uuid variant: %v", err)
	}
	if err := dv.Validate(context.Background(), &Doc{ID: "nope"}); err == nil {
		t.Error("no variant should match")
	}
}

// TestGuideEmbedsFiles pins the guide to the files it shows: every file
// under guide/ appears verbatim in docs/GUIDE.md, so editing one without
// the other fails here instead of drifting.
func TestGuideEmbedsFiles(t *testing.T) {
	guide, err := os.ReadFile(filepath.Join("..", "docs", "GUIDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"guide/signup/model.go",
		"guide/signup/fields.gen.go",
		"guide/signup/rules.go",
		"guide/cmd/signup-server/main.go",
		"guide/cmd/signup-export/main.go",
	} {
		src, err := os.ReadFile(rel)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(guide), strings.TrimRight(string(src), "\n")) {
			t.Errorf("docs/GUIDE.md does not embed %s verbatim", rel)
		}
	}
}
