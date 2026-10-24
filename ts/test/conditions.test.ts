import { describe, expect, it } from "vitest";

import { createValidator } from "../src/index.js";
import { evaluate, present, executable, scopeOf, type Cond, type ExecutableEntry } from "../src/conditions.js";
import type { Manifest } from "../src/manifest.js";

/**
 * Cross-field rules the schema cannot express, executed in the browser.
 *
 * These are the rules that used to reach the frontend as a warning only:
 * "end must be after start" could not be checked until submit. The Go
 * emitter now carries them as data and the runtime evaluates them, so the
 * tests here are about one thing above all — the browser must agree with
 * the server, and must stay silent when it cannot decide.
 */

const eventSchema = {
  $schema: "https://json-schema.org/draft/2020-12/schema",
  type: "object",
  properties: {
    start: { type: "string", format: "date-time" },
    end: { type: "string", format: "date-time" },
    kind: { type: "string" },
    note: { type: "string" },
  },
  required: ["start", "end"],
} as const;

/** The shape the Go emitter produces for `end` after `start`. */
const afterManifest: Manifest = {
  entries: [
    {
      path: "$.end",
      kind: "rule",
      name: "after",
      class: "cel",
      ref: "Start",
      reason: "JSON Schema cannot compare two properties; carried as a condition a runtime can evaluate",
      cond: { kind: "fieldcmp", field: "end", op: "gt", fieldB: "start" },
      code: "after",
      message: "must be after Start",
    },
  ],
};

const quiet = { onNonPortable: () => undefined, serverRules: false } as const;

describe("cross-field rules in the browser", () => {
  it("reports the same issue the Go executors report", () => {
    const v = createValidator(eventSchema, { manifest: afterManifest, ...quiet });
    const r = v.validate({ start: "2026-01-02T00:00:00Z", end: "2026-01-01T00:00:00Z" });
    expect(r.ok).toBe(false);
    if (r.ok) return;
    expect(r.issues).toHaveLength(1);
    expect(r.issues[0]).toMatchObject({
      code: "after",
      path: "$.end",
      message: "must be after Start",
      rule: "after",
    });
    expect(r.issues[0]?.segments).toEqual(["end"]);
  });

  it("accepts a value that satisfies the comparison", () => {
    const v = createValidator(eventSchema, { manifest: afterManifest, ...quiet });
    expect(v.validate({ start: "2026-01-01T00:00:00Z", end: "2026-01-02T00:00:00Z" }).ok).toBe(true);
  });

  it("stays silent when an operand is missing rather than guessing", () => {
    const v = createValidator(eventSchema, { manifest: afterManifest, ...quiet });
    // `end` is absent: the schema's own required check reports that, and the
    // cross-field rule must not pile on an "after" issue it cannot judge.
    const r = v.validate({ start: "2026-01-01T00:00:00Z" });
    expect(r.ok).toBe(false);
    if (r.ok) return;
    expect(r.issues.some((i) => i.code === "after")).toBe(false);
  });

  it("does not warn about an entry it can execute", () => {
    const seen: string[] = [];
    createValidator(eventSchema, {
      manifest: afterManifest,
      serverRules: false,
      onNonPortable: (e) => seen.push(e.name),
    });
    expect(seen).toEqual([]);
  });

  it("still warns about entries it cannot execute", () => {
    const seen: string[] = [];
    createValidator(eventSchema, {
      serverRules: false,
      manifest: {
        entries: [
          { path: "$.kind", kind: "rule", name: "custom", class: "opaque", ref: "vip", reason: "server only" },
        ],
      },
      onNonPortable: (e) => seen.push(e.name),
    });
    expect(seen).toEqual(["custom"]);
  });
});

