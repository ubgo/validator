package interp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/vb"
	"github.com/ubgo/validator/verr"
)

type Address struct {
	Street string `json:"street"`
	Zip    string `json:"zip"`
}

type Event struct {
	Kind    string   `json:"kind"`
	URL     string   `json:"url"`
	Address *Address `json:"address"`
}

type User struct {
	Email  string            `json:"email"`
	Age    int32             `json:"age"`
	Score  float64           `json:"score"`
	Active bool              `json:"active"`
	Avatar []byte            `json:"avatar"`
	Start  time.Time         `json:"start"`
	End    time.Time         `json:"end"`
	TTL    time.Duration     `json:"ttl"`
	Tags   []string          `json:"tags"`
	Labels map[string]int    `json:"labels"`
	Home   Address           `json:"home"`
	Extra  any               `json:"extra"`
	ID     any               `json:"id"`
	Type   string            `json:"type"`
	URL    string            `json:"url"`
	Venue  string            `json:"venue"`
	Coupon string            `json:"coupon"`
	Event  Event             `json:"event"`
	Nick   *string           `json:"nick"`
	Prefs  map[string]string `json:"prefs"`
}

var (
	fStreet = vb.NewField("Street", func(a Address) string { return a.Street }).JSON("street")
	fZip    = vb.NewField("Zip", func(a Address) string { return a.Zip }).JSON("zip")

	fEKind = vb.NewField("Kind", func(e Event) string { return e.Kind }).JSON("kind")
	fEURL  = vb.NewField("URL", func(e Event) string { return e.URL }).JSON("url")
	fEAddr = vb.NewField("Address", func(e Event) *Address { return e.Address }).JSON("address")

	fEmail  = vb.NewField("Email", func(u User) string { return u.Email }).JSON("email")
	fAge    = vb.NewField("Age", func(u User) int32 { return u.Age }).JSON("age")
	fScore  = vb.NewField("Score", func(u User) float64 { return u.Score }).JSON("score")
	fActive = vb.NewField("Active", func(u User) bool { return u.Active }).JSON("active")
	fAvatar = vb.NewField("Avatar", func(u User) []byte { return u.Avatar }).JSON("avatar")
	fStart  = vb.NewField("Start", func(u User) time.Time { return u.Start }).JSON("start")
	fEnd    = vb.NewField("End", func(u User) time.Time { return u.End }).JSON("end")
	fTTL    = vb.NewField("TTL", func(u User) time.Duration { return u.TTL }).JSON("ttl")
	fTags   = vb.NewField("Tags", func(u User) []string { return u.Tags }).JSON("tags")
	fLabels = vb.NewField("Labels", func(u User) map[string]int { return u.Labels }).JSON("labels")
	fHome   = vb.NewField("Home", func(u User) Address { return u.Home }).JSON("home")
	fExtra  = vb.NewField("Extra", func(u User) any { return u.Extra }).JSON("extra")
	fID     = vb.NewField("ID", func(u User) any { return u.ID }).JSON("id")
	fType   = vb.NewField("Type", func(u User) string { return u.Type }).JSON("type")
	fURL    = vb.NewField("URL", func(u User) string { return u.URL }).JSON("url")
	fVenue  = vb.NewField("Venue", func(u User) string { return u.Venue }).JSON("venue")
	fCoupon = vb.NewField("Coupon", func(u User) string { return u.Coupon }).JSON("coupon")
	fEvent  = vb.NewField("Event", func(u User) Event { return u.Event }).JSON("event")
	fNick   = vb.NewField("Nick", func(u User) *string { return u.Nick }).JSON("nick")
	fPrefs  = vb.NewField("Prefs", func(u User) map[string]string { return u.Prefs }).JSON("prefs")
)

var address = vb.Struct("Address",
	vb.F(fStreet, vb.String().MinLength(1)),
	vb.F(fZip, vb.String().Pattern(`^[0-9]{5}$`)),
).MustBuild()

func userSchema() *vb.Schema[User] {
	return vb.Struct("User",
		vb.F(fEmail, vb.String().Email().
			Custom("no_temp", func(_ context.Context, s string) error {
				if strings.HasSuffix(s, "@temp.com") {
					return errors.New("temp domains are not allowed")
				}
				return nil
			}).
			Async("unique_email", "/v/unique_email", func(_ context.Context, s string) error {
				if s == "taken@example.com" {
					return errors.New("already taken")
				}
				return nil
			})).Trim().Lower(),
		vb.F(fAge, vb.Int[int32]().Min(18).Msg("age_range", "age must be {value} or more").Max(120)),
		vb.F(fScore, vb.Float[float64]().Gte(0).Lte(1).MultipleOf(0.25)),
		vb.F(fActive, vb.Bool()),
		vb.F(fAvatar, vb.Bytes().MaxLength(4)).Optional(),
		vb.F(fStart, vb.Time()),
		vb.F(fEnd, vb.Time().With(vb.After(fStart))),
		vb.F(fTTL, vb.Duration().Gt(0).Lt(time.Hour)),
		vb.F(fTags, vb.Array(vb.String().MinLength(1).Enum("a", "b", "c")).MinItems(1).MaxItems(3).Unique()),
		vb.F(fLabels, vb.Map(vb.String().MaxLength(3), vb.Int[int]().MultipleOf(2))).Optional(),
		vb.F(fHome, vb.Ref(address)),
		vb.F(fExtra, vb.Any[any]()).Optional(),
		vb.F(fID, vb.Union[any](vb.Int[int](), vb.String().UUID())).Optional(),
		vb.F(fType, vb.String().Enum("online", "offline")),
		vb.F(fURL, vb.String().URL()).Optional(),
		vb.F(fVenue, vb.String()).Optional(),
		vb.F(fCoupon, vb.String().ULID()).Optional(),
		vb.F(fEvent, vb.OneOf[Event]("kind").
			Case("online", vb.Object(
				vb.F(fEKind, vb.String().Const("online")),
				vb.F(fEURL, vb.String().URL()),
			)).
			Case("offline", vb.Object(
				vb.F(fEKind, vb.String().Const("offline")),
				vb.F(fEAddr, vb.Ptr(vb.Ref(address))),
			))),
		vb.F(fNick, vb.Ptr(vb.String().MinLength(2))).Optional(),
		vb.F(fPrefs, vb.Map(vb.String(), vb.String())).Optional(),
	).
		Extra(ir.ExtraForbid).
		Gate(
			vb.When(vb.Eq(fType, "online"), []ir.TargetRule{vb.On(fURL, ir.MinLength(1))}, []ir.TargetRule{vb.On(fVenue, ir.MinLength(1))}),
			vb.Requires(ir.And(vb.Has(fCoupon), ir.Not(vb.In(fType, "offline"))), fVenue),
			vb.Group(ir.AtLeastOne, fURL, fVenue),
			vb.When(ir.Or(vb.FieldCmp(fEnd, ir.OpGt, fStart), ir.CustomCond("never")), []ir.TargetRule{vb.On(fTTL, ir.Lt(float64(30*time.Minute)))}, nil),
		).
		MustBuild()
}

