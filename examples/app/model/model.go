// Package model is the domain of the proving application: one booking
// form, rich enough to exercise every part of the library that a real
// consumer touches.
//
// It exists to be USED rather than tested in isolation. Everything else in
// this repository proves a component in a vacuum; this proves the seams
// between them — that rules written once in Go reach a browser, that the
// two sides report the same issue for the same input, and that the whole
// loop is something a person could actually build against.
package model

import "time"

//go:generate validatorctl fields
//go:generate go run ./../cmd/export
//go:generate validatorctl gen . --schema booking.ir.json

// Booking is the form a guest submits. The json names are the wire names
// the browser sees, so an issue path from Go is a path the frontend can
// resolve to an input.
type Booking struct {
	// Contact details: ordinary field rules, and one that needs the server.
	Email string `json:"email"`
	Phone string `json:"phone"`

	// The stay: two dates whose ORDER matters, which JSON Schema cannot
	// state and which therefore rides to the browser in the manifest.
	CheckIn  time.Time `json:"check_in"`
	CheckOut time.Time `json:"check_out"`

	// Party size and room, with a relationship between them that no single
	// field can express: it belongs to the booking as a whole.
	Guests int    `json:"guests"`
	Room   string `json:"room"`

	// Optional, and conditionally required: a note is demanded only for
	// the cases the gates below single out.
	Note string `json:"note"`

	// Extras, exercising a collection.
	Extras []string `json:"extras"`
}

// Capacity is what each room can hold. The object-level rule below reads
// it, which is the kind of check that has no owning field.
var Capacity = map[string]int{
	"single": 1,
	"double": 2,
	"suite":  4,
}
