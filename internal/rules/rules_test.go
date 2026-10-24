package rules

import (
	"math/rand/v2"
	"net/mail"
	"net/url"
	"reflect"
	"regexp"
	"strings"
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

// TestFastPathsAreSubsets pins the fast-path contract: whatever emailFast
// or urlFast accepts, the reference implementation accepts too, so the
// public answer never depends on which path ran. Random strings over the
// relevant alphabets plus a corpus of edge cases.
func TestFastPathsAreSubsets(t *testing.T) {
	refEmail := func(s string) bool { a, err := mail.ParseAddress(s); return err == nil && a.Address == s }
	refURL := func(s string) bool { u, err := url.Parse(s); return err == nil && u.Scheme != "" && u.Host != "" }
	corpus := []string{
		"a@b", "a@b.c", "a.b@c.d", ".a@b", "a.@b", "a..b@c", "a@.b", "a@b.", "a@b..c", "@b", "a@", "a@@b", "a b@c",
		"a@b_c", "\"q\"@b", "a@[1.2.3.4]", "ünï@b.c", "a@b-c.d", "a@-b", "a!#$%&'*+-/=?^_`{|}~@x.y",
		"https://x.io/p", "http://x", "x://y", "1x://y", "https://", "https:///p", "https://x.io:80/p?q=1#f", "https://x.io:/p",
		"https://x.io/%zz", "https://x.io/%2F", "https://x.io/a b", "https://[::1]/", "https://x.io/p\x7f", "ftp://a.b/c;d=e",
		"https://x.io?q", "https://x.io#f", "https://x..io", "https://x.io./", "mailto:a@b", "//x.io/p", "https://x.io/p?q=a%2",
	}
	// Fixed seed so a failure reproduces; the sizes are large enough to
	// reach every branch of both scanners several times per run.
	const fuzzCases, fuzzMaxLen = 20000, 24
	r := rand.New(rand.NewPCG(1, 2))
	alphabet := []byte("abcAZ09.@-_!#%:/?&=+~()[]{} \"'\\`|^$*,;\x7f\u00ff")
	for range fuzzCases {
		n := r.IntN(fuzzMaxLen)
		b := make([]byte, n)
		for i := range b {
			b[i] = alphabet[r.IntN(len(alphabet))]
		}
		corpus = append(corpus, string(b), "https://"+string(b), "a@"+string(b))
	}
	for _, s := range corpus {
		if emailFast(s) && !refEmail(s) {
			t.Errorf("emailFast accepts %q, net/mail does not", s)
		}
		// The mirror property: the reject path may only say no to what the
		// reference also rejects. A wrong "no" here silently fails a valid
		// address, which is worse than a slow one.
		if emailReject(s) && refEmail(s) {
			t.Errorf("emailReject rejects %q, net/mail accepts it", s)
		}
		if !strings.Contains(s, ":") && refURL(s) {
			t.Errorf("URL's no-colon rejection is wrong for %q", s)
		}
		if urlFast(s) && !refURL(s) {
			t.Errorf("urlFast accepts %q, net/url does not", s)
		}
		// And the public functions agree with the reference exactly.
		if Email(s) != refEmail(s) {
			t.Errorf("Email(%q) = %v, reference %v", s, Email(s), refEmail(s))
		}
		if URL(s) != refURL(s) {
			t.Errorf("URL(%q) = %v, reference %v", s, URL(s), refURL(s))
		}
	}
	// The common shapes take the fast path and do not allocate.
	for _, s := range []string{"ada@example.com", "a.b+tag@sub.example.co"} {
		if !emailFast(s) {
			t.Errorf("emailFast(%q) should hit", s)
		}
	}
	for _, s := range []string{"https://x.io", "https://x.io/p?q=1&r=%20#frag", "http://localhost:8080/"} {
		if !urlFast(s) {
			t.Errorf("urlFast(%q) should hit", s)
		}
	}
	if n := testing.AllocsPerRun(200, func() { Email("ada@example.com"); URL("https://x.io/p") }); n != 0 {
		t.Errorf("fast paths allocate %.1f", n)
	}
}

// TestInValueMatchesIn pins the fast path to the reference: InValue must
// answer exactly what In answers, for every combination of value kind and
// candidate kind the two can meet. A fast path that disagrees with the
// semantics it is optimizing is a correctness bug, not a speed-up.
func TestInValueMatchesIn(t *testing.T) {
	three, str, tru := 3, "3", true
	values := []any{
		nil, 3, int8(3), int64(3), uint(3), 3.0, 3.5, float32(2.5),
		"3", "x", "", true, false, &three, &str, &tru,
		[]int{1}, map[string]int{"a": 1}, struct{ A int }{1},
	}
	// Every candidate set worth trying: singletons, pairs, and the lot.
	var sets [][]any
	for _, v := range values {
		sets = append(sets, []any{v})
	}
	sets = append(sets, values, nil, []any{}, []any{3, "3"}, []any{"x", 3.0}, []any{nil, true})

	for _, v := range values {
		rv := reflect.ValueOf(v)
		if !rv.IsValid() {
			continue // In(nil, ...) has no reflect.Value counterpart to pass
		}
		for _, set := range sets {
			want := In(v, set)
			got := InValue(rv, set)
			if got != want {
				t.Errorf("InValue(%#v, %#v) = %v, In = %v", v, set, got, want)
			}
		}
	}
	// The fast kinds must not allocate.
	strVal, numVal, boolVal := reflect.ValueOf("b"), reflect.ValueOf(7), reflect.ValueOf(true)
	enum := []any{"a", "b", "c"}
	nums := []any{1, 7, 9}
	bools := []any{false, true}
	if n := testing.AllocsPerRun(200, func() {
		InValue(strVal, enum)
		InValue(numVal, nums)
		InValue(boolVal, bools)
	}); n != 0 {
		t.Errorf("InValue allocates %.1f per call on the fast kinds", n)
	}
}

// FuzzFastPaths is TestFastPathsAreSubsets driven by the fuzzer: for any
// input at all, the public functions must agree with the reference
// implementations they optimize. The accept and reject shortcuts are the
// only place in this library where a performance change can silently alter
// a validation answer, so they get a fuzz target of their own.
func FuzzFastPaths(f *testing.F) {
	for _, seed := range []string{
		"ada@example.com", "not-an-email", "@x.io", "a@", "a@@b", `"q"@b.io`, "a@[1.2.3.4]",
		"https://x.io/p", "x://y", "https://", "no-colon", "mailto:a@b", "https://x.io:80/p?q=1#f",
		"123e4567-e89b-12d3-a456-426614174000", "01ARZ3NDEKTSV4RRFFQ69G5FAV", "01IRZ3NDEKTSV4RRFFQ69G5FAV",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		wantEmail := func() bool { a, err := mail.ParseAddress(s); return err == nil && a.Address == s }()
		if Email(s) != wantEmail {
			t.Fatalf("Email(%q) = %v, net/mail says %v", s, Email(s), wantEmail)
		}
		wantURL := func() bool { u, err := url.Parse(s); return err == nil && u.Scheme != "" && u.Host != "" }()
		if URL(s) != wantURL {
			t.Fatalf("URL(%q) = %v, net/url says %v", s, URL(s), wantURL)
		}
		if UUID(s) != uuidRe.MatchString(s) {
			t.Fatalf("UUID(%q) = %v, regexp says %v", s, UUID(s), uuidRe.MatchString(s))
		}
		if ULID(s) != ulidRe.MatchString(s) {
			t.Fatalf("ULID(%q) = %v, regexp says %v", s, ULID(s), ulidRe.MatchString(s))
		}
	})
}