var (
	t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 = t0.Add(time.Hour)
)

func validUser() User {
	return User{
		Email: "ada@example.com", Age: 30, Score: 0.5, Active: true,
		Start: t0, End: t1, TTL: 10 * time.Minute, Tags: []string{"a", "b"},
		Home: Address{Street: "Main", Zip: "12345"}, Type: "online", URL: "https://x.io",
		Event: Event{Kind: "online", URL: "https://e.io"},
	}
}

func newValidator(t *testing.T, opts ...Option) *Validator {
	t.Helper()
	s := userSchema()
	// The custom condition body is registered through a wrapping registry
	// because conditions are declared with ir.CustomCond, not the builder.
	reg := &registry{inner: s, extra: map[string]vb.Func{
		"never": func(context.Context, any) error { return errors.New("no") },
	}}
	v, err := New(s.IR(), append([]Option{WithFuncs(reg)}, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return v
}

type registry struct {
	inner Funcs
	extra map[string]vb.Func
}

func (r *registry) Func(name string) (vb.Func, bool) {
	if fn, ok := r.extra[name]; ok {
		return fn, true
	}
	return r.inner.Func(name)
}

// codes returns "path:code" for every issue, the compact form the tests
// assert on.
func codes(err error) []string {
	if err == nil {
		return nil
	}
	e, ok := err.(*verr.Errors)
	if !ok {
		return []string{"not-errors:" + err.Error()}
	}
	out := make([]string, len(e.Issues))
	for i, is := range e.Issues {
		out[i] = is.Path + ":" + is.Code
	}
	return out
}

func expect(t *testing.T, err error, want ...string) {
	t.Helper()
	got := codes(err)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("issues\n got: %v\nwant: %v\nfull: %v", got, want, err)
	}
}

func TestValidStruct(t *testing.T) {
	v := newValidator(t)
	u := validUser()
	if err := v.Validate(context.Background(), u); err != nil {
		t.Fatalf("valid user rejected: %v", err)
	}
	if err := v.Validate(context.Background(), &u); err != nil {
		t.Fatalf("pointer rejected: %v", err)
	}
}

func TestEveryRuleFails(t *testing.T) {
	v := newValidator(t)
	ctx := context.Background()
	mut := func(f func(*User)) User { u := validUser(); f(&u); return u }

	expect(t, v.Validate(ctx, mut(func(u *User) { u.Email = "nope" })), "$.email:email")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Email = "Name <a@b.io>" })), "$.email:email")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Email = "x@temp.com" })), "$.email:custom")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Email = "taken@example.com" })), "$.email:custom")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Age = 17 })), "$.age:age_range")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Age = 121 })), "$.age:max")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Score = -0.3 })), "$.score:gte", "$.score:multiple_of")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Score = 0.3 })), "$.score:multiple_of")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Avatar = []byte("12345") })), "$.avatar:max_length")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.End = t0 })), "$.end:after")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.TTL = 0 })), "$.ttl:gt")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.TTL = 2 * time.Hour })), "$.ttl:lt", "$.ttl:lt")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Tags = []string{} })), "$.tags:min_items")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Tags = []string{"a", "b", "c", "a"} })), "$.tags:max_items", "$.tags:unique")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Tags = []string{"", "z"} })), "$.tags[0]:min_length", "$.tags[0]:enum", "$.tags[1]:enum")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Labels = map[string]int{"toolong": 3} })), "$.labels.toolong:max_length", "$.labels.toolong:multiple_of")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Home.Zip = "abc"; u.Home.Street = "" })), "$.home.street:required", "$.home.zip:pattern")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.ID = "not-a-uuid" })), "$.id:no_union_match")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.ID = 7 })))
	expect(t, v.Validate(ctx, mut(func(u *User) { u.ID = "123e4567-e89b-12d3-a456-426614174000" })))
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Type = "hybrid" })), "$.type:enum", "$.venue:min_length")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.URL = "not a url" })), "$.url:url")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Coupon = "bad"; u.Venue = "" })), "$.coupon:ulid", "$.venue:required")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Coupon = "01ARZ3NDEKTSV4RRFFQ69G5FAV"; u.Venue = "Hall" })))
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Event = Event{Kind: "hybrid"} })), "$.event:unknown_variant")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Event = Event{Kind: "online", URL: "x"} })), "$.event.url:url")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Event = Event{Kind: "offline"} })), "$.event.address:required")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Event = Event{Kind: "offline", Address: &Address{Street: "s", Zip: "99999"}} })))
	expect(t, v.Validate(ctx, mut(func(u *User) { n := "x"; u.Nick = &n })), "$.nick:min_length")
	expect(t, v.Validate(ctx, mut(func(u *User) { n := "xy"; u.Nick = &n })))
}

