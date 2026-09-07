package rules

import (
	"net/mail"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"time"
)

// Allocation-free fast paths for the two format rules whose reference
// implementations allocate: net/mail and net/url build parsed structures
// on every call, which put seven allocations on a generated validator's
// success path for one email and one URL.
//
// The contract is asymmetric on purpose. A fast path may only ACCEPT a
// value the reference implementation also accepts; anything it cannot
// vouch for — quoted local parts, IPv6 hosts, unusual characters — it
// hands to the reference, which stays the semantic authority. So the
// answer never changes; only the cost of the common case does.
// TestFastPathsAreSubsets fuzzes that property.

// Email reports whether s is a bare addr-spec (no display name), the
// net/mail acceptance narrowed to the address itself.
func Email(s string) bool {
	if emailReject(s) {
		return false
	}
	if emailFast(s) {
		return true
	}
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s
}

// emailReject is the mirror of emailFast: where the accept path may only
// say yes to what net/mail also accepts, this may only say no to what
// net/mail also rejects. It exists because the accept path leaves every
// INVALID address paying for a full net/mail parse (270 ns and 9
// allocations, against 47 ns and none for a valid one), and a validator
// meets invalid input by definition.
//
// Each test below is a property of the addr-spec grammar, not a heuristic:
// an address must contain an "@", and neither the local part nor the
// domain may be empty. Anything subtler is left to net/mail.
func emailReject(s string) bool {
	at := strings.IndexByte(s, '@')
	return at < 0 || at == 0 || at == len(s)-1
}

// emailFast accepts dot-atom@dot-atom in ASCII: RFC 5322 atext on the
// left, letters/digits/hyphen labels on the right, no empty labels. Every
// such string is an addr-spec net/mail parses back to itself.
func emailFast(s string) bool {
	at := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '@' {
			if at >= 0 {
				return false
			}
			at = i
		}
	}
	if at <= 0 || at == len(s)-1 {
		return false
	}
	return dotAtom(s[:at], isAtext) && dotAtom(s[at+1:], isLDH)
}

// dotAtom checks a run of labels separated by single dots, each non-empty
// and made only of allowed bytes.
func dotAtom(s string, allowed func(byte) bool) bool {
	if s == "" || s[0] == '.' || s[len(s)-1] == '.' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '.' {
			if s[i-1] == '.' {
				return false
			}
			continue
		}
		if !allowed(c) {
			return false
		}
	}
	return true
}

// isAtext is RFC 5322 atext: printable ASCII minus specials.
func isAtext(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '/', '=', '?', '^', '_', '`', '{', '|', '}', '~':
		return true
	}
	return false
}

// isLDH is letters, digits, hyphen: the hostname label alphabet.
func isLDH(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-'
}

// URL requires an absolute URL with scheme and host.
func URL(s string) bool {
	// Same asymmetry as Email: a string with no ":" cannot carry a scheme,
	// so url.Parse would leave Scheme empty and the check would fail. Every
	// other shape goes to url.Parse.
	if !strings.Contains(s, ":") {
		return false
	}
	if urlFast(s) {
		return true
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme != "" && u.Host != ""
}

// urlFast accepts scheme://host[:port][/path][?query][#fragment] where the
// scheme is RFC 3986, the host is a dot-atom of LDH labels, the port is
// digits, and the rest is unreserved / sub-delim / pchar bytes with valid
// percent-escapes. net/url parses every such string with that scheme and
// host; anything else falls back to it.
func urlFast(s string) bool {
	i := 0
	for i < len(s) && s[i] != ':' {
		c := s[i]
		// RFC 3986: a scheme starts with a letter, then letters, digits,
		// "+", "-" or ".".
		ok := isAlpha(c)
		if i > 0 {
			ok = ok || isDigit(c) || c == '+' || c == '-' || c == '.'
		}
		if !ok {
			return false
		}
		i++
	}
	if i == 0 || i+3 > len(s) || s[i:i+3] != "://" {
		return false
	}
	i += 3
	host := i
	for i < len(s) && isLDH(s[i]) || i < len(s) && s[i] == '.' {
		i++
	}
	if !dotAtom(s[host:i], isLDH) {
		return false
	}
	if i < len(s) && s[i] == ':' {
		i++
		port := i
		for i < len(s) && isDigit(s[i]) {
			i++
		}
		if i == port {
			return false
		}
	}
	if i < len(s) && s[i] != '/' && s[i] != '?' && s[i] != '#' {
		return false
	}
	for i < len(s) {
		c := s[i]
		switch {
		case c == '%':
			if i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2]) {
				return false
			}
			i += 3
			continue
		case isURLByte(c):
			i++
			continue
		}
		return false
	}
	return true
}

func isAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isHex(c byte) bool {
	return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// isURLByte is the pchar / query / fragment alphabet minus '%', which is
// checked with its two hex digits.
func isURLByte(c byte) bool {
	if isAlpha(c) || isDigit(c) {
		return true
	}
	switch c {
	case '-', '.', '_', '~', '!', '$', '&', '\'', '(', ')', '*', '+', ',', ';', '=', ':', '@', '/', '?', '#':
		return true
	}
	return false
}

// InValue is [In] over a reflect.Value. The interpreter reaches the enum
// and const rules holding a reflect.Value; calling In forces two boxing
// allocations per check, one to turn the value into an `any` and one
// inside Norm. This walks the candidates without boxing for the three
// kinds that cover every practical enum (string, number, bool) and falls
// back to In for anything else, so the answer is always In's answer.
// TestInValueMatchesIn pins that equivalence.
func InValue(rv reflect.Value, values []any) bool {
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return In(nil, values)
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.String:
		s := rv.String()
		for _, c := range values {
			if cs, isStr := c.(string); isStr {
				if cs == s {
					return true
				}
				continue
			}
			// A candidate of some other shape can still normalize to this
			// string (a *string, say); those are rare, so pay for them only
			// when they appear.
			if Norm(c) == any(s) {
				return true
			}
		}
		return false

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		var f float64
		switch rv.Kind() {
		case reflect.Float32, reflect.Float64:
			f = rv.Float()
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			f = float64(rv.Uint())
		default:
			f = float64(rv.Int())
		}
		for _, c := range values {
			if cf, isNum := candidateFloat(c); isNum {
				if cf == f {
					return true
				}
				continue
			}
			if Norm(c) == any(f) {
				return true
			}
		}
		return false

	case reflect.Bool:
		b := rv.Bool()
		for _, c := range values {
			if cb, isBool := c.(bool); isBool {
				if cb == b {
					return true
				}
				continue
			}
			if Norm(c) == any(b) {
				return true
			}
		}
		return false
	}
	return In(rv.Interface(), values)
}

// candidateFloat reads a directly-typed numeric candidate. A type switch on
// an interface value does not allocate, which is the point.
func candidateFloat(c any) (float64, bool) {
	switch x := c.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint8:
		return float64(x), true
	case uint16:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	}
	return 0, false
}

// UUID and ULID have fixed shapes, so a direct scan decides them without a
// regexp engine. Measured on a valid input, the regexp cost 228 ns; this
// costs a few. The regexps remain in rules.go as the reference these must
// agree with, and TestUUIDULIDMatchRegexp plus FuzzFastPaths check that
// agreement on arbitrary input.

// uuidLen is the canonical 8-4-4-4-12 length, dashes included.
const uuidLen = 36

// uuidDashes are the positions the dashes must occupy.
var uuidDashes = [4]int{8, 13, 18, 23}

// UUID matches RFC 4122 text form: hex digits in 8-4-4-4-12 groups.
func UUID(s string) bool {
	if len(s) != uuidLen {
		return false
	}
	for _, i := range uuidDashes {
		if s[i] != '-' {
			return false
		}
	}
	for i := 0; i < uuidLen; i++ {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !isHex(s[i]) {
			return false
		}
	}
	return true
}

// ulidLen is the fixed length of a ULID's Crockford base32 encoding.
const ulidLen = 26

// ULID matches the ULID grammar: 26 Crockford base32 characters, the first
// no greater than '7' so the 48-bit timestamp cannot overflow.
func ULID(s string) bool {
	if len(s) != ulidLen || s[0] < '0' || s[0] > '7' {
		return false
	}
	for i := 0; i < ulidLen; i++ {
		if !isCrockford(s[i]) {
			return false
		}
	}
	return true
}

// isCrockford reports whether c is in Crockford's base32 alphabet as ULID
// uses it: digits and upper-case letters excluding I, L, O and U, which are
// omitted because they read as 1, 1, 0 and V.
func isCrockford(c byte) bool {
	switch {
	case c >= '0' && c <= '9':
		return true
	case c >= 'A' && c <= 'H', c == 'J', c == 'K', c == 'M', c == 'N',
		c >= 'P' && c <= 'T', c >= 'V' && c <= 'Z':
		return true
	}
	return false
}

// The long-tail formats. Each was added on one test: a fixed,
// international grammar, checkable without a lookup table that goes
// stale. Anything national or dialect-dependent stays a Pattern rule the
// author owns and can correct.

