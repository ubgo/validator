package cel_test

import (
	"context"
	"strings"
	"testing"
	"time"

	celgo "cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"

	"github.com/ubgo/validator/cel"
	"github.com/ubgo/validator/interp"
	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/vb"
	"github.com/ubgo/validator/verr"
)

// The point of this package is that expressions state what the typed
// condition vocabulary cannot: arithmetic and aggregation. So the tests
// are mostly end-to-end through the interpreter, on schemas whose rules
// could not be written any other way.

type Booking struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Nights int       `json:"nights"`
	Total  float64   `json:"total"`
	Note   string    `json:"note"`
}

var (
	fStart  = vb.NewField("Start", func(b Booking) time.Time { return b.Start }).JSON("start")
	fEnd    = vb.NewField("End", func(b Booking) time.Time { return b.End }).JSON("end")
	fNights = vb.NewField("Nights", func(b Booking) int { return b.Nights }).JSON("nights")
	fTotal  = vb.NewField("Total", func(b Booking) float64 { return b.Total }).JSON("total")
	fNote   = vb.NewField("Note", func(b Booking) string { return b.Note }).JSON("note")
)

// bookingWith builds the schema with one expression-gated rule: when the
// expression holds, Note is required.
func bookingWith(expr string) *vb.Schema[Booking] {
	return vb.Struct("Booking",
		vb.F(fStart, vb.Time()),
		vb.F(fEnd, vb.Time()),
		vb.F(fNights, vb.Int[int]()),
		vb.F(fTotal, vb.Float[float64]()),
		vb.F(fNote, vb.String()).Optional(),
	).Gate(
		vb.Requires(ir.Expr(expr), fNote),
	).MustBuild()
}

func run(t *testing.T, expr string, b Booking, opts ...cel.Option) *verr.Errors {
	t.Helper()
	v, err := interp.FromSchema(bookingWith(expr), interp.WithExpr(cel.New(opts...)))
	if err != nil {
		t.Fatalf("compiling %q: %v", expr, err)
	}
	return v.Run(context.Background(), &b)
}

// gated reports whether the expression held, by looking for the issue its
// gate produces when Note is absent.
func gated(errs *verr.Errors) bool {
	for _, is := range errs.Issues {
		if is.Path == "$.note" && is.Code == verr.CodeRequired {
			return true
		}
	}
	return false
}

func TestArithmeticAcrossFields(t *testing.T) {
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		expr string
		book Booking
		want bool
	}{
		{
			// The rule this whole package exists for: a span, not a
			// comparison. No typed condition can state it.
			name: "a stay longer than a week needs a note",
			expr: `end - start > duration('168h')`,
			book: Booking{Start: jan1, End: jan1.AddDate(0, 0, 10)},
			want: true,
		},
		{
			name: "a short stay does not",
			expr: `end - start > duration('168h')`,
			book: Booking{Start: jan1, End: jan1.AddDate(0, 0, 2)},
			want: false,
		},
		{
			name: "arithmetic on numbers",
			expr: `total / double(nights) > 200.0`,
			book: Booking{Start: jan1, End: jan1, Nights: 2, Total: 500},
			want: true,
		},
		{
			name: "arithmetic that does not trip",
			expr: `total / double(nights) > 200.0`,
			book: Booking{Start: jan1, End: jan1, Nights: 4, Total: 500},
			want: false,
		},
		{
			name: "combining fields and literals",
			expr: `nights > 3 && total > 100.0`,
			book: Booking{Start: jan1, End: jan1, Nights: 5, Total: 200},
			want: true,
		},
		{
			name: "timestamps compare as instants",
			expr: `end > start`,
			book: Booking{Start: jan1, End: jan1.AddDate(0, 0, 1)},
			want: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := gated(run(t, c.expr, c.book)); got != c.want {
				t.Errorf("expression %q held=%v, want %v", c.expr, got, c.want)
			}
		})
	}
}

func TestAbsentFieldIsNull(t *testing.T) {
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// An optional field the value does not carry must be testable rather
	// than an evaluation error that silently reads as false.
	errs := run(t, `note == null`, Booking{Start: jan1, End: jan1})
	if !gated(errs) {
		t.Errorf("`note == null` should hold when note is absent: %+v", errs.Issues)
	}
}

