# Changelog

All notable changes to **validator** are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
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
