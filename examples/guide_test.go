package examples

// This file is docs/GUIDE.md's proof. The guide's files live in guide/
// (compiled by the build); the behaviors the guide claims for them are
// asserted here, in the guide's order. When the guide changes, change this
// file; when this file changes, change the guide.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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

// --- GUIDE §4.11 Unknown keys --------------------------------------------------

func TestGuideUnknownKeys(t *testing.T) {
	type Tiny struct {
		Name string `json:"name"`
	}
	name := vb.NewField("Name", func(x Tiny) string { return x.Name }).JSON("name")
	strict := vb.Struct("Tiny", vb.F(name, vb.String())).Extra(ir.ExtraForbid).MustBuild()
	v, _ := interp.FromSchema(strict)
	errs := v.Run(context.Background(), map[string]any{"name": "a", "typo": 1})
	if errs.Len() != 1 || errs.Issues[0].Code != verr.CodeUnknownKey || errs.Issues[0].Path != "$.typo" {
		t.Errorf("forbid: %v", errs)
	}
	// Struct input has no unknown keys to reject; the policy is about maps.
	if err := v.Validate(context.Background(), &Tiny{Name: "a"}); err != nil {
		t.Error(err)
	}
}

// --- GUIDE §4.4 Custom gate predicates ------------------------------------------------

// vipRegistry answers the schema's own names and adds the "vip" predicate
// a gate refers to by name. Both interp.WithFuncs and generated code accept
// it: it implements Func (and, for generated code, TypedFunc).
type vipRegistry struct{ base *vb.Schema[signup.Signup] }

func (r vipRegistry) Func(name string) (vb.Func, bool) {
	if name == "vip" {
		return func(_ context.Context, v any) error {
			if s, ok := v.(signup.Signup); ok && strings.HasSuffix(s.Email, "@vip.example.com") {
				return nil
			}
			return errors.New("not vip")
		}, true
	}
	return r.base.Func(name)
}

func TestGuideCustomPredicate(t *testing.T) {
	gated := vb.Struct("Signup",
		vb.F(signup.SignupFields.Email, vb.String().Email()),
		vb.F(signup.SignupFields.Age, vb.Int[int]()),
	).Gate(
		// Non-VIPs must be 21+; the predicate is Go-only and lands in the manifest.
		vb.When(ir.CustomCond("vip"), nil, []ir.TargetRule{vb.On(signup.SignupFields.Age, ir.Min(21))}),
	).MustBuild()
	v, err := interp.New(gated.IR(), interp.WithFuncs(vipRegistry{base: gated}))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Validate(context.Background(), &signup.Signup{Email: "a@vip.example.com", Age: 18}); err != nil {
		t.Errorf("vip: %v", err)
	}
	if err := v.Validate(context.Background(), &signup.Signup{Email: "a@example.com", Age: 18}); err == nil {
		t.Error("non-vip under 21 passed")
	}
	// Without a registry that knows "vip", New refuses instead of guessing.
	if _, err := interp.FromSchema(gated); err == nil {
		t.Error("unresolved predicate accepted")
	}
}

// --- GUIDE §4.5 OneOf default case -----------------------------------------------------

func TestGuideOneOfDefault(t *testing.T) {
	type Event struct {
		Kind string `json:"kind"`
		URL  string `json:"url"`
	}
	kind := vb.NewField("Kind", func(e Event) string { return e.Kind }).JSON("kind")
	url := vb.NewField("URL", func(e Event) string { return e.URL }).JSON("url")
	type Doc struct {
		Event Event `json:"event"`
	}
	ev := vb.NewField("Event", func(d Doc) Event { return d.Event }).JSON("event")
	s := vb.Struct("Doc", vb.F(ev, vb.OneOf[Event]("kind").
		Case("online", vb.Object(vb.F(kind, vb.String().Const("online")), vb.F(url, vb.String().URL()))).
		Default(vb.Object(vb.F(kind, vb.String()), vb.F(url, vb.String()).Optional())))).MustBuild()
	v, _ := interp.FromSchema(s)
	if err := v.Validate(context.Background(), &Doc{Event: Event{Kind: "other"}}); err != nil {
		t.Errorf("default case: %v", err)
	}
	if err := v.Validate(context.Background(), &Doc{Event: Event{Kind: "online", URL: "nope"}}); err == nil {
		t.Error("online case not applied")
	}
}

