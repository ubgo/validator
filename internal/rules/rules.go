// Package rules holds the rule semantics shared by every Go executor: the
// interpreter and generated code must agree on what "email" or "unique"
// means and on the code and message each failure carries. One
// implementation, two callers, no drift (docsi/MISSION.md ADR-VAL-003).
package rules

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"net"
	"net/mail"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/verr"
)

// Text is a kind's default error code and message template (docsi/IR.md
// §6). A rule's Meta.Code / Meta.Message override per rule.
type Text struct {
	Code string
	Tmpl string
}

// Texts is the closed table of defaults, keyed by rule kind.
var Texts = map[ir.RuleKind]Text{
	ir.RuleMinLength:    {"min_length", "must be at least {value} characters"},
	ir.RuleMaxLength:    {"max_length", "must be at most {value} characters"},
	ir.RuleMinItems:     {"min_items", "must have at least {value} items"},
	ir.RuleMaxItems:     {"max_items", "must have at most {value} items"},
	ir.RuleUnique:       {"unique", "items must be unique"},
	ir.RuleMin:          {"min", "must be at least {value}"},
	ir.RuleMax:          {"max", "must be at most {value}"},
	ir.RuleGt:           {"gt", "must be greater than {value}"},
	ir.RuleGte:          {"gte", "must be at least {value}"},
	ir.RuleLt:           {"lt", "must be less than {value}"},
	ir.RuleLte:          {"lte", "must be at most {value}"},
	ir.RuleMultipleOf:   {"multiple_of", "must be a multiple of {value}"},
	ir.RulePattern:      {"pattern", "must match {regex}"},
	ir.RuleEnum:         {"enum", "must be one of {values}"},
	ir.RuleConst:        {"const", "must equal {value}"},
	ir.RuleEmail:        {"email", "must be a valid email address"},
	ir.RuleURL:          {"url", "must be a valid URL"},
	ir.RuleUUID:         {"uuid", "must be a valid UUID"},
	ir.RuleULID:         {"ulid", "must be a valid ULID"},
	ir.RuleIPv4:         {"ipv4", "must be a valid IPv4 address"},
	ir.RuleIPv6:         {"ipv6", "must be a valid IPv6 address"},
	ir.RuleIP:           {"ip", "must be a valid IP address"},
	ir.RuleCIDR:         {"cidr", "must be a valid CIDR block"},
	ir.RuleMAC:          {"mac", "must be a valid MAC address"},
	ir.RuleHostname:     {"hostname", "must be a valid hostname"},
	ir.RuleBase64:       {"base64", "must be valid base64"},
	ir.RuleJSON:         {"json", "must be valid JSON"},
	ir.RuleSemver:       {"semver", "must be a valid semantic version"},
	ir.RuleDate:         {"date", "must be a date (YYYY-MM-DD)"},
	ir.RuleDateTime:     {"datetime", "must be an RFC 3339 date-time"},
	ir.RuleNotBlank:     {"not_blank", "must not be blank"},
	ir.RuleCreditCard:   {"credit_card", "must be a valid card number"},
	ir.RuleJWT:          {"jwt", "must be a JWT"},
	ir.RuleCountryCode:  {"country_code", "must be a two-letter country code"},
	ir.RuleCurrencyCode: {"currency_code", "must be a three-letter currency code"},
	ir.RuleHexColor:     {"hex_color", "must be a hex color"},
	ir.RuleE164:         {"e164", "must be an E.164 phone number"},
	ir.RuleLatitude:     {"latitude", "must be between -90 and 90"},
	ir.RuleLongitude:    {"longitude", "must be between -180 and 180"},
	ir.RuleAfter:        {"after", "must be after {field}"},
	ir.RuleBefore:       {"before", "must be before {field}"},
	ir.RuleCustom:       {verr.CodeCustom, "{error}"},
	ir.RuleAsync:        {verr.CodeCustom, "{error}"},
}

// TextFor returns the code and template for a rule, honoring its Meta
// overrides.
func TextFor(r *ir.Rule) (code, tmpl string) {
	t := Texts[r.Kind]
	code, tmpl = t.Code, t.Tmpl
	if r.Meta.Code != "" {
		code = r.Meta.Code
	}
	if r.Meta.Message != "" {
		tmpl = r.Meta.Message
	}
	return code, tmpl
}

