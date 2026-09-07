package jsonschema

import (
	"reflect"
	"strings"

	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/verr"
)

// The IR contract: a JSON Schema OF the IR's own serialized form
// (docsi/IR.md §0, third row). Non-Go tools — the TypeScript runtime, a
// Python emitter, an editor — validate and type IR documents against it,
// so the wire format is a checked contract rather than prose.
//
// It is derived from the Go types by reflection so it cannot drift from
// them: a new field on a node struct appears here on the next run. The
// only hand-written part is the TypeNode union, because an interface has
// no reflectable member list; it is enumerated from the same kind → struct
// table the decoder dispatches on.

// ContractID is the $id of the contract document.
const ContractID = "https://ubgo.dev/schemas/validator-ir-v1.json"

// Wire field names the contract refers to by name: the discriminator every
// node carries and the document version. They are the json tags of
// ir.TypeNode implementations and ir.Schema; TestContract pins them to the
// tags so a rename on the Go side fails here instead of drifting.
const (
	wireKind    = "kind"
	wireVersion = "version"
)

// contractDraft is the dialect the contract is written in.
const contractDraft = Draft2020

// Contract builds the JSON Schema for [ir.Schema].
func Contract() *Object {
	c := &contract{defs: NewObject(), building: map[string]bool{}}
	// The root is inlined, not a def: a document IS a Schema, so a $ref
	// to itself would only add indirection.
	root := c.structOf(reflect.TypeFor[ir.Schema]())
	doc := NewObject().
		Set(kwSchema, string(contractDraft)).
		Set(kwID, ContractID).
		Set(kwTitle, "ubgo/validator IR "+ir.Version).
		Set(kwDescription, "Serialized rule-tree IR. Version, root and defs; nodes are discriminated by \""+wireKind+"\".")
	doc.Merge(root)
	doc.Set(kwAllOf, []any{NewObject().Set(kwProperties, NewObject().Set(wireVersion, NewObject().Set(kwConst, ir.Version)))})
	c.typeNodeDef()
	doc.Set(contractDraft.defsKey(), c.defs)
	return doc
}

type contract struct {
	defs *Object
	// building guards recursion through Condition.sub and TypeNode: a type
	// being registered is referenced, not re-entered.
	building map[string]bool
}

// nodeKinds enumerates the union members with their wire kinds, the same
// set newNode dispatches on.
var nodeKinds = []struct {
	kind ir.Kind
	typ  reflect.Type
}{
	{ir.KindObject, reflect.TypeFor[ir.ObjectNode]()},
	{ir.KindString, reflect.TypeFor[ir.StringNode]()},
	{ir.KindInt, reflect.TypeFor[ir.IntNode]()},
	{ir.KindFloat, reflect.TypeFor[ir.FloatNode]()},
	{ir.KindBool, reflect.TypeFor[ir.BoolNode]()},
	{ir.KindBytes, reflect.TypeFor[ir.BytesNode]()},
	{ir.KindTime, reflect.TypeFor[ir.TimeNode]()},
	{ir.KindDuration, reflect.TypeFor[ir.DurationNode]()},
	{ir.KindArray, reflect.TypeFor[ir.ArrayNode]()},
	{ir.KindMap, reflect.TypeFor[ir.MapNode]()},
	{ir.KindUnion, reflect.TypeFor[ir.UnionNode]()},
	{ir.KindOneOf, reflect.TypeFor[ir.OneOfNode]()},
	{ir.KindRef, reflect.TypeFor[ir.RefNode]()},
	{ir.KindAny, reflect.TypeFor[ir.AnyNode]()},
}

var typeNodeIface = reflect.TypeFor[ir.TypeNode]()

// typeNodeDefName is the def holding the discriminated union.
const typeNodeDefName = "TypeNode"

// typeNodeDef emits the discriminated union and one def per node kind.
func (c *contract) typeNodeDef() {
	variants := make([]any, 0, len(nodeKinds))
	for _, nk := range nodeKinds {
		name := nk.typ.Name()
		obj := c.structOf(nk.typ)
		props, _ := obj.Get(kwProperties)
		po := props.(*Object)
		// Prepend the discriminator so it reads first.
		withKind := NewObject().Set(wireKind, NewObject().Set(kwConst, string(nk.kind)))
		withKind.Merge(po)
		obj.Set(kwProperties, withKind)
		req, _ := obj.Get(kwRequired)
		reqs := []any{wireKind}
		if r, ok := req.([]any); ok {
			reqs = append(reqs, r...)
		}
		obj.Set(kwRequired, reqs)
		obj.Set(kwAdditionalProperties, false)
		c.defs.Set(name, obj)
		variants = append(variants, NewObject().Set(kwRef, contractDraft.refPrefix()+name))
	}
	c.defs.Set(typeNodeDefName, NewObject().
		Set(kwDescription, "A type node, discriminated by kind.").
		Set(kwOneOf, variants))
}

