package rules

import (
	"net/mail"
	"net/url"
	"reflect"
	"strings"
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
		if i == 0 && !isAlpha(c) || i > 0 && !(isAlpha(c) || isDigit(c) || c == '+' || c == '-' || c == '.') {
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