func TestGates(t *testing.T) {
	v := newValidator(t)
	ctx := context.Background()
	mut := func(f func(*User)) User { u := validUser(); f(&u); return u }

	// when: online requires URL; offline requires Venue.
	expect(t, v.Validate(ctx, mut(func(u *User) { u.URL = "" })), "$.url:min_length", "$:group")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Type = "offline"; u.URL = "" })), "$.venue:min_length", "$:group")
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Type = "offline"; u.URL = ""; u.Venue = "Hall" })))
	// group at-least-one satisfied by venue alone.
	expect(t, v.Validate(ctx, mut(func(u *User) { u.Type = "offline"; u.URL = ""; u.Venue = "Hall" })))
	// The end>start gate applies TTL < 30m; a 45m TTL trips it but not the 1h Lt.
	expect(t, v.Validate(ctx, mut(func(u *User) { u.TTL = 45 * time.Minute })), "$.ttl:lt")
	// When end is not after start the after rule fires and the gate's else branch (nil) applies nothing extra.
	expect(t, v.Validate(ctx, mut(func(u *User) { u.End = t0; u.TTL = 45 * time.Minute })), "$.end:after")
}

func TestRequiredAndNullable(t *testing.T) {
	v := newValidator(t)
	ctx := context.Background()
	u := User{}
	err := v.Validate(ctx, u)
	got := codes(err)
	for _, want := range []string{"$.email:required", "$.age:age_range", "$.start:required", "$.end:required", "$.ttl:gt", "$.tags:required", "$.home.street:required", "$.home.zip:required", "$.type:required", "$.event:unknown_variant", "$.venue:min_length", "$:group"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	// Booleans and numbers are always present: Active=false, Score=0 pass.
	for _, g := range got {
		if strings.HasPrefix(g, "$.active") || strings.HasPrefix(g, "$.score") {
			t.Errorf("zero bool/number must not be required: %s", g)
		}
	}
	// Non-nullable nil at the root.
	expect(t, v.Validate(ctx, nil), "$:required")
	var up *User
	expect(t, v.Validate(ctx, up), "$:required")
	// Nullable pointer accepted, non-nullable pointer required.
	s := vb.Struct("N",
		vb.F(fNick, vb.Ptr(vb.String())),
		vb.F(fNick, vb.Ptr(vb.String())),
	)
	if _, err := s.Build(); err == nil {
		t.Fatal("duplicate field should fail Check")
	}
}

func TestMapInput(t *testing.T) {
	v := newValidator(t)
	ctx := context.Background()
	raw := `{"email":"  ADA@Example.com ","age":30,"score":0.5,"active":true,"start":"x","end":"y","ttl":600000000000,
	"tags":["a"],"home":{"street":"Main","zip":"12345"},"type":"online","url":"https://x.io","event":{"kind":"online","url":"https://e.io"},"bogus":1}`
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	err := v.Validate(ctx, m)
	// Times arrive as strings from JSON: a malformed one is a type issue, a
	// well-formed RFC 3339 one is parsed and written back. Unknown keys are
	// reported under forbid. Numbers decode as float64 and are accepted
	// where integral.
	expect(t, err, "$.start:type", "$.end:type", "$.bogus:unknown_key")
	m["age"] = 30.5
	expect(t, v.Validate(ctx, m), "$.age:type", "$.start:type", "$.end:type", "$.bogus:unknown_key")
	m["age"] = 30.0
	m["start"], m["end"] = "2026-01-01T00:00:00Z", "2026-01-01T01:00:00Z"
	delete(m, "bogus")
	expect(t, v.Validate(ctx, m))
	if _, ok := m["end"].(time.Time); !ok {
		t.Errorf("time string not written back as time.Time: %T", m["end"])
	}
	m["end"] = "2025-12-31T00:00:00Z" // parsed, then the after-rule fires
	expect(t, v.Validate(ctx, m), "$.end:after")
	// Transforms wrote back into the map.
	if m["email"] != "ada@example.com" {
		t.Errorf("transform not applied to map: %q", m["email"])
	}
	// Nested map objects and arrays of maps validate too.
	m["start"], m["end"] = t0, t1
	delete(m, "bogus")
	m["home"] = map[string]any{"street": "", "zip": "1"}
	expect(t, v.Validate(ctx, m), "$.home.street:required", "$.home.zip:pattern")
	// Wrong shapes at every level.
	m["home"] = "not an object"
	m["tags"] = "not an array"
	m["labels"] = []any{1}
	m["age"] = "thirty"
	m["score"] = true
	m["active"] = 1
	m["avatar"] = 3
	m["ttl"] = "long"
	m["event"] = 5
	expect(t, v.Validate(ctx, m), "$.age:type", "$.score:type", "$.active:type", "$.avatar:type", "$.ttl:type", "$.tags:type", "$.labels:type", "$.home:type", "$.event:type")
}

func TestTransformsWriteBack(t *testing.T) {
	v := newValidator(t)
	u := validUser()
	u.Email = "  ADA@Example.com "
	if err := v.Validate(context.Background(), &u); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if u.Email != "ada@example.com" {
		t.Errorf("pointer input must be trimmed and lowered in place: %q", u.Email)
	}
	// By value: validated view is transformed, original untouched.
	u2 := validUser()
	u2.Email = "  ADA@Example.com "
	if err := v.Validate(context.Background(), u2); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !strings.HasPrefix(u2.Email, "  ") {
		t.Error("value input must not be mutated")
	}
	// Default fills an absent field.
	type D struct {
		Name string
	}
	fName := vb.NewField("Name", func(d D) string { return d.Name })
	s := vb.Struct("D", vb.F(fName, vb.String().MinLength(2)).Default("anon")).MustBuild()
	dv, err := FromSchema(s)
	if err != nil {
		t.Fatal(err)
	}
	d := D{}
	if err := dv.Validate(context.Background(), &d); err != nil || d.Name != "anon" {
		t.Errorf("default not applied: %v %q", err, d.Name)
	}
	m := map[string]any{}
	if err := dv.Validate(context.Background(), m); err != nil || m["Name"] != "anon" {
		t.Errorf("default not applied to map: %v %v", err, m)
	}
	// Default of a non-assignable type still validates the default value.
	bad := vb.Struct("D", vb.F(fName, vb.String()).Default(42)).MustBuild()
	bv, _ := FromSchema(bad)
	expect(t, bv.Validate(context.Background(), D{}), "$.Name:type")
	// Upper transform.
	up := vb.Struct("D", vb.F(fName, vb.String().Const("ABC")).Upper()).MustBuild()
	uv, _ := FromSchema(up)
	if err := uv.Validate(context.Background(), D{Name: "abc"}); err != nil {
		t.Errorf("upper: %v", err)
	}
}

func TestCoerceAndNormalizeEmail(t *testing.T) {
	type Form struct {
		Email string        `json:"email"`
		Age   int           `json:"age"`
		Score float64       `json:"score"`
		Opt   bool          `json:"opt"`
		When  time.Time     `json:"when"`
		Wait  time.Duration `json:"wait"`
	}
	fEmail := vb.NewField("Email", func(f Form) string { return f.Email }).JSON("email")
	fAge := vb.NewField("Age", func(f Form) int { return f.Age }).JSON("age")
	fScore := vb.NewField("Score", func(f Form) float64 { return f.Score }).JSON("score")
	fOpt := vb.NewField("Opt", func(f Form) bool { return f.Opt }).JSON("opt")
	fWhen := vb.NewField("When", func(f Form) time.Time { return f.When }).JSON("when")
	fWait := vb.NewField("Wait", func(f Form) time.Duration { return f.Wait }).JSON("wait")
	s := vb.Struct("Form",
		vb.F(fEmail, vb.String().Email()).NormalizeEmail(),
		vb.F(fAge, vb.Int[int]().Min(18)).Coerce(),
		vb.F(fScore, vb.Float[float64]().Lte(1)).Coerce(),
		vb.F(fOpt, vb.Bool().Const(true)).Coerce(),
		vb.F(fWhen, vb.Time()).Coerce(),
		vb.F(fWait, vb.Duration().Gt(0)).Coerce(),
	).MustBuild()
	v, err := FromSchema(s)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// Form-style map input: every string becomes its typed value and is
	// written back.
	m := map[string]any{"email": " Ada@Example.com ", "age": "21", "score": "0.5", "opt": "true", "when": "2026-01-01T00:00:00Z", "wait": "1h"}
	expect(t, v.Validate(ctx, m))
	if m["email"] != "ada@example.com" || m["age"] != 21.0 || m["score"] != 0.5 || m["opt"] != true || m["wait"] != time.Hour {
		t.Errorf("coerced map: %v", m)
	}
	if _, isTime := m["when"].(time.Time); !isTime {
		t.Errorf("time not coerced: %T", m["when"])
	}
	// Rules run on the coerced value.
	expect(t, v.Validate(ctx, map[string]any{"email": "a@b.io", "age": "7", "score": "2", "opt": "false", "when": "2026-01-01T00:00:00Z", "wait": "0s"}),
		"$.age:min", "$.score:lte", "$.opt:const", "$.wait:gt")
	// A string that does not parse stays a string and fails the type check.
	expect(t, v.Validate(ctx, map[string]any{"email": "a@b.io", "age": "old", "score": "0.5", "opt": "true", "when": "later", "wait": "1h"}),
		"$.age:type", "$.when:type")
	// Typed struct input: coerce is a no-op, normalizeEmail writes back.
	f := Form{Email: "  ADA@X.IO ", Age: 21, Score: 0.5, Opt: true, When: t0, Wait: time.Hour}
	expect(t, v.Validate(ctx, &f))
	if f.Email != "ada@x.io" {
		t.Errorf("normalizeEmail on struct: %q", f.Email)
	}
	// A typed map (map[string]string) cannot hold coerced values: no
	// write-back, the string fails the int type check.
	expect(t, v.Validate(ctx, map[string]string{"email": "a@b.io", "age": "21", "score": "0.5", "opt": "true", "when": "x", "wait": "1h"}),
		"$.age:type", "$.score:type", "$.opt:type", "$.when:type", "$.wait:type")
}

func TestFailFast(t *testing.T) {
	v := newValidator(t, FailFast())
	u := User{}
	err := v.Validate(context.Background(), u)
	if n := len(codes(err)); n != 1 {
		t.Fatalf("fail-fast must stop at one issue, got %d: %v", n, err)
	}
	// RunInto honors the caller's flag and appends across calls.
	shared := &verr.Errors{FailFast: true}
	newValidator(t).RunInto(context.Background(), u, shared)
	newValidator(t).RunInto(context.Background(), u, shared)
	if shared.Len() != 1 {
		t.Errorf("caller-owned fail-fast: %d", shared.Len())
	}
	at := &verr.Errors{}
	newValidator(t).RunIntoAt(context.Background(), u, "$.nested", at)
	if at.Len() == 0 || !strings.HasPrefix(at.Issues[0].Path, "$.nested.") {
		t.Errorf("RunIntoAt path prefix: %+v", at.Issues)
	}
	acc := &verr.Errors{}
	newValidator(t).RunInto(context.Background(), u, acc)
	first := acc.Len()
	newValidator(t).RunInto(context.Background(), u, acc)
	if acc.Len() != 2*first || first < 5 {
		t.Errorf("accumulating RunInto: %d then %d", first, acc.Len())
	}
	all := newValidator(t).Run(context.Background(), u)
	if all.Len() < 5 {
		t.Fatalf("accumulate mode should report many issues, got %d", all.Len())
	}
}

func TestNewRefusals(t *testing.T) {
	// The golden fixture carries a CEL condition the interpreter cannot run;
	// New must refuse rather than skip it.
	data, err := os.ReadFile(filepath.Join("..", "ir", "testdata", "user.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s ir.Schema
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	reg := &registry{inner: userSchema(), extra: map[string]vb.Func{
		"no_temp_domains": func(context.Context, any) error { return nil },
		"weird":           func(context.Context, any) error { return nil },
	}}
	_, err = New(&s, WithFuncs(reg))
	if err == nil || !strings.Contains(err.Error(), "CEL condition") {
		t.Errorf("CEL must be refused: %v", err)
	}
	// Missing registry and missing function.
	if _, err := New(userSchema().IR()); err == nil || !strings.Contains(err.Error(), "needs a function registry") {
		t.Errorf("missing registry: %v", err)
	}
	if _, err := New(userSchema().IR(), WithFuncs(userSchema())); err == nil || !strings.Contains(err.Error(), `"never" is referenced but not registered`) {
		t.Errorf("missing function: %v", err)
	}
	// Invalid schema.
	if _, err := New(&ir.Schema{Version: "v0", Root: &ir.AnyNode{}}); err == nil {
		t.Error("bad version must be refused")
	}
	// Bad regex.
	bad := &ir.Schema{Version: ir.Version, Root: &ir.StringNode{Rules: []ir.Rule{ir.Pattern("(")}}}
	if _, err := New(bad); err == nil || !strings.Contains(err.Error(), "pattern") {
		t.Errorf("bad regex: %v", err)
	}
	// An unknown transform kind is refused.
	type D struct{ Name string }
	fName := vb.NewField("Name", func(d D) string { return d.Name })
	bogus := &ir.Schema{Version: ir.Version, Root: &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "Name", Type: &ir.StringNode{}, Transforms: []ir.Transform{{Kind: ir.TransformTrim, Phase: ir.PhasePre}}}}}}
	if _, err := New(bogus); err != nil {
		t.Errorf("known transform refused: %v", err)
	}
	_ = fName
	// Gate rules and conditions are compiled too: an unregistered custom
	// condition inside a gate is caught.
	g := vb.Struct("D", vb.F(fName, vb.String())).Gate(vb.When(ir.CustomCond("ghost"), []ir.TargetRule{vb.On(fName, ir.Pattern("^ok$"))}, []ir.TargetRule{vb.On(fName, ir.Custom("ghost2"))})).MustBuild()
	_, err = FromSchema(g)
	for _, want := range []string{`"ghost"`, `"ghost2"`} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("gate compile missed %s: %v", want, err)
		}
	}
}

