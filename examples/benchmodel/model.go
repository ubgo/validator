// Package benchmodel is the fixture for the cross-library benchmark: one
// struct carrying go-playground tags AND a vb schema stating the same
// rules, so the two libraries are measured on identical work. It is a
// normal (non-test) package because the code generators read source files
// and skip _test.go.
package benchmodel

import "time"

//go:generate validatorctl fields
//go:generate go run ./internal/export
//go:generate validatorctl gen . --schema bench.ir.json

// BenchUser is the benchmark subject. The validate tags and the schema in
// rules.go must stay equivalent; TestBenchmarkRulesAreEquivalent checks it.
type BenchUser struct {
	Email    string    `json:"email"    validate:"required,email"`
	Name     string    `json:"name"     validate:"required,min=2,max=40"`
	Age      int       `json:"age"      validate:"required,gte=18,lte=120"`
	Plan     string    `json:"plan"     validate:"required,oneof=free pro team"`
	Website  string    `json:"website"  validate:"omitempty,url"`
	UUID     string    `json:"uuid"     validate:"required,uuid4"`
	Tags     []string  `json:"tags"     validate:"omitempty,max=5,dive,min=1"`
	Score    float64   `json:"score"    validate:"gte=0,lte=100"`
	Country  string    `json:"country"  validate:"required,iso3166_1_alpha2"`
	Created  time.Time `json:"created"  validate:"required"`
	Nickname string    `json:"nickname" validate:"omitempty,min=2,max=16"`
}

// Valid is the success-path fixture.
func Valid() BenchUser {
	return BenchUser{
		Email: "ada@example.com", Name: "Ada", Age: 36, Plan: "pro",
		Website: "https://example.com", UUID: "123e4567-e89b-42d3-a456-426614174000",
		Tags: []string{"a", "b"}, Score: 88.5, Country: "GB",
		Created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Nickname: "ada",
	}
}

// Invalid breaks four fields: a bad email, an age below the minimum, a plan
// outside the enum, and a malformed UUID.
func Invalid() BenchUser {
	u := Valid()
	u.Email, u.Age, u.Plan, u.UUID = "not-an-email", 12, "enterprise", "nope"
	return u
}
