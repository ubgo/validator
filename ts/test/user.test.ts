import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { describe, expect, it, vi } from "vitest";

import {
  createValidator,
  DRAFT_07,
  DRAFT_2020,
  jsonPathToSegments,
  localize,
  pointerToSegments,
  render,
  segmentsToJSONPath,
  toIssues,
  valueAt,
  type Manifest,
  type ManifestEntry,
  type Validator,
} from "../src/index.js";

const here = dirname(fileURLToPath(import.meta.url));
const golden = join(here, "..", "..", "jsonschema", "testdata");

const schema2020 = JSON.parse(readFileSync(join(golden, "user.2020.schema.json"), "utf8")) as Record<string, unknown>;
const schema07 = JSON.parse(readFileSync(join(golden, "user.07.schema.json"), "utf8")) as Record<string, unknown>;
const manifest = JSON.parse(readFileSync(join(golden, "user.manifest.json"), "utf8")) as Manifest;

/** A value that satisfies every local rule of the golden schema. */
function validUser(): Record<string, unknown> {
  return {
    email: "ada@example.com",
    age: 30,
    score: 0.5,
    active: true,
    start: "2026-01-01T00:00:00Z",
    end: "2026-01-01T01:00:00Z",
    ttl: 600000000000,
    tags: ["a", "b"],
    labels: { k: 2 },
    home: { street: "Main", zip: "12345" },
    extra: null,
    idOrName: 7,
    type: "online",
    url: "https://x.io",
    event: { kind: "online", url: "https://e.io" },
  };
}

function codes(v: Validator, value: unknown): string[] {
  const r = v.validate(value);
  return r.ok ? [] : r.issues.map((i) => `${i.path}:${i.code}`);
}

describe("golden schema (2020-12)", () => {
  const v = createValidator(schema2020, { manifest, onNonPortable: () => undefined });

  it("accepts a valid user", () => {
    expect(v.validate(validUser())).toEqual({ ok: true, value: validUser() });
  });

  it("reports Go-shaped issues with Go codes", () => {
    const u = validUser();
    u["email"] = "nope";
    u["age"] = 17;
    u["tags"] = ["a", "a", "zzz"];
    u["home"] = { street: "", zip: "abc" };
    u["event"] = { kind: "offline" };
    delete u["type"];
    const r = v.validate(u);
    expect(r.ok).toBe(false);
    if (r.ok) return;
    const got = r.issues.map((i) => `${i.path}:${i.code}`);
    expect(got).toEqual(
      expect.arrayContaining([
        "$.email:email",
        "$.age:min",
        "$.tags:unique",
        "$.tags[2]:enum",
        "$.home.street:min_length",
        "$.home.zip:pattern",
        "$.event.address:required",
        "$.type:required",
      ]),
    );
    const age = r.issues.find((i) => i.path === "$.age");
    expect(age).toMatchObject({ code: "min", rule: "min", message: "must be at least 18", params: { value: 18 } });
    expect(age?.segments).toEqual(["age"]);
  });

  it("enforces forbid-extra, gates and groups", () => {
    const u = validUser();
    u["bogus"] = 1;
    expect(codes(v, u)).toContain("$.bogus:unknown_key");

    const offline = validUser();
    offline["type"] = "offline";
    delete offline["url"];
    // when(type==online) else venue.minLength(1) → venue absent passes JSON
    // Schema (no property, no constraint) but the exactly-one group fails.
    expect(codes(v, offline)).toEqual(["$:group"]);
    offline["venue"] = "Hall";
    expect(codes(v, offline)).toEqual([]);

    const both = validUser();
    both["venue"] = "Hall";
    expect(codes(v, both)).toEqual(["$:group"]);
  });

  it("routes discriminated unions by tag", () => {
    const u = validUser();
    u["event"] = { kind: "online", url: "not a url" };
    expect(codes(v, u)).toEqual(["$.event.url:url"]);
    u["event"] = { kind: "mystery" }; // default: any
    expect(codes(v, u)).toEqual([]);
    u["event"] = { kind: "offline", address: { street: "s", zip: "99999" } };
    expect(codes(v, u)).toEqual([]);
  });

  it("splits the manifest into non-portable and server entries", () => {
    expect(v.serverEntries.map((e) => e.ref)).toEqual(["unique_email"]);
    // "after" is absent on purpose: it carries a condition, so the runtime
    // executes it rather than deferring it to the server.
    expect(v.nonPortable.map((e) => e.name).sort()).toEqual(["custom", "lower", "trim", "when"]);
    expect(v.manifest).toBe(manifest);
    expect(v.schema).toBe(schema2020);
  });

  it("implements Standard Schema", async () => {
    const std = v["~standard"];
    expect(std.version).toBe(1);
    expect(std.vendor).toBe("ubgo");
    // Server rules exist, so ~standard.validate is async.
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ ok: true })));
    const withFetch = createValidator(schema2020, { manifest, onNonPortable: () => undefined, fetch: fetchMock, debounceMs: 1 });
    const okRes = await withFetch["~standard"].validate(validUser());
    expect(okRes).toEqual({ value: validUser() });
    const bad = validUser();
    bad["age"] = 1;
    const badRes = await withFetch["~standard"].validate(bad);
    expect(badRes).toEqual({ issues: [{ message: "must be at least 18", path: ["age"] }] });
  });

  it("implements Standard JSON Schema and refuses the wrong dialect", () => {
    const js = v["~standard"].jsonSchema;
    expect(js?.input({ target: "draft-2020-12" })).toEqual(schema2020);
    expect(js?.output({ target: "draft-2020-12" })).toEqual(schema2020);
    expect(() => js?.input({ target: "draft-07" })).toThrow(/regenerate .* --draft 07/);
    expect(() => js?.input({ target: "openapi-3.0" })).toThrow(/openapi-3\.0 was requested.*--draft 07/);
  });
});

