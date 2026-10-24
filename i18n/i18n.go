// Package i18n is the message catalog: one map from issue code to message
// template, shared by Go and the TypeScript runtime, so both ends render
// the same words for the same failure (docsi/IR.md §6).
//
// Codes are the contract; messages are presentation. Executors always
// produce an issue with a code and its params; a catalog re-renders the
// message for a locale from those two, so translations live in one JSON
// file per language and never in rule definitions. The default English
// catalog is derived from the same table the executors use, which keeps it
// complete by construction — a test asserts every known code has an entry.
package i18n

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/ubgo/validator/internal/rules"
	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/verr"
)

// DefaultLocale is the locale of the built-in catalog.
const DefaultLocale = "en"

// Catalog maps issue codes to message templates for one locale.
// Placeholders are {name} for issue params, rendered by [verr.Render].
type Catalog struct {
	Locale   string            `json:"locale"`
	Messages map[string]string `json:"messages"`
}

// structural is the English text for the codes executors emit without a
// rule, in one place so the catalog and the executors cannot disagree.
var structural = map[string]string{
	verr.CodeRequired:   "field is required",
	verr.CodeType:       "expected {expected}",
	verr.CodeUnknownKey: "unknown key",
	verr.CodeVariant:    "unknown variant {value}",
	verr.CodeUnion:      "value matches no variant",
	verr.CodeGroup:      "{mode} of {fields} required, {present} present",
	verr.CodeCustom:     "{error}",
}

// Default returns the built-in English catalog: every rule kind's default
// template plus the structural codes.
func Default() Catalog {
	msgs := make(map[string]string, len(rules.Texts)+len(structural))
	for _, t := range rules.Texts {
		msgs[t.Code] = t.Tmpl
	}
	for code, tmpl := range structural {
		msgs[code] = tmpl
	}
	return Catalog{Locale: DefaultLocale, Messages: msgs}
}

// Codes lists every code the default catalog knows, sorted — the set a
// translation should cover.
func Codes() []string {
	def := Default()
	out := make([]string, 0, len(def.Messages))
	for c := range def.Messages {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// Load reads a catalog from JSON. A catalog with no messages or no locale
// is refused: an empty translation would silently fall back to English
// for everything, which is exactly the surprise a loader should prevent.
func Load(r io.Reader) (Catalog, error) {
	var c Catalog
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Catalog{}, fmt.Errorf("i18n: %w", err)
	}
	if c.Locale == "" {
		return Catalog{}, errors.New("i18n: catalog has no locale")
	}
	if len(c.Messages) == 0 {
		return Catalog{}, errors.New("i18n: catalog has no messages")
	}
	return c, nil
}

// Merge layers override on top of base: override's messages win, base
// fills the gaps, and the result carries override's locale. Use it so a
// partial translation still renders every code.
func Merge(base, override Catalog) Catalog {
	out := Catalog{Locale: override.Locale, Messages: make(map[string]string, len(base.Messages)+len(override.Messages))}
	if out.Locale == "" {
		out.Locale = base.Locale
	}
	for k, v := range base.Messages {
		out.Messages[k] = v
	}
	for k, v := range override.Messages {
		out.Messages[k] = v
	}
	return out
}

// Missing lists the default codes a catalog does not translate.
func (c Catalog) Missing() []string {
	var out []string
	for _, code := range Codes() {
		if _, ok := c.Messages[code]; !ok {
			out = append(out, code)
		}
	}
	return out
}

// Message renders one issue's message from the catalog, or returns the
// issue's existing message when the code is not translated.
func (c Catalog) Message(is verr.Issue) string {
	tmpl, ok := c.Messages[is.Code]
	if !ok {
		return is.Message
	}
	return verr.Render(tmpl, is.Params)
}

// Localize returns a copy of errs with every message re-rendered from the
// catalog. Codes, paths and params are untouched; a nil input stays nil.
func (c Catalog) Localize(errs *verr.Errors) *verr.Errors {
	if errs == nil {
		return nil
	}
	out := &verr.Errors{Issues: make([]verr.Issue, len(errs.Issues))}
	for i, is := range errs.Issues {
		is.Message = c.Message(is)
		out.Issues[i] = is
	}
	return out
}

// JSON renders the catalog with sorted keys and a trailing newline — the
// file translators edit.
func (c Catalog) JSON() ([]byte, error) {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// KindCode returns the default code for a rule kind, so tooling can map
// vocabulary to catalog entries without reaching into the rule table.
func KindCode(k ir.RuleKind) (string, bool) {
	t, ok := rules.Texts[k]
	return t.Code, ok
}
