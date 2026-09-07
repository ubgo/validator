package ir

// RuleKind names a constraint. Aligned to JSON Schema keywords wherever one
// exists (serde_valid's interop trick) so the JSON Schema emitter is a rename
// table, not a translation layer.
//
// Closed set within a major IR version; a consumer meeting an unknown kind
// must error, never skip (docsi/IR.md §9).
type RuleKind string

// Rule kinds, grouped as in docsi/IR.md §4.
const (
	// Length and size.
	RuleMinLength RuleKind = "minLength"
	RuleMaxLength RuleKind = "maxLength"
	RuleMinItems  RuleKind = "minItems"
	RuleMaxItems  RuleKind = "maxItems"
	RuleUnique    RuleKind = "unique"

	// Numeric bounds. Min/Max are inclusive (JSON Schema minimum/maximum);
	// Gt/Lt are exclusive; Gte/Lte are inclusive aliases kept because they
	// read naturally in the builder.
	RuleMin        RuleKind = "min"
	RuleMax        RuleKind = "max"
	RuleGt         RuleKind = "gt"
	RuleGte        RuleKind = "gte"
	RuleLt         RuleKind = "lt"
	RuleLte        RuleKind = "lte"
	RuleMultipleOf RuleKind = "multipleOf"

	// Membership and literal.
	RulePattern RuleKind = "pattern"
	RuleEnum    RuleKind = "enum"
	RuleConst   RuleKind = "const"

	// String formats. Those with a JSON Schema "format" export as one; the
	// rest export as a pattern or are reported in the manifest.
	RuleEmail        RuleKind = "email"
	RuleURL          RuleKind = "url"
	RuleUUID         RuleKind = "uuid"
	RuleULID         RuleKind = "ulid"
	RuleIPv4         RuleKind = "ipv4"
	RuleIPv6         RuleKind = "ipv6"
	RuleIP           RuleKind = "ip"
	RuleCIDR         RuleKind = "cidr"
	RuleMAC          RuleKind = "mac"
	RuleHostname     RuleKind = "hostname"
	RuleBase64       RuleKind = "base64"
	RuleJSON         RuleKind = "json"
	RuleSemver       RuleKind = "semver"
	RuleDate         RuleKind = "date"
	RuleDateTime     RuleKind = "datetime"
	RuleNotBlank     RuleKind = "notBlank"
	RuleCreditCard   RuleKind = "creditCard"
	RuleJWT          RuleKind = "jwt"
	RuleCountryCode  RuleKind = "countryCode"
	RuleCurrencyCode RuleKind = "currencyCode"
	RuleHexColor     RuleKind = "hexColor"
	RuleE164         RuleKind = "e164"

	// The five below were chosen from go-playground's long tail on one
	// test: is the grammar fixed, international, and checkable without a
	// lookup table that goes stale? A national identifier or a postcode
	// fails it — those belong in a Pattern rule the author owns, which
	// does not pretend to be authoritative.
	RuleTimezone  RuleKind = "timezone"
	RuleBase64URL RuleKind = "base64url"
	RuleISBN      RuleKind = "isbn"
	RuleBIC       RuleKind = "bic"
	RuleMongoID   RuleKind = "mongoID"

	// Numeric formats: geographic coordinates.
	RuleLatitude  RuleKind = "latitude"
	RuleLongitude RuleKind = "longitude"

	// Cross-field temporal/ordinal comparison against a sibling field.
	// Vanilla JSON Schema cannot express these; they compile to CEL.
	RuleAfter  RuleKind = "after"
	RuleBefore RuleKind = "before"

	// The two non-portable kinds (ADR-VAL-005 tiers 2 and 3).
	RuleCustom RuleKind = "custom"
	RuleAsync  RuleKind = "async"
)

// Rule parameter keys. Params is a map at the JSON boundary (resolves IR
// open question Q2: flexible on the wire, typed through these constants and
// the constructors below, so no caller writes a bare "value" string).
const (
	ParamValue  = "value"
	ParamValues = "values"
	ParamRegex  = "regex"
	ParamField  = "field"

	// ParamLabel is the WIRE name of the field ParamField resolves, carried
	// only so a message can name what the user sees. Paths use wire names
	// everywhere, so a message that said "must be after CheckIn" beside a
	// path of "$.check_out" was showing two names for one field. Optional:
	// when absent the message falls back to ParamField.
	ParamLabel    = "label"
	ParamRef      = "ref"
	ParamEndpoint = "endpoint"
)

// ruleSpec describes a kind's contract: which params it requires and its
// inherent portability. One table drives constructors, [Check], and the
// portability default, so a new kind is registered in exactly one place.
type ruleSpec struct {
	required    []string
	portability Portability
}

