package ir

import (
	"errors"
	"fmt"
	"strings"
)

// Check validates the STRUCTURE of a schema — not any value against it.
//
// It is the gate every consumer runs before trusting a document, whether
// the document was built in Go or decoded from JSON: version matches, every
// kind is known, every ref resolves, every rule carries the params its kind
// requires, portability is never claimed wider than the kind allows, and
// discriminators name real fields. Every finding is reported (errors are
// joined), because a document with three problems should not need three
// round-trips to fix.
func Check(s *Schema) error {
	if s == nil {
		return errors.New("ir: nil schema")
	}
	c := &checker{defs: s.Defs}
	if s.Version != Version {
		c.errf("version", "got %q, want %q", s.Version, Version)
	}
	if s.Root == nil {
		c.errf("root", "missing")
	} else {
		c.node("root", s.Root)
	}
	for _, name := range sortedKeys(s.Defs) {
		c.node("defs."+name, s.Defs[name])
	}
	return errors.Join(c.errs...)
}

// checker accumulates findings during a walk.
type checker struct {
	defs map[string]TypeNode
	errs []error
}

func (c *checker) errf(path, format string, args ...any) {
	c.errs = append(c.errs, fmt.Errorf("ir: %s: %s", path, fmt.Sprintf(format, args...)))
}

func (c *checker) node(path string, n TypeNode) {
	if n == nil {
		c.errf(path, "nil node")
		return
	}
	if _, known := newNode(n.Kind()); !known {
		c.errf(path, "unknown kind %q", n.Kind())
		return
	}
	for i, r := range Rules(n) {
		c.rule(fmt.Sprintf("%s.rules[%d]", path, i), r)
	}
	switch t := n.(type) {
	case *ObjectNode:
		c.object(path, t)
	case *RefNode:
		if _, ok := c.defs[t.Ref]; !ok {
			c.errf(path, "ref %q does not resolve to a def", t.Ref)
		}
	case *OneOfNode:
		c.oneOf(path, t)
	case *ArrayNode:
		c.node(path+".elem", t.Elem)
	case *MapNode:
		c.node(path+".key", t.Key)
		c.node(path+".val", t.Val)
	case *UnionNode:
		if len(t.Variants) == 0 {
			c.errf(path, "union has no variants")
		}
		for i, v := range t.Variants {
			c.node(fmt.Sprintf("%s.variants[%d]", path, i), v)
		}
	}
}

func (c *checker) object(path string, o *ObjectNode) {
	if o.Extra != "" {
		if _, ok := extraPolicies[o.Extra]; !ok {
			c.errf(path, "unknown extra policy %q", o.Extra)
		}
	}
	names := make(map[string]struct{}, len(o.Fields))
	for i, f := range o.Fields {
		fp := fmt.Sprintf("%s.fields[%d]", path, i)
		if f.Name == "" {
			c.errf(fp, "field has no name")
		} else if _, dup := names[f.Name]; dup {
			c.errf(fp, "duplicate field name %q", f.Name)
		}
		names[f.Name] = struct{}{}
		for j, tr := range f.Transforms {
			c.transform(fmt.Sprintf("%s.transforms[%d]", fp, j), tr)
		}
		c.node(fp+".type", f.Type)
	}
	for i, g := range o.Gated {
		c.gate(fmt.Sprintf("%s.gated[%d]", path, i), g, names)
	}
}

func (c *checker) oneOf(path string, o *OneOfNode) {
	if o.Discriminator == "" {
		c.errf(path, "oneof has no discriminator")
	}
	if len(o.Cases) == 0 {
		c.errf(path, "oneof has no cases")
	}
	for _, key := range sortedKeys(o.Cases) {
		cp := fmt.Sprintf("%s.cases[%s]", path, key)
		c.node(cp, o.Cases[key])
		// A variant must be an object carrying the discriminator, otherwise
		// the value could never select it and the emitted if/then/else would
		// be unreachable. The discriminator is a WIRE name (it is what the
		// JSON carries), so match the field's json name first, then its Go
		// name for schemas that declare no wire names.
		if obj, ok := o.Cases[key].(*ObjectNode); ok && o.Discriminator != "" && !hasWireField(obj, o.Discriminator) {
			c.errf(cp, "variant does not declare discriminator field %q", o.Discriminator)
		}
	}
	if o.Default != nil {
		c.node(path+".default", o.Default)
	}
}

// hasWireField matches a wire name against the object's fields: an exact
// json name, or — for fields without one — the Go name compared
// case-insensitively, mirroring encoding/json's key matching.
func hasWireField(o *ObjectNode, wire string) bool {
	for _, f := range o.Fields {
		if f.JSONName == wire || (f.JSONName == "" && strings.EqualFold(f.Name, wire)) {
			return true
		}
	}
	return false
}

