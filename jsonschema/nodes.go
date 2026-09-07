package jsonschema

import (
	"math"
	"strconv"

	"github.com/ubgo/validator/internal/rules"
	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/verr"
)

// JSON Schema keywords. Named so a typo is a compile error and so the
// draft-07 divergences are visible in one place.
const (
	kwType                 = "type"
	kwProperties           = "properties"
	kwRequired             = "required"
	kwAdditionalProperties = "additionalProperties"
	kwPropertyNames        = "propertyNames"
	kwItems                = "items"
	kwMinItems             = "minItems"
	kwMaxItems             = "maxItems"
	kwUniqueItems          = "uniqueItems"
	kwMinProperties        = "minProperties"
	kwMaxProperties        = "maxProperties"
	kwMinLength            = "minLength"
	kwMaxLength            = "maxLength"
	kwPattern              = "pattern"
	kwFormat               = "format"
	kwEnum                 = "enum"
	kwConst                = "const"
	kwMinimum              = "minimum"
	kwMaximum              = "maximum"
	kwExclusiveMinimum     = "exclusiveMinimum"
	kwExclusiveMaximum     = "exclusiveMaximum"
	kwMultipleOf           = "multipleOf"
	kwAnyOf                = "anyOf"
	kwOneOf                = "oneOf"
	kwAllOf                = "allOf"
	kwNot                  = "not"
	kwIf                   = "if"
	kwThen                 = "then"
	kwElse                 = "else"
	kwRef                  = "$ref"
	kwSchema               = "$schema"
	kwID                   = "$id"
	kwTitle                = "title"
	kwDescription          = "description"
	kwExamples             = "examples"
	kwDefault              = "default"
	kwContentEncoding      = "contentEncoding"

	// Vendor extensions carrying the IR's error metadata so the TypeScript
	// runtime can key errors on the same codes as Go. JSON Schema permits
	// unknown keywords; validators ignore them.
	kwXCode    = "x-code"
	kwXMessage = "x-message"
)

// Type names.
const (
	tyObject  = "object"
	tyString  = "string"
	tyInteger = "integer"
	tyNumber  = "number"
	tyBoolean = "boolean"
	tyArray   = "array"
	tyNull    = "null"
)

// Format values.
const (
	fmtEmail    = "email"
	fmtURI      = "uri"
	fmtUUID     = "uuid"
	fmtDateTime = "date-time"
	fmtDate     = "date"
	fmtIPv4     = "ipv4"
	fmtIPv6     = "ipv6"
	fmtHostname = "hostname"
	encBase64   = "base64"
)

// ulidPattern is the ULID grammar; JSON Schema has no ulid format.
const ulidPattern = `^[0-7][0-9A-HJKMNP-TV-Z]{25}$`

// node emits the schema for one IR node at path.
func (e *emitter) node(path string, n ir.TypeNode) *Object {
	var out *Object
	switch t := n.(type) {
	case *ir.ObjectNode:
		out = e.object(path, t)
		// Object-level rules go through the same path as any other rule,
		// so a custom or async one lands in the manifest rather than
		// vanishing. Emitting the object first means e.obj is set, which
		// object rules do not need but the shared helper assumes.
		e.rules(path, out, t.Rules)
	case *ir.StringNode:
		out = NewObject().Set(kwType, tyString)
		e.rules(path, out, t.Rules)
	case *ir.IntNode:
		out = NewObject().Set(kwType, tyInteger)
		e.intBounds(out, t)
		e.rules(path, out, t.Rules)
	case *ir.FloatNode:
		out = NewObject().Set(kwType, tyNumber)
		e.rules(path, out, t.Rules)
	case *ir.BoolNode:
		out = NewObject().Set(kwType, tyBoolean)
		e.rules(path, out, t.Rules)
	case *ir.BytesNode:
		out = NewObject().Set(kwType, tyString).Set(kwContentEncoding, encBase64)
		e.rules(path, out, t.Rules)
	case *ir.TimeNode:
		out = NewObject().Set(kwType, tyString).Set(kwFormat, fmtDateTime)
		e.rules(path, out, t.Rules)
	case *ir.DurationNode:
		// Nanoseconds as an integer: the encoding Go's time.Duration
		// marshals to, and the one the interpreter accepts (IR.md Q3).
		out = NewObject().Set(kwType, tyInteger).Set(kwDescription, "duration in nanoseconds")
		e.rules(path, out, t.Rules)
	case *ir.ArrayNode:
		out = NewObject().Set(kwType, tyArray)
		out.Set(kwItems, e.node(path+"[*]", t.Elem))
		e.rules(path, out, t.Rules)
	case *ir.MapNode:
		out = NewObject().Set(kwType, tyObject)
		out.Set(kwPropertyNames, e.node(path+".*", t.Key))
		out.Set(kwAdditionalProperties, e.node(path+".*", t.Val))
		e.rules(path, out, t.Rules)
	case *ir.UnionNode:
		variants := make([]any, 0, len(t.Variants))
		for i, v := range t.Variants {
			variants = append(variants, e.node(path+"|"+strconv.Itoa(i), v))
		}
		out = NewObject().Set(kwAnyOf, variants)
	case *ir.OneOfNode:
		out = e.oneOf(path, t)
	case *ir.RefNode:
		out = NewObject().Set(kwRef, e.draft.refPrefix()+t.Ref)
	case *ir.AnyNode:
		out = NewObject()
	}
	e.meta(out, nodeMeta(n))
	if nullableOf(n) {
		out = nullable(out)
	}
	return out
}