// TestUUIDULIDMatchRegexp pins the hand-written shape checks to the
// regexps they replaced. The regexps stay in rules.go for exactly this:
// a direct scan is only safe while it decides the same set.
func TestUUIDULIDMatchRegexp(t *testing.T) {
	cases := []string{
		"", "x", "123e4567-e89b-12d3-a456-426614174000", "123E4567-E89B-12D3-A456-426614174000",
		"123e4567-e89b-12d3-a456-42661417400", "123e4567-e89b-12d3-a456-4266141740000",
		"123e4567e89b12d3a456426614174000", "123e4567-e89b-12d3-a456-42661417400g",
		"123e4567+e89b-12d3-a456-426614174000", "-23e4567-e89b-12d3-a456-426614174000",
		"01ARZ3NDEKTSV4RRFFQ69G5FAV", "81ARZ3NDEKTSV4RRFFQ69G5FAV", "01arz3ndektsv4rrffq69g5fav",
		"01ARZ3NDEKTSV4RRFFQ69G5FA", "01ARZ3NDEKTSV4RRFFQ69G5FAVX",
		"01IRZ3NDEKTSV4RRFFQ69G5FAV", "01LRZ3NDEKTSV4RRFFQ69G5FAV",
		"01ORZ3NDEKTSV4RRFFQ69G5FAV", "01URZ3NDEKTSV4RRFFQ69G5FAV",
	}
	// Plus every single-character mutation of a valid ULID, which is where
	// an alphabet mistake would hide.
	valid := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	for i := range valid {
		for c := byte(0); c < 128; c++ {
			cases = append(cases, valid[:i]+string(c)+valid[i+1:])
		}
	}
	for _, s := range cases {
		if got, want := UUID(s), uuidRe.MatchString(s); got != want {
			t.Errorf("UUID(%q) = %v, regexp says %v", s, got, want)
		}
		if got, want := ULID(s), ulidRe.MatchString(s); got != want {
			t.Errorf("ULID(%q) = %v, regexp says %v", s, got, want)
		}
	}
	var sink bool
	if n := testing.AllocsPerRun(200, func() {
		sink = UUID("123e4567-e89b-12d3-a456-426614174000") && ULID(valid)
	}); n != 0 {
		t.Errorf("UUID/ULID allocate %.1f per call", n)
	}
	if !sink {
		t.Error("the allocation probe did not validate its own fixtures")
	}
}