// bicPattern is ISO 9362: 4 letters institution, 2 letters country,
// 2 alphanumeric location, optional 3 alphanumeric branch.
const BICPattern = `^[A-Z]{4}[A-Z]{2}[A-Z0-9]{2}([A-Z0-9]{3})?$`

// MongoIDPattern is a 24-character hex ObjectID.
const MongoIDPattern = `^[0-9a-fA-F]{24}$`

// Base64URLPattern is RFC 4648 §5: the URL-safe alphabet, with optional
// padding. Length must be a multiple of 4 once padded, which the scan
// below enforces because a regex cannot.
const Base64URLPattern = `^[A-Za-z0-9_-]*={0,2}$`

// Timezone reports whether s names a zone the Go runtime can load. It
// answers exactly what time.LoadLocation answers, so a value that passes
// here will not fail when the program uses it.
//
// "Local" is refused deliberately: it is a machine-dependent alias, not a
// zone, and accepting it would mean a value that validates in one
// deployment and behaves differently in another.
//
// One caveat worth knowing: LoadLocation reads the zone database from
// disk, so on a case-insensitive filesystem — macOS by default — it
// accepts "europe/berlin" as readily as "Europe/Berlin". The same input
// is refused on Linux. This function reports what the running program
// would do rather than imposing a stricter rule the program does not
// follow; if canonical casing matters, pair it with a Pattern.
func Timezone(s string) bool {
	if s == "" || s == "Local" {
		return false
	}
	_, err := time.LoadLocation(s)
	return err == nil
}

// Base64URL reports the URL-safe alphabet with correct padding.
func Base64URL(s string) bool {
	if s == "" {
		return true // an empty string encodes empty bytes, as in Base64
	}
	body := strings.TrimRight(s, "=")
	if pad := len(s) - len(body); pad > 2 {
		return false
	} else if pad > 0 && len(s)%4 != 0 {
		// Padding is optional in RFC 4648 §5 and a JWT segment carries
		// none. But a string that IS padded must be padded correctly,
		// or it is a truncated standard-base64 string rather than a
		// deliberate unpadded one.
		return false
	}
	if len(body)%4 == 1 {
		// No amount of padding makes this a whole number of bytes.
		return false
	}
	for i := 0; i < len(body); i++ {
		c := body[i]
		if !isAlpha(c) && !isDigit(c) && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

// ISBN reports a valid ISBN-10 or ISBN-13 including its check digit.
// Hyphens and spaces are decoration in the printed form and are ignored.
func ISBN(s string) bool {
	var d []byte
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '-' || c == ' ':
		case isDigit(c), c == 'X', c == 'x':
			d = append(d, c)
		default:
			return false
		}
	}
	switch len(d) {
	case 10:
		return isbn10(d)
	case 13:
		return isbn13(d)
	}
	return false
}

// isbn10 sums each digit times its position, modulo 11. The last digit may
// be X, standing for ten.
func isbn10(d []byte) bool {
	sum := 0
	for i := 0; i < 9; i++ {
		if !isDigit(d[i]) {
			return false
		}
		sum += int(d[i]-'0') * (10 - i)
	}
	last := d[9]
	switch {
	case last == 'X' || last == 'x':
		sum += 10
	case isDigit(last):
		sum += int(last - '0')
	default:
		return false
	}
	return sum%11 == 0
}

// isbn13 is the EAN-13 checksum: alternating weights of 1 and 3.
func isbn13(d []byte) bool {
	sum := 0
	for i := 0; i < 13; i++ {
		if !isDigit(d[i]) {
			return false // X is valid only as an ISBN-10 check digit
		}
		w := 1
		if i%2 == 1 {
			w = 3
		}
		sum += int(d[i]-'0') * w
	}
	return sum%10 == 0
}

// BIC reports a SWIFT business identifier code.
func BIC(s string) bool { return bicRe.MatchString(s) }

// MongoID reports a 24-character hexadecimal ObjectID.
func MongoID(s string) bool {
	if len(s) != 24 {
		return false
	}
	for i := 0; i < 24; i++ {
		if !isHex(s[i]) {
			return false
		}
	}
	return true
}

// bicRe is compiled once; the pattern is exported for the emitter.
var bicRe = regexp.MustCompile(BICPattern)

// ISBNShapePattern is the digit SHAPE of an ISBN, for consumers that can
// only express a pattern. It deliberately does not check the checksum,
// which no regular expression can; the emitter reports that gap.
const ISBNShapePattern = `^(?:[0-9][- ]?){9}[0-9Xx]$|^(?:[0-9][- ]?){12}[0-9]$`
