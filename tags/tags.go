// Package tags migrates go-playground/validator struct tags into the IR
// (docsi/MISSION.md ADR-VAL-006: tags are a migration INPUT, never a
// runtime surface).
//
// A team on go-playground has hundreds of `validate:"required,email"` tags
// and no way to export them. Migrate reads the tags from source, maps every
// tag it understands onto IR rules, gates and field attributes, and lists
// every tag it does not understand per field. Nothing is dropped silently:
// the report is part of the result and the CLI fails on it unless told to
// accept a partial migration. The output is an IR document (exportable to
// JSON Schema and TypeScript immediately) and, through [Builder], the
// equivalent `vb` source so the team can delete the tags and keep editing
// rules as code.
//
// Mapping choices that differ from a naive reading of go-playground:
//
//   - A field without `required` is Optional in the IR. go-playground skips
//     zero values unless `required` is present, and the IR's presence rule
//     mirrors that.
//   - `min`/`max`/`len`/`gt`/... on strings and collections are length
//     rules; on numbers they are bounds — decided by the field's Go type,
//     which is why the migration reads source rather than tag strings alone.
//   - `dive` splits a tag list: everything before applies to the collection,
//     everything after to each element.
//   - Nested structs from the same package become Refs to their own
//     migrated schema; go-playground validates them implicitly, and so does
//     the IR.
package tags

import (
	"errors"
	"fmt"
	"go/ast"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ubgo/validator/accessors"
	"github.com/ubgo/validator/ir"
)

// TagKey is the struct tag key go-playground reads.
const TagKey = "validate"

// Options configures a migration.
type Options struct {
	// Dir is the package directory. Required.
	Dir string

	// Type is the root struct to migrate. Required. Structs it references
	// (directly, through pointers, slices and maps) are migrated into Defs.
	Type string

	// Generator is recorded in the schema's metadata.
	Generator string
}

// Unsupported is a tag the migration could not express, with where it was.
type Unsupported struct {
	Type  string
	Field string
	Tag   string
	// Reason says why: unknown tag, needs a type the IR lacks, etc.
	Reason string
}

func (u Unsupported) String() string {
	return fmt.Sprintf("%s.%s: %q — %s", u.Type, u.Field, u.Tag, u.Reason)
}

// Result is a completed migration.
type Result struct {
	Schema *ir.Schema

	// Unsupported lists every tag that did not make it into the IR. An
	// empty list means the migration is lossless.
	Unsupported []Unsupported

	// Types lists every struct that was migrated (root first).
	Types []string
}

// Migrate reads the package and builds the IR for the root type.
func Migrate(opts Options) (*Result, error) {
	if opts.Dir == "" || opts.Type == "" {
		return nil, errors.New("tags: Dir and Type are required")
	}
	pkg, err := accessors.ParsePackage(opts.Dir, "")
	if err != nil {
		return nil, fmt.Errorf("tags: %w", err)
	}
	m := &migrator{pkg: pkg, defs: map[string]ir.TypeNode{}, done: map[string]bool{}}
	root, ok := m.object(opts.Type)
	if !ok {
		return nil, fmt.Errorf("tags: struct %s not found in %s", opts.Type, opts.Dir)
	}
	s := &ir.Schema{Version: ir.Version, Root: root, Meta: ir.SchemaMeta{Title: opts.Type, Generator: opts.Generator}}
	if len(m.defs) > 0 {
		s.Defs = m.defs
	}
	if err := ir.Check(s); err != nil {
		// The migration built a tree Check rejects: a bug here, not a tag
		// problem — surface it rather than hand back a document that
		// every consumer will refuse.
		return nil, fmt.Errorf("tags: migrated schema fails Check: %w", err)
	}
	sort.Slice(m.unsupported, func(i, j int) bool {
		return m.unsupported[i].String() < m.unsupported[j].String()
	})
	return &Result{Schema: s, Unsupported: m.unsupported, Types: append([]string{opts.Type}, m.order...)}, nil
}