// TestLongTailFormats covers the five long-tail formats. Each was chosen because its grammar is fixed and
// international; the tests below are mostly about the edges where a naive
// implementation would be wrong.
func TestLongTailFormats(t *testing.T) {
	t.Run("timezone", func(t *testing.T) {
		for _, ok := range []string{"UTC", "Europe/Berlin", "America/New_York", "Asia/Kolkata"} {
			if !Timezone(ok) {
				t.Errorf("Timezone(%q) should pass", ok)
			}
		}
		for _, bad := range []string{"", "Mars/Olympus", "GMT+5", "Local"} {
			if Timezone(bad) {
				t.Errorf("Timezone(%q) should fail", bad)
			}
		}
		// "Local" is refused on purpose: it is a machine-dependent alias,
		// so accepting it would mean a value that means different things
		// in different deployments.
		//
		// Casing is deliberately NOT asserted. LoadLocation reads the zone
		// database from disk, so "europe/berlin" is accepted on a
		// case-insensitive filesystem and refused on Linux. This function
		// reports what the running program would do; asserting either way
		// would make the suite pass on one platform and fail on another.
	})

	t.Run("base64url", func(t *testing.T) {
		// Unpadded is valid and is what a JWT segment looks like; 2, 3 and
		// 4 characters all encode a whole number of bytes.
		for _, ok := range []string{"", "ab", "abc", "aGVsbG8", "aGVsbG8=", "a-b_cd", "YQ=="} {
			if !Base64URL(ok) {
				t.Errorf("Base64URL(%q) should pass", ok)
			}
		}
		// A remainder of one character encodes no whole byte, padded or
		// not, so it is invalid at any length.
		for _, bad := range []string{"a+b/c", "aGVsbG8===", "aGVsbG8!", "aGVsbG8=x", "a", "abcde"} {
			if Base64URL(bad) {
				t.Errorf("Base64URL(%q) should fail", bad)
			}
		}
		// The alphabets are disjoint where it matters: a standard-base64
		// string with + or / is not valid here, and vice versa.
		if Base64URL("a+/b") || !Base64("YQ==") {
			t.Error("the two alphabets must not be confused")
		}
	})

	t.Run("isbn", func(t *testing.T) {
		for _, ok := range []string{
			"0306406152",        // ISBN-10
			"0-306-40615-2",     // hyphens are decoration
			"0 306 40615 2",     // so are spaces
			"080442957X",        // X as the check digit
			"9780306406157",     // ISBN-13
			"978-0-306-40615-7", // hyphenated
		} {
			if !ISBN(ok) {
				t.Errorf("ISBN(%q) should pass", ok)
			}
		}
		for _, bad := range []string{
			"", "0306406153", "9780306406158", // wrong check digits
			"030640615", "97803064061570", // wrong lengths
			"97803064X157", // X is only valid as an ISBN-10 check digit
			"0306,40615,2", // an unexpected separator
		} {
			if ISBN(bad) {
				t.Errorf("ISBN(%q) should fail", bad)
			}
		}
	})

	t.Run("bic", func(t *testing.T) {
		for _, ok := range []string{"DEUTDEFF", "DEUTDEFF500", "NEDSZAJJXXX"} {
			if !BIC(ok) {
				t.Errorf("BIC(%q) should pass", ok)
			}
		}
		for _, bad := range []string{"", "DEUTDEF", "DEUTDEFF5", "deutdeff", "DEUT1EFF"} {
			if BIC(bad) {
				t.Errorf("BIC(%q) should fail", bad)
			}
		}
	})

	t.Run("mongoID", func(t *testing.T) {
		for _, ok := range []string{"507f1f77bcf86cd799439011", "AAAAAAAAAAAAAAAAAAAAAAAA"} {
			if !MongoID(ok) {
				t.Errorf("MongoID(%q) should pass", ok)
			}
		}
		for _, bad := range []string{"", "507f1f77bcf86cd79943901", "507f1f77bcf86cd7994390111", "507f1f77bcf86cd79943901g"} {
			if MongoID(bad) {
				t.Errorf("MongoID(%q) should fail", bad)
			}
		}
	})

	// Every exported pattern must be RE2, because the emitter hands them
	// to ECMA-262 engines unchanged.
	for name, pat := range map[string]string{
		"bic": BICPattern, "mongo": MongoIDPattern,
		"base64url": Base64URLPattern, "isbn": ISBNShapePattern,
	} {
		re, err := regexp.Compile(pat)
		if err != nil {
			t.Errorf("%s pattern is not RE2: %v", name, err)
			continue
		}
		// And the shape patterns must at least accept what the Go check
		// accepts, or the browser would reject a value the server takes.
		switch name {
		case "bic":
			if !re.MatchString("DEUTDEFF") {
				t.Error("the BIC pattern rejects a valid BIC")
			}
		case "mongo":
			if !re.MatchString("507f1f77bcf86cd799439011") {
				t.Error("the ObjectID pattern rejects a valid id")
			}
		case "isbn":
			for _, ok := range []string{"0306406152", "978-0-306-40615-7"} {
				if !re.MatchString(ok) {
					t.Errorf("the ISBN shape pattern rejects %q", ok)
				}
			}
		}
	}
}
