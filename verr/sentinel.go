package verr

import "errors"

// Matching a result against errors.Is.
//
// A caller who wants to react to one kind of failure — "was anything
// required missing?" — should not have to reach into the issue list and
// compare strings. Go's answer to that is errors.Is, and this file is what
// makes it work.
//
// The design question is what to match against. A named sentinel per code
// would be fifty exported variables that drift the moment a rule kind is
// added, and would still leave custom codes out. So the matchable value is
// a TYPE, [Code], whose value is the code string. Every code the library
// emits has a named constant, and a code this library has never heard of
// still works:
//
//	errors.Is(err, verr.CodeRequired)          // a named structural code
//	errors.Is(err, verr.Code("min_length"))    // a rule code
//	errors.Is(err, verr.Code("weak_password")) // a code you defined with Msg
//
// Matching is over the WHOLE result: errors.Is reports true when ANY issue
// carries that code. That is what a caller means by "did this fail because
// something was required", and it is the only reading that stays useful
// when a request produces several issues at once. To act on one field,
// read the issues directly or use [Errors.ByPath] — errors.Is answers a
// yes-or-no question and deliberately does not say which field.

// Code is an issue code, comparable with errors.Is against an [Errors] or
// a single [Issue].
//
// It is an error itself so it can be the target of errors.Is, but it is
// never returned as one; a validation failure is always an [Errors].
type Code string

// Error implements error. The text is the bare code, which is what a
// caller sees if one is ever printed by mistake.
func (c Code) Error() string { return string(c) }

// Is reports whether the target is the same code, so errors.Is works in
// both directions.
func (c Code) Is(target error) bool {
	other, ok := target.(Code)
	return ok && other == c
}

// Structural codes as matchable values. These mirror the string constants
// above one for one; the strings stay because they are what appears on the
// wire and in the TypeScript runtime.
const (
	// ErrRequired matches a missing non-optional key or a null where null
	// is not allowed.
	ErrRequired Code = CodeRequired

	// ErrType matches a value of the wrong shape for its node.
	ErrType Code = CodeType

	// ErrUnknownKey matches an undeclared key under extra=forbid.
	ErrUnknownKey Code = CodeUnknownKey

	// ErrVariant matches a discriminator that named no case.
	ErrVariant Code = CodeVariant

	// ErrUnion matches a value that fit no variant of a union.
	ErrUnion Code = CodeUnion

	// ErrGroup matches a group gate whose presence count is wrong.
	ErrGroup Code = CodeGroup

	// ErrCustom matches a custom or async rule that returned an error
	// without setting a code of its own.
	ErrCustom Code = CodeCustom
)

// Is implements the errors.Is protocol for a whole result: true when any
// issue carries the target code.
//
// A nil *Errors matches nothing, so `errors.Is(err, verr.ErrRequired)` is
// safe on the value a passing validation returns.
func (e *Errors) Is(target error) bool {
	code, ok := target.(Code)
	if !ok || e == nil {
		return false
	}
	for _, is := range e.Issues {
		if Code(is.Code) == code {
			return true
		}
	}
	return false
}

// Is implements the errors.Is protocol for a single issue.
func (i Issue) Is(target error) bool {
	code, ok := target.(Code)
	return ok && Code(i.Code) == code
}

// Has reports whether any issue carries the code. It is what [Errors.Is]
// does, spelled directly, for a caller who has an *Errors in hand and does
// not need the errors.Is ceremony.
func (e *Errors) Has(code Code) bool { return e.Is(code) }

// First returns the first issue carrying the code, and whether one was
// found. Use it when the answer to "did this fail because X" needs the
// path or params, which errors.Is cannot carry.
func (e *Errors) First(code Code) (Issue, bool) {
	if e == nil {
		return Issue{}, false
	}
	for _, is := range e.Issues {
		if Code(is.Code) == code {
			return is, true
		}
	}
	return Issue{}, false
}

// AllOf returns every issue carrying the code, in report order.
func (e *Errors) AllOf(code Code) []Issue {
	if e == nil {
		return nil
	}
	var out []Issue
	for _, is := range e.Issues {
		if Code(is.Code) == code {
			out = append(out, is)
		}
	}
	return out
}

// As is a convenience for the common shape of a handler: turn any error
// into the validation result it carries, or report that it is not one.
//
//	if errs, ok := verr.As(err); ok {
//		w.WriteHeader(422)
//		json.NewEncoder(w).Encode(errs)
//		return
//	}
func As(err error) (*Errors, bool) {
	var e *Errors
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
