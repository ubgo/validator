# Changelog

All notable changes to **validator** are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Rules on the whole struct: `vb.Struct(...).Custom(name, fn)` and `.Async(name, endpoint, fn)`, where the function receives the entire value. `ir.ObjectNode` gained a `Rules` field, so the IR can finally state a check whose subject is the object rather than a field. A gate could never express this, because a gate decides whether other rules apply and reports nothing itself. Both executors run them, they report at the object's path and before the fields, `rulehttp` serves the async ones, and the emitter reports them rather than dropping them.
- Guide section 4.13 on expressions, plus corrections to four places that still described CEL as planned and refused. The portability table now has rows for expressions with and without the engine.
- CEL expressions execute, through a new `github.com/ubgo/validator/cel` module. It is a separate module on purpose: the core has no third-party dependencies and cel-go is not small, so the engine is registered explicitly with `interp.WithExpr(cel.New())` and a schema containing an expression is still refused at startup without it. Expressions cover what the typed conditions cannot, arithmetic and aggregation across fields. Generated code delegates expression gates back to the interpreter rather than compiling CEL into Go, so there is one implementation of the language and not two; `rt.ExprRegistry` is how the engine reaches it. `cel/internal/example` proves both executors report identical issues.
- Cross-field rules now run in the browser. JSON Schema cannot compare two properties, so `after`, `before` and gates keyed on a field-to-field comparison previously reached the frontend as a warning only. The manifest entry now carries `cond` (the condition tree in wire names), `then`/`else` (the branch fragments), and the `code`/`message` Go reports, and the TypeScript runtime executes them: same code, same wording, no round trip. An undecidable condition yields no issue, so the browser never reports what the server would not.
- Cross-library benchmark against go-playground/validator (`examples/compare_bench_test.go`, fixture `examples/benchmodel`), with a test asserting the two rule sets are equivalent so the comparison is fair. Numbers published in the README and the guide, including where the interpreter is still behind.
- Fuzz targets for every untrusted-input surface, each pinning behavioral properties rather than only absence of panics: IR decode and round-trip stability (`ir`), interpreter map input including fail-fast/accumulate first-issue agreement and lax-never-rejects (`interp`), and the HTTP handler's response contract and endpoint isolation (`rulehttp`). `task fuzz`; `task ci` runs a short smoke of each.
- `task vuln` (govulncheck across all three modules) as its own CI job plus a weekly schedule, kept out of the deterministic code gate on purpose.

### Fixed
- `StructBuilder` mutated its receiver: `Gate`, `Extra`, `Source` and `Generator` modified the builder in place and returned it, so two schemas derived from one base shared state and the second silently inherited the first's gates. Every builder in `vb` is documented as an immutable value; this one was not. Now copies, and a test pins it.
- The CEL evaluation timeout did nothing. cel-go only consults the context when a program is built with `InterruptCheckFrequency`, so `WithTimeout` was a claim rather than a bound, and even an already-cancelled context returned a result. Now set, and `TestTimeoutIsEnforced` fails without it.
- `task lint` swallowed real findings: `[ -x $bin ] && $bin run ... || echo skipping` treated a failing linter as a missing one, so a green gate hid three staticcheck findings, one of them a generator defect. Absence now skips; a finding fails.
- The generator emitted `if !(!v.Field.IsZero())` for required time fields (SA4013) and an empty `} else {}` for optional fields with no rules. It now renders absence directly and omits an empty else.

### Changed
- `UUID` and `ULID` decide their fixed shapes by direct scan instead of a regexp: a valid UUID went from 228 ns to 40 ns, which alone halved the generated success path (385 to 198 ns/op). The regexps stay in `rules.go` as the reference the scans are tested against.
- Format checks gained sound fast rejections to match their fast acceptances: an address with no `@`, or an empty local part or domain, is refused without a `net/mail` parse (269 ns and 9 allocations down to 4 ns and none), and a string with no `:` is not a URL. Both are properties of the grammars, not heuristics, and `FuzzFastPaths` checks every input against `net/mail` and `net/url` directly. Failure path: interpreter 3225 to 2876 ns/op, generated 1374 to 999 ns/op, now about 1.7x faster than go-playground.
- Interpreter allocations reduced on both paths: the declared-names map is built only when unknown keys are actually checked (map input under `ExtraForbid`), and enum/const checks go through `rules.InValue`, which compares a `reflect.Value` against the candidates without boxing. Success path 2265 → 1880 ns/op and 704 → 184 B/op, now at parity with go-playground on time and a third of its memory.
- Module floor lowered to Go 1.24 (was pinned to the 1.26 toolchain); CI gate now runs on 1.24 and 1.26.

