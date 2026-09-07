package example

import (
	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/vb"
)

// Schema requires a note for a stay longer than a week, and for a party
// larger than the nights booked. Both are arithmetic across fields: the
// typed condition vocabulary can compare two fields but cannot subtract
// or divide them, which is exactly what expressions are for.
var Schema = vb.Struct("Stay",
	vb.F(StayFields.Start, vb.Time()),
	vb.F(StayFields.End, vb.Time()),
	vb.F(StayFields.Guests, vb.Int[int]().Min(1)),
	vb.F(StayFields.Note, vb.String().MinLength(3)).Optional(),
).Gate(
	vb.Requires(ir.Expr(`end - start > duration('168h')`), StayFields.Note),
	vb.When(ir.Expr(`guests > 4`),
		[]ir.TargetRule{vb.On(StayFields.Note, ir.MinLength(10))}, nil),
).MustBuild()
