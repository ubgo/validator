package interp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/vb"
	"github.com/ubgo/validator/verr"
)

// The interpreter's map path receives whatever a JSON body decoded to, so
// it is an untrusted-input surface. These targets pin the properties an
// HTTP handler depends on.

type fuzzUser struct {
	Email string `json:"email"`
	Age   int    `json:"age"`
	Tags  []string
	Home  *fuzzAddr `json:"home"`
	Kind  string    `json:"kind"`
}

type fuzzAddr struct {
	Zip string `json:"zip"`
}

var (
	fzEmail = vb.NewField("Email", func(u fuzzUser) string { return u.Email }).JSON("email")
	fzAge   = vb.NewField("Age", func(u fuzzUser) int { return u.Age }).JSON("age")
	fzTags  = vb.NewField("Tags", func(u fuzzUser) []string { return u.Tags }).JSON("tags")
	fzHome  = vb.NewField("Home", func(u fuzzUser) *fuzzAddr { return u.Home }).JSON("home")
	fzKind  = vb.NewField("Kind", func(u fuzzUser) string { return u.Kind }).JSON("kind")
	fzZip   = vb.NewField("Zip", func(a fuzzAddr) string { return a.Zip }).JSON("zip")

	fuzzAddrSchema = vb.Struct("Addr", vb.F(fzZip, vb.String().Pattern(`^[0-9]{5}$`))).MustBuild()

	// One schema with a bit of everything the walker can meet: formats,
	// bounds, a collection, a nested ref, transforms and a gate.
	fuzzSchema = vb.Struct("User",
		vb.F(fzEmail, vb.String().Email()).Trim().Lower(),
		vb.F(fzAge, vb.Int[int]().Min(18).Max(120)).Coerce(),
		vb.F(fzTags, vb.Array(vb.String().MinLength(1)).MaxItems(4).Unique()).Optional(),
		vb.F(fzHome, vb.Ptr(vb.Ref(fuzzAddrSchema))).Optional(),
		vb.F(fzKind, vb.String().Enum("a", "b")).Optional(),
	).Gate(
		vb.Requires(vb.Eq(fzKind, "a"), fzTags),
	).MustBuild()
)

// FuzzValidateMap runs arbitrary decoded JSON through the interpreter and
// pins three properties:
//
//   - It never panics, whatever shape arrives. A handler validating a
//     hostile body must return 422, not crash the process.
//   - Every issue carries a code and a path, because a caller renders by
//     code and locates by path; an issue missing either is unusable.
//   - Fail-fast and accumulating runs agree on the FIRST issue. Both walk
//     the schema in the same order, so a caller can switch modes without
//     the reported error changing.
func FuzzValidateMap(f *testing.F) {
	for _, seed := range []string{
		`{"email":"a@b.io","age":30}`,
		`{"email":" A@B.IO ","age":"30","kind":"a","tags":["x"]}`,
		`{"email":1,"age":{},"tags":[1,2],"home":{"zip":"x"}}`,
		`{"home":{"zip":"12345"}}`,
		`[]`,
		`null`,
		`{"tags":["x","x"]}`,
	} {
		f.Add([]byte(seed))
	}

	v, err := FromSchema(fuzzSchema)
	if err != nil {
		f.Fatal(err)
	}
	lax, err := FromSchema(fuzzSchema, Lax())
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return // not JSON; the decoder rejected it before we got here
		}
		ctx := context.Background()

		// Each run gets its own decode: transforms write back, so sharing a
		// value between runs would compare a mutated input against a fresh one.
		decode := func() any {
			var out any
			_ = json.Unmarshal(data, &out)
			return out
		}

		all := v.Run(ctx, decode())
		for _, is := range all.Issues {
			if is.Code == "" {
				t.Fatalf("issue with no code: %+v (input %s)", is, data)
			}
			if is.Path == "" {
				t.Fatalf("issue with no path: %+v (input %s)", is, data)
			}
		}

		first := &verr.Errors{FailFast: true}
		v.RunInto(ctx, decode(), first)
		switch {
		case all.Len() == 0 && first.Len() != 0:
			t.Fatalf("fail-fast found an issue an accumulating run did not: %+v (input %s)", first.Issues, data)
		case all.Len() != 0 && first.Len() != 1:
			t.Fatalf("fail-fast reported %d issues (input %s)", first.Len(), data)
		case all.Len() != 0 && !sameIssue(first.Issues[0], all.Issues[0]):
			t.Fatalf("fail-fast first issue differs:\n%+v\n%+v\n(input %s)", first.Issues[0], all.Issues[0], data)
		}

		// Lax coercion widens what is accepted; it must never turn a value
		// the default mode accepted into a failure.
		if all.Len() == 0 {
			if laxErrs := lax.Run(ctx, decode()); laxErrs.Len() != 0 {
				t.Fatalf("lax rejected what the default accepted: %+v (input %s)", laxErrs.Issues, data)
			}
		}
	})
}

// FuzzCompileSchema pins the compile step: New must decide, never panic.
// A schema it accepts must also run without panicking on a hostile value,
// which is what makes "load a schema from a file" a safe operation.
func FuzzCompileSchema(f *testing.F) {
	golden, err := json.Marshal(fuzzSchema.IR())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(golden)
	f.Add([]byte(`{"version":"` + ir.Version + `","root":{"kind":"string","rules":[{"kind":"pattern","params":{"regex":"^a+$"}}]}}`))
	f.Add([]byte(`{"version":"` + ir.Version + `","root":{"kind":"any"}}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var s ir.Schema
		if err := json.Unmarshal(data, &s); err != nil {
			return
		}
		v, err := New(&s)
		if err != nil {
			return // refused: unresolved names, CEL, bad regex — all correct
		}
		// Accepted, so it must run on anything without panicking.
		for _, value := range []any{
			nil,
			map[string]any{},
			map[string]any{"a": []any{1, "x", nil}},
			[]any{1, 2},
			"string",
			float64(1),
			true,
		} {
			_ = v.Run(context.Background(), value)
		}
	})
}

// sameIssue compares two issues by their serialized form: Issue carries a
// params map, so it is not comparable with ==.
func sameIssue(a, b verr.Issue) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}
