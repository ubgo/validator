package rt

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/vb"
	"github.com/ubgo/validator/verr"
)

type reg map[string]vb.Func

func (r reg) Func(name string) (vb.Func, bool) { f, ok := r[name]; return f, ok }

func TestIssues(t *testing.T) {
	errs := &verr.Errors{}
	Fail(errs, "min", "$.a", "at least {value}", "min", map[string]any{"value": 3})
	Required(errs, "$.b")
	Null(errs, "$.c")
	Group(errs, "$", ir.ExactlyOne, []string{"x", "y"}, 2)
	want := []string{"$.a: at least 3", "$.b: field is required", "$.c: value is required", "$: exactly-one of [x y] required, 2 present"}
	for i, w := range want {
		if errs.Issues[i].Error() != w {
			t.Errorf("issue %d = %q want %q", i, errs.Issues[i].Error(), w)
		}
	}
	if errs.Issues[0].Rule != "min" || errs.Issues[3].Code != verr.CodeGroup || errs.Issues[3].Params["present"] != 2 {
		t.Errorf("fields %+v", errs.Issues)
	}
}

func TestCallAndCond(t *testing.T) {
	ctx := context.Background()
	r := reg{"ok": func(context.Context, any) error { return nil }, "bad": func(context.Context, any) error { return errors.New("no") }}
	if err := Call(ctx, nil, "ok", 1); !errors.Is(err, ErrNoRegistry) {
		t.Errorf("nil registry: %v", err)
	}
	var unreg *ErrUnregistered
	if err := Call(ctx, r, "missing", 1); !errors.As(err, &unreg) || unreg.Name != "missing" || err.Error() != "rt: rule missing is not registered" {
		t.Errorf("unregistered: %v", err)
	}
	if Call(ctx, r, "ok", 1) != nil || Call(ctx, r, "bad", 1) == nil {
		t.Error("Call")
	}
	if !Cond(ctx, r, "ok", 1) || Cond(ctx, r, "bad", 1) || Cond(ctx, r, "missing", 1) || Cond(ctx, nil, "ok", 1) {
		t.Error("Cond")
	}
}

func TestPresent(t *testing.T) {
	var np *int
	var ns []int
	var nm map[string]int
	x := 1
	cases := []struct {
		in   any
		want bool
	}{
		{nil, false}, {np, false}, {ns, false}, {nm, false}, {"", false}, {time.Time{}, false},
		{&x, true}, {[]int{}, true}, {map[string]int{}, true}, {"a", true}, {0, true}, {false, true}, {time.Now(), true}, {struct{}{}, true},
	}
	for _, c := range cases {
		if got := Present(c.in); got != c.want {
			t.Errorf("Present(%v) = %v want %v", c.in, got, c.want)
		}
	}
}

func TestDelegates(t *testing.T) {
	if !Email("a@b.io") || !URL("https://x.io") || !UUID("123e4567-e89b-12d3-a456-426614174000") || !ULID("01ARZ3NDEKTSV4RRFFQ69G5FAV") {
		t.Error("formats")
	}
	if !MultipleOf(0.3, 0.1) || MultipleOf(1, 0) || RuneLen("héé") != 3 {
		t.Error("numeric")
	}
	if !Unique([]int{1, 2}) || Unique([]int{1, 1}) || !In(2, []any{1.0, 2.0}) || !Eq(int8(2), 2.0) || Eq("2", 2) {
		t.Error("set ops")
	}
	if !Compare(2, 1, ir.OpGt) || Compare("a", 1, ir.OpGt) || !Compare(time.Time{}, time.Time{}, ir.OpEq) {
		t.Error("Compare")
	}
	if itoa(0) != "0" || itoa(42) != "42" || itoa(-7) != "-7" || sprintFields(nil) != "[]" {
		t.Error("formatting helpers")
	}
}

func TestPath(t *testing.T) {
	var root Path
	if root.String() != "$" || (*Path)(nil).String() != "$" {
		t.Error("root")
	}
	f := root.Field(".tags")
	i := f.Index(2)
	k := i.KeyStr("name")
	n := k.KeyInt(7)
	x := n.Key(2.5)
	if got := x.String(); got != "$.tags[2].name.7.2.5" {
		t.Errorf("chain %q", got)
	}
	if f.String() != "$.tags" || i.String() != "$.tags[2]" {
		t.Error("intermediate")
	}
}

func TestUniqueOf(t *testing.T) {
	if !UniqueOf([]string{"a", "b"}) || UniqueOf([]int{1, 1}) || !UniqueOf([]float64{}) || !UniqueOf([]bool{true}) {
		t.Error("UniqueOf")
	}
	// Above the small-slice threshold the set path is used.
	big := make([]int, smallUnique+4)
	for i := range big {
		big[i] = i
	}
	if !UniqueOf(big) {
		t.Error("large unique")
	}
	big[len(big)-1] = 0
	if UniqueOf(big) {
		t.Error("large duplicate")
	}
}

type addr struct{ S string }

