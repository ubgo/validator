// Package jsonschema emits JSON Schema from the IR and, alongside it, the
// portability manifest that lists every rule the schema could NOT carry
// (docsi/MISSION.md ADR-VAL-002, ADR-VAL-004).
//
// JSON Schema is the seam between Go and every other language: a Go program
// can emit a JSON document deterministically, and the TypeScript runtime,
// Ajv, OpenAPI tooling and other-language validators all consume it. It is
// deliberately a LOSSY projection of the IR — custom functions, server
// round-trips, cross-field comparisons and value transforms have no JSON
// Schema form — and the manifest is what keeps that loss visible instead of
// silent (skeleton honesty: never drop a rule without reporting it).
//
// Determinism is a contract: the same IR produces byte-identical output so
// generated files diff cleanly. Keys are emitted in a fixed authoring order
// via [Object]; maps are walked in sorted order.
//
// Two dialects: draft 2020-12 (the default; $defs, prefixItems-era
// keywords) and draft-07 (definitions, for Ajv-classic and OpenAPI 3.0
// consumers). The IR vocabulary was chosen to match JSON Schema keywords,
// so most rules are a rename; the exceptions are listed in the manifest.
package jsonschema

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ubgo/validator/ir"
)

// Draft selects the JSON Schema dialect.
type Draft string

const (
	// Draft2020 is JSON Schema 2020-12, the default.
	Draft2020 Draft = "https://json-schema.org/draft/2020-12/schema"

	// Draft07 is JSON Schema draft-07, for Ajv in classic mode and
	// OpenAPI 3.0 consumers.
	Draft07 Draft = "http://json-schema.org/draft-07/schema#"
)

// defsKey returns the definitions container keyword for the dialect.
func (d Draft) defsKey() string {
	if d == Draft07 {
		return "definitions"
	}
	return "$defs"
}

// refPrefix returns the $ref prefix into the definitions container.
func (d Draft) refPrefix() string { return "#/" + d.defsKey() + "/" }

// Option configures an emission.
type Option func(*emitter)

// WithDraft selects the dialect; the default is [Draft2020].
func WithDraft(d Draft) Option { return func(e *emitter) { e.draft = d } }

// WithID sets the schema's $id.
func WithID(id string) Option { return func(e *emitter) { e.id = id } }

// Result is one emission: the schema document and its manifest.
type Result struct {
	// Schema is the JSON Schema document, ready to marshal.
	Schema *Object

	// Manifest lists everything the schema could not express.
	Manifest Manifest
}