type migrator struct {
	pkg         *accessors.Package
	defs        map[string]ir.TypeNode
	done        map[string]bool
	order       []string
	unsupported []Unsupported
}

// object migrates one struct into an ObjectNode, registering it in Defs
// when it is referenced by another (the caller decides).
func (m *migrator) object(typeName string) (*ir.ObjectNode, bool) {
	decl, ok := m.pkg.Struct(typeName)
	if !ok {
		return nil, false
	}
	obj := &ir.ObjectNode{}
	for _, f := range decl.Type.Fields.List {
		if len(f.Names) == 0 {
			m.report(typeName, m.pkg.TypeString(f.Type), "", "embedded fields are not migrated; promote them explicitly")
			continue
		}
		tag := tagOf(f.Tag)
		if tag == "-" {
			continue
		}
		typeExpr := m.pkg.TypeString(f.Type)
		for _, id := range f.Names {
			if !id.IsExported() {
				continue
			}
			field := ir.FieldNode{Name: id.Name, JSONName: jsonName(f.Tag, id.Name), Optional: true}
			node, gates := m.field(typeName, id.Name, typeExpr, tag, &field)
			field.Type = node
			obj.Fields = append(obj.Fields, field)
			obj.Gated = append(obj.Gated, gates...)
		}
	}
	return obj, true
}

// field maps one field's Go type and tag list onto a type node and any
// object-level gates the tags imply.
func (m *migrator) field(typeName, fieldName, typeExpr, tag string, field *ir.FieldNode) (ir.TypeNode, []ir.GatedRules) {
	collectionTags, elemTags := splitDive(parseTags(tag))
	node := m.typeNode(typeName, fieldName, typeExpr, elemTags)
	var gates []ir.GatedRules
	for _, t := range collectionTags {
		switch t.name {
		case "required":
			field.Optional = false
		case "omitempty", "omitnil", "structonly", "nostructlevel", "":
			// Presence-related or structural no-ops in the IR model.
		case "required_if", "required_unless", "required_with", "required_without", "required_with_all", "required_without_all":
			if g, ok := gateFor(t, fieldName); ok {
				gates = append(gates, g)
			} else {
				m.report(typeName, fieldName, t.raw, "malformed conditional requirement")
			}
		default:
			if !m.applyRule(typeName, fieldName, node, t) {
				m.report(typeName, fieldName, t.raw, unsupportedReason(t, node))
			}
		}
	}
	return node, gates
}

// unsupportedReason explains, per known tag, why it did not migrate and
// what to do instead — a report that only says "no IR equivalent" sends
// the reader back to the source to guess.
func unsupportedReason(t tagItem, n ir.TypeNode) string {
	if strings.Contains(t.raw, "|") {
		return "OR-groups (a|b) have no IR form; split into a oneof or a custom rule"
	}
	switch t.name {
	case "eqfield", "nefield", "gtefield", "ltefield", "gtfield", "ltfield", "eqcsfield", "necsfield", "gtcsfield", "ltcsfield":
		if _, isTime := n.(*ir.TimeNode); isTime {
			return "only gtfield/ltfield on time fields migrate (after/before); the rest need the CEL layer (v2)"
		}
		return "cross-field comparison on a non-time field needs the CEL layer (v2); express it as a Requires gate or a custom rule"
	case "excludes", "excludesall", "excludesrune":
		return "negative content match needs lookahead, which is not RE2-portable; use a custom rule"
	case "isdefault":
		return "isdefault has no portable meaning (frontend cannot know Go zero values); use a const rule or a custom rule"
	case "unique":
		if t.value != "" {
			return "unique=<field> on slices of structs is not modeled; use a custom rule over the slice"
		}
		return "unique applies to slices only"
	case "datetime":
		return "only RFC 3339 layouts migrate (date / datetime); other layouts need a pattern rule"
	case "dive":
		return "dive is only meaningful on slices and maps"
	case "required_if", "required_unless", "required_with", "required_without", "required_with_all", "required_without_all":
		return "malformed conditional requirement"
	}
	if _, isStr := n.(*ir.StringNode); !isStr {
		if _, known := classPatterns[t.name]; known || isFormatTag(t.name) {
			return "format tag on a non-string field"
		}
	}
	return "no IR equivalent"
}

