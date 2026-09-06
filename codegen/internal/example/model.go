// Package example is the codegen fixture AND its proof: validate.gen.go
// beside this file is emitted by the generator, compiled by `go build
// ./...`, and run against the interpreter on the same inputs in
// conformance_test.go. A generator that emits invalid Go fails the build; a
// generator that diverges from the interpreter fails the conformance test.
package example

import "time"

// Address is shared by value and by pointer.
type Address struct {
	Street string `json:"street"`
	Zip    string `json:"zip"`
}

// Order exercises every node kind codegen supports.
type Order struct {
	Email    string            `json:"email"`
	Qty      int               `json:"qty"`
	Weight   float64           `json:"weight"`
	Rush     bool              `json:"rush"`
	Note     *string           `json:"note"`
	Placed   time.Time         `json:"placed"`
	Ship     *time.Time        `json:"ship"`
	Window   time.Duration     `json:"window"`
	Tags     []string          `json:"tags"`
	Scores   []int             `json:"scores"`
	Meta     map[string]string `json:"meta"`
	Billing  Address           `json:"billing"`
	Shipping *Address          `json:"shipping"`
	Lines    []Line            `json:"lines"`
	Raw      []byte            `json:"raw"`
	Extra    any               `json:"extra"`
	Coupon   string            `json:"coupon"`
	Type     string            `json:"type"`
	URL      string            `json:"url"`
	Venue    string            `json:"venue"`
	Sku      string            `json:"sku"`
	Nick     string            `json:"nick"`
	Ref      any               `json:"ref"`
	Event    Event             `json:"event"`
	Pref     *string           `json:"pref"`
	Host     string            `json:"host"`
	Ver      string            `json:"ver"`
	Addr     string            `json:"addr"`
	Choice   any               `json:"choice"`
	Code     string            `json:"code"`
	Bag      any               `json:"bag"`
	Contact  string            `json:"contact"`
	Units    int               `json:"units"`
	Card     string            `json:"card"`
	Lat      float64           `json:"lat"`
}

// Event is a discriminated union over one Go struct.
type Event struct {
	Kind    string   `json:"kind"`
	URL     string   `json:"url"`
	Address *Address `json:"address"`
}

// Line is an inline object inside a slice.
type Line struct {
	Sku string `json:"sku"`
	Qty int    `json:"qty"`
}