// JSON renders the schema with two-space indentation and a trailing
// newline — the form generated files are written in.
func (r *Result) JSON() ([]byte, error) {
	data, err := json.MarshalIndent(r.Schema, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Emit projects an IR schema to JSON Schema. It checks the IR first; a
// document that fails [ir.Check] is refused rather than partially emitted.
func Emit(s *ir.Schema, opts ...Option) (*Result, error) {
	if err := ir.Check(s); err != nil {
		return nil, err
	}
	e := &emitter{draft: Draft2020, schema: s}
	for _, o := range opts {
		o(e)
	}
	root := NewObject()
	root.Set("$schema", string(e.draft))
	if e.id != "" {
		root.Set("$id", e.id)
	}
	if s.Meta.Title != "" {
		root.Set("title", s.Meta.Title)
	}
	body := e.node(rootPath, s.Root)
	root.Merge(body)
	if len(s.Defs) > 0 {
		defs := NewObject()
		for _, name := range sortedKeys(s.Defs) {
			defs.Set(name, e.node(rootPath+"."+name, s.Defs[name]))
		}
		root.Set(e.draft.defsKey(), defs)
	}
	return &Result{Schema: root, Manifest: e.manifest}, nil
}

// rootPath is the JSONPath of the root value in manifest entries.
const rootPath = "$"

// emitter carries emission state: the dialect and the growing manifest.
type emitter struct {
	draft    Draft
	id       string
	schema   *ir.Schema
	manifest Manifest
}

// Object is an insertion-ordered JSON object. encoding/json sorts map
// keys, which is deterministic but reads badly ("additionalProperties"
// before "type"); this keeps the authoring order so emitted schemas read
// the way a person would write them, while staying byte-stable.
type Object struct {
	keys []string
	vals map[string]any
}

// NewObject returns an empty ordered object.
func NewObject() *Object { return &Object{vals: map[string]any{}} }

// Set adds or replaces a key, keeping first-insertion order.
func (o *Object) Set(key string, value any) *Object {
	if _, exists := o.vals[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = value
	return o
}

// Get reads a key.
func (o *Object) Get(key string) (any, bool) {
	v, ok := o.vals[key]
	return v, ok
}

// Keys returns the keys in insertion order.
func (o *Object) Keys() []string { return append([]string{}, o.keys...) }

// Len returns the number of keys.
func (o *Object) Len() int { return len(o.keys) }

// Merge copies every key of other into o, preserving other's order for
// new keys.
func (o *Object) Merge(other *Object) *Object {
	for _, k := range other.keys {
		o.Set(k, other.vals[k])
	}
	return o
}

// MarshalJSON implements json.Marshaler in insertion order.
func (o *Object) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(o.vals[k])
		if err != nil {
			return nil, fmt.Errorf("jsonschema: key %q: %w", k, err)
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON implements json.Unmarshaler, preserving key order so a
// round trip of an emitted document is stable.
func (o *Object) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok != json.Delim('{') {
		return fmt.Errorf("jsonschema: expected object, got %v", tok)
	}
	o.keys, o.vals = nil, map[string]any{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := keyTok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		val, err := decodeValue(raw)
		if err != nil {
			return err
		}
		o.Set(key, val)
	}
	_, err = dec.Token()
	return err
}

// decodeValue decodes nested objects as *Object so order survives.
func decodeValue(raw json.RawMessage) (any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		obj := NewObject()
		if err := obj.UnmarshalJSON(trimmed); err != nil {
			return nil, err
		}
		return obj, nil
	}
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, err
		}
		out := make([]any, 0, len(items))
		for _, it := range items {
			v, err := decodeValue(it)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// TSModule renders the result as a TypeScript module: the schema and the
// manifest as `as const` literals. A JSON file loses its literal types on
// import, but a const module keeps them, which is what lets the runtime
// infer the validated value's type from the schema (json-schema-to-ts's
// FromSchema) instead of asking the caller for it.
//
// name is the identifier prefix: "user" gives userSchema and userManifest.
func (r *Result) TSModule(name string) ([]byte, error) {
	if name == "" {
		return nil, fmt.Errorf("jsonschema: TSModule needs a name")
	}
	// json-schema-to-ts does not follow $ref into $defs, so the module
	// carries a copy with every non-recursive ref inlined; the document is
	// still valid JSON Schema and validates identically, it just repeats
	// shared definitions. Recursive refs stay as $ref (and infer unknown).
	schemaJSON, err := json.MarshalIndent(Inline(r.Schema), "", "  ")
	if err != nil {
		return nil, err
	}
	manifestJSON, err := json.MarshalIndent(r.Manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString("// Code generated by validatorctl ir export; DO NOT EDIT.\n\n")
	fmt.Fprintf(&b, "/** JSON Schema for %s, as a const literal so its value type can be inferred. */\n", name)
	fmt.Fprintf(&b, "export const %sSchema = %s as const;\n\n", name, string(schemaJSON))
	fmt.Fprintf(&b, "/** Portability manifest for %s: every rule the schema could not carry. */\n", name)
	fmt.Fprintf(&b, "export const %sManifest = %s as const;\n", name, string(manifestJSON))
	return b.Bytes(), nil
}

// Inline returns a copy of a schema document with every $ref into its
// definitions container replaced by the referenced definition, except refs
// that would recurse, which are kept. The defs container is dropped when
// nothing refers to it any more.
func Inline(doc *Object) *Object {
	var defs *Object
	var defsKey string
	for _, key := range []string{"$defs", "definitions"} {
		if d, ok := doc.Get(key); ok {
			if o, isObj := d.(*Object); isObj {
				defs, defsKey = o, key
			}
		}
	}
	out := inlineValue(doc, defs, defsKey, map[string]bool{}).(*Object)
	if defs != nil && !stillReferenced(out, defsKey) {
		trimmed := NewObject()
		for _, k := range out.Keys() {
			if k != defsKey {
				v, _ := out.Get(k)
				trimmed.Set(k, v)
			}
		}
		return trimmed
	}
	return out
}

// inlineValue deep-copies v, expanding refs; active tracks defs on the
// current expansion path so a cycle is left as a $ref.
func inlineValue(v any, defs *Object, defsKey string, active map[string]bool) any {
	switch x := v.(type) {
	case *Object:
		if ref, ok := x.Get("$ref"); ok && x.Len() == 1 && defs != nil {
			if name, isStr := ref.(string); isStr {
				prefix := "#/" + defsKey + "/"
				if len(name) > len(prefix) && name[:len(prefix)] == prefix {
					target := name[len(prefix):]
					if def, found := defs.Get(target); found && !active[target] {
						active[target] = true
						expanded := inlineValue(def, defs, defsKey, active)
						delete(active, target)
						return expanded
					}
				}
			}
		}
		out := NewObject()
		for _, k := range x.Keys() {
			val, _ := x.Get(k)
			if k == defsKey {
				out.Set(k, val)
				continue
			}
			out.Set(k, inlineValue(val, defs, defsKey, active))
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = inlineValue(e, defs, defsKey, active)
		}
		return out
	}
	return v
}

// stillReferenced reports whether any $ref into defsKey survives outside
// the definitions container itself.
func stillReferenced(v any, defsKey string) bool {
	switch x := v.(type) {
	case *Object:
		for _, k := range x.Keys() {
			if k == defsKey {
				continue
			}
			val, _ := x.Get(k)
			if k == "$ref" {
				if s, ok := val.(string); ok && len(s) > 3 && s[:3] == "#/"+defsKey[:1] {
					return true
				}
			}
			if stillReferenced(val, defsKey) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if stillReferenced(e, defsKey) {
				return true
			}
		}
	}
	return false
}