describe("golden schema (draft-07)", () => {
  it("validates identically", () => {
    const v = createValidator(schema07, { manifest, onNonPortable: () => undefined });
    expect(v.validate(validUser()).ok).toBe(true);
    const u = validUser();
    u["score"] = 2;
    expect(codes(v, u)).toEqual(["$.score:max"]);
    const js = v["~standard"].jsonSchema;
    expect(js?.input({ target: "draft-07" })).toEqual(schema07);
    expect(js?.input({ target: "openapi-3.0" })).toEqual(schema07);
    expect(() => js?.input({ target: "draft-2020-12" })).toThrow(/--draft 2020/);
    // Synchronous ~standard.validate when server rules are disabled.
    const sync = createValidator(schema07, { manifest, onNonPortable: () => undefined, serverRules: false });
    expect(sync["~standard"].validate(validUser())).toEqual({ value: validUser() });
  });

  it("defaults to 2020-12 without $schema and rejects unknown dialects", () => {
    const bare = { ...schema2020 };
    delete bare["$schema"];
    expect(createValidator(bare).validate(validUser()).ok).toBe(true);
    expect(() => createValidator({ $schema: "http://example.com/other" })).toThrow(/unsupported \$schema/);
    expect(DRAFT_07).toMatch(/draft-07/);
    expect(DRAFT_2020).toMatch(/2020-12/);
  });
});

