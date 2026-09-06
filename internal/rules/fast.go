package rules

import (
	"net/mail"
	"net/url"
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
	if emailFast(s) {
		return true
	}
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s
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
