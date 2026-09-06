package interp

import (
	"maps"
	"reflect"
	"time"

	"github.com/ubgo/validator/internal/rules"
	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/verr"
)

// rules applies each rule to val, honoring per-rule conditions.
func (r *run) rules(path string, rs []ir.Rule, val reflect.Value, sc *scope) {
	for i := range rs {
		if r.stopped {
			return
		}
		rule := &rs[i]
		if rule.Cond != nil && !r.cond(sc, *rule.Cond) {
			continue
		}
		ok, extra := r.apply(rule, val, sc)
		if ok {
			continue
		}
		params := make(map[string]any, len(rule.Params)+len(extra))
		maps.Copy(params, rule.Params)
		maps.Copy(params, extra)
		code, tmpl := rules.TextFor(rule)
		r.issue(verr.Issue{
			Code: code, Path: path, Rule: string(rule.Kind),
			Message: verr.Render(tmpl, params), Params: params,
		})
	}
}

// apply evaluates one rule. It returns pass/fail plus any extra message
// parameters (a custom function's error text, for instance).
func (r *run) apply(rule *ir.Rule, val reflect.Value, sc *scope) (bool, map[string]any) {
	switch rule.Kind {
	case ir.RuleMinLength:
		n, _ := paramInt(rule.Params[ir.ParamValue])
		return length(val) >= n, nil
	case ir.RuleMaxLength:
		n, _ := paramInt(rule.Params[ir.ParamValue])
		return length(val) <= n, nil
	case ir.RuleMinItems:
		n, _ := paramInt(rule.Params[ir.ParamValue])
		return val.Len() >= n, nil
	case ir.RuleMaxItems:
		n, _ := paramInt(rule.Params[ir.ParamValue])
		return val.Len() <= n, nil
	case ir.RuleUnique:
		return rules.Unique(val), nil
	case ir.RuleMin, ir.RuleGte:
		return compareNum(val, rule.Params[ir.ParamValue], func(a, b float64) bool { return a >= b }), nil
	case ir.RuleMax, ir.RuleLte:
		return compareNum(val, rule.Params[ir.ParamValue], func(a, b float64) bool { return a <= b }), nil
	case ir.RuleGt:
		return compareNum(val, rule.Params[ir.ParamValue], func(a, b float64) bool { return a > b }), nil
	case ir.RuleLt:
		return compareNum(val, rule.Params[ir.ParamValue], func(a, b float64) bool { return a < b }), nil
	case ir.RuleMultipleOf:
		return compareNum(val, rule.Params[ir.ParamValue], rules.MultipleOf), nil
	case ir.RulePattern:
		pat, _ := rule.Params[ir.ParamRegex].(string)
		return r.v.regex[pat].MatchString(val.String()), nil
	case ir.RuleEnum:
		values, _ := rule.Params[ir.ParamValues].([]any)
		return rules.In(val.Interface(), values), nil
	case ir.RuleConst:
		return rules.Norm(val.Interface()) == rules.Norm(rule.Params[ir.ParamValue]), nil
	case ir.RuleEmail:
		return rules.Email(val.String()), nil
	case ir.RuleURL:
		return rules.URL(val.String()), nil
	case ir.RuleUUID:
		return rules.UUID(val.String()), nil
	case ir.RuleULID:
		return rules.ULID(val.String()), nil
	case ir.RuleIPv4:
		return rules.IPv4(val.String()), nil
	case ir.RuleIPv6:
		return rules.IPv6(val.String()), nil
	case ir.RuleIP:
		return rules.IP(val.String()), nil
	case ir.RuleCIDR:
		return rules.CIDR(val.String()), nil
	case ir.RuleMAC:
		return rules.MAC(val.String()), nil
	case ir.RuleHostname:
		return rules.Hostname(val.String()), nil
	case ir.RuleBase64:
		return rules.Base64(val.String()), nil
	case ir.RuleJSON:
		return rules.JSON(val.String()), nil
	case ir.RuleSemver:
		return rules.Semver(val.String()), nil
	case ir.RuleDate:
		return rules.Date(val.String()), nil
	case ir.RuleDateTime:
		return rules.DateTime(val.String()), nil
	case ir.RuleNotBlank:
		return rules.NotBlank(val.String()), nil
	case ir.RuleCreditCard:
		return rules.CreditCard(val.String()), nil
	case ir.RuleJWT:
		return rules.JWT(val.String()), nil
	case ir.RuleCountryCode:
		return rules.CountryCode(val.String()), nil
	case ir.RuleCurrencyCode:
		return rules.CurrencyCode(val.String()), nil
	case ir.RuleHexColor:
		return rules.HexColor(val.String()), nil
	case ir.RuleE164:
		return rules.E164(val.String()), nil
	case ir.RuleLatitude:
		f, ok := toFloat(val)
		return ok && rules.Latitude(f), nil
	case ir.RuleLongitude:
		f, ok := toFloat(val)
		return ok && rules.Longitude(f), nil
	case ir.RuleAfter, ir.RuleBefore:
		return r.compareTime(rule, val, sc), nil
	case ir.RuleCustom, ir.RuleAsync:
		name, _ := rule.Params[ir.ParamRef].(string)
		if err := r.v.funcs[name](r.ctx, val.Interface()); err != nil {
			return false, map[string]any{"error": err.Error()}
		}
		return true, nil
	}
	// Unknown kinds are rejected by ir.Check before we get here.
	return true, nil
}

// length is the rune count for strings and the byte count for byte slices —
// JSON Schema's minLength counts code points, not bytes.
func length(val reflect.Value) int {
	if val.Kind() == reflect.String {
		return rules.RuneLen(val.String())
	}
	return val.Len()
}

// compareNum applies op to the value and the parameter as float64s. A
// non-numeric operand (schema error already caught by Check, or a value
// whose kind slipped past the type check) fails closed.
func compareNum(val reflect.Value, param any, op func(a, b float64) bool) bool {
	a, ok := toFloat(val)
	if !ok {
		return false
	}
	b, ok := paramFloat(param)
	if !ok {
		return false
	}
	return op(a, b)
}

// compareTime resolves the sibling named by the rule and compares. A
// sibling that is absent or not a time cannot be compared and passes,
// matching JSON Schema's "no constraint applies" semantics; the sibling's
// own required/type rules report its problem.
func (r *run) compareTime(rule *ir.Rule, val reflect.Value, sc *scope) bool {
	field, _ := rule.Params[ir.ParamField].(string)
	other, present := sc.field(field)
	if !present {
		return true
	}
	other, _ = deref(other)
	if !other.IsValid() || other.Type() != timeType || val.Type() != timeType {
		return true
	}
	a, b := val.Interface().(time.Time), other.Interface().(time.Time)
	if rule.Kind == ir.RuleAfter {
		return a.After(b)
	}
	return a.Before(b)
}
