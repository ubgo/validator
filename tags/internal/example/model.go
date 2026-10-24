// Package example is the tag-migration fixture AND its proof: schema.gen.go
// beside this file is emitted by tags.Builder from the migrated IR, is
// compiled by `go build ./...`, and its test asserts that building it
// yields the very IR the migration produced. Tags here are written in
// the conventional `validate` syntax, exactly as a team would have them.
package example

import "time"

//go:generate validatorctl fields

// Address is nested by value and by pointer.
type Address struct {
	Street string `json:"street" validate:"required,min=1"`
	Zip    string `json:"zip" validate:"omitempty,len=5,numeric"`
}

// Signup covers the tag vocabulary the migration maps.
type Signup struct {
	Email    string            `json:"email" validate:"required,email"`
	Name     string            `json:"name" validate:"required,min=2,max=40,alpha"`
	Age      int               `json:"age" validate:"required,gte=18,lte=120"`
	Score    float64           `json:"score" validate:"omitempty,gt=0,lt=1"`
	Plan     string            `json:"plan" validate:"required,oneof=free pro 'team plus'"`
	Level    int8              `json:"level" validate:"oneof=1 2 3"`
	Accept   bool              `json:"accept" validate:"eq=true"`
	Site     *string           `json:"site" validate:"omitempty,url"`
	ID       string            `json:"id" validate:"required,uuid4"`
	Ref      string            `json:"ref" validate:"omitempty,ulid"`
	Code     string            `json:"code" validate:"omitempty,startswith=ub-,endswith=-x,contains=go"`
	Tags     []string          `json:"tags" validate:"required,min=1,max=5,unique,dive,min=2,alphanum"`
	Scores   []int             `json:"scores" validate:"omitempty,len=3,dive,gte=0,lte=10"`
	Labels   map[string]string `json:"labels" validate:"omitempty,max=3,dive,max=8"`
	Home     Address           `json:"home" validate:"required"`
	Work     *Address          `json:"work" validate:"omitempty"`
	Start    time.Time         `json:"start" validate:"required"`
	End      time.Time         `json:"end" validate:"required,gtfield=Start"`
	Raw      []byte            `json:"raw" validate:"omitempty,max=16"`
	Coupon   string            `json:"coupon" validate:"required_if=Plan pro,required_with=Ref"`
	Venue    string            `json:"venue" validate:"required_unless=Plan free"`
	Phone    string            `json:"phone" validate:"required_without=Email,e164"`
	Extra    any               `json:"extra"`
	Internal string            `json:"-" validate:"required"`
	Ignored  string            `validate:"-"`
	Weird    string            `json:"weird" validate:"required,isdefault,eqfield=Name"`
	Hex      string            `json:"hex" validate:"omitempty,hexadecimal,lowercase"`
	Nick     string            `json:"nick" validate:"omitempty,gt=1,lt=10"`
	unexport string            `validate:"required"`
}

// HasUnexported reads the unexported field so the fixture is honest about
// it: unexported fields carry real data the migration must skip, and are
// not dead code.
func (s Signup) HasUnexported() bool { return s.unexport != "" }
