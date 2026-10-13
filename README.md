# validator

> Author validation once in Go. Run it everywhere.

[![ci](https://github.com/ubgo/validator/actions/workflows/ci.yml/badge.svg)](https://github.com/ubgo/validator/actions/workflows/ci.yml)

**Status: early development.** The serializable IR (`ir` package) is the first shipped piece. Validation executors, JSON Schema export, and TypeScript generation follow — see the roadmap below.

`validator` is a struct-native Go validation library whose rules are stored as a **serializable intermediate representation (IR)** instead of opaque closures. Because the rules are data, the same tree that validates in Go can be exported to JSON Schema and compiled to a Standard-Schema-compatible TypeScript validator. The backend stays the single source of truth; the frontend validator is generated, never hand-maintained; and any rule that genuinely cannot cross the language boundary is listed in a portability manifest instead of being silently dropped.

**New here? Read the [user guide](docs/GUIDE.md)**: a five-step quickstart, the end-to-end workflow with diagrams, and every feature with a snippet. Every snippet in it is executed by `examples/guide_test.go`. For the full symbol list there is a generated [API reference](docs/API.md), and [pkg.go.dev](https://pkg.go.dev/github.com/ubgo/validator) renders the doc comments.

## Why

Across the Go validators we surveyed, one capability combination is held by nobody: struct-native authoring, serializable rules, JSON Schema export, and multi-language output together. `protovalidate` has the reach but forces a protobuf data model. `go-playground/validator` has the rule breadth but rules are opaque tag strings. The fluent libraries (`ozzo`, `valgo`, `zog`) have nice ergonomics but store rules as Go closures you cannot introspect. That empty cell is this project.

## The one rule

**Rules are data, never closures.** `.Min(3)` records `{"kind":"min","params":{"value":3}}`, not `func(v) error`. That is the whole reason export is possible. The only opaque node is `custom`, and it is explicitly marked non-portable.

## Layout

| Path | What lives there |
|---|---|
| `ir/` | The IR: type nodes, rules, conditions, portability classes, versioned JSON round-trip, structural `Check`. |
| `i18n/` | Message catalog: export the default with `validatorctl catalog`, translate, load on both sides. |
| `verr/` | Error model: `Issue{Code, Path, Message, Rule, Params}`. Same codes on the TS side. |
| `interp/` | Reflection interpreter: `interp.FromSchema(schema)` then `Validate(ctx, value)`. |
| `jsonschema/` | JSON Schema emitter (2020-12, draft-07) plus the portability manifest of everything the schema cannot carry. |
| `vb/` | The typed builder: `vb.Struct`, `vb.F(ref, builder)`, per-kind builders, rename-safe conditions. Compiles to the IR. |
| `examples/` | Proofs against independent implementations: a third-party JSON Schema validator, and the real TS runtime under Node against a live rule server. |
| `cli/` | `validatorctl` — separate module so the library stays dependency-light. |
| `codegen/` | `validatorctl gen`: reflection-free `Validate<Type>` functions from the IR, conformance-tested against the interpreter. |
| `tags/` | `validatorctl migrate`: go-playground `validate` tags → IR plus builder source, unsupported tags reported. |
| `accessors/` | `validatorctl fields`: generates `<Type>Fields` accessors so rules name fields by symbol, not string. |
| `rulehttp/` | Serves async rules over HTTP for the frontend round-trip: opt-in, rate-limited, auth hook. |
| `ts/` | `@ubgo/validator`, the TypeScript runtime: Ajv + Standard Schema + manifest-aware round-trips. |
| `cel/` | Optional expression engine, its own module so the library keeps zero dependencies. Adds arithmetic and aggregation across fields. |

## Install

Requires Go 1.24 or later; CI builds every push on 1.24 and on the current release.

```sh
go get github.com/ubgo/validator
go install github.com/ubgo/validator/cli/cmd/validatorctl@latest

# Only if you need expressions (arithmetic or aggregation across fields):
go get github.com/ubgo/validator/cel
```

The library has no third-party dependencies. The expression engine is a separate module so that stays true for everyone who does not use it.

## Roadmap (v1)

1. **IR** — shipped: nodes, rules, conditions, JSON round-trip, `Check`.
2. **Typed builder** — shipped: `vb` compiles to the IR and reproduces the golden fixture byte for byte.
3. **Reflection interpreter** — shipped: `interp` runs the IR against structs or decoded JSON, refuses anything it cannot execute.
4. **Codegen executor** — shipped: `validatorctl gen` emits reflection-free `Validate<Type>` functions; an 86-case conformance suite requires identical issues from both executors. The success path is allocation-free through `Validate<Type>Into`.
5. **Codegen accessors** — shipped: `validatorctl fields` generates `UserFields` for rename-safe rule authoring.
6. **JSON Schema emitter** — shipped: draft 2020-12 and draft-07, discriminator-keyed if/then for variants, and a manifest of every non-portable rule.
7. **TypeScript runtime shim** — shipped: `@ubgo/validator` in `ts/`, Ajv + Standard Schema + Standard JSON Schema, same issue codes as Go.
8. **Portability manifest** — shipped with the emitter: every custom, async, cross-field or transform rule listed with a class and reason.

## Authoring (builder)

```go
//go:generate validatorctl fields
// → fields.gen.go: UserFields.Email, UserFields.Age, … as vb.FieldRef[User, T]

var UserSchema = vb.Struct("User",
	vb.F(UserFields.Email, vb.String().Email().
		Custom("no_temp_domains", noTempDomains).          // opaque: runs in Go, reported to the frontend
		Async("unique_email", "/v/unique_email", unique)), // server: frontend round-trips
	vb.F(UserFields.Age, vb.Int[int32]().Min(18).Max(120)),
).Gate(
	vb.Requires(vb.Eq(UserFields.Age, 18), UserFields.Email),
).MustBuild()

doc := UserSchema.IR() // *ir.Schema — serializable, exportable

v, _ := interp.FromSchema(UserSchema)
if err := v.Validate(ctx, &user); err != nil {
	issues := err.(*verr.Errors).Issues // [{code:"min", path:"$.age", ...}]
}
```

`vb.F(UserFields.Age, vb.String())` is a compile error: a builder's value type must match the field's Go type.

## Messages and translations

```sh
validatorctl catalog > en.json          # every issue code with its English template
validatorctl catalog --check de.json    # exit 1 if the translation misses a code
```

```go
de := i18n.Merge(i18n.Default(), loaded)   // partial translations fall back to English
issues := de.Localize(v.Run(ctx, &user))   // same codes, German messages
```

```ts
createValidator(schema, { manifest, catalog: de }); // browser renders the same German
```

## Migrating from go-playground/validator

```sh
validatorctl fields ./models                                   # UserFields accessors
validatorctl migrate ./models --type User --out user.ir.json --builder user_schema.gen.go
```

Every tag with an IR equivalent becomes a rule or gate; every tag without one is listed, and the command exits non-zero unless you pass `--allow-partial`. The emitted builder source is yours to edit; the tags can be deleted.

## Generated executor

```sh
validatorctl ir export ... # or: json.Marshal(UserSchema.IR())
validatorctl gen ./models --schema user.ir.json   # → models/validate.gen.go
```

```go
errs := models.ValidateUser(ctx, &user, UserSchema) // *verr.Errors, same issues as the interpreter

first := &verr.Errors{FailFast: true}                       // stop at the first issue
models.ValidateUserInto(ctx, &user, UserSchema, first)
```

### Measured against go-playground/validator

The same struct and the same rules, written once as `validate:"..."` tags and once as a schema, on Apple M-series, Go 1.26, reporting the fastest of five runs. A test asserts the two rule sets accept the same value and fail the same fields, so neither side is doing less work. Source: `examples/compare_bench_test.go`, fixture `examples/benchmodel`.

Valid input, the success path:

| | ns/op | B/op | allocs/op |
|---|---|---|---|
| ubgo generated | 198 | 0 | 0 |
| ubgo interpreter | 1687 | 184 | 15 |
| go-playground/validator | 1874 | 547 | 14 |

Invalid input, four fields failing:

| | ns/op | B/op | allocs/op |
|---|---|---|---|
| ubgo generated, caller-owned result | 1015 | 1009 | 17 |
| ubgo generated | 1150 | 1522 | 20 |
| go-playground/validator | 1728 | 1432 | 34 |
| ubgo interpreter | 2840 | 1602 | 29 |

Read the two tables as different jobs, not as one number. Validating a value that passes produces nothing; validating a value that fails has to build an error for every broken field. Measured on the generated executor, that is the whole story:

| issues produced | ns/op | allocs/op |
|---|---|---|
| 0 | 376 | 0 |
| 1 | 425 | 3 |
| 2 | 765 | 9 |
| 3 | 1329 | 18 |

So an issue costs roughly 300 ns and 5 allocations, because each one carries a parameter map and a rendered message. That is a deliberate trade: it is what lets the same error be re-rendered in another language, or in the browser, from its code and parameters. go-playground returns a lightweight error object and renders text only if asked, so it pays less per error and gives you less.

Against that background: the generated executor is roughly nine times faster than the incumbent on valid input while allocating nothing, and about 1.5 times faster on invalid input. The interpreter is faster than the incumbent on valid input using a third of the memory, and about 1.6 times slower when there are errors, where reflection is the remaining cost. One caveat kept in view: the invalid fixture's email has no "@" and so takes an allocation-free rejection; an address malformed more subtly still pays for a full `net/mail` parse, because this library decides `email` exactly where go-playground uses a regular expression.

Reproduce with:

```sh
go test -run xxx -bench BenchmarkCompare -benchmem ./examples/
```

### Against itself

On the example fixture (arm64; a union, a discriminated union, transforms, two custom rules and a gate predicate), a valid order costs:

| Path | ns/op | allocs/op |
|---|---|---|
| `ValidateOrderInto` (caller-owned issue list) | 490 | 0 |
| `ValidateOrder` | 510 | 1 (the result) |
| `interp` | 9700 | 81 |

Paths are built lazily on the stack and rendered only when an issue is reported. Custom rules and gate predicates reach the author's typed function through `rt.CallT`, so no value is boxed on the way; the email and URL formats take allocation-free fast paths that accept only a proven subset of what `net/mail` and `net/url` accept and defer everything else to them. Run `go test -bench . ./codegen/internal/example/`.

### Coercion: strict or lax

Struct input is never coerced; a typed field already has its type. On `map[string]any` input (decoded JSON, form values) the interpreter converts strings for int, float, bool, time and duration fields according to a mode:

```go
interp.FromSchema(UserSchema)                 // default: only fields marked .Coerce()
interp.FromSchema(UserSchema, interp.Lax())   // every coercible field, except those marked .NoCoerce()
interp.FromSchema(UserSchema, interp.Strict()) // only marked fields (names the intent; same as the default today)
```

Field markers beat the call option in both directions: `.Coerce()` always converts, `.NoCoerce()` never does. RFC 3339 strings are also accepted for `time.Time` fields on map input and written back as `time.Time`.

## Export

```go
res, _ := jsonschema.Emit(UserSchema.IR())
doc, _ := res.JSON()               // JSON Schema 2020-12, byte-deterministic
for _, e := range res.Manifest.Entries {
	// e.g. {Path:"$.email", Kind:"rule", Name:"async", Class:"server", Endpoint:"/v/unique_email"}
}
```

Or from the CLI: `validatorctl ir export user.ir.json --out user.schema.json`. The manifest goes to stderr; `--json` returns schema and manifest in one envelope for CI gates.

The IR itself has a schema. `validatorctl ir contract` (or `jsonschema.Contract()`) prints the JSON Schema every serialized IR document conforms to, generated from the Go types so it cannot drift; non-Go tools validate and type IR files against it. `ir.Check` also lints every `pattern` rule: it must be valid RE2 *and* free of RE2-only syntax (inline flags, named groups, `\A`, `\z`, `\Q`, POSIX and Unicode classes) so the same regex runs unchanged in the browser.

## Frontend

```ts
import { createValidator } from "@ubgo/validator";

import { userSchema, userManifest } from "./user.schema"; // validatorctl ir export --ts user.schema.ts

const user = createValidator(userSchema, { manifest: userManifest }); // value type inferred from the schema
user.validate(input);              // sync: JSON Schema rules, Go-identical issue codes
await user.validateAsync(input);   // + debounced round-trips for async rules
useForm({ validators: { onChange: user } }); // any Standard Schema consumer
```

Rules the schema cannot carry are reported through the manifest, never dropped: opaque rules warn once and run on the server, async rules round-trip to `POST /v/<rule>`.

Serving that round-trip in Go is one call, and it runs the same function `Validate()` runs:

```go
h, _ := rulehttp.FromSchema(UserSchema, rulehttp.WithAuthorize(requireSession))
h.Mount(mux) // POST /v/unique_email → {"ok":false,"issues":[{"code":"custom","message":"already taken"}]}
```

## Non-goals

Not protobuf-shaped. Not a Go→TS *type* generator. Not arbitrary Go→JS translation of closures. Not a forms framework. No `init()` registration or global side effects.

## Development

`task ci` is the gate: format, vet, lint, race tests for the library and CLI, typecheck plus coverage-thresholded tests plus build for the TypeScript runtime, then the proof module. `task build:cross` compiles for linux, darwin, windows and freebsd. The GitHub Actions workflows run on manual dispatch only, so the local `task ci` is the gate.

## License

Apache-2.0.
