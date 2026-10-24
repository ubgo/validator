/**
 * Evaluating the rules JSON Schema cannot express.
 *
 * JSON Schema has no way to compare two properties, so a cross-field rule
 * (`end` after `start`) or a gate keyed on one used to reach the browser as
 * nothing but a warning: the user had to submit before being told. The Go
 * emitter now carries those elements in the manifest as data — the
 * condition tree in wire names, plus the JSON Schema fragment each branch
 * would have produced — and this module executes them.
 *
 * Only comparisons are evaluated here. Branch constraints are handed back
 * to Ajv as ordinary schema, so there is exactly one implementation of
 * what `minLength` means on this side, and it is the same one the rest of
 * the schema uses.
 */

import type { ManifestEntry } from "./manifest.js";
import { jsonPathToSegments } from "./manifest.js";

/** Comparison operators, matching Go's `ir.CmpOp`. */
export type CmpOp = "eq" | "ne" | "gt" | "gte" | "lt" | "lte";

/** Presence directions, matching Go's `ir.PresenceOp`. */
export type PresenceOp = "present" | "absent";

/**
 * A condition as the manifest carries it: the Go `ir.Condition`, with
 * field names already translated to wire names by the emitter.
 *
 * `expr` and `custom` never appear here — the emitter omits the condition
 * entirely for those, because neither can be evaluated from data alone.
 */
export interface Cond {
  readonly kind: "cmp" | "fieldcmp" | "presence" | "in" | "and" | "or" | "not";
  readonly field?: string;
  readonly op?: string;
  readonly value?: unknown;
  readonly fieldB?: string;
  readonly values?: ReadonlyArray<unknown>;
  readonly sub?: ReadonlyArray<Cond>;
}

/**
 * Reads a property by wire name from the object under validation.
 * Returns undefined for a missing property and for a non-object value,
 * which the presence and comparison rules below both treat as absent.
 */
function prop(value: unknown, name: string | undefined): unknown {
  if (name === undefined || value === null || typeof value !== "object") return undefined;
  return (value as Record<string, unknown>)[name];
}

/**
 * Presence, matching the Go interpreter's rule exactly: a missing key,
 * null, an empty string and an empty array are absent; every number and
 * boolean, including 0 and false, is present.
 *
 * Keeping this identical to Go matters more than it looks: a form that
 * says "required" in the browser and "optional" on the server, or the
 * reverse, is worse than one that only checks on submit.
 */
export function present(v: unknown): boolean {
  if (v === undefined || v === null) return false;
  if (typeof v === "string") return v !== "";
  if (Array.isArray(v)) return v.length > 0;
  return true;
}

/**
 * Orders two values the way Go's comparison rules do. Numbers compare
 * numerically, strings lexically, booleans false < true, and RFC 3339
 * date-times by their instant, which is what makes `after`/`before` work
 * on the wire where a time is a string.
 *
 * Returns undefined when the pair has no ordering, which callers treat as
 * "cannot decide here" and leave to the server rather than guessing.
 */
function compare(a: unknown, b: unknown): number | undefined {
  if (typeof a === "number" && typeof b === "number") return a === b ? 0 : a < b ? -1 : 1;
  if (typeof a === "boolean" && typeof b === "boolean") return a === b ? 0 : a ? 1 : -1;
  if (typeof a === "string" && typeof b === "string") {
    const ta = Date.parse(a);
    const tb = Date.parse(b);
    // Both must parse as dates, or neither: comparing a date against a
    // plain string by its epoch value would be nonsense.
    if (!Number.isNaN(ta) && !Number.isNaN(tb) && isDateLike(a) && isDateLike(b)) {
      return ta === tb ? 0 : ta < tb ? -1 : 1;
    }
    return a === b ? 0 : a < b ? -1 : 1;
  }
  return undefined;
}

/** RFC 3339 starts with a date; a bare number would otherwise parse. */
function isDateLike(s: string): boolean {
  return /^\d{4}-\d{2}-\d{2}/.test(s);
}

/** Deep equality for the JSON shapes a rule literal can take. */
function equal(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (typeof a === "number" && typeof b === "number") return a === b;
  if (Array.isArray(a) || Array.isArray(b)) {
    // An array is never equal to a plain object, even one whose keys are
    // its indices: JSON distinguishes them and so must this.
    if (!Array.isArray(a) || !Array.isArray(b)) return false;
    return a.length === b.length && a.every((x, i) => equal(x, b[i]));
  }
  if (a && b && typeof a === "object" && typeof b === "object") {
    const ka = Object.keys(a as object);
    const kb = Object.keys(b as object);
    return (
      ka.length === kb.length &&
      ka.every((k) => equal((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k]))
    );
  }
  return false;
}