// nullable widens a schema to accept null: a scalar "type" becomes
// [type, "null"]; anything else (a $ref, an anyOf, an object with allOf)
// is wrapped in anyOf.
func nullable(o *Object) *Object {
	if t, ok := o.Get(kwType); ok {
		if s, isStr := t.(string); isStr && o.Len() == 1 {
			return NewObject().Set(kwType, []any{s, tyNull})
		}
	}
	return NewObject().Set(kwAnyOf, []any{o, NewObject().Set(kwType, tyNull)})
}

// meta emits description and examples.
func (e *emitter) meta(o *Object, m ir.NodeMeta) {
	if m.Description != "" {
		o.Set(kwDescription, m.Description)
	}
	if len(m.Examples) > 0 {
		o.Set(kwExamples, m.Examples)
	}
}

// intBounds derives minimum/maximum from the IR's bit width and sign.
// Only widths JSON numbers represent exactly get bounds (≤ 32 bits, and
// the unsigned floor); 64-bit extremes exceed 2^53 and would be lies.
func (e *emitter) intBounds(o *Object, t *ir.IntNode) {
	if t.Unsigned {
		o.Set(kwMinimum, 0)
	}
	if t.Bits == 0 || t.Bits > 32 {
		return
	}
	if t.Unsigned {
		o.Set(kwMaximum, math.Pow(2, float64(t.Bits))-1)
		return
	}
	half := math.Pow(2, float64(t.Bits-1))
	o.Set(kwMinimum, -half)
	o.Set(kwMaximum, half-1)
}

// rules emits each rule as a keyword or records it in the manifest. A
// rule carrying a condition cannot be a bare keyword; the enclosing object
// lifts it into if/then (see object), so here it is skipped.
func (e *emitter) rules(path string, o *Object, rules []ir.Rule) {
	for _, r := range rules {
		if r.Cond != nil {
			continue
		}
		e.rule(path, o, r)
	}
}

