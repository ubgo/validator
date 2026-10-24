package example_test

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/ubgo/validator/cel"
	"github.com/ubgo/validator/cel/internal/example"
	"github.com/ubgo/validator/codegen/rt"
	"github.com/ubgo/validator/interp"
	"github.com/ubgo/validator/vb"
	"github.com/ubgo/validator/verr"
)

// Registry serves the schema's names and, through ExprCompiler, the CEL
// engine the generated code needs when it hands an expression gate back to
// the interpreter. Without this method the delegation would fail at
// runtime, which is why the "no engine" case below is also tested.
type Registry struct{}

func (Registry) Func(name string) (vb.Func, bool) { return example.Schema.Func(name) }
func (Registry) TypedFunc(name string) (any, bool) {
	return example.Schema.TypedFunc(name)
}
func (Registry) ExprCompiler() interp.ExprCompiler { return cel.New() }

// noExpr is a registry that does NOT supply an engine.
type noExpr struct{}

func (noExpr) Func(name string) (vb.Func, bool) { return example.Schema.Func(name) }

var jan1 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func keys(e *verr.Errors) []string {
	out := make([]string, 0, e.Len())
	for _, is := range e.Issues {
		p, _ := json.Marshal(is.Params)
		out = append(out, is.Path+"|"+is.Code+"|"+is.Rule+"|"+is.Message+"|"+string(p))
	}
	sort.Strings(out)
	return out
}

func cases() []example.Stay {
	return []example.Stay{
		// Short stay, small party: both gates quiet.
		{Start: jan1, End: jan1.AddDate(0, 0, 2), Guests: 2},
		// Over a week, no note: the first gate fires.
		{Start: jan1, End: jan1.AddDate(0, 0, 10), Guests: 2},
		// Over a week, with a note: quiet again.
		{Start: jan1, End: jan1.AddDate(0, 0, 10), Guests: 2, Note: "long stay"},
		// Big party, short note: the second gate's length rule fires.
		{Start: jan1, End: jan1.AddDate(0, 0, 2), Guests: 6, Note: "big"},
		// Big party, long note: quiet.
		{Start: jan1, End: jan1.AddDate(0, 0, 2), Guests: 6, Note: "a party of six"},
		// Both gates at once, and a field rule failing too.
		{Start: jan1, End: jan1.AddDate(0, 0, 30), Guests: 0},
		// Reversed dates: the span is negative, so neither gate fires.
		{Start: jan1.AddDate(0, 0, 10), End: jan1, Guests: 2},
	}
}

// TestExecutorsAgree is the invariant: whatever route CEL takes, the two
// executors report the same issues for the same value.
func TestExecutorsAgree(t *testing.T) {
	v, err := interp.FromSchema(example.Schema, interp.WithExpr(cel.New()))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i, in := range cases() {
		a, b := in, in
		got := keys(example.ValidateStay(ctx, &a, Registry{}))
		want := keys(v.Run(ctx, &b))
		if len(got) != len(want) {
			t.Errorf("case %d: generated %v, interpreter %v", i, got, want)
			continue
		}
		for j := range got {
			if got[j] != want[j] {
				t.Errorf("case %d issue %d:\n generated  %s\n interpreter %s", i, j, got[j], want[j])
			}
		}
	}
}

// TestExpressionsActuallyFire guards against the whole suite passing
// because neither executor evaluates anything.
func TestExpressionsActuallyFire(t *testing.T) {
	ctx := context.Background()
	long := example.Stay{Start: jan1, End: jan1.AddDate(0, 0, 10), Guests: 2}
	errs := example.ValidateStay(ctx, &long, Registry{})
	if errs.Len() == 0 {
		t.Fatal("a ten-night stay with no note should fail the span gate")
	}
	if errs.Issues[0].Path != "$.note" || errs.Issues[0].Code != verr.CodeRequired {
		t.Errorf("unexpected issue: %+v", errs.Issues[0])
	}
	short := example.Stay{Start: jan1, End: jan1.AddDate(0, 0, 2), Guests: 2}
	if e := example.ValidateStay(ctx, &short, Registry{}); e.Len() != 0 {
		t.Errorf("a two-night stay should pass: %+v", e.Issues)
	}
}

// TestWithoutAnEngineTheDelegationReports checks the failure is loud. A
// registry with no ExprCompiler cannot evaluate the gate, and the runtime
// must say so rather than treat the gate as satisfied.
func TestWithoutAnEngineTheDelegationReports(t *testing.T) {
	long := example.Stay{Start: jan1, End: jan1.AddDate(0, 0, 10), Guests: 2}
	errs := example.ValidateStay(context.Background(), &long, noExpr{})
	if errs.Len() == 0 {
		t.Fatal("a missing engine must be reported, not silently skipped")
	}
	if errs.Issues[0].Code != verr.CodeCustom {
		t.Errorf("want a custom-coded failure, got %+v", errs.Issues[0])
	}
}

// TestGeneratedDelegates pins that the generated file really does hand the
// expression gates to the interpreter, so the agreement above is not
// coincidence.
func TestGeneratedDelegates(t *testing.T) {
	var _ rt.Registry = Registry{}
	var _ rt.ExprRegistry = Registry{}
}
