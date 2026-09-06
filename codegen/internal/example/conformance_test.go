package example

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ubgo/validator/interp"
	"github.com/ubgo/validator/verr"
)

var (
	t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 = t0.Add(time.Hour)
)

func validOrder() Order {
	return Order{
		Email: "ada@example.com", Qty: 3, Weight: 2.5, Rush: false,
		Placed: t0, Window: time.Hour, Tags: []string{"a"},
		Billing: Address{Street: "Main", Zip: "12345"},
		Lines:   []Line{{Sku: "123e4567-e89b-12d3-a456-426614174000", Qty: 1}},
		Type:    "online", URL: "https://x.io",
		Nick: "ada", Event: Event{Kind: "online", URL: "https://e.io"},
		Pref: ptr("a"), Units: 1,
	}
}

// TestValidOrderIsValid pins the benchmark fixture: BenchmarkGenerated
// measures the success path only while this holds.
func TestValidOrderIsValid(t *testing.T) {
	o := validOrder()
	if got := key(ValidateOrder(context.Background(), &o, Registry{})); len(got) != 0 {
		t.Fatalf("validOrder is not valid: %v", got)
	}
}

// key renders issues in a sorted, comparable form. Order differs between
// executors only for map iteration, so sorting is the honest comparison.
func key(e *verr.Errors) []string {
	if e == nil {
		return nil
	}
	out := make([]string, 0, e.Len())
	for _, is := range e.Issues {
		p, _ := json.Marshal(is.Params)
		out = append(out, is.Path+"|"+is.Code+"|"+is.Rule+"|"+is.Message+"|"+string(p))
	}
	sort.Strings(out)
	return out
}