// rule emits one unconditional rule.
func (e *emitter) rule(path string, o *Object, r ir.Rule) {
	switch r.Kind {
	case ir.RuleMinLength:
		if _, base64 := o.Get(kwContentEncoding); base64 {
			e.lossy(path, r, "byte length has no exact base64 equivalent")
			return
		}
		o.Set(kwMinLength, r.Params[ir.ParamValue])
	case ir.RuleMaxLength:
		if _, base64 := o.Get(kwContentEncoding); base64 {
			e.lossy(path, r, "byte length has no exact base64 equivalent")
			return
		}
		o.Set(kwMaxLength, r.Params[ir.ParamValue])
	case ir.RuleMinItems:
		if t, _ := o.Get(kwType); t == tyObject {
			o.Set(kwMinProperties, r.Params[ir.ParamValue])
		} else {
			o.Set(kwMinItems, r.Params[ir.ParamValue])
		}
	case ir.RuleMaxItems:
		if t, _ := o.Get(kwType); t == tyObject {
			o.Set(kwMaxProperties, r.Params[ir.ParamValue])
		} else {
			o.Set(kwMaxItems, r.Params[ir.ParamValue])
		}
	case ir.RuleUnique:
		o.Set(kwUniqueItems, true)
	case ir.RuleMin, ir.RuleGte:
		o.Set(kwMinimum, r.Params[ir.ParamValue])
	case ir.RuleMax, ir.RuleLte:
		o.Set(kwMaximum, r.Params[ir.ParamValue])
	case ir.RuleGt:
		o.Set(kwExclusiveMinimum, r.Params[ir.ParamValue])
	case ir.RuleLt:
		o.Set(kwExclusiveMaximum, r.Params[ir.ParamValue])
	case ir.RuleMultipleOf:
		o.Set(kwMultipleOf, r.Params[ir.ParamValue])
	case ir.RulePattern:
		o.Set(kwPattern, r.Params[ir.ParamRegex])
	case ir.RuleEnum:
		o.Set(kwEnum, r.Params[ir.ParamValues])
	case ir.RuleConst:
		o.Set(kwConst, r.Params[ir.ParamValue])
	case ir.RuleEmail:
		o.Set(kwFormat, fmtEmail)
	case ir.RuleURL:
		o.Set(kwFormat, fmtURI)
	case ir.RuleUUID:
		o.Set(kwFormat, fmtUUID)
	case ir.RuleULID:
		o.Set(kwPattern, ulidPattern)
	case ir.RuleIPv4:
		o.Set(kwFormat, fmtIPv4)
	case ir.RuleIPv6:
		o.Set(kwFormat, fmtIPv6)
	case ir.RuleHostname:
		o.Set(kwFormat, fmtHostname)
	case ir.RuleDate:
		o.Set(kwFormat, fmtDate)
	case ir.RuleDateTime:
		o.Set(kwFormat, fmtDateTime)
	case ir.RuleIP:
		// No single format covers both families; anyOf of the two formats.
		o.Set(kwAnyOf, []any{NewObject().Set(kwFormat, fmtIPv4), NewObject().Set(kwFormat, fmtIPv6)})
	case ir.RuleCIDR:
		o.Set(kwPattern, rules.CIDRPattern)
	case ir.RuleMAC:
		o.Set(kwPattern, rules.MACPattern)
	case ir.RuleBase64:
		o.Set(kwPattern, rules.Base64Pattern)
	case ir.RuleSemver:
		o.Set(kwPattern, rules.SemverPattern)
	case ir.RuleNotBlank:
		o.Set(kwPattern, rules.NotBlankPattern)
	case ir.RuleJSON:
		e.lossy(path, r, "JSON Schema cannot parse a string as JSON; checked server-side")
	case ir.RuleCreditCard:
		// The digit shape exports; the Luhn checksum cannot, and is reported.
		o.Set(kwPattern, rules.CreditCardPattern)
		e.lossy(path, r, "pattern carries the digit shape only; the Luhn checksum is checked server-side")
	case ir.RuleJWT:
		o.Set(kwPattern, rules.JWTPattern)
	case ir.RuleCountryCode:
		o.Set(kwPattern, rules.CountryCodePattern)
	case ir.RuleCurrencyCode:
		o.Set(kwPattern, rules.CurrencyCodePattern)
	case ir.RuleHexColor:
		o.Set(kwPattern, rules.HexColorPattern)
	case ir.RuleE164:
		o.Set(kwPattern, rules.E164Pattern)
	case ir.RuleLatitude:
		o.Set(kwMinimum, -90)
		o.Set(kwMaximum, 90)
	case ir.RuleLongitude:
		o.Set(kwMinimum, -180)
		o.Set(kwMaximum, 180)
	case ir.RuleAfter, ir.RuleBefore:
		field, _ := r.Params[ir.ParamField].(string)
		entry := Entry{Path: path, Kind: KindRule, Name: string(r.Kind), Class: ClassCEL, Ref: field,
			Reason: "JSON Schema cannot compare two properties; carried as a condition a runtime can evaluate"}
		// e.obj is the object being emitted, so the sibling's wire name is
		// resolvable here; without it the entry is still reported, just not
		// executable.
		if e.obj != nil {
			entry.Cond = crossFieldCond(e.obj, lastSeg(path), r)
			// The same code and wording the Go executors produce, so the
			// browser reports the identical issue.
			code, tmpl := rules.TextFor(&r)
			// LabelParams first: the message names the sibling by its wire
			// name, which is what the browser and the path both use.
			entry.Code, entry.Message = code, verr.Render(tmpl, rules.LabelParams(&r, r.Params))
		}
		e.manifest.Add(entry)
	case ir.RuleCustom, ir.RuleAsync:
		class, _ := r.EffectivePortability()
		ref, _ := r.Params[ir.ParamRef].(string)
		endpoint, _ := r.Params[ir.ParamEndpoint].(string)
		reason := "custom Go function runs server-side only"
		if r.Kind == ir.RuleAsync {
			reason = "async rule runs server-side; frontend round-trips"
		}
		e.manifest.Add(Entry{Path: path, Kind: KindRule, Name: string(r.Kind), Class: Class(class), Ref: ref, Endpoint: endpoint, Reason: reason})
	}
	// Error metadata rides along as vendor keywords so the TS runtime can
	// report the same code the Go side does.
	if r.Meta.Code != "" {
		o.Set(kwXCode, r.Meta.Code)
	}
	if r.Meta.Message != "" {
		o.Set(kwXMessage, r.Meta.Message)
	}
}