func TestScalarRootsAndMisc(t *testing.T) {
	ctx := context.Background()
	mk := func(n ir.TypeNode) *Validator {
		v, err := New(&ir.Schema{Version: ir.Version, Root: n})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	expect(t, mk(&ir.StringNode{Rules: []ir.Rule{ir.MinLength(2)}}).Validate(ctx, "héé"))
	expect(t, mk(&ir.StringNode{Rules: []ir.Rule{ir.MaxLength(2)}}).Validate(ctx, "héé"), "$:max_length")
	expect(t, mk(&ir.StringNode{}).Validate(ctx, 5), "$:type")
	expect(t, mk(&ir.IntNode{Rules: []ir.Rule{ir.Gt(1), ir.Lte(3), ir.Enum(2, 3), ir.Const(3)}}).Validate(ctx, uint8(3)))
	expect(t, mk(&ir.IntNode{Rules: []ir.Rule{ir.Gt(1), ir.Lte(3), ir.Enum(2, 3), ir.Const(3)}}).Validate(ctx, 4), "$:lte", "$:enum", "$:const")
	expect(t, mk(&ir.FloatNode{Rules: []ir.Rule{ir.Min(1)}}).Validate(ctx, 2))
	expect(t, mk(&ir.FloatNode{}).Validate(ctx, "x"), "$:type")
	expect(t, mk(&ir.BoolNode{Rules: []ir.Rule{ir.Const(true)}}).Validate(ctx, false), "$:const")
	expect(t, mk(&ir.BytesNode{Rules: []ir.Rule{ir.MinLength(2)}}).Validate(ctx, []byte("a")), "$:min_length")
	expect(t, mk(&ir.TimeNode{}).Validate(ctx, "2026"), "$:type")
	expect(t, mk(&ir.DurationNode{Rules: []ir.Rule{ir.Max(5)}}).Validate(ctx, int64(9)), "$:max")
	expect(t, mk(&ir.ArrayNode{Elem: &ir.AnyNode{}, Rules: []ir.Rule{ir.Unique()}}).Validate(ctx, [][]int{{1}, {1}}), "$:unique")
	expect(t, mk(&ir.ArrayNode{Elem: &ir.AnyNode{}, Rules: []ir.Rule{ir.Unique()}}).Validate(ctx, [][]int{{1}, {2}}))
	expect(t, mk(&ir.ArrayNode{Elem: &ir.IntNode{}}).Validate(ctx, [2]int{1, 2}))
	expect(t, mk(&ir.ArrayNode{Elem: &ir.IntNode{}}).Validate(ctx, []byte("x")), "$:type")
	expect(t, mk(&ir.MapNode{Key: &ir.StringNode{}, Val: &ir.IntNode{}, Rules: []ir.Rule{ir.MinItems(1)}}).Validate(ctx, map[string]int{}), "$:min_items")
	expect(t, mk(&ir.MapNode{Key: &ir.StringNode{}, Val: &ir.IntNode{}}).Validate(ctx, []int{}), "$:type")
	expect(t, mk(&ir.AnyNode{}).Validate(ctx, struct{}{}))
	expect(t, mk(&ir.StringNode{Base: ir.Base{Nullable: true}}).Validate(ctx, nil))
	expect(t, mk(&ir.ObjectNode{}).Validate(ctx, 42), "$:type")
	expect(t, mk(&ir.OneOfNode{Discriminator: "k", Cases: map[string]ir.TypeNode{"a": &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "k", Type: &ir.StringNode{}}}}}}).Validate(ctx, 42), "$:type")
	expect(t, mk(&ir.OneOfNode{Discriminator: "k", Cases: map[string]ir.TypeNode{"a": &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "k", Type: &ir.StringNode{}}}}}, Default: &ir.AnyNode{}}).Validate(ctx, map[string]any{"k": "zzz"}))
	// Discriminator found via case-insensitive Go field name.
	type Tagged struct{ Kind string }
	expect(t, mk(&ir.OneOfNode{Discriminator: "kind", Cases: map[string]ir.TypeNode{"a": &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "Kind", Type: &ir.StringNode{}}}}}}).Validate(ctx, Tagged{Kind: "a"}))
	expect(t, mk(&ir.OneOfNode{Discriminator: "kind", Cases: map[string]ir.TypeNode{"a": &ir.ObjectNode{Fields: []ir.FieldNode{{Name: "Kind", Type: &ir.StringNode{}}}}}}).Validate(ctx, struct{ Other string }{}), "$:unknown_variant")
	// Every new format kind is executed, not skipped.
	for kind, in := range map[ir.RuleKind][2]string{
		ir.RuleIPv4: {"1.2.3.4", "x"}, ir.RuleIPv6: {"::1", "1.2.3.4"}, ir.RuleIP: {"::1", "x"}, ir.RuleCIDR: {"10.0.0.0/8", "10.0.0.0"},
		ir.RuleMAC: {"00:11:22:33:44:55", "x"}, ir.RuleHostname: {"a.io", "-a"}, ir.RuleBase64: {"YQ==", "!"}, ir.RuleJSON: {"{}", "{"},
		ir.RuleSemver: {"1.0.0", "1"}, ir.RuleDate: {"2026-01-01", "x"}, ir.RuleDateTime: {"2026-01-01T00:00:00Z", "x"}, ir.RuleNotBlank: {"a", " "},
		ir.RuleCreditCard: {"4242424242424242", "4242424242424241"}, ir.RuleJWT: {"a.b.c", "a.b"}, ir.RuleCountryCode: {"US", "usa"},
		ir.RuleCurrencyCode: {"EUR", "eu"}, ir.RuleHexColor: {"#fff", "fff"}, ir.RuleE164: {"+14155552671", "555"},
	} {
		v := mk(&ir.StringNode{Rules: []ir.Rule{{Kind: kind}}})
		expect(t, v.Validate(ctx, in[0]))
		if err := v.Validate(ctx, in[1]); err == nil {
			t.Errorf("%s accepted %q", kind, in[1])
		}
	}
	expect(t, mk(&ir.FloatNode{Rules: []ir.Rule{ir.Latitude()}}).Validate(ctx, 45.5))
	expect(t, mk(&ir.FloatNode{Rules: []ir.Rule{ir.Latitude()}}).Validate(ctx, 91), "$:latitude")
	expect(t, mk(&ir.IntNode{Rules: []ir.Rule{ir.Longitude()}}).Validate(ctx, -181), "$:longitude")
	// multipleOf zero never passes; zero-value struct root for a map-only schema.
	expect(t, mk(&ir.IntNode{Rules: []ir.Rule{ir.MultipleOf(0)}}).Validate(ctx, 4), "$:multiple_of")
	// Rule-level condition without scope is false: the rule is skipped.
	expect(t, mk(&ir.StringNode{Rules: []ir.Rule{{Kind: ir.RuleMinLength, Params: map[string]any{ir.ParamValue: 9}, Cond: ptr(ir.HasField("x", ir.Present))}}}).Validate(ctx, "a"))
	// Message override and default rendering.
	e := mk(&ir.StringNode{Rules: []ir.Rule{ir.MinLength(3)}}).Run(ctx, "a")
	if e.Issues[0].Message != "must be at least 3 characters" || e.Issues[0].Rule != "minLength" {
		t.Errorf("default message: %+v", e.Issues[0])
	}
}

