// Package example proves the CEL module end to end: the same schema, with
// an expression no typed condition could state, executed by BOTH the
// interpreter and generated code, required to report identical issues.
//
// That conformance is the project's standing invariant, and CEL is the one
// place where the two executors take genuinely different routes: generated
// code delegates expression gates back to the interpreter rather than
// compiling CEL into Go. This package is what keeps that delegation honest.
package example

import "time"

//go:generate validatorctl fields
//go:generate go run ./internal/export
//go:generate validatorctl gen . --schema stay.ir.json

// Stay is a hotel booking. The rule worth having is about the SPAN between
// two dates, which is arithmetic, so it cannot be written as a comparison.
type Stay struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Guests int       `json:"guests"`
	Note   string    `json:"note"`
}