describe("gates keyed on a cross-field condition", () => {
  const gateManifest: Manifest = {
    entries: [
      {
        path: "$",
        kind: "gate",
        name: "when",
        class: "cel",
        reason: "comparing two properties needs CEL",
        cond: { kind: "fieldcmp", field: "end", op: "gt", fieldB: "start" },
        then: { type: "object", properties: { kind: { type: "string", minLength: 2 } } },
        else: { type: "object", properties: { note: { type: "string", minLength: 3 } } },
      },
    ],
  };

  it("applies the then branch when the condition holds", () => {
    const v = createValidator(eventSchema, { manifest: gateManifest, ...quiet });
    const r = v.validate({ start: "2026-01-01T00:00:00Z", end: "2026-01-02T00:00:00Z", kind: "x" });
    expect(r.ok).toBe(false);
    if (r.ok) return;
    expect(r.issues[0]).toMatchObject({ code: "min_length", path: "$.kind" });
  });

  it("applies the else branch when it does not", () => {
    const v = createValidator(eventSchema, { manifest: gateManifest, ...quiet });
    const r = v.validate({ start: "2026-01-02T00:00:00Z", end: "2026-01-01T00:00:00Z", note: "ab" });
    expect(r.ok).toBe(false);
    if (r.ok) return;
    expect(r.issues[0]).toMatchObject({ code: "min_length", path: "$.note" });
  });

  it("passes when the selected branch is satisfied", () => {
    const v = createValidator(eventSchema, { manifest: gateManifest, ...quiet });
    expect(
      v.validate({ start: "2026-01-01T00:00:00Z", end: "2026-01-02T00:00:00Z", kind: "conference" }).ok,
    ).toBe(true);
  });

  it("requires-style gates carry a required list", () => {
    const v = createValidator(eventSchema, {
      ...quiet,
      manifest: {
        entries: [
          {
            path: "$",
            kind: "gate",
            name: "requires",
            class: "cel",
            reason: "comparing two properties needs CEL",
            cond: { kind: "fieldcmp", field: "end", op: "gt", fieldB: "start" },
            then: { type: "object", required: ["note"] },
          },
        ],
      },
    });
    const r = v.validate({ start: "2026-01-01T00:00:00Z", end: "2026-01-02T00:00:00Z" });
    expect(r.ok).toBe(false);
    if (r.ok) return;
    expect(r.issues[0]).toMatchObject({ code: "required", path: "$.note" });
  });
});