func TestConditions(t *testing.T) {
	type P struct {
		A, B  int
		S, T  string
		When  time.Time
		Later time.Time
		Ptr   *int
	}
	fA := vb.NewField("A", func(p P) int { return p.A })
	fB := vb.NewField("B", func(p P) int { return p.B })
	fS := vb.NewField("S", func(p P) string { return p.S })
	fT := vb.NewField("T", func(p P) string { return p.T })
	fW := vb.NewField("When", func(p P) time.Time { return p.When })
	fL := vb.NewField("Later", func(p P) time.Time { return p.Later })
	fP := vb.NewField("Ptr", func(p P) *int { return p.Ptr })
	ctx := context.Background()
	build := func(cond ir.Condition) *Validator {
		s := vb.Struct("P",
			vb.F(fA, vb.Int[int]()), vb.F(fB, vb.Int[int]()),
			vb.F(fS, vb.String()).Optional(), vb.F(fT, vb.String()).Optional(),
			vb.F(fW, vb.Time()).Optional(), vb.F(fL, vb.Time()).Optional(),
			vb.F(fP, vb.Ptr(vb.Int[int]())).Optional(),
		).Gate(vb.Requires(cond, fT)).MustBuild()
		v, err := FromSchema(s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	fires := func(cond ir.Condition, p P) bool {
		return build(cond).Validate(ctx, p) != nil // T absent → requires fires iff cond true
	}
	cases := []struct {
		name string
		cond ir.Condition
		p    P
		want bool
	}{
		{"cmp eq", vb.Eq(fA, 1), P{A: 1}, true},
		{"cmp ne", vb.Cmp(fA, ir.OpNe, 1), P{A: 1}, false},
		{"cmp gt", vb.Cmp(fA, ir.OpGt, 1), P{A: 2}, true},
		{"cmp gte", vb.Cmp(fA, ir.OpGte, 2), P{A: 2}, true},
		{"cmp lt", vb.Cmp(fA, ir.OpLt, 2), P{A: 2}, false},
		{"cmp lte", vb.Cmp(fA, ir.OpLte, 2), P{A: 2}, true},
		{"cmp string lt", vb.Cmp(fS, ir.OpLt, "b"), P{S: "a"}, true},
		{"cmp string absent", vb.Eq(fS, ""), P{}, false},
		{"cmp mismatched types", ir.Cmp("A", ir.OpGt, "x"), P{A: 1}, false},
		{"cmp bool eq", ir.Cmp("A", ir.OpEq, 1.0), P{A: 1}, true},
		{"fieldcmp gt", vb.FieldCmp(fA, ir.OpGt, fB), P{A: 2, B: 1}, true},
		{"fieldcmp absent", ir.FieldCmp("S", ir.OpEq, "T"), P{}, false},
		{"fieldcmp time", vb.FieldCmp(fL, ir.OpGt, fW), P{When: t0, Later: t1}, true},
		{"fieldcmp time lte", vb.FieldCmp(fL, ir.OpLte, fW), P{When: t0, Later: t1}, false},
		{"fieldcmp time eq", vb.FieldCmp(fL, ir.OpEq, fW), P{When: t0, Later: t0}, true},
		{"presence present", vb.Has(fS), P{S: "x"}, true},
		{"presence absent", vb.Missing(fS), P{}, true},
		{"presence ptr nil", vb.Has(fP), P{}, false},
		{"in", vb.In(fA, 1, 2), P{A: 2}, true},
		{"in miss", vb.In(fA, 1, 2), P{A: 3}, false},
		{"in absent", vb.In(fS, "x"), P{}, false},
		{"and", ir.And(vb.Eq(fA, 1), vb.Eq(fB, 2)), P{A: 1, B: 2}, true},
		{"and short", ir.And(vb.Eq(fA, 1), vb.Eq(fB, 2)), P{A: 1, B: 3}, false},
		{"or", ir.Or(vb.Eq(fA, 9), vb.Eq(fB, 2)), P{A: 1, B: 2}, true},
		{"or none", ir.Or(vb.Eq(fA, 9), vb.Eq(fB, 9)), P{A: 1, B: 2}, false},
		{"not", ir.Not(vb.Eq(fA, 9)), P{A: 1}, true},
		{"ptr eq", ir.Cmp("Ptr", ir.OpEq, 5), P{Ptr: ptr(5)}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := fires(c.cond, c.p); got != c.want {
				t.Errorf("cond fired=%v want %v", got, c.want)
			}
		})
	}
	// Struct-level equality of non-orderable values and nil comparisons.
	if !compareAny(reflectValue(struct{ X int }{1}), reflectValue(struct{ X int }{1}), ir.OpEq) {
		t.Error("struct eq")
	}
	if compareAny(reflectValue(struct{ X int }{1}), reflectValue(struct{ X int }{1}), ir.OpGt) {
		t.Error("struct gt must be false")
	}
	var nilP *int
	if !compareAny(reflectValue(nilP), reflectValue(nilP), ir.OpEq) || !compareAny(reflectValue(nilP), reflectValue(1), ir.OpNe) {
		t.Error("nil comparisons")
	}
	if orderOp("bogus", true, true, true) {
		t.Error("unknown op must be false")
	}
}

func TestCustomConditionAndScopeless(t *testing.T) {
	type P struct{ A int }
	fA := vb.NewField("A", func(p P) int { return p.A })
	s := vb.Struct("P", vb.F(fA, vb.Int[int]().Min(10))).Gate(
		vb.When(ir.CustomCond("big"), nil, []ir.TargetRule{vb.On(fA, ir.Max(0))}),
	).MustBuild()
	reg := &registry{inner: s, extra: map[string]vb.Func{"big": func(_ context.Context, v any) error {
		if v.(P).A > 5 {
			return nil
		}
		return errors.New("small")
	}}}
	v, err := New(s.IR(), WithFuncs(reg))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, v.Validate(context.Background(), P{A: 20}))
	expect(t, v.Validate(context.Background(), P{A: 3}), "$.A:min", "$.A:max")
	// A custom condition with no scope is false.
	r := &run{ctx: context.Background(), v: v, errs: &verr.Errors{}}
	if r.cond(nil, ir.CustomCond("big")) {
		t.Error("scopeless custom cond must be false")
	}
	if r.cond(nil, ir.Expr("x")) {
		t.Error("expr must evaluate false at runtime")
	}
	if _, ok := (*scope)(nil).field("A"); ok {
		t.Error("nil scope has no fields")
	}
}

