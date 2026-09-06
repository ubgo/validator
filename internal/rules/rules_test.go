package rules

import (
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/ubgo/validator/ir"
)

func TestFormats(t *testing.T) {
	if !Email("a@b.io") || Email("Name <a@b.io>") || Email("nope") {
		t.Error("Email")
	}
	if !URL("https://x.io/p") || URL("/relative") || URL("x.io") || URL("http://") {
		t.Error("URL")
	}
	if !UUID("123e4567-e89b-12d3-a456-426614174000") || UUID("123e4567") {
		t.Error("UUID")
	}
	if !ULID("01ARZ3NDEKTSV4RRFFQ69G5FAV") || ULID("81ARZ3NDEKTSV4RRFFQ69G5FAV") || ULID("short") {
		t.Error("ULID")
	}
}

func TestNumeric(t *testing.T) {
	if !MultipleOf(0.3, 0.1) || !MultipleOf(10, 5) || MultipleOf(7, 2) || MultipleOf(4, 0) {
		t.Error("MultipleOf")
	}
	if RuneLen("héé") != 3 || RuneLen("") != 0 {
		t.Error("RuneLen")
	}
}

func TestUnique(t *testing.T) {
	if !Unique(reflect.ValueOf([]int{1, 2})) || Unique(reflect.ValueOf([]int{1, 1})) || !Unique(reflect.ValueOf([]int{1})) {
		t.Error("comparable")
	}
	if !Unique(reflect.ValueOf([][]int{{1}, {2}})) || Unique(reflect.ValueOf([][]int{{1}, {1}})) {
		t.Error("deep")
	}
	if !Unique(reflect.ValueOf([2]string{"a", "b"})) {
		t.Error("array")
	}
}

func TestNormAndIn(t *testing.T) {
	x := 3
	cases := []struct {
		in   any
		want any
	}{
		{nil, nil}, {(*int)(nil), nil}, {&x, 3.0}, {int8(3), 3.0}, {uint16(3), 3.0}, {3.5, 3.5},
		{"s", "s"}, {true, true}, {[]int{1}, "[1]"}, {any(&x), 3.0},
	}
	for _, c := range cases {
		if got := Norm(c.in); got != c.want {
			t.Errorf("Norm(%v) = %v want %v", c.in, got, c.want)
		}
	}
	if !In(3, []any{1.0, 3.0}) || In("3", []any{3}) || !In("a", []any{"a"}) || In(nil, nil) {
		t.Error("In")
	}
}

func TestTextFor(t *testing.T) {
	r := ir.MinLength(3)
	if code, tmpl := TextFor(&r); code != "min_length" || tmpl != "must be at least {value} characters" {
		t.Errorf("defaults %s %s", code, tmpl)
	}
	r.Meta.Code, r.Meta.Message = "short", "too short"
	if code, tmpl := TextFor(&r); code != "short" || tmpl != "too short" {
		t.Errorf("overrides %s %s", code, tmpl)
	}
	unknown := ir.Rule{Kind: "nope"}
	if code, _ := TextFor(&unknown); code != "" {
		t.Error("unknown kind has no default")
	}
	// Every registered kind has a text entry — the table must not lag the
	// vocabulary.
	for _, k := range []ir.RuleKind{ir.RuleMinLength, ir.RuleMaxLength, ir.RuleMinItems, ir.RuleMaxItems, ir.RuleUnique, ir.RuleMin, ir.RuleMax, ir.RuleGt, ir.RuleGte, ir.RuleLt, ir.RuleLte, ir.RuleMultipleOf, ir.RulePattern, ir.RuleEnum, ir.RuleConst, ir.RuleEmail, ir.RuleURL, ir.RuleUUID, ir.RuleULID, ir.RuleIPv4, ir.RuleIPv6, ir.RuleIP, ir.RuleCIDR, ir.RuleMAC, ir.RuleHostname, ir.RuleBase64, ir.RuleJSON, ir.RuleSemver, ir.RuleDate, ir.RuleDateTime, ir.RuleNotBlank, ir.RuleCreditCard, ir.RuleJWT, ir.RuleCountryCode, ir.RuleCurrencyCode, ir.RuleHexColor, ir.RuleE164, ir.RuleLatitude, ir.RuleLongitude, ir.RuleAfter, ir.RuleBefore, ir.RuleCustom, ir.RuleAsync} {
		if _, ok := Texts[k]; !ok {
			t.Errorf("no text for %s", k)
		}
	}
}

