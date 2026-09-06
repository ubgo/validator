# Contributing to validator

Thanks for your interest in improving **validator**. This guide covers how to get set up, the conventions we follow, and what a good pull request looks like.

By participating, you agree to abide by our [Code of Conduct](CODE_OF_CONDUCT.md).

## Getting started

```sh
git clone https://github.com/ubgo/validator.git
cd validator
task test        # library + CLI tests
task ci          # the full gate: fmt-check + vet + lint + race tests, both modules
```

The repo is two Go modules — the library at the root and the CLI under `cli/` — stitched together for local development by `go.work`, so changes to the library are visible to the CLI immediately. Everything runs through the [Taskfile](Taskfile.yml): `task --list` shows every command.

## The one rule (non-negotiable)

**Rules are data, never closures.** Every authoring surface records a readable IR node; nothing on the IR stores a `func`. A closure cannot be serialized, so it can never become JSON Schema or a TypeScript validator, and the entire point of this library is that it can. The single sanctioned opaque node is `custom`, and it is explicitly marked non-portable. A PR that puts a function value on the IR will be declined regardless of ergonomics.

## Ways to contribute

- **Report a bug** — open an issue using the bug template; include steps to reproduce, your Go version, and what you expected.
- **Request a feature** — open an issue using the feature template; describe the problem first, then your proposed solution. Note the deliberate non-goals in the README (not protobuf-shaped, not a Go→TS type generator, not a forms framework) — PRs adding those will be declined with thanks.
- **Send a pull request** — for anything non-trivial, open an issue first so we can agree on the approach before you write code.

## Branches & commits

- Branch off `main`. Use a short descriptive branch name (`fix/...`, `feat/...`, `docs/...`).
- Keep commits focused; one logical change per commit where practical.

## Pull request checklist

- [ ] The change is scoped and described (link the issue it closes).
- [ ] `task ci` passes (CI runs the same on Linux and macOS, plus a cross-compile matrix).
- [ ] New behavior has a pinning test. A new IR node or rule kind has a JSON round-trip test and a `Check` test.
- [ ] Generator changes ship with a runnable example that compiles and runs the generated artifact (the build is the test).
- [ ] Docs/README/CHANGELOG updated if the change is user-facing.
- [ ] No unrelated files or formatting churn.

## Changelog

User-facing changes go under `[Unreleased]` in [CHANGELOG.md](CHANGELOG.md), following [Keep a Changelog](https://keepachangelog.com/).

## Questions

Open a [discussion or issue](https://github.com/ubgo/validator/issues). We're happy to help you land your first contribution.