// isFormatTag lists the string-format tags applyRule understands, so the
// wrong-type reason can name the mismatch.
func isFormatTag(name string) bool {
	switch name {
	case "email", "url", "uri", "http_url", "uuid", "uuid4", "uuid_rfc4122", "uuid4_rfc4122", "ipv4", "ip4_addr", "ipv6", "ip6_addr", "ip", "ip_addr",
		"cidr", "cidrv4", "cidrv6", "mac", "hostname", "hostname_rfc1123", "fqdn", "base64", "json", "semver", "notblank",
		"credit_card", "jwt", "iso3166_1_alpha2", "country_code", "iso4217", "hexcolor", "e164":
		return true
	}
	return false
}

// typeNode maps a Go type expression to an IR node, applying element tags
// (the part after `dive`) to the innermost element.
func (m *migrator) typeNode(typeName, fieldName, typeExpr string, elemTags []tagItem) ir.TypeNode {
	switch {
	case strings.HasPrefix(typeExpr, "*"):
		n := m.typeNode(typeName, fieldName, strings.TrimPrefix(typeExpr, "*"), elemTags)
		setNullable(n)
		return n
	case typeExpr == "[]byte":
		return &ir.BytesNode{}
	case strings.HasPrefix(typeExpr, "[]"):
		inner, rest := splitDive(elemTags)
		elem := m.typeNode(typeName, fieldName, strings.TrimPrefix(typeExpr, "[]"), rest)
		arr := &ir.ArrayNode{Elem: elem}
		for _, t := range inner {
			if !m.applyRule(typeName, fieldName, elem, t) {
				m.report(typeName, fieldName, t.raw, "no IR equivalent on element")
			}
		}
		return arr
	case strings.HasPrefix(typeExpr, "map["):
		k, v, ok := splitMapType(typeExpr)
		if !ok {
			return &ir.AnyNode{}
		}
		inner, rest := splitDive(elemTags)
		val := m.typeNode(typeName, fieldName, v, rest)
		for _, t := range inner {
			if !m.applyRule(typeName, fieldName, val, t) {
				m.report(typeName, fieldName, t.raw, "no IR equivalent on map value")
			}
		}
		return &ir.MapNode{Key: m.typeNode(typeName, fieldName, k, nil), Val: val}
	case typeExpr == "string":
		return withRules(&ir.StringNode{}, m, typeName, fieldName, elemTags)
	case typeExpr == "bool":
		return withRules(&ir.BoolNode{}, m, typeName, fieldName, elemTags)
	case typeExpr == "float32":
		return withRules(&ir.FloatNode{Bits: 32}, m, typeName, fieldName, elemTags)
	case typeExpr == "float64":
		return withRules(&ir.FloatNode{Bits: 64}, m, typeName, fieldName, elemTags)
	case typeExpr == "time.Time":
		return withRules(&ir.TimeNode{}, m, typeName, fieldName, elemTags)
	case typeExpr == "time.Duration":
		return withRules(&ir.DurationNode{}, m, typeName, fieldName, elemTags)
	case typeExpr == "any", typeExpr == "interface{}":
		return &ir.AnyNode{}
	}
	if bits, unsigned, ok := intType(typeExpr); ok {
		return withRules(&ir.IntNode{Bits: bits, Unsigned: unsigned}, m, typeName, fieldName, elemTags)
	}
	// A struct from this package: migrate it into Defs and reference it.
	if _, ok := m.pkg.Struct(typeExpr); ok {
		if !m.done[typeExpr] {
			m.done[typeExpr] = true
			obj, _ := m.object(typeExpr)
			m.defs[typeExpr] = obj
			m.order = append(m.order, typeExpr)
		}
		for _, t := range elemTags {
			m.report(typeName, fieldName, t.raw, "tags on a nested struct field apply to the struct's own tags")
		}
		return &ir.RefNode{Ref: typeExpr}
	}
	for _, t := range elemTags {
		m.report(typeName, fieldName, t.raw, "field type "+typeExpr+" has no IR kind")
	}
	return &ir.AnyNode{}
}