func TestProbesAndHelpers(t *testing.T) {
	if s, ok := AsString("x"); !ok || s != "x" {
		t.Error("AsString")
	}
	if _, ok := AsString(1); ok {
		t.Error("AsString non-string")
	}
	if f, ok := AsIntegral(int8(3)); !ok || f != 3 {
		t.Error("AsIntegral int")
	}
	if f, ok := AsIntegral(uint16(4)); !ok || f != 4 {
		t.Error("AsIntegral uint")
	}
	if f, ok := AsIntegral(5.0); !ok || f != 5 {
		t.Error("AsIntegral whole float")
	}
	if _, ok := AsIntegral(5.5); ok {
		t.Error("AsIntegral fraction")
	}
	if _, ok := AsIntegral("5"); ok {
		t.Error("AsIntegral string")
	}
	if f, ok := AsNumber(2.5); !ok || f != 2.5 {
		t.Error("AsNumber float")
	}
	if f, ok := AsNumber(uint(2)); !ok || f != 2 {
		t.Error("AsNumber uint")
	}
	if f, ok := AsNumber(int(2)); !ok || f != 2 {
		t.Error("AsNumber int")
	}
	if _, ok := AsNumber(true); ok {
		t.Error("AsNumber bool")
	}
	if p, ok := AsStruct[addr](addr{"v"}); !ok || p.S != "v" {
		t.Error("AsStruct value")
	}
	if p, ok := AsStruct[addr](&addr{"p"}); !ok || p.S != "p" {
		t.Error("AsStruct pointer")
	}
	if _, ok := AsStruct[addr]((*addr)(nil)); ok {
		t.Error("AsStruct nil pointer")
	}
	if _, ok := AsStruct[addr](1); ok {
		t.Error("AsStruct other")
	}
	errs := &verr.Errors{}
	Union(errs, "$.a", 2)
	Variant(errs, "$.b", "kind", "x")
	if errs.Issues[0].Code != verr.CodeUnion || errs.Issues[0].Params["variants"] != 2 {
		t.Errorf("Union %+v", errs.Issues[0])
	}
	if errs.Issues[1].Code != verr.CodeVariant || errs.Issues[1].Message != `unknown variant "x"` || errs.Issues[1].Params["discriminator"] != "kind" {
		t.Errorf("Variant %+v", errs.Issues[1])
	}
	if Trim("  a ") != "a" || Lower("A") != "a" || Upper("a") != "A" || *Ptr(3) != 3 || NormalizeEmail(" A@B.IO ") != "a@b.io" {
		t.Error("string helpers")
	}
}

func TestFormatDelegates(t *testing.T) {
	if !IPv4("1.2.3.4") || !IPv6("::1") || !IP("1.2.3.4") || !CIDR("1.2.3.0/24") || !MAC("00:11:22:33:44:55") || !Hostname("a.io") ||
		!Base64("YQ==") || !JSON("[]") || !Semver("1.2.3") || !Date("2026-01-02") || !DateTime("2026-01-02T00:00:00Z") || !NotBlank("x") ||
		!CreditCard("4242424242424242") || !JWT("a.b.c") || !CountryCode("US") || !CurrencyCode("USD") || !HexColor("#fff") || !E164("+123") || !Latitude(0) || !Longitude(0) {
		t.Error("format delegates")
	}
}

func TestInterpretFallback(t *testing.T) {
	doc := `{"version":"ubgo.validator.ir/v1","root":{"kind":"string","rules":[{"kind":"minLength","params":{"value":2}}]}}`
	s := MustSchema(doc)
	errs := &verr.Errors{}
	p := (&Path{}).Field(".name")
	Interpret(context.Background(), nil, s, "a", &p, errs)
	if errs.Len() != 1 || errs.Issues[0].Path != "$.name" || errs.Issues[0].Code != "min_length" {
		t.Errorf("fallback issues %+v", errs.Issues)
	}
	// Cached: a second call reuses the compiled interpreter.
	Interpret(context.Background(), nil, s, "ab", &p, errs)
	if errs.Len() != 1 {
		t.Error("valid value added an issue")
	}
	// A schema the interpreter refuses (custom rule, no registry) is reported.
	custom := MustSchema(`{"version":"ubgo.validator.ir/v1","root":{"kind":"string","rules":[{"kind":"custom","params":{"ref":"f"},"portable":"opaque"}]}}`)
	errs = &verr.Errors{}
	Interpret(context.Background(), nil, custom, "a", &p, errs)
	if errs.Len() != 1 || !strings.Contains(errs.Issues[0].Message, "fallback unavailable") {
		t.Errorf("refusal must be reported: %+v", errs.Issues)
	}
	// With a registry the same schema runs; a map-typed registry is not
	// cacheable and still works.
	errs = &verr.Errors{}
	r := reg{"f": func(context.Context, any) error { return errors.New("no") }}
	if cacheable(r) {
		t.Error("map registry must not be cacheable")
	}
	Interpret(context.Background(), r, custom, "a", &p, errs)
	Interpret(context.Background(), r, custom, "a", &p, errs)
	if errs.Len() != 2 || errs.Issues[0].Code != "custom" {
		t.Errorf("registry-backed fallback: %+v", errs.Issues)
	}
	if !cacheable(nil) || !cacheable(&ptrReg{}) {
		t.Error("nil and pointer registries are cacheable")
	}
	for _, bad := range []string{`{`, `{"version":"v0","root":{"kind":"any"}}`} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("MustSchema must panic on %s", bad)
				}
			}()
			MustSchema(bad)
		}()
	}
}

type ptrReg struct{}

func (*ptrReg) Func(string) (vb.Func, bool) { return nil, false }