// ruleSpecs is the registry of known kinds.
var ruleSpecs = map[RuleKind]ruleSpec{
	RuleMinLength:    {required: []string{ParamValue}, portability: Portable},
	RuleMaxLength:    {required: []string{ParamValue}, portability: Portable},
	RuleMinItems:     {required: []string{ParamValue}, portability: Portable},
	RuleMaxItems:     {required: []string{ParamValue}, portability: Portable},
	RuleUnique:       {portability: Portable},
	RuleMin:          {required: []string{ParamValue}, portability: Portable},
	RuleMax:          {required: []string{ParamValue}, portability: Portable},
	RuleGt:           {required: []string{ParamValue}, portability: Portable},
	RuleGte:          {required: []string{ParamValue}, portability: Portable},
	RuleLt:           {required: []string{ParamValue}, portability: Portable},
	RuleLte:          {required: []string{ParamValue}, portability: Portable},
	RuleMultipleOf:   {required: []string{ParamValue}, portability: Portable},
	RulePattern:      {required: []string{ParamRegex}, portability: Portable},
	RuleEnum:         {required: []string{ParamValues}, portability: Portable},
	RuleConst:        {required: []string{ParamValue}, portability: Portable},
	RuleEmail:        {portability: Portable},
	RuleURL:          {portability: Portable},
	RuleUUID:         {portability: Portable},
	RuleULID:         {portability: Portable},
	RuleIPv4:         {portability: Portable},
	RuleIPv6:         {portability: Portable},
	RuleIP:           {portability: Portable},
	RuleCIDR:         {portability: Portable},
	RuleMAC:          {portability: Portable},
	RuleHostname:     {portability: Portable},
	RuleBase64:       {portability: Portable},
	RuleJSON:         {portability: Portable},
	RuleSemver:       {portability: Portable},
	RuleDate:         {portability: Portable},
	RuleDateTime:     {portability: Portable},
	RuleNotBlank:     {portability: Portable},
	RuleCreditCard:   {portability: Portable},
	RuleJWT:          {portability: Portable},
	RuleCountryCode:  {portability: Portable},
	RuleCurrencyCode: {portability: Portable},
	RuleHexColor:     {portability: Portable},
	RuleE164:         {portability: Portable},
	RuleTimezone:     {portability: Portable},
	RuleBase64URL:    {portability: Portable},
	RuleISBN:         {portability: Portable},
	RuleBIC:          {portability: Portable},
	RuleMongoID:      {portability: Portable},
	RuleLatitude:     {portability: Portable},
	RuleLongitude:    {portability: Portable},
	RuleAfter:        {required: []string{ParamField}, portability: Portable},
	RuleBefore:       {required: []string{ParamField}, portability: Portable},
	RuleCustom:       {required: []string{ParamRef}, portability: Opaque},
	RuleAsync:        {required: []string{ParamRef}, portability: Server},
}

// Known reports whether k is a registered rule kind.
func (k RuleKind) Known() bool {
	_, ok := ruleSpecs[k]
	return ok
}

// DefaultPortability returns the portability class inherent to a kind, or
// false for an unknown kind. Custom is opaque and async is server by
// construction; a document may only make a rule LESS portable than its
// default (e.g. wasm for a custom rule), never more — [Check] enforces that.
func (k RuleKind) DefaultPortability() (Portability, bool) {
	spec, ok := ruleSpecs[k]
	if !ok {
		return "", false
	}
	return spec.portability, true
}

// Rule is one constraint on a value. It is pure data: Kind names the check,
// Params carries its operands, and nothing here can hold a function.
type Rule struct {
	Kind RuleKind `json:"kind"`

	// Params are the kind-specific operands, keyed by the Param* constants.
	Params map[string]any `json:"params,omitempty"`

	// Cond, when set, gates the rule: it applies only if the condition holds
	// (docsi/IR.md §5).
	Cond *Condition `json:"cond,omitempty"`

	// Portable is the explicit portability class. Empty means "the kind's
	// default" — read it through [Rule.EffectivePortability].
	Portable Portability `json:"portable,omitempty"`

	Meta NodeMeta `json:"meta,omitzero"`
}

// EffectivePortability returns the explicit class when set, otherwise the
// kind's inherent default. Returns false for an unknown kind with no
// explicit class, because guessing "portable" there is the silent-drop trap.
func (r Rule) EffectivePortability() (Portability, bool) {
	if r.Portable != "" {
		return r.Portable, true
	}
	return r.Kind.DefaultPortability()
}

// Param returns a parameter by key, with presence.
func (r Rule) Param(key string) (any, bool) {
	v, ok := r.Params[key]
	return v, ok
}

// Typed constructors. They are the only sanctioned way to build the common
// rules so the param keys stay consistent with the registry.

func single(kind RuleKind, key string, v any) Rule {
	return Rule{Kind: kind, Params: map[string]any{key: v}}
}