// withRules applies element tags as rules on a scalar node.
func withRules(n ir.TypeNode, m *migrator, typeName, fieldName string, ts []tagItem) ir.TypeNode {
	for _, t := range ts {
		if !m.applyRule(typeName, fieldName, n, t) {
			m.report(typeName, fieldName, t.raw, "no IR equivalent")
		}
	}
	return n
}

// Regexes for go-playground's character-class tags, in the RE2 ∩ ECMA-262
// subset so they export unchanged.
var classPatterns = map[string]string{
	"alpha":        `^[a-zA-Z]+$`,
	"alphanum":     `^[a-zA-Z0-9]+$`,
	"alphaunicode": `^[\p{L}]+$`,
	"numeric":      `^[-+]?[0-9]+(?:\.[0-9]+)?$`,
	"number":       `^[0-9]+$`,
	"hexadecimal":  `^(?:0[xX])?[0-9a-fA-F]+$`,
	"hexcolor":     `^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`,
	"lowercase":    `^[^A-Z]*$`,
	"uppercase":    `^[^a-z]*$`,
	"ascii":        `^[\x00-\x7F]*$`,
	"printascii":   `^[\x20-\x7E]*$`,
	"e164":         `^\+[1-9]\d{1,14}$`,
	"ulid":         `^[0-7][0-9A-HJKMNP-TV-Z]{25}$`,
}

