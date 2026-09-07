package jsonschema

import (
	"sort"

	"github.com/ubgo/validator/ir"
)

// Class says why an entry could not be expressed in JSON Schema. The
// three portability classes come straight from the IR; the other two are
// emitter facts: something needs CEL to cross, or the schema keyword is an
// approximation of the rule.
type Class string

const (
	// ClassOpaque: a custom Go function. Frontend stubs and warns; backend
	// enforces.
	ClassOpaque Class = Class(ir.Opaque)

	// ClassServer: an async rule. Frontend round-trips to the endpoint.
	ClassServer Class = Class(ir.Server)

	// ClassWasm: a custom function the author chose to ship as WASM.
	ClassWasm Class = Class(ir.Wasm)

	// ClassCEL: expressible only as a CEL expression (cross-field
	// comparison, relational condition). Portable through the CEL layer,
	// invisible to plain JSON Schema consumers.
	ClassCEL Class = "cel"

	// ClassLossy: the schema carries an approximation or nothing (a value
	// transform, a byte-length rule on a base64 string). Runs server-side
	// as written.
	ClassLossy Class = "lossy"
)

// Kind says what sort of IR element the entry describes.
type Kind string

const (
	KindRule      Kind = "rule"
	KindCondition Kind = "condition"
	KindTransform Kind = "transform"
	KindGate      Kind = "gate"
)

// Entry is one thing the JSON Schema does not carry.
type Entry struct {
	// Path locates the value the element applies to, JSONPath style.
	Path string `json:"path"`

	Kind  Kind   `json:"kind"`
	Name  string `json:"name"`
	Class Class  `json:"class"`

	// Ref is the registered function name for custom/async elements.
	Ref string `json:"ref,omitempty"`

	// Endpoint is the round-trip URL for server rules, when declared.
	Endpoint string `json:"endpoint,omitempty"`

	// Reason is a one-line human explanation.
	Reason string `json:"reason"`

	// Cond is the executable form of an element JSON Schema could not
	// express, with wire names so a JSON-only consumer can resolve them.
	// Present for cross-field rules and for gates keyed on a condition the
	// schema cannot state.
	//
	// Two shapes, distinguished by whether Then/Else are set:
	//   - Cond alone: the condition must hold, and an issue is reported
	//     against Path when it does not. This is how after/before arrive.
	//   - Cond with Then and/or Else: a gate. Evaluate Cond, then validate
	//     the value against the matching fragment.
	//
	// nil when the element cannot be executed anywhere but the server (a
	// custom predicate, or CEL without the CEL module).
	Cond *ir.Condition `json:"cond,omitempty"`

	// Then and Else are the JSON Schema fragments a gate's branches would
	// have produced, for a runtime to validate with its own validator.
	Then *Object `json:"then,omitempty"`
	Else *Object `json:"else,omitempty"`

	// Code and Message are what the Go executors report when Cond alone is
	// the rule and it does not hold. Carried so the browser produces the
	// identical issue rather than an approximation of it — one wording,
	// one code, both sides. Empty for gates, whose branch issues come from
	// the schema fragments.
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// Manifest is the per-schema report of non-portable elements. Consumers
// (the TypeScript generator, CI gates) read it to stub, round-trip, or
// fail the build; nobody has to discover a dropped rule in production.
type Manifest struct {
	Entries []Entry `json:"entries"`
}

// Add appends an entry.
func (m *Manifest) Add(e Entry) { m.Entries = append(m.Entries, e) }

// Len returns the entry count.
func (m *Manifest) Len() int { return len(m.Entries) }

// Portable reports whether the schema carried every rule — an empty
// manifest.
func (m *Manifest) Portable() bool { return len(m.Entries) == 0 }

// ByClass groups entries by class.
func (m *Manifest) ByClass() map[Class][]Entry {
	out := map[Class][]Entry{}
	for _, e := range m.Entries {
		out[e.Class] = append(out[e.Class], e)
	}
	return out
}

// sortedKeys returns map keys in lexical order for deterministic walks.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
