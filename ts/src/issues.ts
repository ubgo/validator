/**
 * Ajv error → `Issue` mapping.
 *
 * The runtime's issues must match Go's `verr.Issue` shape and codes so a
 * frontend and a backend can share one message catalog (docsi/IR.md §6).
 * Ajv reports keyword failures; this file renames keywords to the Go codes,
 * lifts the IR's `x-code` / `x-message` vendor keywords when the schema
 * carries them, and renders the same default English text Go renders.
 */

import type { ErrorObject } from "ajv";

import { pointerToSegments, segmentsToJSONPath } from "./manifest.js";

/** One validation failure. Mirrors Go's `verr.Issue` field for field. */
export interface Issue {
  /** Machine-readable code, e.g. `min_length`. Same set as Go. */
  readonly code: string;
  /** JSONPath of the failing value, wire names: `$.tags[0].name`. */
  readonly path: string;
  /** Rendered default message; localize from `code` + `params` instead. */
  readonly message: string;
  /** The rule/keyword that failed, when a rule failed. */
  readonly rule?: string;
  /** Message template variables: the rule operand plus context. */
  readonly params?: Readonly<Record<string, unknown>>;
  /** Path as Standard Schema segments, for adapters. */
  readonly segments: ReadonlyArray<PropertyKey>;
}

/** Structural codes shared with Go's `verr`. */
export const CODE_REQUIRED = "required";
export const CODE_TYPE = "type";
export const CODE_UNKNOWN_KEY = "unknown_key";
export const CODE_VARIANT = "unknown_variant";
export const CODE_UNION = "no_union_match";
export const CODE_GROUP = "group";
export const CODE_CUSTOM = "custom";

/** Vendor keywords the Go emitter writes for error metadata. */
export const X_CODE = "x-code";
export const X_MESSAGE = "x-message";

/**
 * Ajv keyword → Go code and default template. `{value}` is the operand
 * (Ajv's `params.limit` for bounds, `allowedValues` for enum, …).
 */
const KEYWORDS: Readonly<Record<string, { code: string; tmpl: string; rule?: string }>> = {
  minLength: { code: "min_length", tmpl: "must be at least {value} characters", rule: "minLength" },
  maxLength: { code: "max_length", tmpl: "must be at most {value} characters", rule: "maxLength" },
  minItems: { code: "min_items", tmpl: "must have at least {value} items", rule: "minItems" },
  maxItems: { code: "max_items", tmpl: "must have at most {value} items", rule: "maxItems" },
  minProperties: { code: "min_items", tmpl: "must have at least {value} items", rule: "minItems" },
  maxProperties: { code: "max_items", tmpl: "must have at most {value} items", rule: "maxItems" },
  uniqueItems: { code: "unique", tmpl: "items must be unique", rule: "unique" },
  minimum: { code: "min", tmpl: "must be at least {value}", rule: "min" },
  maximum: { code: "max", tmpl: "must be at most {value}", rule: "max" },
  exclusiveMinimum: { code: "gt", tmpl: "must be greater than {value}", rule: "gt" },
  exclusiveMaximum: { code: "lt", tmpl: "must be less than {value}", rule: "lt" },
  multipleOf: { code: "multiple_of", tmpl: "must be a multiple of {value}", rule: "multipleOf" },
  pattern: { code: "pattern", tmpl: "must match {value}", rule: "pattern" },
  enum: { code: "enum", tmpl: "must be one of {value}", rule: "enum" },
  const: { code: "const", tmpl: "must equal {value}", rule: "const" },
  required: { code: CODE_REQUIRED, tmpl: "field is required" },
  type: { code: CODE_TYPE, tmpl: "expected {value}" },
  additionalProperties: { code: CODE_UNKNOWN_KEY, tmpl: "unknown key" },
  oneOf: { code: CODE_GROUP, tmpl: "exactly one of the alternatives must match" },
  anyOf: { code: CODE_UNION, tmpl: "value matches no variant" },
  not: { code: "not", tmpl: "must not match the excluded schema" },
};

/** JSON Schema `format` → Go rule code. */
const FORMATS: Readonly<Record<string, { code: string; tmpl: string; rule?: string }>> = {
  email: { code: "email", tmpl: "must be a valid email address" },
  uri: { code: "url", tmpl: "must be a valid URL" },
  uuid: { code: "uuid", tmpl: "must be a valid UUID" },
  ipv4: { code: "ipv4", tmpl: "must be a valid IPv4 address" },
  ipv6: { code: "ipv6", tmpl: "must be a valid IPv6 address" },
  hostname: { code: "hostname", tmpl: "must be a valid hostname" },
  date: { code: "date", tmpl: "must be a date (YYYY-MM-DD)" },
  // date-time is both the time node's wire type and the datetime rule; the
  // Go interpreter reports a type issue for a non-time value, so mirror it.
  "date-time": { code: CODE_TYPE, tmpl: "expected time" },
};