// typeOf maps a Go type to a schema, registering named structs as defs.
func (c *contract) typeOf(t reflect.Type) *Object {
	switch {
	case t == typeNodeIface:
		return NewObject().Set(kwRef, contractDraft.refPrefix()+typeNodeDefName)
	case t.Kind() == reflect.Pointer:
		return c.typeOf(t.Elem())
	case t.Kind() == reflect.Struct:
		name := t.Name()
		if name == "" || t.PkgPath() == "" {
			return c.structOf(t)
		}
		if _, done := c.defs.Get(name); !done && !c.building[name] {
			c.building[name] = true
			c.defs.Set(name, c.structOf(t))
			delete(c.building, name)
		}
		return NewObject().Set(kwRef, contractDraft.refPrefix()+name)
	case t.Kind() == reflect.Slice:
		return NewObject().Set(kwType, tyArray).Set(kwItems, c.typeOf(t.Elem()))
	case t.Kind() == reflect.Map:
		return NewObject().Set(kwType, tyObject).Set(kwAdditionalProperties, c.typeOf(t.Elem()))
	case t.Kind() == reflect.String:
		return NewObject().Set(kwType, tyString)
	case t.Kind() == reflect.Bool:
		return NewObject().Set(kwType, tyBoolean)
	case t.Kind() >= reflect.Int && t.Kind() <= reflect.Uint64:
		return NewObject().Set(kwType, tyInteger)
	case t.Kind() == reflect.Float32 || t.Kind() == reflect.Float64:
		return NewObject().Set(kwType, tyNumber)
	}
	// interface{} and anything else: unconstrained.
	return NewObject()
}

// structOf maps a struct's exported json-tagged fields to properties,
// flattening embedded structs (Base) the way encoding/json does.
func (c *contract) structOf(t reflect.Type) *Object {
	props := NewObject()
	var required []any
	c.collectFields(t, props, &required)
	obj := NewObject().Set(kwType, tyObject).Set(kwProperties, props)
	if len(required) > 0 {
		obj.Set(kwRequired, required)
	}
	return obj
}

func (c *contract) collectFields(t reflect.Type, props *Object, required *[]any) {
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			c.collectFields(f.Type, props, required)
			continue
		}
		tag := f.Tag.Get("json")
		name, opts, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		props.Set(name, c.typeOf(f.Type))
		// A field is required on the wire when encoding/json always writes
		// it: no omitempty / omitzero option.
		if !strings.Contains(opts, "omitempty") && !strings.Contains(opts, "omitzero") {
			*required = append(*required, name)
		}
	}
}

// ErrorContractID is the $id of the error-model document.
const ErrorContractID = "https://ubgo.dev/schemas/validator-errors-v1.json"

// ErrorContract builds the JSON Schema for [verr.Errors], the shape every
// executor returns and every consumer parses.
//
// Why it exists: the TypeScript runtime declares its own `Issue`
// interface, by hand, and a Go handler's 422 body is read by whatever the
// caller wrote. Three descriptions of one shape, two of them
// hand-maintained, is how a `params` field quietly becomes optional on one
// side and required on the other. This one is reflected from the Go
// struct, so it cannot drift from what is actually sent.
//
// It is a contract for the wire, not a validator you run: the issues a
// validator produces are already well-formed. Use it to type a client, to
// document a 422 response in OpenAPI, or to check a hand-rolled consumer.
func ErrorContract() *Object {
	c := &contract{defs: NewObject(), building: map[string]bool{}}
	root := c.structOf(reflect.TypeFor[verr.Errors]())
	doc := NewObject().
		Set(kwSchema, string(contractDraft)).
		Set(kwID, ErrorContractID).
		Set(kwTitle, "ubgo/validator errors").
		Set(kwDescription, "The body a failed validation returns: a list of issues, each locating one failure and naming it by a code a translator can key on.")
	doc.Merge(root)

	// The reflector cannot know these, because Go's type system does not
	// carry them: a code is any string, but the structural ones are worth
	// naming, and params is a free-form map by design.
	if issue, ok := c.defs.Get("Issue"); ok {
		annotateIssue(issue.(*Object))
	}
	doc.Set(contractDraft.defsKey(), c.defs)
	return doc
}

// structuralCodes are the codes an executor emits without a rule. They are
// listed in the schema as documentation, not as an enum: a rule code or a
// caller's own code from Msg is equally valid, so constraining the field
// would reject perfectly good issues.
var structuralCodes = []any{
	verr.CodeRequired, verr.CodeType, verr.CodeUnknownKey,
	verr.CodeVariant, verr.CodeUnion, verr.CodeGroup, verr.CodeCustom,
}

// annotateIssue adds what reflection cannot see.
func annotateIssue(issue *Object) {
	props, ok := issue.Get(kwProperties)
	if !ok {
		return
	}
	p := props.(*Object)
	if code, found := p.Get("code"); found {
		code.(*Object).
			Set(kwDescription, "Machine-readable identifier for the failure. Rule kinds have default codes (min_length, email); a rule's Meta.Code overrides; the structural values below are emitted without a rule. Not constrained to a closed set — a caller's own code is equally valid.").
			Set(kwExamples, structuralCodes)
	}
	if path, found := p.Get("path"); found {
		path.(*Object).Set(kwDescription, `JSONPath of the failing value, in wire names: "$.tags[0].name". "$" is the whole value.`)
	}
	if msg, found := p.Get("message"); found {
		msg.(*Object).Set(kwDescription, "Rendered English message. A localizing consumer should ignore it and render from code plus params instead.")
	}
	if rule, found := p.Get("rule"); found {
		rule.(*Object).Set(kwDescription, "The rule kind that failed. Absent for structural failures, which no rule produced.")
	}
	if params, found := p.Get("params"); found {
		params.(*Object).Set(kwDescription, "Template variables for the message: the rule's operands, plus context such as the sibling field a cross-field rule names.")
	}
}