// MinLength requires len(s) >= n.
func MinLength(n int) Rule { return single(RuleMinLength, ParamValue, n) }

// MaxLength requires len(s) <= n.
func MaxLength(n int) Rule { return single(RuleMaxLength, ParamValue, n) }

// MinItems requires len(collection) >= n.
func MinItems(n int) Rule { return single(RuleMinItems, ParamValue, n) }

// MaxItems requires len(collection) <= n.
func MaxItems(n int) Rule { return single(RuleMaxItems, ParamValue, n) }

// Unique requires every element of a collection to be distinct.
func Unique() Rule { return Rule{Kind: RuleUnique} }

// Min requires v >= n (inclusive).
func Min(n float64) Rule { return single(RuleMin, ParamValue, n) }

// Max requires v <= n (inclusive).
func Max(n float64) Rule { return single(RuleMax, ParamValue, n) }

// Gt requires v > n.
func Gt(n float64) Rule { return single(RuleGt, ParamValue, n) }

// Gte requires v >= n.
func Gte(n float64) Rule { return single(RuleGte, ParamValue, n) }

// Lt requires v < n.
func Lt(n float64) Rule { return single(RuleLt, ParamValue, n) }

// Lte requires v <= n.
func Lte(n float64) Rule { return single(RuleLte, ParamValue, n) }

// MultipleOf requires v to be an integer multiple of n.
func MultipleOf(n float64) Rule { return single(RuleMultipleOf, ParamValue, n) }

// Pattern requires the string to match regex. Portability caveat: the
// pattern must stay inside the RE2 ∩ ECMA-262 common subset to run
// identically in Go and the browser; a later lint pass enforces that.
func Pattern(regex string) Rule { return single(RulePattern, ParamRegex, regex) }

// Enum requires the value to be one of values.
func Enum(values ...any) Rule { return single(RuleEnum, ParamValues, values) }

// Const requires the value to equal v exactly.
func Const(v any) Rule { return single(RuleConst, ParamValue, v) }

// Email requires an RFC 5322 address.
func Email() Rule { return Rule{Kind: RuleEmail} }

// URL requires an absolute URL.
func URL() Rule { return Rule{Kind: RuleURL} }

// UUID requires an RFC 4122 UUID.
func UUID() Rule { return Rule{Kind: RuleUUID} }

// ULID requires a ULID.
func ULID() Rule { return Rule{Kind: RuleULID} }

// IPv4 requires a dotted-quad IPv4 address.
func IPv4() Rule { return Rule{Kind: RuleIPv4} }

// IPv6 requires an IPv6 address.
func IPv6() Rule { return Rule{Kind: RuleIPv6} }

// IP requires an IPv4 or IPv6 address.
func IP() Rule { return Rule{Kind: RuleIP} }

// CIDR requires an IPv4 or IPv6 CIDR block.
func CIDR() Rule { return Rule{Kind: RuleCIDR} }

// MAC requires a hardware address in any form net.ParseMAC accepts.
func MAC() Rule { return Rule{Kind: RuleMAC} }

// Hostname requires an RFC 1123 hostname (labels of letters, digits and
// hyphens, at most 253 characters).
func Hostname() Rule { return Rule{Kind: RuleHostname} }

// Base64 requires standard base64 text with padding.
func Base64() Rule { return Rule{Kind: RuleBase64} }

// JSON requires the string to parse as a JSON document.
func JSON() Rule { return Rule{Kind: RuleJSON} }

// Semver requires a Semantic Versioning 2.0.0 version.
func Semver() Rule { return Rule{Kind: RuleSemver} }

// Date requires an RFC 3339 full-date (YYYY-MM-DD).
func Date() Rule { return Rule{Kind: RuleDate} }

// DateTime requires an RFC 3339 date-time string.
func DateTime() Rule { return Rule{Kind: RuleDateTime} }

// NotBlank requires at least one non-whitespace character.
func NotBlank() Rule { return Rule{Kind: RuleNotBlank} }

// CreditCard requires 12–19 digits (spaces and dashes allowed) that pass
// the Luhn check.
func CreditCard() Rule { return Rule{Kind: RuleCreditCard} }

// JWT requires three base64url segments separated by dots.
func JWT() Rule { return Rule{Kind: RuleJWT} }

// CountryCode requires the SHAPE of an ISO 3166-1 alpha-2 code (two upper
// case letters); it does not check against the registry, so a retired or
// unassigned code passes. Same on both sides by design.
func CountryCode() Rule { return Rule{Kind: RuleCountryCode} }

// CurrencyCode requires the shape of an ISO 4217 code (three upper case
// letters), not registry membership.
func CurrencyCode() Rule { return Rule{Kind: RuleCurrencyCode} }