// applyRule maps one tag onto a rule on node n, by node kind. Returns
// false when the tag has no equivalent for that kind.
func (m *migrator) applyRule(typeName, fieldName string, n ir.TypeNode, t tagItem) bool {
	add := func(r ir.Rule) bool { appendRule(n, r); return true }
	num := func() (float64, bool) { f, err := strconv.ParseFloat(t.value, 64); return f, err == nil }
	count := func() (int, bool) { i, err := strconv.Atoi(t.value); return i, err == nil }
	isLen := isLengthKind(n)
	switch t.name {
	case "omitempty", "omitnil", "structonly", "nostructlevel":
		// Structural no-ops wherever they appear (after dive too).
		return true
	case "email":
		return isString(n) && add(ir.Email())
	case "url", "uri", "http_url":
		return isString(n) && add(ir.URL())
	case "uuid", "uuid4", "uuid_rfc4122", "uuid4_rfc4122":
		return isString(n) && add(ir.UUID())
	case "ipv4", "ip4_addr":
		return isString(n) && add(ir.IPv4())
	case "ipv6", "ip6_addr":
		return isString(n) && add(ir.IPv6())
	case "ip", "ip_addr":
		return isString(n) && add(ir.IP())
	case "cidr", "cidrv4", "cidrv6":
		return isString(n) && add(ir.CIDR())
	case "mac":
		return isString(n) && add(ir.MAC())
	case "hostname", "hostname_rfc1123", "fqdn":
		return isString(n) && add(ir.Hostname())
	case "base64":
		return isString(n) && add(ir.Base64())
	case "json":
		return isString(n) && add(ir.JSON())
	case "semver":
		return isString(n) && add(ir.Semver())
	case "datetime":
		// go-playground's datetime takes a layout; only the RFC 3339 forms
		// map onto the IR's date/datetime kinds.
		switch t.value {
		case "2006-01-02":
			return isString(n) && add(ir.Date())
		case "2006-01-02T15:04:05Z07:00", "":
			return isString(n) && add(ir.DateTime())
		}
		return false
	case "notblank":
		return isString(n) && add(ir.NotBlank())
	case "credit_card":
		return isString(n) && add(ir.CreditCard())
	case "jwt":
		return isString(n) && add(ir.JWT())
	case "iso3166_1_alpha2", "country_code":
		return isString(n) && add(ir.CountryCode())
	case "iso4217":
		return isString(n) && add(ir.CurrencyCode())
	case "hexcolor":
		return isString(n) && add(ir.HexColor())
	case "e164":
		return isString(n) && add(ir.E164())
	case "latitude":
		return isNumeric(n) && add(ir.Latitude())
	case "longitude":
		return isNumeric(n) && add(ir.Longitude())
	case "unique":
		if _, ok := n.(*ir.ArrayNode); ok {
			return add(ir.Unique())
		}
		return false
	case "oneof":
		values := splitOneOf(t.value)
		if len(values) == 0 {
			return false
		}
		if isString(n) {
			anys := make([]any, len(values))
			for i, v := range values {
				anys[i] = v
			}
			return add(ir.Enum(anys...))
		}
		if isNumeric(n) {
			anys := make([]any, 0, len(values))
			for _, v := range values {
				f, err := strconv.ParseFloat(v, 64)
				if err != nil {
					return false
				}
				anys = append(anys, f)
			}
			return add(ir.Enum(anys...))
		}
		return false
	case "eq":
		if isString(n) {
			return add(ir.Const(t.value))
		}
		if isNumeric(n) {
			f, ok := num()
			return ok && add(ir.Const(f))
		}
		if _, ok := n.(*ir.BoolNode); ok {
			b, err := strconv.ParseBool(t.value)
			return err == nil && add(ir.Const(b))
		}
		return false
	case "min":
		if isLen {
			c, ok := count()
			if !ok {
				return false
			}
			if _, isStr := n.(*ir.StringNode); isStr {
				return add(ir.MinLength(c))
			}
			if _, isBytes := n.(*ir.BytesNode); isBytes {
				return add(ir.MinLength(c))
			}
			return add(ir.MinItems(c))
		}
		f, ok := num()
		return ok && isNumeric(n) && add(ir.Min(f))
	case "max":
		if isLen {
			c, ok := count()
			if !ok {
				return false
			}
			if _, isStr := n.(*ir.StringNode); isStr {
				return add(ir.MaxLength(c))
			}
			if _, isBytes := n.(*ir.BytesNode); isBytes {
				return add(ir.MaxLength(c))
			}
			return add(ir.MaxItems(c))
		}
		f, ok := num()
		return ok && isNumeric(n) && add(ir.Max(f))
	case "len":
		c, ok := count()
		if !ok || !isLen {
			return false
		}
		if _, isStr := n.(*ir.StringNode); isStr {
			return add(ir.MinLength(c)) && add(ir.MaxLength(c))
		}
		if _, isBytes := n.(*ir.BytesNode); isBytes {
			return add(ir.MinLength(c)) && add(ir.MaxLength(c))
		}
		return add(ir.MinItems(c)) && add(ir.MaxItems(c))
	case "gt", "gte", "lt", "lte":
		if isLen {
			// go-playground treats these as length comparisons on strings
			// and collections; the IR has inclusive length bounds only.
			c, ok := count()
			if !ok {
				return false
			}
			switch t.name {
			case "gt":
				c++
			case "lt":
				c--
			}
			lower := t.name == "gt" || t.name == "gte"
			if _, isStr := n.(*ir.StringNode); isStr || isBytes(n) {
				if lower {
					return add(ir.MinLength(c))
				}
				return add(ir.MaxLength(c))
			}
			if lower {
				return add(ir.MinItems(c))
			}
			return add(ir.MaxItems(c))
		}
		f, ok := num()
		if !ok || !isNumeric(n) {
			return false
		}
		switch t.name {
		case "gt":
			return add(ir.Gt(f))
		case "gte":
			return add(ir.Gte(f))
		case "lt":
			return add(ir.Lt(f))
		}
		return add(ir.Lte(f))
	case "gtfield", "gtefield":
		if _, ok := n.(*ir.TimeNode); ok && t.value != "" {
			return add(ir.After(t.value))
		}
		return false
	case "ltfield", "ltefield":
		if _, ok := n.(*ir.TimeNode); ok && t.value != "" {
			return add(ir.Before(t.value))
		}
		return false
	case "contains":
		return isString(n) && add(ir.Pattern(regexp.QuoteMeta(t.value)))
	case "startswith":
		return isString(n) && add(ir.Pattern("^"+regexp.QuoteMeta(t.value)))
	case "endswith":
		return isString(n) && add(ir.Pattern(regexp.QuoteMeta(t.value)+"$"))
	}
	if re, ok := classPatterns[t.name]; ok && isString(n) {
		if t.name == "ulid" {
			return add(ir.ULID())
		}
		return add(ir.Pattern(re))
	}
	return false
}