func TestNewFormats(t *testing.T) {
	yes := map[string][]string{
		"ipv4": {"192.168.0.1", "0.0.0.0"}, "ipv6": {"::1", "2001:db8::8a2e:370:7334"}, "ip": {"10.0.0.1", "::1"},
		"cidr": {"10.0.0.0/8", "2001:db8::/32"}, "mac": {"00:1A:2b:3c:4d:5e", "00-1a-2b-3c-4d-5e", "001a.2b3c.4d5e"},
		"hostname": {"example.com", "a-b.c1", "localhost"}, "base64": {"aGVsbG8=", "", "YQ=="}, "json": {`{"a":[1]}`, "1", "null"},
		"semver": {"1.0.0", "2.1.3-beta.1+build.5", "0.0.1"}, "date": {"2026-02-28", "2024-02-29"},
		"datetime": {"2026-01-01T00:00:00Z", "2026-01-01T10:00:00+05:30"}, "notblank": {"a", " x "},
	}
	no := map[string][]string{
		"ipv4": {"256.1.1.1", "::1", "1.2.3", "a.b.c.d"}, "ipv6": {"192.168.0.1", "zz::1"}, "ip": {"nope", ""},
		"cidr": {"10.0.0.0", "10.0.0.0/33"}, "mac": {"00:1A:2b", "zz:zz:zz:zz:zz:zz", "0000.0000.0000.0000.0000.0000.0000.0000"},
		"hostname": {"", "-bad.com", "a..b", "toolong" + string(make([]byte, 250)) + ".com", "under_score.com"},
		"base64":   {"aGVsbG8", "!!!!", "aGVsbG8==="}, "json": {"{", "", "nope"},
		"semver": {"1.0", "01.0.0", "1.0.0-", "v1.0.0"}, "date": {"2026-13-01", "2026-02-30", "2026/01/01", "20260101"},
		"datetime": {"2026-01-01", "2026-01-01T25:00:00Z", "x"}, "notblank": {"", "   ", "\t\n"},
	}
	fns := map[string]func(string) bool{
		"ipv4": IPv4, "ipv6": IPv6, "ip": IP, "cidr": CIDR, "mac": MAC, "hostname": Hostname, "base64": Base64,
		"json": JSON, "semver": Semver, "date": Date, "datetime": DateTime, "notblank": NotBlank,
	}
	for name, fn := range fns {
		for _, v := range yes[name] {
			if !fn(v) {
				t.Errorf("%s(%q) should pass", name, v)
			}
		}
		for _, v := range no[name] {
			if fn(v) {
				t.Errorf("%s(%q) should fail", name, v)
			}
		}
	}
	// Every exported pattern must compile under RE2; the emitter hands them
	// to ECMA-262 engines unchanged.
	for name, pat := range map[string]string{"base64": Base64Pattern, "semver": SemverPattern, "mac": MACPattern, "date": DatePattern, "notblank": NotBlankPattern, "cidr": CIDRPattern} {
		if _, err := regexp.Compile(pat); err != nil {
			t.Errorf("%s pattern is not RE2: %v", name, err)
		}
	}
}

func TestTransforms(t *testing.T) {
	if NormalizeEmail("  Ada@Example.COM ") != "ada@example.com" {
		t.Error("NormalizeEmail")
	}
	cases := []struct {
		kind ir.Kind
		in   any
		want any
		ok   bool
	}{
		{ir.KindInt, "42", 42.0, true}, {ir.KindInt, " 7 ", 7.0, true}, {ir.KindInt, "4.2", "4.2", false}, {ir.KindInt, 42, 42, false},
		{ir.KindFloat, "2.5", 2.5, true}, {ir.KindFloat, "x", "x", false},
		{ir.KindBool, "true", true, true}, {ir.KindBool, "0", false, true}, {ir.KindBool, "yes", "yes", false},
		{ir.KindTime, "2026-01-01T00:00:00Z", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), true}, {ir.KindTime, "2026", "2026", false},
		{ir.KindDuration, "1h30m", 90 * time.Minute, true}, {ir.KindDuration, "1500", time.Duration(1500), true}, {ir.KindDuration, "soon", "soon", false},
		{ir.KindString, "s", "s", false},
	}
	for _, c := range cases {
		got, ok := Coerce(c.kind, c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("Coerce(%s, %v) = %v,%v want %v,%v", c.kind, c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestMoreFormats(t *testing.T) {
	if !CreditCard("4242 4242 4242 4242") || !CreditCard("4111-1111-1111-1111") || CreditCard("4242424242424241") || CreditCard("123") || CreditCard("abcd") {
		t.Error("CreditCard")
	}
	if !JWT("eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abc-_") || !JWT("a.b.") || JWT("a.b") || JWT("a.b.c.d") {
		t.Error("JWT")
	}
	if !CountryCode("US") || CountryCode("us") || CountryCode("USA") || !CurrencyCode("USD") || CurrencyCode("US") {
		t.Error("codes")
	}
	if !HexColor("#fff") || !HexColor("#FF00aa") || !HexColor("#ff00aa80") || HexColor("fff") || HexColor("#ffff") {
		t.Error("HexColor")
	}
	if !E164("+14155552671") || E164("14155552671") || E164("+0123") {
		t.Error("E164")
	}
	if !Latitude(90) || Latitude(90.1) || !Longitude(-180) || Longitude(181) {
		t.Error("coordinates")
	}
	for name, pat := range map[string]string{"cc": CreditCardPattern, "jwt": JWTPattern, "country": CountryCodePattern, "currency": CurrencyCodePattern, "hex": HexColorPattern, "e164": E164Pattern} {
		if _, err := regexp.Compile(pat); err != nil {
			t.Errorf("%s pattern is not RE2: %v", name, err)
		}
	}
}
