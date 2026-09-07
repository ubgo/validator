// Package cel evaluates the expression conditions the IR carries as data,
// using Google's Common Expression Language.
//
// # Why this is a separate module
//
// The validator's core has no third-party dependencies, which is a
// property worth protecting: a project can adopt it without inheriting a
// dependency tree, and its build stays fast and auditable. cel-go is not
// small, and most schemas never contain an expression. So the engine lives
// here, in its own module, and is registered explicitly:
//
//	v, err := interp.FromSchema(OrderSchema, interp.WithExpr(cel.New()))
//
// Without it, a schema containing an expression is refused at startup
// rather than silently skipped, the same promise every other element gets.
//
// # What expressions are for
//
// The typed condition vocabulary already compares two fields, so
// `end > start` needs no expression. Expressions exist for the shapes it
// cannot state: arithmetic and aggregation.
//
//	vb.When(ir.Expr(`end - start <= duration('720h')`), ...)   // a month apart at most
//	vb.When(ir.Expr(`total == items.map(i, i.price).sum()`), ...)
//
// # Variables
//
// An expression sees the enclosing object's fields by WIRE name, the same
// names the JSON Schema export and the browser use, so one expression
// reads the same on both sides of the boundary. Every declared field is
// always bound: one the value does not carry is null, so `field == null`
// is how an author tests for absence.
//
// # Portability
//
// This runs in Go only. The JSON Schema emitter classes an expression
// condition as `cel` and the frontend leaves it to the server, exactly as
// it did before this package existed. Nothing here changes what crosses
// the language boundary; it changes what the Go side can execute.
package cel

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	celgo "cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"

	"github.com/ubgo/validator/interp"
)

// Compiler compiles expressions. Safe for concurrent use; build one with
// [New] and share it across schemas.
type Compiler struct {
	envOpts []celgo.EnvOption
	timeout time.Duration
}

// Option configures a [Compiler].
type Option func(*config)

type config struct {
	envOpts []celgo.EnvOption
	timeout time.Duration
}

// interruptEvery is how many comprehension iterations pass between checks
// of the context. Small enough that a bounded evaluation stops promptly,
// large enough that the check is not the cost.
const interruptEvery = 100

// DefaultTimeout bounds a single evaluation. CEL is not Turing complete,
// but a comprehension over a large list is still work, and an expression
// runs inside a request. Zero disables the bound.
const DefaultTimeout = 100 * time.Millisecond

// WithEnv adds cel-go environment options: custom functions, extensions,
// declared types. Applied after this package's own defaults, so they win.
func WithEnv(opts ...celgo.EnvOption) Option {
	return func(c *config) { c.envOpts = append(c.envOpts, opts...) }
}

// WithTimeout bounds one evaluation; zero disables the bound.
func WithTimeout(d time.Duration) Option { return func(c *config) { c.timeout = d } }

// New builds a compiler. Each expression gets an environment declaring
// exactly the fields of the object it is attached to, so naming a field
// the schema does not have fails at startup, not on the first request
// that reaches the expression.
func New(opts ...Option) *Compiler {
	cfg := config{timeout: DefaultTimeout}
	for _, o := range opts {
		o(&cfg)
	}
	return &Compiler{envOpts: cfg.envOpts, timeout: cfg.timeout}
}

// CompileExpr implements [interp.ExprCompiler].
//
// The environment is built per expression, declaring exactly the fields
// the enclosing object has. That is what turns a typo into a startup
// error naming the unknown identifier, instead of an expression that
// quietly evaluates against a missing variable on every request.
func (c *Compiler) CompileExpr(source string, vars []string) (interp.ExprProgram, error) {
	if source == "" {
		return nil, errors.New("cel: empty expression")
	}
	// Every field is declared dynamic. The IR knows each field's type, but
	// an expression is authored against the wire shape, where a time is a
	// string on map input and a time.Time on struct input, and a number may
	// be int or double. Dyn keeps one expression correct for both.
	envOpts := make([]celgo.EnvOption, 0, len(vars)+2)
	for _, name := range vars {
		envOpts = append(envOpts, celgo.Variable(name, celgo.DynType))
	}
	envOpts = append(envOpts, celgo.OptionalTypes())
	envOpts = append(envOpts, c.envOpts...)

	env, err := celgo.NewEnv(envOpts...)
	if err != nil {
		return nil, fmt.Errorf("cel: building the environment: %w", err)
	}
	ast, iss := env.Compile(source)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("cel: %w", iss.Err())
	}
	if ast.OutputType() != celgo.BoolType && ast.OutputType() != celgo.DynType {
		return nil, fmt.Errorf("cel: expression must be boolean, got %s", ast.OutputType())
	}
	// InterruptCheckFrequency is what makes ContextEval honor cancellation:
	// without it cel-go never looks at the context, and a timeout would be
	// a claim rather than a bound. TestTimeoutIsEnforced proves it holds.
	prog, err := env.Program(ast,
		celgo.EvalOptions(celgo.OptOptimize),
		celgo.InterruptCheckFrequency(interruptEvery),
	)
	if err != nil {
		return nil, fmt.Errorf("cel: %w", err)
	}
	return &program{prog: prog, timeout: c.timeout, source: source, vars: vars}, nil
}

// program is one compiled expression.
type program struct {
	prog    celgo.Program
	timeout time.Duration
	source  string
	vars    []string
}

// EvalExpr implements [interp.ExprProgram].
func (p *program) EvalExpr(ctx context.Context, vars map[string]any) (bool, error) {
	if p.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.timeout)
		defer cancel()
	}
	out, _, err := p.prog.ContextEval(ctx, p.activation(vars))
	if err != nil {
		return false, fmt.Errorf("cel: evaluating %q: %w", p.source, err)
	}
	b, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("cel: %q produced %T, not a bool", p.source, out.Value())
	}
	return b, nil
}

// activation binds every declared variable. A field the value does not
// carry is bound to CEL's null rather than left out: cel-go treats an
// unbound declared variable as an error, which would turn "this optional
// field is absent" into "this expression is broken". Null lets an author
// write `field == null` and have it mean what it says.
func (p *program) activation(vars map[string]any) map[string]any {
	adapted := adapt(vars)
	out := make(map[string]any, len(p.vars))
	for _, name := range p.vars {
		if v, ok := adapted[name]; ok {
			out[name] = v
			continue
		}
		out[name] = types.NullValue
	}
	return out
}

// adapt converts Go values the interpreter hands over into shapes CEL
// understands. Two conversions matter and the rest pass through:
//
//   - time.Time and time.Duration become CEL's own timestamp and duration,
//     so `end - start <= duration('24h')` works on typed struct input.
//   - a nil value is dropped here and rebound as null by activation, so an
//     absent optional field is testable rather than an evaluation error.
func adapt(vars map[string]any) map[string]any {
	out := make(map[string]any, len(vars))
	for k, v := range vars {
		if v == nil {
			continue
		}
		out[k] = adaptValue(v)
	}
	return out
}

func adaptValue(v any) any {
	switch x := v.(type) {
	case time.Time:
		return types.Timestamp{Time: x}
	case time.Duration:
		return types.Duration{Duration: x}
	case ref.Val:
		return x
	}
	rv := reflect.ValueOf(v)
	// A pointer to one of the above (an optional field) reads through.
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		return adaptValue(rv.Elem().Interface())
	}
	// RFC 3339 strings stay strings: on map input a time arrives as text,
	// and CEL's timestamp() converts it where an author wants that.
	return v
}
