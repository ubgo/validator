// Package verr is the validation error model shared by every executor
// (reflection interpreter, generated code) and mirrored by the TypeScript
// runtime.
//
// Why a separate package: the error shape is API. Both executors must
// produce identical issues for the same input (the conformance suite
// compares them), the CLI and HTTP layers serialize it, and the frontend
// keys on the same codes to localize messages. One definition, imported by
// all, keeps that contract in one place (docsi/IR.md §6).
package verr

import (
	"fmt"
	"strings"
)

// Issue is one validation failure. Code is what programs and translators
// key on; Message is the default human rendering; Path locates the value.
type Issue struct {
	// Code is the machine-readable failure identifier, e.g. "min_length".
	// Defaults come from the rule kind; a rule's Meta.Code overrides.
	Code string `json:"code"`

	// Path is a JSONPath-style locator using wire names: $.tags[0].name.
	Path string `json:"path"`

	// Message is the rendered default message. Localizing consumers should
	// ignore it and render from Code + Params.
	Message string `json:"message"`

	// Rule is the rule kind that failed, when a rule failed (empty for
	// structural failures such as a missing required field).
	Rule string `json:"rule,omitempty"`

	// Params are the template variables for the message: the rule's
	// operands plus "value" when it is safe to echo.
	Params map[string]any `json:"params,omitempty"`
}

// Error implements error.
func (i Issue) Error() string {
	if i.Path == "" {
		return i.Message
	}
	return i.Path + ": " + i.Message
}

// Errors is the aggregate result of a validation run: zero or more issues.
// A nil *Errors means valid; executors return nil rather than an empty
// list so callers can use the ordinary err != nil check.
type Errors struct {
	Issues []Issue `json:"issues"`

	// FailFast makes Add ignore every issue after the first. Executors read
	// it through Stopped to skip the remaining work; the first issue is the
	// same one an accumulating run would report first, because both walk
	// the schema in the same order.
	FailFast bool `json:"-"`
}

// Error implements error with one line per issue.
func (e *Errors) Error() string {
	if e == nil || len(e.Issues) == 0 {
		return "validation passed"
	}
	lines := make([]string, len(e.Issues))
	for i, is := range e.Issues {
		lines[i] = is.Error()
	}
	return strings.Join(lines, "\n")
}

// Add appends an issue, unless a fail-fast run already holds one.
func (e *Errors) Add(issue Issue) {
	if e.FailFast && len(e.Issues) > 0 {
		return
	}
	e.Issues = append(e.Issues, issue)
}

// Stopped reports whether a fail-fast run has its issue and executors may
// stop walking. Safe on nil.
func (e *Errors) Stopped() bool { return e != nil && e.FailFast && len(e.Issues) > 0 }

// Len returns the issue count; safe on nil.
func (e *Errors) Len() int {
	if e == nil {
		return 0
	}
	return len(e.Issues)
}

// OrNil returns e as an error, or nil when there are no issues — the
// executor's return convention.
func (e *Errors) OrNil() error {
	if e == nil || len(e.Issues) == 0 {
		return nil
	}
	return e
}

// ByPath groups issues by path, preserving first-seen order of paths.
func (e *Errors) ByPath() map[string][]Issue {
	out := map[string][]Issue{}
	if e == nil {
		return out
	}
	for _, is := range e.Issues {
		out[is.Path] = append(out[is.Path], is)
	}
	return out
}

// Render fills a message template. Placeholders are {name} for keys of
// params; unknown placeholders are left verbatim so a typo is visible
// rather than silently blank.
func Render(template string, params map[string]any) string {
	if len(params) == 0 || !strings.Contains(template, "{") {
		return template
	}
	var b strings.Builder
	for {
		open := strings.IndexByte(template, '{')
		if open < 0 {
			b.WriteString(template)
			return b.String()
		}
		closeIdx := strings.IndexByte(template[open:], '}')
		if closeIdx < 0 {
			b.WriteString(template)
			return b.String()
		}
		key := template[open+1 : open+closeIdx]
		b.WriteString(template[:open])
		if v, ok := params[key]; ok {
			fmt.Fprint(&b, v)
		} else {
			b.WriteString(template[open : open+closeIdx+1])
		}
		template = template[open+closeIdx+1:]
	}
}

// Structural codes: failures that are not a rule's fault. Closed set,
// shared with the TypeScript runtime.
const (
	// CodeRequired: a non-optional key is absent or a non-nullable value is
	// null/zero.
	CodeRequired = "required"

	// CodeType: the value has the wrong shape for the node (string where an
	// object was expected).
	CodeType = "type"

	// CodeUnknownKey: an object with extra=forbid received an undeclared key.
	CodeUnknownKey = "unknown_key"

	// CodeVariant: a oneof discriminator matched no case and no default.
	CodeVariant = "unknown_variant"

	// CodeUnion: a union value matched no variant.
	CodeUnion = "no_union_match"

	// CodeGroup: a group gate's presence count is wrong.
	CodeGroup = "group"

	// CodeCustom: a custom or async function returned an error and set no
	// code of its own.
	CodeCustom = "custom"
)