// lossy records a rule the schema approximates or omits.
func (e *emitter) lossy(path string, r ir.Rule, reason string) {
	e.manifest.Add(Entry{Path: path, Kind: KindRule, Name: string(r.Kind), Class: ClassLossy, Reason: reason})
}

// object emits an object node: properties, required, additionalProperties,
// defaults, lifted conditional rules and gates.
func (e *emitter) object(path string, o *ir.ObjectNode) *Object {
	prev := e.obj
	e.obj = o
	defer func() { e.obj = prev }()
	out := NewObject().Set(kwType, tyObject)
	props := NewObject()
	var required []any
	var conds []any
	for i := range o.Fields {
		f := &o.Fields[i]
		wire := wireName(f)
		fp := path + "." + wire
		prop := e.node(fp, f.Type)
		if f.Meta.Description != "" {
			prop.Set(kwDescription, f.Meta.Description)
		}
		for _, t := range f.Transforms {
			if t.Kind == ir.TransformDefault {
				prop.Set(kwDefault, t.Params[ir.ParamValue])
				continue
			}
			if t.Kind == ir.TransformStrict {
				// Strict only pins map input against lax coercion; a JSON
				// Schema with coerceTypes off already behaves that way, and
				// with it on Ajv cannot exempt one property. Reported so the
				// runtime can warn when a strict field sits in a coercing
				// schema.
				e.manifest.Add(Entry{Path: fp, Kind: KindTransform, Name: string(t.Kind), Class: ClassLossy, Reason: "Ajv coerceTypes is global; this field is pinned strict server-side"})
				continue
			}
			reason := "JSON Schema validates, it does not transform; applied server-side"
			if t.Kind == ir.TransformCoerce {
				reason = "JSON Schema does not coerce; the TypeScript runtime enables Ajv coerceTypes for schemas carrying this entry"
			}
			e.manifest.Add(Entry{Path: fp, Kind: KindTransform, Name: string(t.Kind), Class: ClassLossy, Reason: reason})
		}
		props.Set(wire, prop)
		if !f.Optional {
			required = append(required, wire)
		}
		// Conditional rules on the field's own node lift to object level,
		// where the condition can see sibling properties.
		for _, r := range ir.Rules(f.Type) {
			if r.Cond == nil {
				continue
			}
			if c := e.liftedRule(path, o, fp, wire, r); c != nil {
				conds = append(conds, c)
			}
		}
	}
	out.Set(kwProperties, props)
	if len(required) > 0 {
		out.Set(kwRequired, required)
	}
	switch o.Extra {
	case ir.ExtraForbid:
		out.Set(kwAdditionalProperties, false)
	case ir.ExtraAllow:
		out.Set(kwAdditionalProperties, true)
	}
	for _, g := range o.Gated {
		if c := e.gate(path, o, g); c != nil {
			conds = append(conds, c)
		}
	}
	if len(conds) > 0 {
		out.Set(kwAllOf, conds)
	}
	return out
}