### Added
- `docs/GUIDE.md`: the user guide (quickstart, workflow diagrams, every feature with a snippet, CLI reference, portability table), proven by `examples/guide_test.go`.
- `jsonschema.Contract()` and `validatorctl ir contract`: the JSON Schema of the IR wire format, generated from the Go types; the `examples` module proves the golden IR documents conform and malformed ones do not.
- `ir.CheckPattern`, wired into `ir.Check`: every `pattern` rule must be valid RE2 and free of RE2-only syntax so it runs unchanged under ECMA-262.
- Coercion modes in `interp`: `Lax()`, `Strict()`, `WithCoerce(mode)`; field marker `vb.Field.NoCoerce()` (IR transform kind `strict`). Field markers beat call options.
- RFC 3339 strings for `time.Time` fields on `map[string]any` input are parsed and written back.
- `rt.CallT` / `rt.CondT` and `vb.Schema.TypedFunc`: generated code calls custom rules and gate predicates through the author's typed function, no boxing. Allocation-free fast paths for the email and URL formats. Generated success path: 0 allocs via `Validate<Type>Into`.
- `validatorctl migrate` reports a specific reason and a suggested alternative for every known-unsupported tag (OR-groups, non-time cross-field comparisons, `excludes*`, `isdefault`, `unique=<field>`, non-RFC 3339 `datetime` layouts, format tags on non-strings).
- `ir` package: the serializable rule-tree IR (`ubgo.validator.ir/v1`) — type nodes, rules, conditions, gated rules, portability classes, versioned JSON round-trip and structural `Check`.
- `validatorctl` CLI skeleton with `version` and `ir check`.
- Rule vocabulary: creditCard, jwt, countryCode, currencyCode, hexColor, e164, latitude, longitude.
- Integration test with Hono's Standard Schema middleware proves adapter-free interop.
- `validatorctl ir export --ts`: const TypeScript module; `createValidator` infers the value type from it (`Infer<S>`).
- Transforms `coerce` (map input) and `normalizeEmail` run in both executors; TS runtime mirrors coercion via Ajv.
- `codegen`: typed probes for unions on concrete fields; explicit interpreter fallback (`rt.Interpret`) for interface-typed discriminated unions and unions with collection variants.
- Fail-fast for both executors via `verr.Errors.FailFast`; `Validate<Type>Into` and `interp.RunInto` write into caller-owned issue lists.
- `i18n` package, `validatorctl catalog` (export + `--check`), and the TS runtime `catalog` option: one code → message catalog for Go and the browser.
- Rule vocabulary: ipv4, ipv6, ip, cidr, mac, hostname, base64, json, semver, date, datetime, notBlank across every executor, the emitter, the TS runtime and the tag migration.
- `examples` proof module: emitted JSON Schema validated by an independent library; TypeScript runtime round-trip against a live `rulehttp` server.
- `tags` package and `validatorctl migrate`: go-playground struct tags → IR document and `vb` builder source, with a per-field report of anything not migrated.
- `codegen`: transforms, unions and discriminated unions compile to Go; conformance suite extended to 58 cases.
- `codegen/rt.Path`: lazily rendered, stack-allocated paths for generated validators; typed inline enum/const/compare/unique checks.
- `codegen` package and `validatorctl gen`: reflection-free `Validate<Type>` functions generated from the IR, conformance-tested against the interpreter; `codegen/rt` runtime; shared `internal/rules` semantics.
- `accessors` package and `validatorctl fields`: generate typed `<Type>Fields` accessors (`vb.FieldRef`) from a package's structs into `fields.gen.go`.
- `rulehttp` package: HTTP handler serving async rules at their declared endpoints with body cap, rate limiting and an authorization hook; `vb.Schema.Endpoints()`.
- `@ubgo/validator` TypeScript runtime (`ts/`): Ajv-backed validation of emitted schemas, Standard Schema and Standard JSON Schema, Go-identical issue codes, manifest-driven warn-once and debounced server round-trips.
- `jsonschema` package: JSON Schema emitter (draft 2020-12 and draft-07) with a portability manifest listing every rule the schema cannot carry; `validatorctl ir export`.
- `verr` package: the validation error model (issue code, JSONPath, rule, params, rendered message).
- `interp` package: reflection interpreter executing the IR against structs or JSON maps, with accumulate/fail-fast modes, pre-transforms, gates, unions and discriminated unions.
- `vb` package: typed builder (`vb.Struct`, `vb.F`, per-kind type builders, rename-safe condition sugar) compiling to the IR; conformance-tested against the golden fixture.

### Changed
### Deprecated
### Removed
### Fixed
### Security

<!--
Release process:
  1. Move the relevant [Unreleased] entries under a new version heading below.
  2. Date it: ## [1.2.0] - YYYY-MM-DD
  3. Tag the release (e.g. v1.2.0) and update the link refs at the bottom.
-->

[Unreleased]: https://github.com/ubgo/validator/commits/main