describe("the condition evaluator", () => {
  const obj = { n: 5, s: "b", t: true, when: "2026-06-01T00:00:00Z", other: "2026-01-01T00:00:00Z", empty: "" };

  const cases: ReadonlyArray<[string, Cond, boolean | undefined]> = [
    ["cmp eq", { kind: "cmp", field: "n", op: "eq", value: 5 }, true],
    ["cmp ne", { kind: "cmp", field: "n", op: "ne", value: 5 }, false],
    ["cmp gt", { kind: "cmp", field: "n", op: "gt", value: 4 }, true],
    ["cmp gte equal", { kind: "cmp", field: "n", op: "gte", value: 5 }, true],
    ["cmp lt", { kind: "cmp", field: "n", op: "lt", value: 4 }, false],
    ["cmp lte", { kind: "cmp", field: "n", op: "lte", value: 5 }, true],
    ["cmp on strings", { kind: "cmp", field: "s", op: "gt", value: "a" }, true],
    ["cmp missing field", { kind: "cmp", field: "nope", op: "eq", value: 1 }, undefined],
    ["cmp mismatched types", { kind: "cmp", field: "n", op: "gt", value: "x" }, undefined],
    ["fieldcmp dates", { kind: "fieldcmp", field: "when", op: "gt", fieldB: "other" }, true],
    ["fieldcmp reversed", { kind: "fieldcmp", field: "other", op: "gt", fieldB: "when" }, false],
    ["fieldcmp missing", { kind: "fieldcmp", field: "when", op: "gt", fieldB: "nope" }, undefined],
    ["presence present", { kind: "presence", field: "n", op: "present" }, true],
    ["presence of empty string", { kind: "presence", field: "empty", op: "present" }, false],
    ["presence absent", { kind: "presence", field: "nope", op: "absent" }, true],
    ["in", { kind: "in", field: "s", values: ["a", "b"] }, true],
    ["in miss", { kind: "in", field: "s", values: ["a"] }, false],
    ["and both", { kind: "and", sub: [{ kind: "cmp", field: "n", op: "eq", value: 5 }, { kind: "presence", field: "s", op: "present" }] }, true],
    ["and one false short-circuits", { kind: "and", sub: [{ kind: "cmp", field: "n", op: "eq", value: 9 }, { kind: "cmp", field: "nope", op: "eq", value: 1 }] }, false],
    ["and undecided", { kind: "and", sub: [{ kind: "cmp", field: "n", op: "eq", value: 5 }, { kind: "cmp", field: "nope", op: "eq", value: 1 }] }, undefined],
    ["or one true short-circuits", { kind: "or", sub: [{ kind: "cmp", field: "n", op: "eq", value: 5 }, { kind: "cmp", field: "nope", op: "eq", value: 1 }] }, true],
    ["or undecided", { kind: "or", sub: [{ kind: "cmp", field: "n", op: "eq", value: 9 }, { kind: "cmp", field: "nope", op: "eq", value: 1 }] }, undefined],
    ["not", { kind: "not", sub: [{ kind: "cmp", field: "n", op: "eq", value: 9 }] }, true],
    ["not of undecided", { kind: "not", sub: [{ kind: "cmp", field: "nope", op: "eq", value: 1 }] }, undefined],
    ["empty and", { kind: "and", sub: [] }, undefined],
  ];

  for (const [name, cond, want] of cases) {
    it(name, () => expect(evaluate(cond, obj)).toBe(want));
  }

  it("treats a non-object scope as undecided", () => {
    expect(evaluate({ kind: "cmp", field: "n", op: "eq", value: 5 }, "not an object")).toBe(undefined);
    expect(evaluate({ kind: "cmp", field: "n", op: "eq", value: 5 }, null)).toBe(undefined);
  });

  it("matches Go's presence rule", () => {
    expect(present(0)).toBe(true);
    expect(present(false)).toBe(true);
    expect(present("")).toBe(false);
    expect(present([])).toBe(false);
    expect(present([1])).toBe(true);
    expect(present(null)).toBe(false);
    expect(present(undefined)).toBe(false);
  });

  it("selects only entries carrying a condition", () => {
    const picked = executable([
      { path: "$", kind: "rule", name: "custom", class: "opaque", reason: "r" },
      { path: "$.a", kind: "rule", name: "after", class: "cel", reason: "r", cond: { kind: "fieldcmp" } },
    ]);
    expect(picked.map((e) => e.name)).toEqual(["after"]);
  });
});

describe("scope and defensive paths", () => {
  const entry = (path: string, kind: ExecutableEntry["kind"]): ExecutableEntry => ({
    path,
    kind,
    name: "x",
    class: "cel",
    reason: "r",
    cond: { kind: "fieldcmp" },
  });

  it("resolves a gate's scope to the object it guards", () => {
    const doc = { address: { city: "Berlin", zip: "10115" } };
    // A gate at $.address is evaluated against the address object itself.
    expect(scopeOf(entry("$.address", "gate"), doc)).toEqual(doc.address);
    // A rule at $.address.zip compares siblings, so its scope is the parent.
    expect(scopeOf(entry("$.address.zip", "rule"), doc)).toEqual(doc.address);
    // The root gate sees the whole document.
    expect(scopeOf(entry("$", "gate"), doc)).toEqual(doc);
  });

  it("gives up rather than throwing when the path does not exist", () => {
    expect(scopeOf(entry("$.missing.deep", "gate"), { a: 1 })).toBe(undefined);
    expect(scopeOf(entry("$.a.b", "gate"), { a: "scalar" })).toBe(undefined);
    expect(scopeOf(entry("$.a", "gate"), null)).toBe(undefined);
  });

  it("treats an unknown operator as undecided", () => {
    expect(evaluate({ kind: "cmp", field: "n", op: "between", value: 1 }, { n: 5 })).toBe(undefined);
    expect(evaluate({ kind: "fieldcmp", field: "a", op: "~=", fieldB: "b" }, { a: 1, b: 2 })).toBe(undefined);
  });

  it("treats an unknown condition kind as undecided", () => {
    // A schema emitted by a newer Go version could carry a kind this
    // runtime has never heard of; skipping it defers to the server, which
    // is the only safe reading.
    expect(evaluate({ kind: "sorcery" } as unknown as Cond, { n: 1 })).toBe(undefined);
  });
});