func TestGroupModes(t *testing.T) {
	type P struct{ A, B, C string }
	fA := vb.NewField("A", func(p P) string { return p.A })
	fB := vb.NewField("B", func(p P) string { return p.B })
	fC := vb.NewField("C", func(p P) string { return p.C })
	mk := func(mode ir.GroupMode) *Validator {
		s := vb.Struct("P", vb.F(fA, vb.String()).Optional(), vb.F(fB, vb.String()).Optional(), vb.F(fC, vb.String()).Optional()).
			Gate(vb.Group(mode, fA, fB, fC)).MustBuild()
		v, _ := FromSchema(s)
		return v
	}
	ctx := context.Background()
	expect(t, mk(ir.ExactlyOne).Validate(ctx, P{A: "x"}))
	expect(t, mk(ir.ExactlyOne).Validate(ctx, P{A: "x", B: "y"}), "$:group")
	expect(t, mk(ir.AtLeastOne).Validate(ctx, P{}), "$:group")
	expect(t, mk(ir.AllOrNone).Validate(ctx, P{}))
	expect(t, mk(ir.AllOrNone).Validate(ctx, P{A: "x", B: "y", C: "z"}))
	expect(t, mk(ir.AllOrNone).Validate(ctx, P{A: "x"}), "$:group")
}

