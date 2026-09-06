// Package signup is the user guide's running example: an ordinary struct,
// its generated field accessors, and the rules written against them. Every
// file here is compiled by the build and shown verbatim in docs/GUIDE.md.
package signup

import "time"

// Regenerate everything derived from this package, in order: the field
// accessors, the IR document, then the reflection-free validator.
//
//go:generate validatorctl fields
//go:generate go run ../cmd/signup-export -dir .
//go:generate validatorctl gen . --schema signup.ir.json

// Signup is the request body of POST /signup. Nothing validator-specific
// lives on the struct; rules are in rules.go.
type Signup struct {
	Email    string    `json:"email"`
	Password string    `json:"password"`
	Age      int       `json:"age"`
	Plan     string    `json:"plan"`
	Website  *string   `json:"website"`
	Tags     []string  `json:"tags"`
	Starts   time.Time `json:"starts"`
	Ends     time.Time `json:"ends"`
	Company  string    `json:"company"`
	VATID    string    `json:"vat_id"`
}