describe("a gate on a nested object", () => {
  it("evaluates against the nested object and reports paths under it", () => {
    const schema = {
      $schema: "https://json-schema.org/draft/2020-12/schema",
      type: "object",
      properties: {
        span: {
          type: "object",
          properties: { from: { type: "string" }, to: { type: "string" }, why: { type: "string" } },
        },
      },
    } as const;
    const v = createValidator(schema, {
      ...quiet,
      manifest: {
        entries: [
          {
            path: "$.span",
            kind: "gate",
            name: "requires",
            class: "cel",
            reason: "comparing two properties needs CEL",
            cond: { kind: "fieldcmp", field: "to", op: "gt", fieldB: "from" },
            then: { type: "object", required: ["why"] },
          },
        ],
      },
    });
    const r = v.validate({ span: { from: "2026-01-01T00:00:00Z", to: "2026-02-01T00:00:00Z" } });
    expect(r.ok).toBe(false);
    if (r.ok) return;
    // The issue is re-rooted under the object the gate guards.
    expect(r.issues[0]).toMatchObject({ code: "required", path: "$.span.why" });
    expect(r.issues[0]?.segments).toEqual(["span", "why"]);
  });
});

describe("equality over JSON shapes", () => {
  // A rule literal can be any JSON value, so a condition may compare
  // arrays and objects, not only scalars.
  const doc = { tags: ["a", "b"], meta: { x: 1, y: [2] }, n: 3 };

  it("compares arrays element by element", () => {
    expect(evaluate({ kind: "cmp", field: "tags", op: "eq", value: ["a", "b"] }, doc)).toBe(true);
    expect(evaluate({ kind: "cmp", field: "tags", op: "eq", value: ["b", "a"] }, doc)).toBe(false);
    expect(evaluate({ kind: "cmp", field: "tags", op: "eq", value: ["a"] }, doc)).toBe(false);
    expect(evaluate({ kind: "cmp", field: "tags", op: "ne", value: ["a"] }, doc)).toBe(true);
  });

  it("compares objects by their keys and values, order-independently", () => {
    expect(evaluate({ kind: "cmp", field: "meta", op: "eq", value: { x: 1, y: [2] } }, doc)).toBe(true);
    expect(evaluate({ kind: "cmp", field: "meta", op: "eq", value: { y: [2], x: 1 } }, doc)).toBe(true);
    expect(evaluate({ kind: "cmp", field: "meta", op: "eq", value: { x: 1 } }, doc)).toBe(false);
    expect(evaluate({ kind: "cmp", field: "meta", op: "eq", value: { x: 1, y: [9] } }, doc)).toBe(false);
  });

  it("does not equate different shapes", () => {
    expect(evaluate({ kind: "cmp", field: "tags", op: "eq", value: { 0: "a", 1: "b" } }, doc)).toBe(false);
    expect(evaluate({ kind: "cmp", field: "n", op: "eq", value: [3] }, doc)).toBe(false);
    expect(evaluate({ kind: "in", field: "tags", values: [["a", "b"], ["c"]] }, doc)).toBe(true);
    expect(evaluate({ kind: "in", field: "tags", values: [["c"]] }, doc)).toBe(false);
  });
});