func TestCompileErrorsAreStartupErrors(t *testing.T) {
	cases := []struct {
		name, expr, want string
	}{
		{"syntax", `end >`, "cel:"},
		{"unknown field", `nope > 1`, "undeclared reference"},
		{"not boolean", `nights + 1`, "must be boolean"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := interp.FromSchema(bookingWith(c.expr), interp.WithExpr(cel.New()))
			if err == nil {
				t.Fatalf("expression %q was accepted", c.expr)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not mention %q", err, c.want)
			}
		})
	}
}

func TestEmptyExpressionIsRefusedByTheIR(t *testing.T) {
	// An empty expression never reaches this package: ir.Check rejects it,
	// which is the right layer for "this document is malformed".
	s := &ir.Schema{Version: ir.Version, Root: &ir.ObjectNode{
		Gated: []ir.GatedRules{{Kind: ir.GateWhen, Cond: &ir.Condition{Kind: ir.CondExpr}}},
	}}
	if err := ir.Check(s); err == nil {
		t.Error("an expression condition with no source was accepted")
	}
}

func TestWithoutTheEngineTheSchemaIsRefused(t *testing.T) {
	// The promise the rest of the library makes: what cannot be executed is
	// refused at startup, never skipped at request time.
	_, err := interp.FromSchema(bookingWith(`end > start`))
	if err == nil {
		t.Fatal("a schema with an expression was accepted without an engine")
	}
	if !strings.Contains(err.Error(), "WithExpr") {
		t.Errorf("the error should say how to fix it, got: %v", err)
	}
}

func TestMapInput(t *testing.T) {
	// On map input a time is an RFC 3339 string, so an author converts it.
	// The same schema, the same expression, a different input shape.
	v, err := interp.FromSchema(bookingWith(`timestamp(end) - timestamp(start) > duration('168h')`),
		interp.WithExpr(cel.New()))
	if err != nil {
		t.Fatal(err)
	}
	long := map[string]any{
		"start": "2026-01-01T00:00:00Z", "end": "2026-01-20T00:00:00Z",
		"nights": 19, "total": 100.0,
	}
	if !gated(v.Run(context.Background(), long)) {
		t.Error("the expression should hold for a 19-night stay")
	}
	short := map[string]any{
		"start": "2026-01-01T00:00:00Z", "end": "2026-01-02T00:00:00Z",
		"nights": 1, "total": 100.0, "note": "brief",
	}
	if errs := v.Run(context.Background(), short); errs.Len() != 0 {
		t.Errorf("short stay should pass: %+v", errs.Issues)
	}
}

func TestEvaluationErrorReadsAsFalse(t *testing.T) {
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Integer division by zero is an evaluation error, not a compile error.
	// A gate must not fire on a value whose condition could not be judged,
	// because that would report a violation the author never wrote.
	errs := run(t, `100 / nights > 1`, Booking{Start: jan1, End: jan1, Nights: 0})
	if gated(errs) {
		t.Errorf("a gate fired on an expression that could not be evaluated: %+v", errs.Issues)
	}
	// The same expression on a value it can judge still works, so the test
	// above is about the error and not about the expression being inert.
	if !gated(run(t, `100 / nights > 1`, Booking{Start: jan1, End: jan1, Nights: 2})) {
		t.Error("100 / 2 > 1 should hold")
	}
}

func TestDoubleDivisionIsNotAnError(t *testing.T) {
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Worth pinning because it surprises people: CEL follows IEEE 754 for
	// doubles, so dividing by zero yields infinity rather than failing.
	// An author who wants division by zero to be a failure must use
	// integer division or guard the denominator.
	if !gated(run(t, `total / double(nights) > 1.0`, Booking{Start: jan1, End: jan1, Nights: 0, Total: 5})) {
		t.Error("double division by zero should yield +Inf, which is > 1.0")
	}
}

func TestTimeoutIsEnforced(t *testing.T) {
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// A thousand iterations, and referencing a field so the optimizer
	// cannot fold it away. It runs in a few hundred microseconds, so a
	// nanosecond budget must stop it and a generous one must not.
	const expr = `[1,2,3,4,5,6,7,8,9,10].all(i, [1,2,3,4,5,6,7,8,9,10].all(j, ` +
		`[1,2,3,4,5,6,7,8,9,10].all(k, i * j * k >= nights)))`
	book := Booking{Start: jan1, End: jan1, Nights: 0}

	if gated(run(t, expr, book, cel.WithTimeout(time.Nanosecond))) {
		t.Error("an expression that ran out of time must not satisfy its gate")
	}
	if !gated(run(t, expr, book, cel.WithTimeout(time.Second))) {
		t.Error("the expression should hold when given time to finish")
	}
}