function ordered(op: string, c: number | undefined): boolean | undefined {
  if (c === undefined) return undefined;
  switch (op as CmpOp) {
    case "gt":
      return c > 0;
    case "gte":
      return c >= 0;
    case "lt":
      return c < 0;
    case "lte":
      return c <= 0;
  }
  return undefined;
}

/**
 * Evaluates a condition against the object under validation.
 *
 * Returns undefined when the condition cannot be decided here — an
 * ordering the browser has no rule for, or an operand that is absent.
 * Undecided is deliberately not false: reporting a violation the server
 * would not report is the one failure mode that makes a frontend
 * validator worse than none.
 */
export function evaluate(cond: Cond, value: unknown): boolean | undefined {
  switch (cond.kind) {
    case "cmp": {
      const a = prop(value, cond.field);
      if (a === undefined) return undefined;
      if (cond.op === "eq") return equal(a, cond.value);
      if (cond.op === "ne") return !equal(a, cond.value);
      return ordered(cond.op ?? "", compare(a, cond.value));
    }
    case "fieldcmp": {
      const a = prop(value, cond.field);
      const b = prop(value, cond.fieldB);
      if (a === undefined || b === undefined) return undefined;
      if (cond.op === "eq") return equal(a, b);
      if (cond.op === "ne") return !equal(a, b);
      return ordered(cond.op ?? "", compare(a, b));
    }
    case "presence": {
      const has = present(prop(value, cond.field));
      return (cond.op as PresenceOp) === "absent" ? !has : has;
    }
    case "in": {
      const a = prop(value, cond.field);
      if (a === undefined) return undefined;
      return (cond.values ?? []).some((v) => equal(a, v));
    }
    case "and": {
      const subs = cond.sub ?? [];
      if (subs.length === 0) return undefined;
      let undecided = false;
      for (const s of subs) {
        const r = evaluate(s, value);
        if (r === false) return false; // one false settles an and
        if (r === undefined) undecided = true;
      }
      return undecided ? undefined : true;
    }
    case "or": {
      const subs = cond.sub ?? [];
      if (subs.length === 0) return undefined;
      let undecided = false;
      for (const s of subs) {
        const r = evaluate(s, value);
        if (r === true) return true; // one true settles an or
        if (r === undefined) undecided = true;
      }
      return undecided ? undefined : false;
    }
    case "not": {
      const sub = cond.sub?.[0];
      if (!sub) return undefined;
      const r = evaluate(sub, value);
      return r === undefined ? undefined : !r;
    }
  }
  return undefined;
}

/**
 * A manifest entry this runtime can execute: one carrying a condition.
 * Entries without one stay server-side and are only warned about.
 */
export interface ExecutableEntry extends ManifestEntry {
  readonly cond: Cond;
  readonly then?: Readonly<Record<string, unknown>>;
  readonly else?: Readonly<Record<string, unknown>>;
  /** The issue code Go reports when `cond` alone is the rule and fails. */
  readonly code?: string;
  /** The message Go renders for that issue, already filled in. */
  readonly message?: string;
}

/** Narrows the entries the runtime can act on. */
export function executable(entries: ReadonlyArray<ManifestEntry>): ExecutableEntry[] {
  return entries.filter((e): e is ExecutableEntry => {
    const c = (e as { cond?: unknown }).cond;
    return typeof c === "object" && c !== null;
  });
}

/**
 * The object an entry's condition is evaluated against. A gate is stated
 * at the object it guards (`$`, or `$.address` for a nested one); a
 * cross-field rule is stated at the field, so its condition belongs to
 * that field's parent.
 */
export function scopeOf(entry: ExecutableEntry, value: unknown): unknown {
  const segments = jsonPathToSegments(entry.path);
  // A rule sits on the field; its siblings live one level up.
  const depth = entry.kind === "rule" || entry.kind === "condition" ? segments.length - 1 : segments.length;
  let cur = value;
  for (let i = 0; i < depth; i++) {
    if (cur === null || typeof cur !== "object") return undefined;
    cur = (cur as Record<PropertyKey, unknown>)[segments[i] as PropertyKey];
  }
  return cur;
}