func (c *checker) rule(path string, r Rule) {
	spec, known := ruleSpecs[r.Kind]
	if !known {
		c.errf(path, "unknown rule kind %q", r.Kind)
		return
	}
	var missing []string
	for _, key := range spec.required {
		if _, ok := r.Params[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		c.errf(path, "rule %q missing params: %s", r.Kind, strings.Join(missing, ", "))
	}
	if r.Kind == RulePattern {
		if re, ok := r.Params[ParamRegex].(string); ok {
			if err := CheckPattern(re); err != nil {
				c.errf(path, "%v", err)
			}
		}
	}
	if r.Portable != "" {
		if !r.Portable.Valid() {
			c.errf(path, "unknown portability %q", r.Portable)
		} else if rank(r.Portable) < rank(spec.portability) {
			// Claiming a custom rule is portable is precisely the silent-drop
			// bug; a document may narrow portability, never widen it.
			c.errf(path, "rule %q cannot be %q (kind is inherently %q)", r.Kind, r.Portable, spec.portability)
		}
	}
	if r.Cond != nil {
		c.cond(path+".cond", *r.Cond)
	}
}

// rank orders portability classes from most to least portable so "narrow
// only" is a single comparison.
func rank(p Portability) int {
	switch p {
	case Portable:
		return 0
	case Wasm:
		return 1
	case Opaque:
		return 2
	case Server:
		return 3
	}
	return -1
}

func (c *checker) cond(path string, k Condition) {
	if _, ok := condKinds[k.Kind]; !ok {
		c.errf(path, "unknown condition kind %q", k.Kind)
		return
	}
	switch k.Kind {
	case CondCmp, CondFieldCmp:
		if k.Field == "" {
			c.errf(path, "%s needs field", k.Kind)
		}
		if _, ok := cmpOps[CmpOp(k.Op)]; !ok {
			c.errf(path, "unknown comparison op %q", k.Op)
		}
		if k.Kind == CondFieldCmp && k.FieldB == "" {
			c.errf(path, "fieldcmp needs fieldB")
		}
	case CondPresence:
		if k.Field == "" {
			c.errf(path, "presence needs field")
		}
		if _, ok := presenceOps[PresenceOp(k.Op)]; !ok {
			c.errf(path, "unknown presence op %q", k.Op)
		}
	case CondIn:
		if k.Field == "" {
			c.errf(path, "in needs field")
		}
		if len(k.Values) == 0 {
			c.errf(path, "in needs values")
		}
	case CondAnd, CondOr:
		if len(k.Sub) == 0 {
			c.errf(path, "%s needs sub-conditions", k.Kind)
		}
	case CondNot:
		if len(k.Sub) != 1 {
			c.errf(path, "not needs exactly one sub-condition, got %d", len(k.Sub))
		}
	case CondExpr:
		if k.CEL == "" {
			c.errf(path, "expr needs cel")
		}
	case CondCustom:
		if k.Ref == "" {
			c.errf(path, "custom needs ref")
		}
	}
	for i, sub := range k.Sub {
		c.cond(fmt.Sprintf("%s.sub[%d]", path, i), sub)
	}
}

func (c *checker) gate(path string, g GatedRules, fields map[string]struct{}) {
	if _, ok := gateKinds[g.Kind]; !ok {
		c.errf(path, "unknown gate kind %q", g.Kind)
		return
	}
	switch g.Kind {
	case GateWhen:
		if g.Cond == nil {
			c.errf(path, "when needs cond")
		}
		if len(g.Then) == 0 && len(g.Else) == 0 {
			c.errf(path, "when has neither then nor else")
		}
	case GateRequires:
		if g.Cond == nil {
			c.errf(path, "requires needs cond")
		}
		if len(g.Fields) == 0 {
			c.errf(path, "requires needs fields")
		}
	case GateGroup:
		if _, ok := groupModes[g.Mode]; !ok {
			c.errf(path, "unknown group mode %q", g.Mode)
		}
		if len(g.Fields) < 2 {
			c.errf(path, "group needs at least two fields")
		}
	}
	if g.Cond != nil {
		c.cond(path+".cond", *g.Cond)
	}
	for _, name := range g.Fields {
		if _, ok := fields[name]; !ok {
			c.errf(path, "gate names unknown field %q", name)
		}
	}
	for i, tr := range append(append([]TargetRule{}, g.Then...), g.Else...) {
		tp := fmt.Sprintf("%s.rules[%d]", path, i)
		if _, ok := fields[tr.Field]; !ok {
			c.errf(tp, "targets unknown field %q", tr.Field)
		}
		c.rule(tp, tr.Rule)
	}
}

func (c *checker) transform(path string, t Transform) {
	if _, ok := transformKinds[t.Kind]; !ok {
		c.errf(path, "unknown transform kind %q", t.Kind)
	}
	if _, ok := phases[t.Phase]; !ok {
		c.errf(path, "unknown transform phase %q", t.Phase)
	}
}