describe("server round-trips", () => {
  const withServer = (fetchImpl: typeof fetch, extra: Partial<Parameters<typeof createValidator>[1]> = {}) =>
    createValidator(schema2020, { manifest, onNonPortable: () => undefined, fetch: fetchImpl, debounceMs: 1, ...extra });

  it("posts the field value to the endpoint and merges issues", async () => {
    const calls: Array<{ url: string; body: unknown }> = [];
    const fetchMock: typeof fetch = async (url, init) => {
      calls.push({ url: String(url), body: JSON.parse(String(init?.body)) });
      return new Response(JSON.stringify({ ok: false, issues: [{ code: "taken", message: "already taken" }] }));
    };
    const v = withServer(fetchMock, { baseURL: "https://api.test" });
    const r = await v.validateAsync(validUser());
    expect(calls).toEqual([{ url: "https://api.test/v/unique_email", body: { rule: "unique_email", value: "ada@example.com" } }]);
    expect(r).toEqual({
      ok: false,
      issues: [{ code: "taken", message: "already taken", path: "$.email", segments: ["email"], rule: "async", params: { ref: "unique_email" } }],
    });
    // An empty server path means "this field"; server params merge over the ref.
    const empty = withServer(async () => new Response(JSON.stringify({ ok: false, issues: [{ code: "x", message: "m", path: "", rule: "", params: { error: "e" } }] })));
    const re = await empty.validateAsync(validUser());
    if (!re.ok) expect(re.issues[0]).toEqual({ code: "x", message: "m", path: "$.email", segments: ["email"], rule: "async", params: { ref: "unique_email", error: "e" } });
  });

  it("returns local issues first without calling the server", async () => {
    const fetchMock = vi.fn();
    const u = validUser();
    u["age"] = 1;
    const r = await withServer(fetchMock as unknown as typeof fetch).validateAsync(u);
    expect(r.ok).toBe(false);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("treats a bare {ok:false} as one custom issue and honors response fields", async () => {
    const v = withServer(async () => new Response(JSON.stringify({ ok: false })));
    const r = await v.validateAsync(validUser());
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.issues[0]).toMatchObject({ code: "custom", path: "$.email" });
    const rich = withServer(async () => new Response(JSON.stringify({ ok: false, issues: [{ code: "x", message: "m", path: "$.other", segments: ["other"], rule: "r", params: { a: 1 } }] })));
    const rr = await rich.validateAsync(validUser());
    if (!rr.ok) expect(rr.issues[0]).toEqual({ code: "x", message: "m", path: "$.other", segments: ["other"], rule: "r", params: { ref: "unique_email", a: 1 } });
  });

  it("surfaces HTTP failures and a missing fetch", async () => {
    const v = withServer(async () => new Response("nope", { status: 500 }));
    await expect(v.validateAsync(validUser())).rejects.toThrow(/responded 500/);
    const noFetch = createValidator(schema2020, { manifest, onNonPortable: () => undefined, fetch: undefined as unknown as typeof fetch, debounceMs: 1 });
    // fetch option undefined falls back to globalThis.fetch; force absence.
    const saved = globalThis.fetch;
    // Simulate an environment without fetch. The global's declared type is
    // non-optional, so the write goes through a narrower view of the object
    // instead of a suppression.
    const g = globalThis as { fetch?: typeof fetch | undefined };
    g.fetch = undefined;
    try {
      const bare = createValidator(schema2020, { manifest, onNonPortable: () => undefined, debounceMs: 1 });
      await expect(bare.validateAsync(validUser())).rejects.toThrow(/no fetch available/);
    } finally {
      globalThis.fetch = saved;
    }
    expect(noFetch.serverEntries.length).toBe(1);
  });

  it("debounces rapid calls per rule into the last request", async () => {
    let calls = 0;
    const v = createValidator(schema2020, {
      manifest,
      onNonPortable: () => undefined,
      debounceMs: 20,
      fetch: async (_url, init) => {
        calls++;
        const body = JSON.parse(String(init?.body)) as { value: string };
        return new Response(JSON.stringify({ ok: body.value === "last@example.com" }));
      },
    });
    const a = validUser();
    a["email"] = "first@example.com";
    const b = validUser();
    b["email"] = "last@example.com";
    const [ra, rb] = await Promise.all([v.validateAsync(a), v.validateAsync(b)]);
    expect(calls).toBe(1);
    // Both callers receive the result of the LAST call.
    expect(ra.ok).toBe(true);
    expect(rb.ok).toBe(true);
  });

  it("propagates a rejected round-trip to every waiter", async () => {
    const v = createValidator(schema2020, {
      manifest,
      onNonPortable: () => undefined,
      debounceMs: 5,
      fetch: async () => {
        throw new Error("network down");
      },
    });
    await expect(Promise.all([v.validateAsync(validUser()), v.validateAsync(validUser())])).rejects.toThrow(/network down/);
  });
});

describe("non-portable reporting", () => {
  it("warns once per entry by default", () => {
    const spy = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    createValidator(schema2020, { manifest });
    createValidator(schema2020, { manifest });
    // Four, not five: the "after" entry is executed here, not warned about.
    expect(spy).toHaveBeenCalledTimes(4);
    expect(spy.mock.calls[0]?.[0]).toMatch(/\$\.email: rule "custom" is opaque/);
    spy.mockRestore();
  });

  it("calls the supplied hook", () => {
    const seen: ManifestEntry[] = [];
    createValidator(schema2020, { manifest, onNonPortable: (e) => seen.push(e) });
    expect(seen.map((e) => `${e.path}:${e.name}`)).toEqual(["$.email:custom", "$.email:trim", "$.email:lower", "$:when"]);
  });

  it("rejects malformed manifests", () => {
    expect(() => createValidator(schema2020, { manifest: { entries: "x" as unknown as [] } })).toThrow(/entries must be an array/);
    expect(() =>
      createValidator(schema2020, { manifest: { entries: [{ path: "$", kind: "rule", name: "x", class: "weird" as never, reason: "" }] } }),
    ).toThrow(/malformed manifest entry/);
    expect(createValidator(schema2020).manifest).toEqual({ entries: [] });
  });
});

describe("vendor keywords and message rendering", () => {
  it("uses x-code and x-message when the schema carries them", () => {
    const v = createValidator({
      $schema: DRAFT_2020,
      type: "object",
      properties: { name: { type: "string", minLength: 3, "x-code": "too_short", "x-message": "need {value}+ chars" } },
    });
    expect(codes(v, { name: "ab" })).toEqual(["$.name:too_short"]);
    const r = v.validate({ name: "ab" });
    if (!r.ok) expect(r.issues[0]?.message).toBe("need 3+ chars");
  });

  it("maps every keyword and format the emitter uses", () => {
    const v = createValidator({
      $schema: DRAFT_2020,
      type: "object",
      properties: {
        s: { type: "string", maxLength: 1, pattern: "^a" },
        n: { type: "number", maximum: 1, exclusiveMinimum: 0, multipleOf: 0.5 },
        i: { type: "integer", exclusiveMaximum: 10, enum: [1, 2, 3] },
        c: { const: "x" },
        a: { type: "array", minItems: 2, maxItems: 3 },
        m: { type: "object", minProperties: 1, maxProperties: 1, additionalProperties: { type: "integer" } },
        e: { type: "string", format: "email" },
        u: { type: "string", format: "uri" },
        id: { type: "string", format: "uuid" },
        t: { type: "string", format: "date-time" },
        ip4: { type: "string", format: "ipv4" },
        ip6: { type: "string", format: "ipv6" },
        host: { type: "string", format: "hostname" },
        d: { type: "string", format: "date" },
        un: { anyOf: [{ type: "integer" }, { type: "boolean" }] },
        nt: { not: { const: "no" } },
      },
    });
    const got = codes(v, {
      s: "bb", n: 2.3, i: 11, c: "y", a: [1], m: {}, e: "x", u: "x", id: "x", t: "x", un: "s", nt: "no",
      ip4: "x", ip6: "x", host: "-x", d: "x",
    });
    expect(got).toEqual(
      expect.arrayContaining([
        "$.s:max_length", "$.s:pattern", "$.n:max", "$.n:multiple_of", "$.i:lt", "$.i:enum", "$.c:const",
        "$.a:min_items", "$.m:min_items", "$.e:email", "$.u:url", "$.id:uuid", "$.t:type", "$.un:no_union_match", "$.nt:not",
        "$.ip4:ipv4", "$.ip6:ipv6", "$.host:hostname", "$.d:date",
      ]),
    );
    expect(codes(v, { n: 0 })).toContain("$.n:gt");
    expect(codes(v, { a: [1, 2, 3, 4] })).toContain("$.a:max_items");
    expect(codes(v, { m: { a: 1, b: 2 } })).toContain("$.m:max_items");
    expect(codes(v, { m: { a: "x" } })).toContain("$.m.a:type");
    // An unknown format falls back to its name as the code.
    const custom = createValidator({ $schema: DRAFT_2020, type: "string", format: "ipv4" });
    expect(codes(custom, "x")).toEqual(["$:ipv4"]);
  });

  it("dedupes repeated Ajv errors and drops if/then wrappers", () => {
    expect(toIssues(null)).toEqual([]);
    const issues = toIssues([
      { keyword: "if", instancePath: "", schemaPath: "#/if", params: { failingKeyword: "then" }, message: "x" },
      { keyword: "minimum", instancePath: "/a", schemaPath: "#/a", params: { limit: 1, comparison: ">=" }, message: "x" },
      { keyword: "minimum", instancePath: "/a", schemaPath: "#/b", params: { limit: 1, comparison: ">=" }, message: "x" },
      { keyword: "weird", instancePath: "/b", schemaPath: "#/c", params: {} },
    ]);
    expect(issues.map((i) => `${i.path}:${i.code}`)).toEqual(["$.a:min", "$.b:weird"]);
    expect(issues[1]?.message).toBe("weird");
    // Branch diagnostics under a failed combinator collapse into the
    // combinator's own issue, matching Go's single group/union issue.
    const branches = toIssues([
      { keyword: "required", instancePath: "", schemaPath: "#/allOf/2/oneOf/0/required", params: { missingProperty: "url" } },
      { keyword: "required", instancePath: "", schemaPath: "#/allOf/2/oneOf/1/required", params: { missingProperty: "venue" } },
      { keyword: "oneOf", instancePath: "", schemaPath: "#/allOf/2/oneOf", params: { passingSchemas: null } },
      { keyword: "required", instancePath: "", schemaPath: "#/required", params: { missingProperty: "type" } },
    ]);
    expect(branches.map((i) => `${i.path}:${i.code}`)).toEqual(["$:group", "$.type:required"]);
  });

  it("renders templates", () => {
    expect(render("a {x} b {y}", { x: 1 })).toBe("a 1 b {y}");
    expect(render("plain", {})).toBe("plain");
  });
});

describe("coercion", () => {
  it("enables Ajv coerceTypes when the manifest declares a coerce transform", () => {
    const schema = { $schema: DRAFT_2020, type: "object", properties: { age: { type: "integer", minimum: 18 } } };
    const withCoerce = createValidator(schema, {
      manifest: { entries: [{ path: "$.age", kind: "transform", name: "coerce", class: "lossy", reason: "r" }] },
      onNonPortable: () => undefined,
    });
    const doc: Record<string, unknown> = { age: "21" };
    expect(withCoerce.validate(doc)).toEqual({ ok: true, value: { age: 21 } });
    expect(doc["age"]).toBe(21); // written back, like the Go side
    const without = createValidator(schema);
    expect(without.validate({ age: "21" }).ok).toBe(false);
  });
});

describe("catalog", () => {
  it("renders messages by code for local and server issues", async () => {
    const catalog = { locale: "de", messages: { min: "mindestens {value}", custom: "Server sagt: {error}" } };
    const v = createValidator(schema2020, {
      manifest,
      onNonPortable: () => undefined,
      catalog,
      debounceMs: 1,
      fetch: async () => new Response(JSON.stringify({ ok: false, issues: [{ code: "custom", message: "already taken", params: { error: "already taken" } }] })),
    });
    const bad = validUser();
    bad["age"] = 1;
    const local = v.validate(bad);
    if (!local.ok) expect(local.issues.find((i) => i.path === "$.age")?.message).toBe("mindestens 18");
    const remote = await v.validateAsync(validUser());
    if (!remote.ok) expect(remote.issues[0]?.message).toBe("Server sagt: already taken");
    // Untranslated codes keep their message.
    const untranslated = validUser();
    untranslated["email"] = "x";
    const r = v.validate(untranslated);
    if (!r.ok) expect(r.issues[0]?.message).toBe("must be a valid email address");
    expect(localize([], catalog)).toEqual([]);
  });
});

describe("fixtures", () => {
  it("test/fixtures/user.schema.ts matches the Go golden", () => {
    const ours = readFileSync(join(here, "fixtures", "user.schema.ts"), "utf8");
    const theirs = readFileSync(join(golden, "user.schema.ts"), "utf8");
    expect(ours).toBe(theirs);
  });
});

describe("path helpers", () => {
  it("converts JSONPath, pointers and segments", () => {
    expect(jsonPathToSegments("$.tags[0].name")).toEqual(["tags", 0, "name"]);
    expect(jsonPathToSegments("$")).toEqual([]);
    expect(jsonPathToSegments("$.a[*].b.*")).toEqual(["a", "*", "b", "*"]);
    expect(jsonPathToSegments("$[x")).toEqual(["x"]);
    expect(jsonPathToSegments("plain")).toEqual(["plain"]);
    expect(pointerToSegments("")).toEqual([]);
    expect(pointerToSegments("/a~1b/0/c~0d")).toEqual(["a/b", 0, "c~d"]);
    expect(segmentsToJSONPath(["a", 0, "b"])).toBe("$.a[0].b");
    expect(valueAt({ a: [{ b: 1 }] }, ["a", 0, "b"])).toBe(1);
    expect(valueAt({ a: [{ b: 1 }] }, ["a", "*", "b"])).toBeUndefined();
    expect(valueAt(null, ["a"])).toBeUndefined();
    expect(valueAt(5, [])).toBe(5);
  });
});

describe("the golden schema's cross-field rule runs in the browser", () => {
  it("reports $.end after $.start without a round trip", () => {
    const v = createValidator(schema2020, { manifest, onNonPortable: () => undefined, serverRules: false });
    const u = validUser();
    // Swap the two so end precedes start; every other field stays valid.
    const start = u["start"];
    u["start"] = u["end"];
    u["end"] = start;
    const r = v.validate(u);
    expect(r.ok).toBe(false);
    if (r.ok) return;
    const after = r.issues.find((i) => i.code === "after");
    expect(after).toBeDefined();
    expect(after?.path).toBe("$.end");
    // The wording comes from the Go emitter, so it matches the server's.
    expect(after?.message).toBe("must be after Start");
  });

  it("leaves a correctly ordered pair alone", () => {
    const v = createValidator(schema2020, { manifest, onNonPortable: () => undefined, serverRules: false });
    const r = v.validate(validUser());
    expect(r.ok).toBe(true);
  });
});
