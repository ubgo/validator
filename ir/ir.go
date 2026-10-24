// Package ir is the serializable rule-tree intermediate representation at
// the heart of github.com/ubgo/validator.
//
// Why this package exists (docsi/MISSION.md ADR-VAL-001): every authoring
// surface — typed builder, generated field accessors, migrated struct tags —
// compiles to this one tree, and every back door — the Go executors, the JSON
// Schema emitter, the TypeScript generator, the portability manifest — reads
// from it. The IR is therefore the product; everything else is a projection.
//
// The single invariant the package enforces: rules are DATA, never closures.
// A node records what a rule is ({"kind":"minLength","params":{"value":3}}),
// not how to run it, because a func value cannot be serialized and so could
// never become JSON Schema or TypeScript. The only opaque node is the custom
// rule, and it is explicitly classified non-portable (§8 of docsi/IR.md) so a
// generator can report it instead of dropping it.
//
// Three formats (docsi/IR.md §0): the Go types here are canonical; the
// versioned JSON produced by [Schema.MarshalJSON] is the stable contract for
// non-Go consumers; a JSON Schema OF the IR is generated from these types
// separately. The Go types may refactor as long as the JSON round-trips —
// see the round-trip tests.
package ir

// Version is the IR shape version carried by every serialized [Schema].
//
// Bumped only on breaking changes to the node shapes (docsi/IR.md §9). New
// rule or condition kinds are additive within a major; a consumer that meets
// an unknown kind must error loudly rather than skip it (skeleton honesty),
// which is what [Check] and the JSON decoder do.
const Version = "ubgo.validator.ir/v1"

// Schema is the top-level IR document: one root type plus the shared or
// recursive definitions it refers to.
type Schema struct {
	// Version gates evolution; must equal [Version] for this package to
	// accept the document.
	Version string `json:"version"`

	// Root is the validated type — almost always an [*ObjectNode] built from
	// a Go struct.
	Root TypeNode `json:"root"`

	// Defs holds named types for recursion and sharing; a [*RefNode] points
	// here by name. Mirrors JSON Schema $defs so export is direct.
	Defs map[string]TypeNode `json:"defs,omitempty"`

	// Meta describes the document itself, not any node.
	Meta SchemaMeta `json:"meta,omitzero"`
}

// SchemaMeta is document-level annotation: where the schema came from and
// what produced it. Purely informational; nothing validates against it.
type SchemaMeta struct {
	// Title is the human name, usually the source struct's short name.
	Title string `json:"title,omitempty"`

	// Source is the fully qualified Go type the schema was derived from,
	// e.g. "github.com/acme/app/models.User", so a generated artifact can be
	// traced back to its origin.
	Source string `json:"source,omitempty"`

	// Generator records which tool and version emitted the document.
	Generator string `json:"generator,omitempty"`
}

// Base carries the two cross-cutting attributes every type node has.
//
// Nullable lives on the TYPE (the value may be null) while optional lives on
// the [FieldNode] (the key may be absent). Go pointers usually mean both, but
// JSON Schema (`required` vs `type:["string","null"]`) and TypeScript schema
// libraries (optional vs nullable) keep them apart, so the IR records them separately to
// export losslessly (docsi/IR.md §2).
type Base struct {
	Nullable bool     `json:"nullable,omitempty"`
	Meta     NodeMeta `json:"meta,omitzero"`
}

// NodeMeta is the annotation layer shared by nodes and rules: the machine
// error code, i18n keys, and JSON Schema description/examples.
//
// Code, not the message, is what cross-language consumers key on: one
// catalog of code → localized message is emitted to both Go and TypeScript so
// errors match on both ends (docsi/IR.md §6).
type NodeMeta struct {
	// Code is the machine-readable error identifier, e.g. "too_short".
	Code string `json:"code,omitempty"`

	// MessageKey is the i18n catalog key for this failure.
	MessageKey string `json:"messageKey,omitempty"`

	// Message is the default templated message when no catalog entry exists;
	// template variables such as {min} and {field} come from Params.
	Message string `json:"message,omitempty"`

	// Params are the template variables for Message.
	Params map[string]any `json:"params,omitempty"`

	// Description flows into the JSON Schema "description" annotation.
	Description string `json:"description,omitempty"`

	// Examples flow into the JSON Schema "examples" annotation.
	Examples []any `json:"examples,omitempty"`
}

// Portability classifies whether a rule or condition can run on the
// frontend. It is the load-bearing enum behind the portability manifest
// (ADR-VAL-004): the generator reports every non-portable node instead of
// silently dropping it, which is the failure mode this project exists to
// prevent.
type Portability string

const (
	// Portable rules are expressible in JSON Schema or CEL and run locally
	// in the browser.
	Portable Portability = "portable"

	// Opaque rules wrap a custom Go function. The frontend gets a stub that
	// warns at runtime; the backend enforces.
	Opaque Portability = "opaque"

	// Server rules need backend state (a database uniqueness check). The
	// frontend compiles them to a debounced round-trip to the server.
	Server Portability = "server"

	// Wasm rules are custom functions the author chose to ship to the
	// browser as a scoped TinyGo-WASM blob. Opt-in only.
	Wasm Portability = "wasm"
)

// portabilities is the closed set of valid classes, used by [Check].
var portabilities = map[Portability]struct{}{
	Portable: {},
	Opaque:   {},
	Server:   {},
	Wasm:     {},
}

// Valid reports whether p is one of the defined classes. The empty string is
// NOT valid — callers should fill defaults with [Rule.EffectivePortability]
// rather than treat empty as portable, because an unlabeled custom rule that
// defaulted to portable would be exactly the silent drop we guard against.
func (p Portability) Valid() bool {
	_, ok := portabilities[p]
	return ok
}
