// Package example is the accessor generator's fixture AND its proof: the
// generated fields.gen.go beside this file is compiled by `go build ./...`
// and exercised by example_test.go, so a generator bug that produces
// invalid Go or wrong types fails the build, not a string comparison.
package example

import (
	"time"

	str "strings"
)

// Address is referenced by User both by value and by pointer.
type Address struct {
	Street string `json:"street"`
	Zip    string `json:"zip,omitempty"`
}

// User covers the shapes the generator must handle: scalars, a stdlib
// type, pointers, slices, maps, nested structs, an interface, a json
// rename, a json "-", a tag equal to the Go name, an unexported field, and
// an embedded field (skipped with a note).
type User struct {
	Address
	Email    string         `json:"email"`
	Age      int32          `json:"age"`
	Start    time.Time      `json:"start"`
	Nick     *string        `json:"nick,omitempty"`
	Tags     []string       `json:"tags"`
	Labels   map[string]int `json:"labels"`
	Home     *Address       `json:"home"`
	Extra    any            `json:"extra"`
	Secret   string         `json:"-"`
	Same     string         `json:"Same"`
	Untagged bool
	Builder  *str.Builder `json:"-"`
	password string
}

// HasPassword reads the unexported field so the fixture stays honest about
// it: unexported fields are real data the generator must skip by default,
// not dead code.
func (u User) HasPassword() bool { return u.password != "" }

// Box is generic and therefore skipped.
type Box[T any] struct {
	Value T
}

// Plain has no tags at all.
type Plain struct {
	A int
	B float64
}

// notAStruct is a named non-struct type and is ignored by the generator.
type notAStruct int

var _ notAStruct = 0