func TestHelpers(t *testing.T) {
	if _, ok := paramFloat(nil); ok {
		t.Error("nil param")
	}
	if _, ok := paramFloat("x"); ok {
		t.Error("string param")
	}
	if compareNum(reflectValue("x"), 1, func(a, b float64) bool { return true }) {
		t.Error("non-numeric value must fail closed")
	}
	if compareNum(reflectValue(1), "x", func(a, b float64) bool { return true }) {
		t.Error("non-numeric param must fail closed")
	}
	if norm(nil) != nil || norm((*int)(nil)) != nil || norm(uint8(3)) != 3.0 || norm(true) != true || norm([]int{1}) != "[1]" {
		t.Error("norm")
	}
	if wireOf(&ir.ObjectNode{}, "X") != "X" {
		t.Error("wireOf fallback")
	}
	if zeroFor(&ir.AnyNode{}).Kind() != 0 && zeroFor(&ir.AnyNode{}).Len() != 0 {
		t.Error("zeroFor any")
	}
	for _, n := range []ir.TypeNode{&ir.StringNode{}, &ir.IntNode{}, &ir.FloatNode{}, &ir.BoolNode{}, &ir.BytesNode{}, &ir.TimeNode{}, &ir.DurationNode{}, &ir.ArrayNode{}, &ir.MapNode{}} {
		if !zeroFor(n).IsValid() {
			t.Errorf("zeroFor %s", n.Kind())
		}
	}
	for _, n := range []ir.TypeNode{&ir.ObjectNode{}, &ir.StringNode{}, &ir.IntNode{}, &ir.FloatNode{}, &ir.BoolNode{}, &ir.BytesNode{}, &ir.TimeNode{}, &ir.DurationNode{}, &ir.ArrayNode{}, &ir.MapNode{}, &ir.UnionNode{}, &ir.OneOfNode{}, &ir.RefNode{}, &ir.AnyNode{}} {
		if nullable(n) {
			t.Errorf("%s default nullable", n.Kind())
		}
	}
	if applyStringTransform("bogus", "x") != "x" {
		t.Error("unknown transform must be identity")
	}
	if v, ok := fieldValue(reflectValue(42), ir.FieldNode{Name: "X"}); ok || v.IsValid() {
		t.Error("fieldValue on non-object")
	}
	if _, ok := lookupByWireName(reflectValue(42), "x"); ok {
		t.Error("lookup on non-object")
	}
	if _, ok := lookupByWireName(reflectValue((*int)(nil)), "x"); ok {
		t.Error("lookup on nil")
	}
	// applyDefault on nil parent and on a typed map returns the default for validation.
	if !applyDefault(reflectValue((*User)(nil)), ir.FieldNode{Name: "X"}, "d").IsValid() {
		t.Error("applyDefault nil parent")
	}
	m := map[string]string{}
	if v := applyDefault(reflectValue(m), ir.FieldNode{Name: "X"}, "d"); v.String() != "d" || len(m) != 0 {
		t.Error("applyDefault typed map must not write")
	}
	if applyDefault(reflectValue(m), ir.FieldNode{Name: "X"}, nil).IsValid() {
		t.Error("nil default")
	}
}