func TestWithEnvAddsFunctions(t *testing.T) {
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// A caller can extend the environment. The same expression must fail to
	// compile without the option and succeed with it, or the option is not
	// what is doing the work.
	const expr = `tripled(nights) > 6`

	if _, err := interp.FromSchema(bookingWith(expr), interp.WithExpr(cel.New())); err == nil {
		t.Fatal("an undeclared function compiled")
	}

	tripled := cel.WithEnv(celgo.Function("tripled",
		celgo.Overload("tripled_int", []*celgo.Type{celgo.IntType}, celgo.IntType,
			celgo.UnaryBinding(func(v ref.Val) ref.Val {
				n, ok := v.(types.Int)
				if !ok {
					return types.NewErr("tripled wants an int")
				}
				return types.Int(n * 3)
			}))))

	v, err := interp.FromSchema(bookingWith(expr), interp.WithExpr(cel.New(tripled)))
	if err != nil {
		t.Fatalf("the custom function did not compile: %v", err)
	}
	if !gated(v.Run(context.Background(), &Booking{Start: jan1, End: jan1, Nights: 3})) {
		t.Error("tripled(3) > 6 should hold")
	}
	if gated(v.Run(context.Background(), &Booking{Start: jan1, End: jan1, Nights: 2})) {
		t.Error("tripled(2) > 6 should not hold")
	}
}

func TestDefaultTimeoutIsPositive(t *testing.T) {
	if cel.DefaultTimeout <= 0 {
		t.Error("the default evaluation budget must be positive")
	}
}

func TestCompilerIsReusable(t *testing.T) {
	// One compiler, several schemas. Each expression gets its own
	// environment, so a variable declared for one schema must not be
	// visible to another.
	c := cel.New()
	if _, err := interp.FromSchema(bookingWith(`nights > 1`), interp.WithExpr(c)); err != nil {
		t.Fatal(err)
	}

	type Other struct {
		A    int    `json:"a"`
		Memo string `json:"memo"`
	}
	fA := vb.NewField("A", func(o Other) int { return o.A }).JSON("a")
	fMemo := vb.NewField("Memo", func(o Other) string { return o.Memo }).JSON("memo")
	otherWith := func(expr string) *ir.Schema {
		return vb.Struct("Other",
			vb.F(fA, vb.Int[int]()),
			vb.F(fMemo, vb.String()).Optional(),
		).Gate(vb.Requires(ir.Expr(expr), fMemo)).MustBuild().IR()
	}

	if _, err := interp.New(otherWith(`a > 0`), interp.WithExpr(c)); err != nil {
		t.Fatalf("a valid expression for Other was refused: %v", err)
	}
	// "nights" belongs to Booking, not Other.
	if _, err := interp.New(otherWith(`nights > 0`), interp.WithExpr(c)); err == nil {
		t.Error("a variable from another schema leaked into this environment")
	}
}

func TestConcurrentEvaluation(t *testing.T) {
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	v, err := interp.FromSchema(bookingWith(`end - start > duration('24h')`), interp.WithExpr(cel.New()))
	if err != nil {
		t.Fatal(err)
	}
	// A Validator is documented as safe for concurrent use, so a compiled
	// program must be too. Run under -race to mean anything.
	done := make(chan bool, 16)
	for i := range 16 {
		go func(i int) {
			b := Booking{Start: jan1, End: jan1.AddDate(0, 0, i)}
			done <- gated(v.Run(context.Background(), &b))
		}(i)
	}
	var held int
	for range 16 {
		if <-done {
			held++
		}
	}
	// Days 2..15 exceed 24h; days 0 and 1 do not.
	if held != 14 {
		t.Errorf("expression held %d times, want 14", held)
	}
}

func TestErrorsNameTheExpression(t *testing.T) {
	// A schema can hold several expressions; an error has to say which one.
	_, err := interp.FromSchema(bookingWith(`end >`), interp.WithExpr(cel.New()))
	if err == nil {
		t.Fatal("expected a compile error")
	}
	if !strings.Contains(err.Error(), "end >") {
		t.Errorf("the error should quote the expression, got: %v", err)
	}
}
