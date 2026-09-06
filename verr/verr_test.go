package verr

import (
	"encoding/json"
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