// gateFor maps a conditional-requirement tag to a requires gate.
func gateFor(t tagItem, fieldName string) (ir.GatedRules, bool) {
	parts := strings.Fields(t.value)
	switch t.name {
	case "required_if", "required_unless":
		if len(parts) < 2 || len(parts)%2 != 0 {
			return ir.GatedRules{}, false
		}
		conds := make([]ir.Condition, 0, len(parts)/2)
		for i := 0; i < len(parts); i += 2 {
			conds = append(conds, ir.Cmp(parts[i], ir.OpEq, parts[i+1]))
		}
		cond := conds[0]
		if len(conds) > 1 {
			cond = ir.And(conds...)
		}
		if t.name == "required_unless" {
			cond = ir.Not(cond)
		}
		return ir.Requires(cond, fieldName), true
	case "required_with", "required_with_all", "required_without", "required_without_all":
		if len(parts) == 0 {
			return ir.GatedRules{}, false
		}
		op := ir.Present
		if strings.HasPrefix(t.name, "required_without") {
			op = ir.Absent
		}
		conds := make([]ir.Condition, 0, len(parts))
		for _, p := range parts {
			conds = append(conds, ir.HasField(p, op))
		}
		var cond ir.Condition
		switch {
		case len(conds) == 1:
			cond = conds[0]
		case strings.HasSuffix(t.name, "_all"):
			cond = ir.And(conds...)
		default:
			cond = ir.Or(conds...)
		}
		return ir.Requires(cond, fieldName), true
	}
	return ir.GatedRules{}, false
}

func (m *migrator) report(typeName, fieldName, tag, reason string) {
	m.unsupported = append(m.unsupported, Unsupported{Type: typeName, Field: fieldName, Tag: tag, Reason: reason})
}

// tagItem is one comma-separated element of a validate tag.
type tagItem struct {
	name  string
	value string
	raw   string
}

// parseTags splits `required,min=3,oneof=a b` into items. go-playground
// allows `|` for OR groups; those are reported as one unsupported item by
// leaving the raw text intact under the name before the pipe.
func parseTags(tag string) []tagItem {
	var out []tagItem
	for _, part := range strings.Split(tag, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, _ := strings.Cut(part, "=")
		out = append(out, tagItem{name: name, value: value, raw: part})
	}
	return out
}

// splitDive separates tags before the first `dive` from those after it.
func splitDive(ts []tagItem) (before, after []tagItem) {
	for i, t := range ts {
		if t.name == "dive" {
			return ts[:i], ts[i+1:]
		}
	}
	return ts, nil
}