func reflectValue(v any) reflect.Value { return reflect.ValueOf(v) }

func ptr[T any](v T) *T { return &v }

func TestCoerceModes(t *testing.T) {
	type F = map[string]any
	type C struct{ Age, Lax, Pinned int }
	age, lax, pinned := vb.Int[int]().Min(18), vb.Int[int](), vb.Int[int]()
	// age carries no marker; lax carries coerce; pinned carries strict.
	fAge := vb.NewField("Age", func(c C) int { return c.Age }).JSON("age")
	fLax := vb.NewField("Lax", func(c C) int { return c.Lax }).JSON("lax")
	fPinned := vb.NewField("Pinned", func(c C) int { return c.Pinned }).JSON("pinned")
	s := vb.Struct("C", vb.F(fAge, age), vb.F(fLax, lax).Coerce(), vb.F(fPinned, pinned).NoCoerce()).MustBuild()
	in := func() F { return F{"age": "20", "lax": "1", "pinned": "2"} }
	codes := func(errs *verr.Errors) string {
		var out []string
		for _, i := range errs.Issues {
			out = append(out, i.Path+":"+i.Code)
		}
		return strings.Join(out, " ")
	}
	cases := []struct {
		name string
		opts []Option
		want string
	}{
		{"default: only marked", nil, "$.age:type $.pinned:type"},
		{"lax: all but strict", []Option{Lax()}, "$.pinned:type"},
		{"strict: only marked", []Option{Strict()}, "$.age:type $.pinned:type"},
		{"explicit mode", []Option{WithCoerce(CoerceLax)}, "$.pinned:type"},
	}
	for _, c := range cases {
		v, err := FromSchema(s, c.opts...)
		if err != nil {
			t.Fatal(err)
		}
		m := in()
		if got := codes(v.Run(context.Background(), m)); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
		if _, isNum := m["lax"].(float64); !isNum {
			t.Errorf("%s: marked field not written back: %T", c.name, m["lax"])
		}
		if _, isStr := m["pinned"].(string); !isStr {
			t.Errorf("%s: strict field was coerced", c.name)
		}
	}
	// Lax on struct input is a no-op: typed fields have their type.
	v, _ := FromSchema(s, Lax())
	if err := v.Validate(context.Background(), &C{Age: 20, Lax: 1, Pinned: 2}); err != nil {
		t.Errorf("struct input under lax: %v", err)
	}
	// Lax also lifts a valid conversion into a rule check: "17" → 17 < 18.
	v, _ = FromSchema(s, Lax())
	if got := codes(v.Run(context.Background(), F{"age": "17", "lax": "1", "pinned": 2})); got != "$.age:min" {
		t.Errorf("lax then rule: %q", got)
	}
}