// --- GUIDE §4.8 Results: RunIntoAt, ByPath, OrNil --------------------------------------------

func TestGuideResults(t *testing.T) {
	v, _ := interp.FromSchema(signup.Schema)
	errs := &verr.Errors{}
	v.RunIntoAt(context.Background(), &signup.Signup{}, "$.body", errs)
	if errs.Issues[0].Path != "$.body.email" {
		t.Errorf("RunIntoAt path: %s", errs.Issues[0].Path)
	}
	byPath := errs.ByPath()
	if len(byPath["$.body.email"]) != 1 {
		t.Errorf("ByPath: %v", byPath)
	}
	var none *verr.Errors
	if none.OrNil() != nil || none.Len() != 0 {
		t.Error("nil-safety")
	}
	// Build reports every problem at once instead of panicking.
	bad := vb.NewField("Email", func(s signup.Signup) string { return s.Email })
	if _, err := vb.Struct("Bad", vb.F(bad, vb.String().Pattern("(")), vb.F(bad, vb.String())).Build(); err == nil {
		t.Error("Build accepted an invalid regex and a duplicate field")
	}
}

// --- GUIDE §4.9 Endpoints and rulehttp options ------------------------------------------------

func TestGuideEndpointsAndOptions(t *testing.T) {
	eps := signup.Schema.Endpoints()
	if len(eps) != 1 || eps[0].Rule != "email_free" || eps[0].Path != "/v/email_free" {
		t.Fatalf("endpoints: %+v", eps)
	}
	h, err := rulehttp.FromSchema(signup.Schema,
		rulehttp.WithMaxBody(256),
		rulehttp.WithLimiter(rulehttp.NoLimit),
		rulehttp.WithAuthorize(func(r *http.Request) error {
			if r.Header.Get("Authorization") != "Bearer secret" {
				return errors.New("no token")
			}
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	// Unauthorized: a transport error, not a validation issue.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v/email_free", strings.NewReader(`{"rule":"email_free","value":"x"}`))
	resp, _ := http.DefaultClient.Do(req)
	var body rulehttp.Response
	_ = json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || body.OK || body.Error == nil || body.Error.Code != rulehttp.CodeUnauthorized {
		t.Errorf("authorize: %d %+v", resp.StatusCode, body)
	}
	// Authorized and valid.
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/v/email_free", strings.NewReader(`{"rule":"email_free","value":"new@example.com"}`))
	req.Header.Set("Authorization", "Bearer secret")
	resp, _ = http.DefaultClient.Do(req)
	_ = json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if !body.OK {
		t.Errorf("valid: %+v", body)
	}
	// Custom (opaque) rules are never served.
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/v/not_reserved", strings.NewReader(`{"rule":"not_reserved","value":"x"}`))
	req.Header.Set("Authorization", "Bearer secret")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("custom rule served over HTTP")
	}
}

// --- GUIDE §4.10 Catalog checks --------------------------------------------------------

func TestGuideCatalogChecks(t *testing.T) {
	de, _ := i18n.Load(strings.NewReader(`{"locale":"de","messages":{"min_length":"mindestens {value} Zeichen"}}`))
	if missing := de.Missing(); len(missing) == 0 || len(missing) != len(i18n.Codes())-1 {
		t.Errorf("Missing: %d of %d", len(missing), len(i18n.Codes()))
	}
	if len(i18n.Merge(i18n.Default(), de).Missing()) != 0 {
		t.Error("merged catalog still missing codes")
	}
	if verr.Render("at least {value} chars", map[string]any{"value": 3}) != "at least 3 chars" {
		t.Error("Render")
	}
}

// --- GUIDE §5 Draft-07, manifest queries, contract ------------------------------------------

func TestGuideExportOptions(t *testing.T) {
	res, err := jsonschema.Emit(signup.Schema.IR(), jsonschema.WithDraft(jsonschema.Draft07))
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := res.JSON()
	if !strings.Contains(string(doc), string(jsonschema.Draft07)) {
		t.Error("draft-07 dialect not set")
	}
	if res.Manifest.Portable() {
		t.Error("signup has non-portable rules")
	}
	by := res.Manifest.ByClass()
	if len(by[jsonschema.ClassServer]) != 1 || len(by[jsonschema.ClassOpaque]) != 1 || len(by[jsonschema.ClassLossy]) != 2 || len(by[jsonschema.ClassCEL]) != 1 {
		t.Errorf("ByClass: %v", by)
	}
	if _, ok := jsonschema.Contract().Get("$defs"); !ok {
		t.Error("contract has no defs")
	}
	// Hand-written IR: decode, check, run — the same path a non-Go tool takes.
	raw, _ := json.Marshal(signup.Schema.IR())
	var hand ir.Schema
	if err := json.Unmarshal(raw, &hand); err != nil {
		t.Fatal(err)
	}
	if err := ir.Check(&hand); err != nil {
		t.Fatal(err)
	}
	// CEL is declared in the IR but refused by the executors today.
	cel := &ir.Schema{Version: ir.Version, Root: &ir.ObjectNode{Gated: []ir.GatedRules{{Cond: &[]ir.Condition{ir.Expr("1 == 1")}[0]}}}}
	if _, err := interp.New(cel); err == nil {
		t.Error("CEL accepted")
	}
}

// TestGuideProgramsCompile extracts every ```go block in docs/GUIDE.md that
// is a whole program (starts with "package main") and builds it inside
// this module, so a snippet that names a wrong identifier fails CI. The
// files under guide/ are covered by the ordinary build; this covers the
// programs that exist only in the guide.
func TestGuideProgramsCompile(t *testing.T) {
	guide, err := os.ReadFile(filepath.Join("..", "docs", "GUIDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join("guide", "snippettmp") // no leading dot: "..." patterns skip those
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	var n int
	var pkgs []string
	for _, block := range strings.Split(string(guide), "```go\n")[1:] {
		src, _, _ := strings.Cut(block, "\n```")
		if !strings.HasPrefix(src, "package main") {
			continue
		}
		n++
		sub := filepath.Join(dir, fmt.Sprintf("p%02d", n))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "main.go"), []byte(src+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		pkgs = append(pkgs, "./"+filepath.ToSlash(sub))
	}
	if n == 0 {
		t.Fatal("no programs found in the guide")
	}
	cmd := exec.Command("go", append([]string{"vet"}, pkgs...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("guide programs do not compile:\n%s", out)
	}
	t.Logf("%d guide programs compiled", n)
}

// TestGuideGeneratedMatchesInterpreter runs the generated ValidateSignup
// (guide/signup/validate.gen.go, from `go generate`) and the interpreter on
// the same inputs: the guide's claim that switching executors changes
// nothing is checked here for the guide's own schema.
func TestGuideGeneratedMatchesInterpreter(t *testing.T) {
	v, _ := interp.FromSchema(signup.Schema)
	inputs := []signup.Signup{
		{},
		{Email: "  Ada@Example.com ", Password: "short", Age: 17, Plan: "team"},
		{Email: "admin@example.com", Password: "correct horse", Age: 30, Plan: "free", Starts: time.Now(), Ends: time.Now().Add(time.Hour)},
		{Email: "ada@example.com", Password: "correct horse", Age: 30, Plan: "free", Starts: time.Now(), Ends: time.Now().Add(time.Hour)},
	}
	for i, in := range inputs {
		a, b := in, in
		got := signup.ValidateSignup(context.Background(), &a, signup.Schema)
		want := v.Run(context.Background(), &b)
		gj, _ := json.Marshal(got.ByPath())
		wj, _ := json.Marshal(want.ByPath())
		if string(gj) != string(wj) {
			t.Errorf("input %d: generated %s\ninterpreter %s", i, gj, wj)
		}
		if a.Email != b.Email {
			t.Errorf("input %d: transforms differ: %q vs %q", i, a.Email, b.Email)
		}
	}
}