func TestConformance(t *testing.T) {
	v, err := interp.New(OrderSchema.IR(), interp.WithFuncs(Registry{}))
	if err != nil {
		t.Fatal(err)
	}
	ptr := func(s string) *string { return &s }
	mut := func(f func(*Order)) Order { o := validOrder(); f(&o); return o }
	cases := map[string]Order{
		"valid":              validOrder(),
		"zero":               {},
		"bad email":          mut(func(o *Order) { o.Email = "nope" }),
		"temp email":         mut(func(o *Order) { o.Email = "x@temp.com" }),
		"taken email":        mut(func(o *Order) { o.Email = "taken@example.com" }),
		"qty low":            mut(func(o *Order) { o.Qty = 0 }),
		"qty high":           mut(func(o *Order) { o.Qty = 101 }),
		"weight":             mut(func(o *Order) { o.Weight = 0 }),
		"weight high":        mut(func(o *Order) { o.Weight = 51 }),
		"note short":         mut(func(o *Order) { o.Note = ptr("x") }),
		"note long":          mut(func(o *Order) { o.Note = ptr("this is far too long") }),
		"note ok":            mut(func(o *Order) { o.Note = ptr("fine") }),
		"ship before":        mut(func(o *Order) { s := t0.Add(-time.Hour); o.Ship = &s }),
		"ship after":         mut(func(o *Order) { s := t1; o.Ship = &s }),
		"window":             mut(func(o *Order) { o.Window = 48 * time.Hour }),
		"tags":               mut(func(o *Order) { o.Tags = []string{"a", "a", "zz", "b"} }),
		"tags empty":         mut(func(o *Order) { o.Tags = []string{} }),
		"scores":             mut(func(o *Order) { o.Scores = []int{1, 1, 11} }),
		"meta":               mut(func(o *Order) { o.Meta = map[string]string{"toolong": "", "b": "x", "c": "y"} }),
		"billing":            mut(func(o *Order) { o.Billing = Address{Zip: "x"} }),
		"shipping":           mut(func(o *Order) { o.Shipping = &Address{Street: "s", Zip: "999"} }),
		"lines":              mut(func(o *Order) { o.Lines = []Line{{Sku: "bad", Qty: 0}} }),
		"lines empty":        mut(func(o *Order) { o.Lines = nil }),
		"raw":                mut(func(o *Order) { o.Raw = []byte("12345") }),
		"extra":              mut(func(o *Order) { o.Extra = 42 }),
		"coupon bad":         mut(func(o *Order) { o.Coupon = "nope" }),
		"coupon needs venue": mut(func(o *Order) { o.Coupon = "01ARZ3NDEKTSV4RRFFQ69G5FAV" }),
		"type":               mut(func(o *Order) { o.Type = "hybrid" }),
		"offline no venue":   mut(func(o *Order) { o.Type = "offline"; o.URL = "" }),
		"offline venue":      mut(func(o *Order) { o.Type = "offline"; o.URL = ""; o.Venue = "Hall" }),
		"url":                mut(func(o *Order) { o.URL = "not a url" }),
		"sku":                mut(func(o *Order) { o.Sku = "other" }),
		"big qty no rush":    mut(func(o *Order) { o.Qty = 60 }),
		"big qty rush":       mut(func(o *Order) { o.Qty = 60; o.Rush = true }),
		"vip":                mut(func(o *Order) { o.Email = "a@vip.example.com"; o.Qty = 20 }),
		"not vip":            mut(func(o *Order) { o.Qty = 20 }),
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			want := key(v.Run(context.Background(), &o))
			got := key(ValidateOrder(context.Background(), &o, Registry{}))
			if strings.Join(want, "\n") != strings.Join(got, "\n") {
				t.Fatalf("executors disagree\ninterp:\n  %s\ngenerated:\n  %s", strings.Join(want, "\n  "), strings.Join(got, "\n  "))
			}
			// Fail-fast: both report exactly the same FIRST issue (unsorted).
			ffi := &verr.Errors{FailFast: true}
			v.RunInto(context.Background(), &o, ffi)
			ffg := &verr.Errors{FailFast: true}
			ValidateOrderInto(context.Background(), &o, Registry{}, ffg)
			if ffi.Len() != ffg.Len() || (ffi.Len() == 1 && (ffi.Issues[0].Path != ffg.Issues[0].Path || ffi.Issues[0].Code != ffg.Issues[0].Code)) {
				t.Fatalf("fail-fast disagree: interp=%+v generated=%+v", ffi.Issues, ffg.Issues)
			}
			if ffi.Len() > 1 {
				t.Fatalf("fail-fast must hold at most one issue: %d", ffi.Len())
			}
		})
	}
	// Transforms write back through the pointer in both executors.
	o2 := validOrder()
	o2.Nick = "  ADA  "
	o2.Pref = ptr("b")
	_ = ValidateOrder(context.Background(), &o2, Registry{})
	if o2.Nick != "ada" || *o2.Pref != "B" {
		t.Errorf("generated transforms: nick=%q pref=%q", o2.Nick, *o2.Pref)
	}
	oc := validOrder()
	oc.Contact = " Ada@Example.COM "
	_ = ValidateOrder(context.Background(), &oc, Registry{})
	if oc.Contact != "ada@example.com" {
		t.Errorf("generated normalizeEmail: %q", oc.Contact)
	}
	o3 := validOrder()
	o3.Nick = ""
	_ = ValidateOrder(context.Background(), &o3, Registry{})
	if o3.Nick != "anon" || o3.Pref == nil || *o3.Pref != "A" {
		t.Errorf("generated defaults: nick=%q pref=%v", o3.Nick, o3.Pref)
	}
	// Nil input and nested nil pointer element.
	if got := ValidateOrder(context.Background(), nil, Registry{}); got.Len() != 1 || got.Issues[0].Code != verr.CodeRequired {
		t.Errorf("nil: %+v", got)
	}
	// Missing registry surfaces as a custom failure, never a pass.
	o := validOrder()
	if got := ValidateOrder(context.Background(), &o, nil); got.Len() == 0 || !strings.Contains(got.Issues[0].Message, "needs a registry") {
		t.Errorf("nil registry: %+v", got)
	}
}

func BenchmarkGeneratedFailFast(b *testing.B) {
	o := validOrder()
	o.Email = "nope"
	b.ReportAllocs()
	for b.Loop() {
		errs := verr.Errors{FailFast: true}
		ValidateOrderInto(context.Background(), &o, Registry{}, &errs)
	}
}

func BenchmarkGenerated(b *testing.B) {
	o := validOrder()
	b.ReportAllocs()
	for b.Loop() {
		_ = ValidateOrder(context.Background(), &o, Registry{})
	}
}

// BenchmarkGeneratedInto is the success path with a caller-owned issue
// list: the one allocation left in BenchmarkGenerated is the result
// object itself.
func BenchmarkGeneratedInto(b *testing.B) {
	o := validOrder()
	errs := &verr.Errors{}
	b.ReportAllocs()
	for b.Loop() {
		errs.Issues = errs.Issues[:0]
		ValidateOrderInto(context.Background(), &o, Registry{}, errs)
	}
}

func BenchmarkInterpreter(b *testing.B) {
	v, _ := interp.New(OrderSchema.IR(), interp.WithFuncs(Registry{}))
	o := validOrder()
	b.ReportAllocs()
	for b.Loop() {
		_ = v.Run(context.Background(), &o)
	}
}

func ptr[T any](v T) *T { return &v }
