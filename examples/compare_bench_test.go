package examples

// A benchmark against the incumbent, go-playground/validator, so the
// project's performance claim is measured against something other than
// itself. It lives in this module because the root library has no
// third-party dependencies.
//
// Fairness rules this file follows, because a benchmark that flatters the
// author is worse than none:
//
//   - One struct, one set of rules, expressed twice: as go-playground tags
//     and as a vb schema, both in the benchmodel package.
//     TestBenchmarkRulesAreEquivalent asserts the two agree on the valid
//     fixture and fail the same fields on the invalid one, so neither side
//     is quietly doing less work.
//   - go-playground caches struct metadata on first use, so every benchmark
//     validates once before the timed loop. Ours compiles at FromSchema,
//     which is also outside the loop.
//   - Both the valid (success) and invalid (failure) paths are measured.
//     Reporting only the success path would hide error-construction cost,
//     and the failure path is where this library is currently weakest.
//   - Each library is used the way its own documentation recommends: one
//     shared, concurrency-safe validator built once.
//
// Run: go test -run xxx -bench BenchmarkCompare -benchmem ./examples/

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	playground "github.com/go-playground/validator/v10"

	"github.com/ubgo/validator/examples/benchmodel"
	"github.com/ubgo/validator/interp"
	"github.com/ubgo/validator/verr"
)

func newPlayground() *playground.Validate {
	v := playground.New(playground.WithRequiredStructEnabled())
	// Match our wire names so both libraries report the same field labels,
	// which is what makes the equivalence check below comparable.
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		name := strings.SplitN(f.Tag.Get("json"), ",", 2)[0]
		if name == "" || name == "-" {
			return f.Name
		}
		return name
	})
	return v
}

func benchInterp(tb testing.TB) *interp.Validator {
	tb.Helper()
	v, err := interp.FromSchema(benchmodel.Schema)
	if err != nil {
		tb.Fatal(err)
	}
	return v
}

// TestBenchmarkRulesAreEquivalent is the fairness check: the two rule sets
// must agree on both fixtures, and on which fields fail. Without it the
// benchmark could quietly compare a heavy rule set against a light one.
func TestBenchmarkRulesAreEquivalent(t *testing.T) {
	pg := newPlayground()
	ours := benchInterp(t)
	ctx := context.Background()

	valid := benchmodel.Valid()
	if err := pg.Struct(valid); err != nil {
		t.Fatalf("go-playground rejected the valid fixture: %v", err)
	}
	if err := ours.Validate(ctx, &valid); err != nil {
		t.Fatalf("ubgo rejected the valid fixture: %v", err)
	}
	if errs := benchmodel.ValidateBenchUser(ctx, &valid, benchmodel.Schema); errs.Len() != 0 {
		t.Fatalf("generated code rejected the valid fixture: %v", errs.Issues)
	}

	invalid := benchmodel.Invalid()
	pgFields := map[string]bool{}
	var pgErrs playground.ValidationErrors
	if errors.As(pg.Struct(invalid), &pgErrs) {
		for _, fe := range pgErrs {
			pgFields[fe.Field()] = true
		}
	}
	fieldsOf := func(errs *verr.Errors) map[string]bool {
		out := map[string]bool{}
		for _, is := range errs.Issues {
			out[strings.TrimPrefix(is.Path, "$.")] = true
		}
		return out
	}
	ourFields := fieldsOf(ours.Run(ctx, &invalid))
	genFields := fieldsOf(benchmodel.ValidateBenchUser(ctx, &invalid, benchmodel.Schema))

	if len(pgFields) == 0 || len(ourFields) == 0 {
		t.Fatalf("a library accepted the invalid fixture: playground=%v ubgo=%v", pgFields, ourFields)
	}
	for f := range pgFields {
		if !ourFields[f] {
			t.Errorf("go-playground fails %q, ubgo does not: rule sets are not equivalent", f)
		}
	}
	for f := range ourFields {
		if !pgFields[f] {
			t.Errorf("ubgo fails %q, go-playground does not: rule sets are not equivalent", f)
		}
	}
	// Both of our executors must agree with each other too, on the very
	// fixture the benchmark reports numbers for.
	for f := range ourFields {
		if !genFields[f] {
			t.Errorf("interpreter fails %q, generated code does not", f)
		}
	}
	for f := range genFields {
		if !ourFields[f] {
			t.Errorf("generated code fails %q, interpreter does not", f)
		}
	}
}

// ---- success path ----------------------------------------------------------

func BenchmarkCompareValid_GoPlayground(b *testing.B) {
	v, u := newPlayground(), benchmodel.Valid()
	_ = v.Struct(u) // warm the struct cache
	b.ReportAllocs()
	for b.Loop() {
		_ = v.Struct(u)
	}
}

func BenchmarkCompareValid_UbgoInterp(b *testing.B) {
	v, ctx, u := benchInterp(b), context.Background(), benchmodel.Valid()
	_ = v.Validate(ctx, &u)
	b.ReportAllocs()
	for b.Loop() {
		_ = v.Validate(ctx, &u)
	}
}

func BenchmarkCompareValid_UbgoCodegen(b *testing.B) {
	ctx, u := context.Background(), benchmodel.Valid()
	b.ReportAllocs()
	for b.Loop() {
		_ = benchmodel.ValidateBenchUser(ctx, &u, benchmodel.Schema)
	}
}

func BenchmarkCompareValid_UbgoCodegenInto(b *testing.B) {
	ctx, u := context.Background(), benchmodel.Valid()
	errs := &verr.Errors{}
	b.ReportAllocs()
	for b.Loop() {
		errs.Issues = errs.Issues[:0]
		benchmodel.ValidateBenchUserInto(ctx, &u, benchmodel.Schema, errs)
	}
}

// ---- failure path ----------------------------------------------------------

func BenchmarkCompareInvalid_GoPlayground(b *testing.B) {
	v, u := newPlayground(), benchmodel.Invalid()
	_ = v.Struct(u)
	b.ReportAllocs()
	for b.Loop() {
		_ = v.Struct(u)
	}
}

func BenchmarkCompareInvalid_UbgoInterp(b *testing.B) {
	v, ctx, u := benchInterp(b), context.Background(), benchmodel.Invalid()
	_ = v.Validate(ctx, &u)
	b.ReportAllocs()
	for b.Loop() {
		_ = v.Validate(ctx, &u)
	}
}

func BenchmarkCompareInvalid_UbgoCodegen(b *testing.B) {
	ctx, u := context.Background(), benchmodel.Invalid()
	b.ReportAllocs()
	for b.Loop() {
		_ = benchmodel.ValidateBenchUser(ctx, &u, benchmodel.Schema)
	}
}

func BenchmarkCompareInvalid_UbgoCodegenInto(b *testing.B) {
	ctx, u := context.Background(), benchmodel.Invalid()
	errs := &verr.Errors{}
	b.ReportAllocs()
	for b.Loop() {
		errs.Issues = errs.Issues[:0]
		benchmodel.ValidateBenchUserInto(ctx, &u, benchmodel.Schema, errs)
	}
}
