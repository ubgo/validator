package example

import (
	"context"
	"testing"
	"time"

	"github.com/ubgo/validator/interp"
	"github.com/ubgo/validator/vb"
)

// TestGeneratedAccessorsCompileAndWork is the codegen-as-test proof: the
// generated UserFields must type-check against real builders and drive a
// real validation. A generator that emits the wrong FieldRef type fails
// here at compile time, not in a string comparison.
func TestGeneratedAccessorsCompileAndWork(t *testing.T) {
	address := vb.Struct("Address",
		vb.F(AddressFields.Street, vb.String().MinLength(1)),
		vb.F(AddressFields.Zip, vb.String()).Optional(),
	).MustBuild()

	// Every generated accessor binds to a builder of its own Go type.
	s := vb.Struct("User",
		vb.F(UserFields.Email, vb.String().Email()),
		vb.F(UserFields.Age, vb.Int[int32]().Min(18)),
		vb.F(UserFields.Start, vb.Time()),
		vb.F(UserFields.Nick, vb.Ptr(vb.String().MinLength(2))).Optional(),
		vb.F(UserFields.Tags, vb.Array(vb.String())).Optional(),
		vb.F(UserFields.Labels, vb.Map(vb.String(), vb.Int[int]())).Optional(),
		vb.F(UserFields.Home, vb.Ptr(vb.Ref(address))).Optional(),
		vb.F(UserFields.Extra, vb.Any[any]()).Optional(),
		vb.F(UserFields.Same, vb.String()).Optional(),
		vb.F(UserFields.Untagged, vb.Bool()),
	).MustBuild()
	plain := vb.Struct("Plain",
		vb.F(PlainFields.A, vb.Int[int]().Min(1)),
		vb.F(PlainFields.B, vb.Float[float64]()),
	).MustBuild()

	v, err := interp.FromSchema(s)
	if err != nil {
		t.Fatal(err)
	}
	u := User{Email: "ada@example.com", Age: 30, Start: time.Now(), Home: &Address{Street: "Main"}, Untagged: true}
	if err := v.Validate(context.Background(), u); err != nil {
		t.Fatalf("valid: %v", err)
	}
	u.Age = 3
	u.Home.Street = ""
	err = v.Validate(context.Background(), u)
	if err == nil || len(err.Error()) == 0 {
		t.Fatal("expected issues")
	}
	pv, _ := interp.FromSchema(plain)
	if err := pv.Validate(context.Background(), Plain{A: 0}); err == nil {
		t.Fatal("plain min")
	}

	// Names and wire names came through as declared.
	if UserFields.Email.Name() != "Email" || UserFields.Same.Name() != "Same" || UserFields.Untagged.Name() != "Untagged" {
		t.Error("names")
	}
	if UserFields.Age.Get(u) != 3 || UserFields.Home.Get(u).Street != "" {
		t.Error("getters")
	}
	// The unexported field has no generated accessor by default; it is
	// still reachable through the type's own methods.
	if (User{password: "x"}).HasPassword() != true || u.HasPassword() {
		t.Error("HasPassword")
	}
}