var (
	uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	ulidRe = regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`)

	// Patterns below are the ones the JSON Schema emitter exports for the
	// kinds JSON Schema has no format for, so Go and the browser agree.
	base64Re = regexp.MustCompile(Base64Pattern)
	semverRe = regexp.MustCompile(SemverPattern)
	macRe    = regexp.MustCompile(MACPattern)
	dateRe   = regexp.MustCompile(DatePattern)
)

// Exported patterns, in the RE2 ∩ ECMA-262 subset, shared with the JSON
// Schema emitter so the frontend checks exactly what Go checks.
const (
	Base64Pattern   = `^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$`
	SemverPattern   = `^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`
	MACPattern      = `^(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}$|^(?:[0-9A-Fa-f]{4}\.){2}[0-9A-Fa-f]{4}$`
	DatePattern     = `^\d{4}-(?:0[1-9]|1[0-2])-(?:0[1-9]|[12]\d|3[01])$`
	NotBlankPattern = `\S`
	// CreditCardPattern is the digit shape only; the Luhn check has no
	// regex and is reported in the manifest.
	CreditCardPattern   = `^[0-9]{12,19}$`
	JWTPattern          = `^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*$`
	CountryCodePattern  = `^[A-Z]{2}$`
	CurrencyCodePattern = `^[A-Z]{3}$`
	HexColorPattern     = `^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`
	E164Pattern         = `^\+[1-9]\d{1,14}$`
	CIDRPattern         = `^(?:(?:25[0-5]|2[0-4]\d|1?\d?\d)(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3}/(?:3[0-2]|[12]?\d)|[0-9a-fA-F:]+/(?:12[0-8]|1[01]\d|\d?\d))$`
)

// FloatEpsilon bounds the remainder check in MultipleOf so 0.3 is a
// multiple of 0.1 despite binary rounding.
const FloatEpsilon = 1e-9

// Email accepts a bare RFC 5322 address and rejects display-name forms
// ("Name <a@b>"), which mail.ParseAddress would otherwise allow.
func Email(s string) bool {
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s
}

// URL requires an absolute URL with scheme and host.
func URL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme != "" && u.Host != ""
}

// UUID matches RFC 4122 text form.
func UUID(s string) bool { return uuidRe.MatchString(s) }

// ULID matches the ULID grammar.
func ULID(s string) bool { return ulidRe.MatchString(s) }

// IPv4 accepts dotted-quad addresses only.
func IPv4(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil && strings.Count(s, ".") == 3
}

// IPv6 accepts textual IPv6 addresses (not IPv4-mapped dotted forms).
func IPv6(s string) bool { ip := net.ParseIP(s); return ip != nil && strings.Contains(s, ":") }

// IP accepts either address family.
func IP(s string) bool { return net.ParseIP(s) != nil }

// CIDR accepts an address with prefix length.
func CIDR(s string) bool { _, _, err := net.ParseCIDR(s); return err == nil }

// MAC accepts the colon, hyphen and dotted forms of 48-bit addresses. The
// exported pattern is the same set, so Go and JSON Schema agree; the
// longer EUI-64/InfiniBand forms net.ParseMAC also knows are excluded on
// purpose.
func MAC(s string) bool { return macRe.MatchString(s) }

// Hostname is RFC 1123: labels of letters, digits and hyphens, at most 253
// characters. The length is checked in code because a lookahead would leave
// the RE2 ∩ ECMA-262 subset; JSON Schema consumers get the standard
// "hostname" format instead of this pattern.
func Hostname(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	return hostnameRe.MatchString(s)
}

var hostnameRe = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)

// Base64 accepts standard-alphabet, padded base64; decoding confirms it.
func Base64(s string) bool {
	if !base64Re.MatchString(s) {
		return false
	}
	_, err := base64.StdEncoding.DecodeString(s)
	return err == nil
}

// JSON accepts any JSON document.
func JSON(s string) bool { return json.Valid([]byte(s)) }

// Semver accepts Semantic Versioning 2.0.0.
func Semver(s string) bool { return semverRe.MatchString(s) }

// Date accepts RFC 3339 full-date and checks the calendar (no Feb 30).
func Date(s string) bool {
	if !dateRe.MatchString(s) {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// DateTime accepts RFC 3339 date-time.
func DateTime(s string) bool { _, err := time.Parse(time.RFC3339, s); return err == nil }

var (
	jwtRe          = regexp.MustCompile(JWTPattern)
	countryRe      = regexp.MustCompile(CountryCodePattern)
	currencyRe     = regexp.MustCompile(CurrencyCodePattern)
	hexColorRe     = regexp.MustCompile(HexColorPattern)
	e164Re         = regexp.MustCompile(E164Pattern)
	creditDigitsRe = regexp.MustCompile(CreditCardPattern)
)

// CreditCard strips spaces and dashes, requires 12–19 digits, and applies
// the Luhn checksum.
func CreditCard(s string) bool {
	digits := strings.NewReplacer(" ", "", "-", "").Replace(s)
	if !creditDigitsRe.MatchString(digits) {
		return false
	}
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

// JWT checks the three-segment base64url shape, not the signature.
func JWT(s string) bool { return jwtRe.MatchString(s) }

// CountryCode checks the ISO 3166-1 alpha-2 shape.
func CountryCode(s string) bool { return countryRe.MatchString(s) }

// CurrencyCode checks the ISO 4217 shape.
func CurrencyCode(s string) bool { return currencyRe.MatchString(s) }

// HexColor accepts #RGB, #RRGGBB and #RRGGBBAA.
func HexColor(s string) bool { return hexColorRe.MatchString(s) }

// E164 accepts + followed by 2–15 digits with a non-zero first digit.
func E164(s string) bool { return e164Re.MatchString(s) }

// Latitude and Longitude are inclusive range checks.
func Latitude(f float64) bool  { return f >= -90 && f <= 90 }
func Longitude(f float64) bool { return f >= -180 && f <= 180 }

// NormalizeEmail is the normalizeEmail transform: trim surrounding space
// and lower-case the whole address. Lower-casing the local part is a
// deliberate simplification (RFC 5321 allows case-sensitive local parts;
// no mainstream provider honors that) so "Ada@Example.com" and
// "ada@example.com" collapse to one value for uniqueness checks.
func NormalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Coerce converts a string carried by JSON or form input into the Go type
// a node expects, for the coerce transform on map input. It returns the
// original value and false when the string does not parse, so the type
// check reports it as it would any wrong-typed value.
func Coerce(kind ir.Kind, v any) (any, bool) {
	s, isStr := v.(string)
	if !isStr {
		return v, false
	}
	s = strings.TrimSpace(s)
	switch kind {
	case ir.KindInt:
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return float64(n), true
		}
	case ir.KindFloat:
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f, true
		}
	case ir.KindBool:
		if b, err := strconv.ParseBool(s); err == nil {
			return b, true
		}
	case ir.KindTime:
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t, true
		}
	case ir.KindDuration:
		if d, err := time.ParseDuration(s); err == nil {
			return d, true
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Duration(n), true
		}
	}
	return v, false
}

// NotBlank requires a non-whitespace character.
func NotBlank(s string) bool { return strings.TrimSpace(s) != "" }

// MultipleOf reports whether a is an integer multiple of b; b == 0 never
// passes.
func MultipleOf(a, b float64) bool {
	if b == 0 {
		return false
	}
	q := a / b
	return math.Abs(q-math.Round(q)) < FloatEpsilon
}

// RuneLen is the length rule's measure for strings: code points, not
// bytes, as JSON Schema's minLength counts.
func RuneLen(s string) int { return utf8.RuneCountInString(s) }

// Unique checks pairwise distinctness of a slice or array. Comparable
// element types use a set; the rest fall back to DeepEqual, which is
// quadratic but correct.
func Unique(val reflect.Value) bool {
	n := val.Len()
	if n < 2 {
		return true
	}
	if val.Type().Elem().Comparable() {
		seen := make(map[any]struct{}, n)
		for i := range n {
			k := val.Index(i).Interface()
			if _, dup := seen[k]; dup {
				return false
			}
			seen[k] = struct{}{}
		}
		return true
	}
	for i := range n {
		for j := i + 1; j < n; j++ {
			if reflect.DeepEqual(val.Index(i).Interface(), val.Index(j).Interface()) {
				return false
			}
		}
	}
	return true
}

// Norm canonicalizes a value for equality: every number becomes float64,
// strings and bools stay, everything else is compared by its printed form
// so a JSON literal and a Go literal of the same meaning are equal.
func Norm(v any) any {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return nil
	}
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint())
	case reflect.Float32, reflect.Float64:
		return rv.Float()
	case reflect.String:
		return rv.String()
	case reflect.Bool:
		return rv.Bool()
	}
	return sprint(rv.Interface())
}

// In reports membership of v in values under Norm equality.
func In(v any, values []any) bool {
	want := Norm(v)
	for _, c := range values {
		if Norm(c) == want {
			return true
		}
	}
	return false
}
