package verr

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestIssueAndErrors(t *testing.T) {
	var e *Errors
	if e.Len() != 0 || e.OrNil() != nil || e.Error() != "validation passed" || len(e.ByPath()) != 0 {
		t.Error("nil Errors must behave as empty")
	}
	e = &Errors{}
	if e.OrNil() != nil {
		t.Error("empty Errors must be nil error")
	}
	e.Add(Issue{Code: "a", Path: "$.x", Message: "bad x"})
	e.Add(Issue{Code: "b", Path: "$.x", Message: "worse x"})
	e.Add(Issue{Code: "c", Message: "root problem"})
	if e.Len() != 3 || e.OrNil() == nil {
		t.Error("issues not counted")
	}
	if got := e.Error(); got != "$.x: bad x\n$.x: worse x\nroot problem" {
		t.Errorf("Error() = %q", got)
	}
	if by := e.ByPath(); len(by["$.x"]) != 2 || len(by[""]) != 1 {
		t.Errorf("ByPath %v", by)
	}
}

func TestJSON(t *testing.T) {
	// encoding/json writes null for a nil pointer before consulting any
	// marshaler, so a nil *Errors is "null" on the wire; callers that want
	// {"issues":[]} marshal &Errors{}.
	var nilErrs *Errors
	data, err := json.Marshal(nilErrs)
	if err != nil || string(data) != `null` {
		t.Errorf("nil marshal %s %v", data, err)
	}
	data, _ = json.Marshal(&Errors{})
	if string(data) != `{"issues":null}` {
		t.Errorf("empty marshal %s", data)
	}
	e := &Errors{Issues: []Issue{{Code: "min_length", Path: "$.name", Message: "m", Rule: "minLength", Params: map[string]any{"value": 3}}}}
	data, err = json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"issues":[{"code":"min_length","path":"$.name","message":"m","rule":"minLength","params":{"value":3}}]}`
	if string(data) != want {
		t.Errorf("got %s", data)
	}
	var back Errors
	if err := json.Unmarshal(data, &back); err != nil || back.Len() != 1 {
		t.Errorf("round trip: %v", err)
	}
}

func TestRender(t *testing.T) {
	cases := []struct {
		tmpl   string
		params map[string]any
		want   string
	}{
		{"plain", nil, "plain"},
		{"at least {value}", map[string]any{"value": 3}, "at least 3"},
		{"{a}-{b}", map[string]any{"a": "x", "b": 2.5}, "x-2.5"},
		{"unknown {nope} stays", map[string]any{"x": 1}, "unknown {nope} stays"},
		{"unclosed {oops", map[string]any{"oops": 1}, "unclosed {oops"},
		{"{value}", map[string]any{"value": []any{"a", "b"}}, "[a b]"},
		{"no braces", map[string]any{"value": 1}, "no braces"},
	}
	for _, c := range cases {
		if got := Render(c.tmpl, c.params); got != c.want {
			t.Errorf("Render(%q) = %q, want %q", c.tmpl, got, c.want)
		}
	}
}

func TestFailFast(t *testing.T) {
	e := &Errors{FailFast: true}
	if e.Stopped() {
		t.Error("empty fail-fast is not stopped")
	}
	e.Add(Issue{Code: "a"})
	e.Add(Issue{Code: "b"})
	if e.Len() != 1 || e.Issues[0].Code != "a" || !e.Stopped() {
		t.Errorf("fail-fast must keep only the first issue: %+v", e.Issues)
	}
	acc := &Errors{}
	acc.Add(Issue{Code: "a"})
	if acc.Stopped() {
		t.Error("accumulating run never stops")
	}
	var nilErrs *Errors
	if nilErrs.Stopped() {
		t.Error("nil is not stopped")
	}
	data, _ := json.Marshal(e)
	if string(data) != `{"issues":[{"code":"a","path":"","message":""}]}` {
		t.Errorf("FailFast must not leak onto the wire: %s", data)
	}
}

// TestSentinels pins errors.Is over a validation result. Before this a
// caller had to compare is.Code against a string, which is the untyped
// pattern this library exists to remove.
func TestSentinels(t *testing.T) {
	errs := &Errors{Issues: []Issue{
		{Code: CodeRequired, Path: "$.email"},
		{Code: "min_length", Path: "$.password", Params: map[string]any{"value": 8}},
		{Code: CodeRequired, Path: "$.name"},
	}}
	var err error = errs

	// The whole result matches when ANY issue carries the code.
	if !errors.Is(err, ErrRequired) {
		t.Error("errors.Is missed a required issue")
	}
	if !errors.Is(err, Code("min_length")) {
		t.Error("errors.Is missed a rule code")
	}
	if errors.Is(err, ErrType) {
		t.Error("errors.Is matched a code that is not present")
	}
	// A code this package has never heard of works the same way, which is
	// what makes .Msg("weak_password", ...) usable.
	if errors.Is(err, Code("weak_password")) {
		t.Error("matched an absent custom code")
	}
	custom := &Errors{Issues: []Issue{{Code: "weak_password"}}}
	if !errors.Is(error(custom), Code("weak_password")) {
		t.Error("a custom code must match like any other")
	}

	// A single issue matches too.
	if !errors.Is(errs.Issues[0], ErrRequired) || errors.Is(errs.Issues[0], ErrType) {
		t.Error("Issue.Is")
	}

	// Nil-safety: the value a passing validation returns must be safe to
	// test, or every call site needs a nil check first.
	var none *Errors
	if errors.Is(error(none), ErrRequired) {
		t.Error("a nil result matched a code")
	}
	if none.Has(ErrRequired) {
		t.Error("Has on nil")
	}
	if _, ok := none.First(ErrRequired); ok {
		t.Error("First on nil")
	}
	if none.AllOf(ErrRequired) != nil {
		t.Error("AllOf on nil")
	}

	// Codes compare in both directions, so errors.Is works whichever way
	// the caller nests them.
	if !errors.Is(ErrRequired, Code(CodeRequired)) {
		t.Error("Code.Is")
	}
	if errors.Is(ErrRequired, ErrType) {
		t.Error("different codes matched")
	}
	if ErrRequired.Error() != "required" {
		t.Errorf("Code.Error = %q", ErrRequired.Error())
	}

	// The helpers that carry more than a yes or no.
	if !errs.Has(ErrRequired) {
		t.Error("Has")
	}
	first, ok := errs.First(ErrRequired)
	if !ok || first.Path != "$.email" {
		t.Errorf("First = %+v", first)
	}
	if all := errs.AllOf(ErrRequired); len(all) != 2 || all[1].Path != "$.name" {
		t.Errorf("AllOf = %+v", all)
	}
	if _, ok := errs.First(ErrType); ok {
		t.Error("First found an absent code")
	}

	// As unwraps a plain error into the result.
	if got, ok := As(err); !ok || got != errs {
		t.Error("As")
	}
	if _, ok := As(errors.New("unrelated")); ok {
		t.Error("As matched an unrelated error")
	}
	if _, ok := As(nil); ok {
		t.Error("As(nil)")
	}
	// It also unwraps through a wrapper, which is why it uses errors.As.
	if _, ok := As(fmt.Errorf("decoding the body: %w", errs)); !ok {
		t.Error("As did not see through a wrapper")
	}
	if !errors.Is(fmt.Errorf("wrapped: %w", errs), ErrRequired) {
		t.Error("errors.Is did not see through a wrapper")
	}
}