/**
 * Keywords Ajv reports as bookkeeping around a real failure. `if` fires
 * alongside the `then`/`else` failure it caused; the nested failure is the
 * one that names the property. Dropping the wrapper keeps issues 1:1 with
 * Go's.
 */
const WRAPPER_KEYWORDS: ReadonlySet<string> = new Set(["if", "then", "else", "allOf"]);

/** Renders `{key}` placeholders; unknown placeholders stay verbatim. */
export function render(template: string, params: Readonly<Record<string, unknown>>): string {
  return template.replace(/\{(\w+)\}/g, (whole, key: string) =>
    key in params ? String(params[key]) : whole,
  );
}

/** Keywords whose branch failures Ajv also reports individually. */
const BRANCH_KEYWORDS: ReadonlySet<string> = new Set(["oneOf", "anyOf"]);

/**
 * Converts Ajv's error list into Go-shaped issues, deduplicated.
 *
 * Ajv reports a failed `oneOf`/`anyOf` AND every error from each branch it
 * tried; Go reports one `group` / `no_union_match` issue. Branch errors are
 * recognised by their schemaPath sitting under the combinator's and dropped.
 */
export function toIssues(errors: ReadonlyArray<ErrorObject> | null | undefined): Issue[] {
  if (!errors) return [];
  const branchRoots = errors.filter((e) => BRANCH_KEYWORDS.has(e.keyword)).map((e) => e.schemaPath + "/");
  const out: Issue[] = [];
  const seen = new Set<string>();
  for (const err of errors) {
    if (WRAPPER_KEYWORDS.has(err.keyword)) continue;
    if (branchRoots.some((root) => err.schemaPath.startsWith(root))) continue;
    const issue = toIssue(err);
    const key = `${issue.path}|${issue.code}|${issue.rule ?? ""}`;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(issue);
  }
  return out;
}

function toIssue(err: ErrorObject): Issue {
  const segments = pointerToSegments(err.instancePath);
  const params: Record<string, unknown> = { ...err.params };
  let entry = KEYWORDS[err.keyword];
  let value: unknown = params["limit"] ?? params["allowedValues"] ?? params["allowedValue"] ?? params["multipleOf"] ?? params["pattern"] ?? params["type"];

  if (err.keyword === "format") {
    const fmt = String(params["format"]);
    const f = FORMATS[fmt];
    if (f === undefined) {
      entry = { code: fmt, tmpl: `must be a valid ${fmt}` };
    } else if (fmt === "date-time") {
      entry = { code: f.code, tmpl: f.tmpl };
    } else {
      entry = { code: f.code, tmpl: f.tmpl, rule: fmt };
    }
    value = fmt;
  } else if (err.keyword === "required") {
    segments.push(String(params["missingProperty"]));
  } else if (err.keyword === "additionalProperties") {
    segments.push(String(params["additionalProperty"]));
    params["key"] = params["additionalProperty"];
  }
  if (value !== undefined) params["value"] = value;

  const parent = (err.parentSchema ?? {}) as Record<string, unknown>;
  const xCode = parent[X_CODE];
  const xMessage = parent[X_MESSAGE];
  const code = typeof xCode === "string" ? xCode : (entry?.code ?? err.keyword);
  const tmpl = typeof xMessage === "string" ? xMessage : (entry?.tmpl ?? err.message ?? err.keyword);

  const issue: Issue = {
    code,
    path: segmentsToJSONPath(segments),
    message: render(tmpl, params),
    params,
    segments,
    ...(entry?.rule !== undefined ? { rule: entry.rule } : {}),
  };
  return issue;
}

/**
 * A message catalog: issue code → template, the same JSON the Go side
 * exports with `validatorctl catalog`. When supplied, messages are
 * rendered from the catalog by code so both ends speak one language.
 */
export interface Catalog {
  readonly locale: string;
  readonly messages: Readonly<Record<string, string>>;
}

/** Re-renders every issue's message from a catalog; untranslated codes keep theirs. */
export function localize(issues: ReadonlyArray<Issue>, catalog: Catalog): Issue[] {
  return issues.map((i) => {
    const tmpl = catalog.messages[i.code];
    return tmpl === undefined ? i : { ...i, message: render(tmpl, i.params ?? {}) };
  });
}
