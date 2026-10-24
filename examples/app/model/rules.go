package model

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/vb"
)

// taken stands in for a database of addresses already registered. A real
// service would query one; the shape of the rule is the same either way.
var taken = map[string]bool{"taken@example.com": true}

// Schema is every rule for a Booking, in one place. Read top to bottom it
// is also a tour of what the library can say:
//
//   - field rules, which cross to the browser as JSON Schema
//   - a transform, which runs server-side and is reported as lossy
//   - an async rule, which the browser round-trips to the server
//   - a cross-field rule, which rides in the manifest as a condition
//   - gates, which become if/then in the schema
//   - an object rule, whose subject is the whole booking
var Schema = vb.Struct("Booking",
	vb.F(BookingFields.Email, vb.String().Email().
		Async("email_free", "/v/email_free", func(_ context.Context, e string) error {
			if taken[strings.ToLower(e)] {
				return errors.New("that address is already registered")
			}
			return nil
		})).Trim().Lower(),

	vb.F(BookingFields.Phone, vb.String().E164()).Optional(),

	vb.F(BookingFields.CheckIn, vb.Time()),
	// Cross-field: JSON Schema cannot compare two properties, so this
	// travels as a condition the frontend evaluates itself.
	vb.F(BookingFields.CheckOut, vb.Time().With(vb.After(BookingFields.CheckIn))),

	vb.F(BookingFields.Guests, vb.Int[int]().Min(1).Max(4)),
	vb.F(BookingFields.Room, vb.String().Enum("single", "double", "suite")),
	vb.F(BookingFields.Note, vb.String().MinLength(10).MaxLength(500)).Optional(),
	vb.F(BookingFields.Extras, vb.Array(vb.String().Enum("breakfast", "parking", "late-checkout")).MaxItems(3).Unique()).Optional(),
).Gate(
	// A suite booked for one person is unusual enough to want a note.
	vb.Requires(ir.And(
		vb.Eq(BookingFields.Room, "suite"),
		vb.Cmp(BookingFields.Guests, ir.OpLte, 1),
	), BookingFields.Note),
).Custom("fits_the_room", func(_ context.Context, b Booking) error {
	// The subject is the booking, not a field: it reads Room AND Guests.
	// No field rule can see both, and a gate could only decide whether
	// other rules apply, not report this.
	max, known := Capacity[b.Room]
	if !known {
		return nil // the enum rule already reported an unknown room
	}
	if b.Guests > max {
		return errors.New("a " + b.Room + " holds at most " + itoa(max) + " guests")
	}
	return nil
}).Custom("stay_length", func(_ context.Context, b Booking) error {
	if b.CheckIn.IsZero() || b.CheckOut.IsZero() {
		return nil // the required rules already reported the absence
	}
	if b.CheckOut.Sub(b.CheckIn) > 30*24*time.Hour {
		return errors.New("a stay may not exceed 30 nights")
	}
	return nil
}).MustBuild()

// itoa avoids strconv for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
