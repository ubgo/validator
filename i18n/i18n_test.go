package i18n

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ubgo/validator/interp"
	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/vb"
	"github.com/ubgo/validator/verr"
)

func TestDefaultCoversEveryCode(t *testing.T) {
	def := Default()
	if def.Locale != DefaultLocale {
		t.Error("locale")
	}
	// Every rule kind's code and every structural code is present.
	for _, k := range []ir.RuleKind{ir.RuleMinLength, ir.RuleEmail, ir.RuleCustom, ir.RuleAsync, ir.RuleSemver, ir.RuleAfter} {
		code, ok := KindCode(k)
		if !ok {
			t.Fatalf("no code for %s", k)
		}
		if _, ok := def.Messages[code]; !ok {
			t.Errorf("default lacks %s", code)
		}
	}
	for _, code := range []string{verr.CodeRequired, verr.CodeType, verr.CodeUnknownKey, verr.CodeVariant, verr.CodeUnion, verr.CodeGroup, verr.CodeCustom} {
		if _, ok := def.Messages[code]; !ok {
			t.Errorf("default lacks structural %s", code)
		}
	}
	if _, ok := KindCode("nope"); ok {
		t.Error("unknown kind")
	}
	codes := Codes()
	if len(codes) != len(def.Messages) || !sortedStrings(codes) {
		t.Error("Codes must list every default code, sorted")
	}
	if len(def.Missing()) != 0 {
		t.Errorf("default is missing %v", def.Missing())
	}
}

func sortedStrings(s []string) bool {
	for i := 1; i < len(s); i++ {
		if s[i-1] > s[i] {
			return false
		}
	}
	return true
}

func TestLoadMergeMissing(t *testing.T) {
	data, err := os.ReadFile("testdata/de.json")
	if err != nil {
		t.Fatal(err)
	}
	de, err := Load(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if de.Locale != "de" || len(de.Messages) != 8 {
		t.Errorf("loaded %+v", de)
	}
	if len(de.Missing()) == 0 {
		t.Error("partial catalog must report missing codes")
	}
	full := Merge(Default(), de)
	if full.Locale != "de" || len(full.Missing()) != 0 || full.Messages["email"] != "muss eine gültige E-Mail-Adresse sein" || full.Messages["url"] != "must be a valid URL" {
		t.Errorf("merge %+v", full)
	}
	if Merge(Default(), Catalog{Messages: map[string]string{"x": "y"}}).Locale != DefaultLocale {
		t.Error("merge keeps the base locale when override has none")
	}
	for _, bad := range []string{`{"locale":"x"}`, `{"messages":{"a":"b"}}`, `{"locale":"x","messages":{},"extra":1}`, `{`} {
		if _, err := Load(strings.NewReader(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	out, err := full.JSON()
	if err != nil || !strings.HasPrefix(string(out), "{\n  \"locale\": \"de\"") || !strings.HasSuffix(string(out), "}\n") {
		t.Errorf("JSON %s %v", out, err)
	}
	var back Catalog
	if err := json.Unmarshal(out, &back); err != nil || len(back.Messages) != len(full.Messages) {
		t.Error("round trip")
	}
}

func TestLocalizeRealIssues(t *testing.T) {
	type U struct {
		Email string `json:"email"`
		Age   int    `json:"age"`
	}
	fEmail := vb.NewField("Email", func(u U) string { return u.Email }).JSON("email")
	fAge := vb.NewField("Age", func(u U) int { return u.Age }).JSON("age")
	s := vb.Struct("U", vb.F(fEmail, vb.String().Email()), vb.F(fAge, vb.Int[int]().Min(18).Msg("age_min", "custom english"))).MustBuild()
	v, err := interp.FromSchema(s)
	if err != nil {
		t.Fatal(err)
	}
	errs := v.Run(context.Background(), U{Email: "x", Age: 3})
	raw, _ := os.ReadFile("testdata/de.json")
	de, _ := Load(bytes.NewReader(raw))
	loc := Merge(Default(), de).Localize(errs)
	if loc.Issues[0].Message != "muss eine gültige E-Mail-Adresse sein" || loc.Issues[0].Code != "email" || loc.Issues[0].Path != "$.email" {
		t.Errorf("localized %+v", loc.Issues[0])
	}
	// A rule-level code override without a catalog entry keeps its own message.
	if loc.Issues[1].Message != "custom english" || loc.Issues[1].Code != "age_min" {
		t.Errorf("untranslated override %+v", loc.Issues[1])
	}
	// The original is untouched, nil stays nil, params render.
	if errs.Issues[0].Message == loc.Issues[0].Message {
		t.Error("Localize must copy")
	}
	if Default().Localize(nil) != nil {
		t.Error("nil")
	}
	req := Default().Message(verr.Issue{Code: verr.CodeGroup, Params: map[string]any{"mode": "exactly-one", "fields": []string{"a", "b"}, "present": 2}})
	if req != "exactly-one of [a b] required, 2 present" {
		t.Error(req)
	}
}