// oneOf emits a discriminated union as discriminator-keyed if/then
// clauses inside allOf, never a raw oneOf, so a failing document is told
// which branch it was in (ADR-VAL-007). Without a default the
// discriminator is also constrained to the known tags.
func (e *emitter) oneOf(path string, o *ir.OneOfNode) *Object {
	tags := sortedKeys(o.Cases)
	out := NewObject().Set(kwType, tyObject)
	disc := NewObject()
	if o.Default == nil {
		enum := make([]any, 0, len(tags))
		for _, t := range tags {
			enum = append(enum, t)
		}
		disc.Set(kwEnum, enum)
	} else {
		disc.Set(kwType, tyString)
	}
	out.Set(kwProperties, NewObject().Set(o.Discriminator, disc))
	out.Set(kwRequired, []any{o.Discriminator})
	clauses := make([]any, 0, len(tags)+1)
	for _, tag := range tags {
		cond := NewObject().
			Set(kwProperties, NewObject().Set(o.Discriminator, NewObject().Set(kwConst, tag))).
			Set(kwRequired, []any{o.Discriminator})
		clauses = append(clauses, NewObject().Set(kwIf, cond).Set(kwThen, e.node(path+"["+tag+"]", o.Cases[tag])))
	}
	if o.Default != nil {
		known := make([]any, 0, len(tags))
		for _, t := range tags {
			known = append(known, t)
		}
		notKnown := NewObject().Set(kwNot, NewObject().
			Set(kwProperties, NewObject().Set(o.Discriminator, NewObject().Set(kwEnum, known))).
			Set(kwRequired, []any{o.Discriminator}))
		clauses = append(clauses, NewObject().Set(kwIf, notKnown).Set(kwThen, e.node(path+"[default]", o.Default)))
	}
	out.Set(kwAllOf, clauses)
	return out
}

// typeKeyword returns the JSON Schema "type" for a node's kind, or "" for
// nodes that have no single type (union, oneof, ref, any). Gated and lifted
// rules are emitted on a bare property schema; stamping the type first keeps
// the document valid under Ajv's strictTypes, which refuses a "minLength"
// with no "type" to apply it to.
func typeKeyword(n ir.TypeNode) string {
	switch n.(type) {
	case *ir.ObjectNode, *ir.MapNode:
		return tyObject
	case *ir.StringNode, *ir.BytesNode, *ir.TimeNode:
		return tyString
	case *ir.IntNode, *ir.DurationNode:
		return tyInteger
	case *ir.FloatNode:
		return tyNumber
	case *ir.BoolNode:
		return tyBoolean
	case *ir.ArrayNode:
		return tyArray
	}
	return ""
}

// fieldType looks a Go field name up in an object and returns its type
// keyword, or "" when unknown.
func fieldType(o *ir.ObjectNode, goName string) string {
	for i := range o.Fields {
		if o.Fields[i].Name == goName {
			return typeKeyword(o.Fields[i].Type)
		}
	}
	return ""
}

// wireName is the key a field appears under in JSON.
func wireName(f *ir.FieldNode) string {
	if f.JSONName != "" {
		return f.JSONName
	}
	return f.Name
}

// wireOf resolves a Go field name to its wire name within an object.
func wireOf(o *ir.ObjectNode, goName string) string {
	for i := range o.Fields {
		if o.Fields[i].Name == goName {
			return wireName(&o.Fields[i])
		}
	}
	return goName
}

// nodeMeta reads the shared annotation through the sealed interface.
func nodeMeta(n ir.TypeNode) ir.NodeMeta { return baseOf(n).Meta }

// nullableOf reads the Nullable flag.
func nullableOf(n ir.TypeNode) bool { return baseOf(n).Nullable }

// baseOf returns a copy of the node's Base.
func baseOf(n ir.TypeNode) ir.Base {
	switch t := n.(type) {
	case *ir.ObjectNode:
		return t.Base
	case *ir.StringNode:
		return t.Base
	case *ir.IntNode:
		return t.Base
	case *ir.FloatNode:
		return t.Base
	case *ir.BoolNode:
		return t.Base
	case *ir.BytesNode:
		return t.Base
	case *ir.TimeNode:
		return t.Base
	case *ir.DurationNode:
		return t.Base
	case *ir.ArrayNode:
		return t.Base
	case *ir.MapNode:
		return t.Base
	case *ir.UnionNode:
		return t.Base
	case *ir.OneOfNode:
		return t.Base
	case *ir.RefNode:
		return t.Base
	case *ir.AnyNode:
		return t.Base
	}
	return ir.Base{}
}
