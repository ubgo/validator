package ir

import (
	"fmt"
	"regexp/syntax"
	"strings"
)

// CheckPattern reports whether a regular expression stays inside the
// subset both Go's RE2 and ECMAScript accept with the same meaning. A
// pattern outside it would validate differently in Go and in the browser —
// or fail to compile on one side — which is exactly the silent divergence
// the IR exists to prevent, so [Check] rejects such rules up front.
//
// Rejected: anything RE2 cannot parse (lookaround, backreferences), inline
// flags `(?i)` (ECMAScript has no inline flags), `\A` / `\z` anchors, `\Q…\E`
// quoting, POSIX classes `[[:alpha:]]`, Unicode class escapes `\p{…}`
// (ECMAScript needs the `u` flag, which Ajv sets, but engines differ on
// script names), and named groups `(?P<n>…)` (ECMAScript spells them `(?<n>…)`).
func CheckPattern(pattern string) error {
	if _, err := syntax.Parse(pattern, syntax.Perl); err != nil {
		return fmt.Errorf("pattern %q is not valid RE2: %w", pattern, err)
	}
	for _, bad := range portabilityTraps {
		if strings.Contains(pattern, bad.needle) {
			return fmt.Errorf("pattern %q uses %s, which %s", pattern, bad.needle, bad.why)
		}
	}
	return nil
}

// portabilityTraps are RE2 constructs with no identical ECMAScript form.
var portabilityTraps = []struct{ needle, why string }{
	{"(?i", "inline flags have no ECMAScript equivalent; set case-insensitivity on the field instead"},
	{"(?m", "inline flags have no ECMAScript equivalent"},
	{"(?s", "inline flags have no ECMAScript equivalent"},
	{"(?U", "inline flags have no ECMAScript equivalent"},
	{"(?P<", "ECMAScript spells named groups (?<name>…); use a non-capturing group"},
	{`\A`, `ECMAScript has no \A; use ^`},
	{`\z`, `ECMAScript has no \z; use $`},
	{`\Q`, `ECMAScript has no \Q…\E quoting; escape characters individually`},
	{"[[:", "POSIX classes are not ECMAScript; use \\w, \\d or explicit ranges"},
	{`\p{`, "Unicode property classes differ between engines; use explicit ranges"},
	{`\P{`, "Unicode property classes differ between engines; use explicit ranges"},
}
