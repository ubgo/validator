package interp

import (
	"context"

	"github.com/ubgo/validator/ir"
)

// The expression hook.
//
// The IR can carry a CEL expression as a condition (`ir.Expr`), which is
// how it states the one thing the typed condition vocabulary cannot:
// arithmetic and aggregation across fields, "end - start < 30 days" or
// "total == sum of line prices". Evaluating CEL needs a real expression
// engine, and this library's core has no third-party dependencies and is
// not going to acquire one for a feature most schemas never use.
//
// So the engine is supplied from outside. github.com/ubgo/validator/cel is
// a separate module wrapping cel-go that satisfies these interfaces; pass
// it with WithExpr and expression conditions start working. Without it,
// New refuses a schema containing one rather than skipping it, which is
// the same promise every other unexecutable element gets.

// ExprCompiler turns an expression's source into something evaluable. It
// is called once per distinct expression at [New], so a syntax error is a
// startup failure and never a per-request surprise.
type ExprCompiler interface {
	// CompileExpr prepares source for evaluation. vars lists the variables
	// the expression may reference: the enclosing object's fields by WIRE
	// name, matching what the JSON Schema export and the frontend see, so
	// one expression reads the same on both sides of the boundary.
	//
	// Passing the names lets an engine reject an expression that mentions a
	// field the schema does not declare, at startup rather than on the
	// request that first evaluates it.
	CompileExpr(source string, vars []string) (ExprProgram, error)
}

// ExprProgram is one compiled expression, evaluated many times. It must be
// safe for concurrent use, because a Validator is.
type ExprProgram interface {
	// EvalExpr evaluates against the enclosing object's fields, keyed by
	// wire name. Absent fields are absent from the map rather than present
	// and zero, so an expression can distinguish them.
	//
	// A non-nil error means the expression could not be decided for this
	// value; the caller treats that as false, the same reading a custom
	// predicate returning an error gets.
	EvalExpr(ctx context.Context, vars map[string]any) (bool, error)
}

// WithExpr supplies the engine for CEL conditions. Without it, [New]
// refuses any schema containing one.
func WithExpr(c ExprCompiler) Option { return func(v *Validator) { v.exprCompiler = c } }

// activation renders the enclosing object as the variables an expression
// sees: wire names to values, absent fields omitted.
//
// Map input is already wire-keyed, so it is handed over directly — an
// expression reading a field the schema does not declare still sees it,
// which matches how the value would look to the frontend.
func activation(sc *scope) map[string]any {
	if sc == nil {
		return nil
	}
	if isStringMap(sc.val) && sc.val.Type() == anyMapType {
		if m, ok := sc.val.Interface().(map[string]any); ok {
			return m
		}
	}
	out := make(map[string]any, len(sc.obj.Fields))
	for i := range sc.obj.Fields {
		f := &sc.obj.Fields[i]
		v, ok := sc.field(f.Name)
		if !ok {
			continue
		}
		out[wireName(f)] = v.Interface()
	}
	return out
}

// evalExpr runs a compiled expression for a condition. A missing program
// cannot happen (New compiled every expression or refused the schema), and
// an evaluation error reads as false, so a gate never fires on a value the
// expression could not judge.
func (r *run) evalExpr(sc *scope, k ir.Condition) bool {
	prog, ok := r.v.exprs[k.CEL]
	if !ok || sc == nil {
		return false
	}
	held, err := prog.EvalExpr(r.ctx, activation(sc))
	return err == nil && held
}