// splitOneOf splits an oneof value on spaces, honoring single quotes.
func splitOneOf(v string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range v {
		switch {
		case r == '\'':
			inQuote = !inQuote
		case r == ' ' && !inQuote:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func tagOf(tag *ast.BasicLit) string {
	if tag == nil {
		return ""
	}
	raw, err := strconv.Unquote(tag.Value)
	if err != nil {
		return ""
	}
	return reflect.StructTag(raw).Get(TagKey)
}

func jsonName(tag *ast.BasicLit, goName string) string {
	if tag == nil {
		return ""
	}
	raw, err := strconv.Unquote(tag.Value)
	if err != nil {
		return ""
	}
	name, _, _ := strings.Cut(reflect.StructTag(raw).Get("json"), ",")
	if name == "" || name == "-" || name == goName {
		return ""
	}
	return name
}

func intType(typeExpr string) (bits int, unsigned, ok bool) {
	switch typeExpr {
	case "int":
		return 0, false, true
	case "uint":
		return 0, true, true
	case "int8":
		return 8, false, true
	case "int16":
		return 16, false, true
	case "int32":
		return 32, false, true
	case "int64":
		return 64, false, true
	case "uint8":
		return 8, true, true
	case "uint16":
		return 16, true, true
	case "uint32":
		return 32, true, true
	case "uint64":
		return 64, true, true
	}
	return 0, false, false
}

func splitMapType(goType string) (string, string, bool) {
	rest, ok := strings.CutPrefix(goType, "map[")
	if !ok {
		return "", "", false
	}
	depth := 1
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return rest[:i], rest[i+1:], true
			}
		}
	}
	return "", "", false
}

func isString(n ir.TypeNode) bool { _, ok := n.(*ir.StringNode); return ok }
func isBytes(n ir.TypeNode) bool  { _, ok := n.(*ir.BytesNode); return ok }
func isNumeric(n ir.TypeNode) bool {
	switch n.(type) {
	case *ir.IntNode, *ir.FloatNode, *ir.DurationNode:
		return true
	}
	return false
}

// isLengthKind reports nodes whose min/max/len tags mean length.
func isLengthKind(n ir.TypeNode) bool {
	switch n.(type) {
	case *ir.StringNode, *ir.BytesNode, *ir.ArrayNode, *ir.MapNode:
		return true
	}
	return false
}

// appendRule adds a rule to any rule-bearing node.
func appendRule(n ir.TypeNode, r ir.Rule) {
	switch t := n.(type) {
	case *ir.StringNode:
		t.Rules = append(t.Rules, r)
	case *ir.IntNode:
		t.Rules = append(t.Rules, r)
	case *ir.FloatNode:
		t.Rules = append(t.Rules, r)
	case *ir.BoolNode:
		t.Rules = append(t.Rules, r)
	case *ir.BytesNode:
		t.Rules = append(t.Rules, r)
	case *ir.TimeNode:
		t.Rules = append(t.Rules, r)
	case *ir.DurationNode:
		t.Rules = append(t.Rules, r)
	case *ir.ArrayNode:
		t.Rules = append(t.Rules, r)
	case *ir.MapNode:
		t.Rules = append(t.Rules, r)
	}
}

func setNullable(n ir.TypeNode) {
	switch t := n.(type) {
	case *ir.ObjectNode:
		t.Nullable = true
	case *ir.StringNode:
		t.Nullable = true
	case *ir.IntNode:
		t.Nullable = true
	case *ir.FloatNode:
		t.Nullable = true
	case *ir.BoolNode:
		t.Nullable = true
	case *ir.BytesNode:
		t.Nullable = true
	case *ir.TimeNode:
		t.Nullable = true
	case *ir.DurationNode:
		t.Nullable = true
	case *ir.ArrayNode:
		t.Nullable = true
	case *ir.MapNode:
		t.Nullable = true
	case *ir.RefNode:
		t.Nullable = true
	case *ir.AnyNode:
		t.Nullable = true
	}
}