// HexColor requires #RGB, #RRGGBB or #RRGGBBAA.
func HexColor() Rule { return Rule{Kind: RuleHexColor} }

// E164 requires an international phone number in E.164 form.
func E164() Rule { return Rule{Kind: RuleE164} }

// Timezone requires an IANA time zone name, "Europe/Berlin". Validated
// against the Go runtime's own database, so it answers what time.LoadLocation
// would answer.
func Timezone() Rule { return Rule{Kind: RuleTimezone} }

// Base64URL requires the URL-safe base64 alphabet (RFC 4648 §5), which
// uses - and _ where standard base64 uses + and /. Distinct from Base64:
// a JWT segment is base64url and fails a standard-alphabet check.
func Base64URL() Rule { return Rule{Kind: RuleBase64URL} }

// ISBN requires a valid ISBN-10 or ISBN-13, checksum included. Hyphens
// and spaces are ignored, as they are decoration in the printed form.
func ISBN() Rule { return Rule{Kind: RuleISBN} }

// BIC requires a SWIFT business identifier code (ISO 9362): four letters
// for the institution, two for the country, two alphanumeric for the
// location, and optionally three more for the branch.
func BIC() Rule { return Rule{Kind: RuleBIC} }

// MongoID requires a MongoDB ObjectID: 24 hexadecimal characters.
func MongoID() Rule { return Rule{Kind: RuleMongoID} }

// Latitude requires a number in [-90, 90].
func Latitude() Rule { return Rule{Kind: RuleLatitude} }

// Longitude requires a number in [-180, 180].
func Longitude() Rule { return Rule{Kind: RuleLongitude} }

// After requires this value to be later than the sibling field named.
func After(field string) Rule { return single(RuleAfter, ParamField, field) }

// AfterLabeled is [After] with the sibling's wire name for the message.
// The Go name still resolves the sibling; the label is what the reader
// sees, and matches the path they see beside it.
func AfterLabeled(field, label string) Rule {
	r := After(field)
	if label != "" && label != field {
		r.Params[ParamLabel] = label
	}
	return r
}

// Before requires this value to be earlier than the sibling field named.
func Before(field string) Rule { return single(RuleBefore, ParamField, field) }

// BeforeLabeled is [Before] with the sibling's wire name for the message.
func BeforeLabeled(field, label string) Rule {
	r := Before(field)
	if label != "" && label != field {
		r.Params[ParamLabel] = label
	}
	return r
}

// Custom references a Go function registered under ref. The IR stores only
// the name — the function itself lives in a registry the executor owns —
// and the rule is classified opaque so the frontend generator stubs and
// reports it (ADR-VAL-005 tier 2).
func Custom(ref string) Rule {
	return Rule{Kind: RuleCustom, Params: map[string]any{ParamRef: ref}, Portable: Opaque}
}

// Async references a registered server-side rule that needs backend state.
// Endpoint is where the frontend round-trips; empty lets the generator
// derive it from the ref (ADR-VAL-005 tier 3).
func Async(ref, endpoint string) Rule {
	params := map[string]any{ParamRef: ref}
	if endpoint != "" {
		params[ParamEndpoint] = endpoint
	}
	return Rule{Kind: RuleAsync, Params: params, Portable: Server}
}

// TransformKind names a value transform applied around validation.
//
// Closed set; transforms are only portable when emitting Zod (JSON Schema
// does not transform), which the manifest reports.
type TransformKind string

const (
	TransformCoerce TransformKind = "coerce"
	// TransformStrict marks a field that must never be coerced, even when
	// the executor runs in lax mode. Field markers beat call options.
	TransformStrict         TransformKind = "strict"
	TransformTrim           TransformKind = "trim"
	TransformLower          TransformKind = "lower"
	TransformUpper          TransformKind = "upper"
	TransformDefault        TransformKind = "default"
	TransformNormalizeEmail TransformKind = "normalizeEmail"
)

// transformKinds is the closed set [Check] accepts.
var transformKinds = map[TransformKind]struct{}{
	TransformCoerce:         {},
	TransformStrict:         {},
	TransformTrim:           {},
	TransformLower:          {},
	TransformUpper:          {},
	TransformDefault:        {},
	TransformNormalizeEmail: {},
}

// Phase says whether a transform runs before validation (coerce, sanitize)
// or after it (normalize).
type Phase string

const (
	PhasePre  Phase = "pre"
	PhasePost Phase = "post"
)

// phases is the closed set [Check] accepts.
var phases = map[Phase]struct{}{PhasePre: {}, PhasePost: {}}

// Transform is one step of a field's transform pipeline (docsi/IR.md §7).
type Transform struct {
	Kind   TransformKind  `json:"kind"`
	Phase  Phase          `json:"phase"`
	Params map[string]any `json:"params,omitempty"`
}
