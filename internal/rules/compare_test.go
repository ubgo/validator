package rules

import (
	"reflect"
	"testing"
	"time"

	"github.com/ubgo/validator/ir"
)

func TestCompare(t *testing.T) {
	t0 := time.Unix(0, 0)
	t1 := t0.Add(time.Second)
	x := 5
	var nilP *int
	cases := []struct {
		name string
		a, b any
		op   ir.CmpOp
		want bool
	}{
		{"num gt", 2, 1, ir.OpGt, true},
		{"num gte eq", 2, 2.0, ir.OpGte, true},
		{"num lt", uint8(1), 2, ir.OpLt, true},
		{"num lte", 3, 2, ir.OpLte, false},
		{"num eq", int64(2), 2.0, ir.OpEq, true},
		{"num ne", 2, 3, ir.OpNe, true},
		{"num vs string", 2, "x", ir.OpGt, false},
		{"string lt", "a", "b", ir.OpLt, true},
		{"string gte", "b", "a", ir.OpGte, true},
		{"time before", t0, t1, ir.OpLt, true},
		{"time after", t1, t0, ir.OpGt, true},
		{"time eq", t0, t0, ir.OpEq, true},
		{"time lte", t1, t0, ir.OpLte, false},
		{"ptr deref", &x, 5, ir.OpEq, true},
		{"nil eq nil", nilP, nilP, ir.OpEq, true},
		{"nil ne value", nilP, 1, ir.OpNe, true},
		{"nil gt", nilP, 1, ir.OpGt, false},
		{"struct eq", struct{ A int }{1}, struct{ A int }{1}, ir.OpEq, true},
		{"struct ne", struct{ A int }{1}, struct{ A int }{2}, ir.OpNe, true},
		{"struct gt", struct{ A int }{1}, struct{ A int }{2}, ir.OpGt, false},
		{"invalid", nil, nil, ir.OpEq, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Compare(reflect.ValueOf(c.a), reflect.ValueOf(c.b), c.op); got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
	if OrderOp("bogus", true, true, true) {
		t.Error("unknown op must be false")
	}
	if _, ok := toFloat(reflect.ValueOf("x")); ok {
		t.Error("toFloat on string")
	}
	if f, ok := toFloat(reflect.ValueOf(float32(1.5))); !ok || f != 1.5 {
		t.Error("toFloat float32")
	}
}
