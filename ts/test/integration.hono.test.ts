import { Hono } from "hono";
import { sValidator } from "@hono/standard-validator";
import { describe, expect, it, vi } from "vitest";

import { createValidator } from "../src/index.js";
import { userSchema, userManifest } from "./fixtures/user.schema.js";

// A real Standard Schema consumer, unmodified: Hono's standard-validator
// middleware takes our validator as-is. If `~standard` drifted from the
// spec, this would fail to type-check or to run — no mocks between us and
// the library.

function validUser() {
  return {
    email: "ada@example.com", age: 30, score: 0.5, active: true,
    start: "2026-01-01T00:00:00Z", end: "2026-01-01T01:00:00Z", ttl: 600000000000,
    tags: ["a", "b"], labels: { k: 2 }, home: { street: "Main", zip: "12345" },
    extra: null, idOrName: 7, type: "online", url: "https://x.io",
    event: { kind: "online", url: "https://e.io" },
  };
}

describe("Hono @hono/standard-validator", () => {
  it("accepts a valid body and rejects an invalid one through the middleware", async () => {
    const user = createValidator(userSchema, { manifest: userManifest, onNonPortable: () => undefined, serverRules: false });
    const app = new Hono().post("/users", sValidator("json", user), (c) => {
      const body = c.req.valid("json");
      return c.json({ received: body.email });
    });

    const ok = await app.request("/users", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(validUser()) });
    expect(ok.status).toBe(200);
    expect(await ok.json()).toEqual({ received: "ada@example.com" });

    const bad = await app.request("/users", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ ...validUser(), age: 3, email: "nope" }),
    });
    expect(bad.status).toBe(400);
    const payload = (await bad.json()) as { success: boolean; error: Array<{ message: string; path?: unknown[] }> };
    expect(payload.success).toBe(false);
    expect(payload.error.map((e) => e.message)).toEqual(expect.arrayContaining(["must be a valid email address", "must be at least 18"]));
    expect(payload.error.find((e) => e.message === "must be at least 18")?.path).toEqual(["age"]);
  });

  it("awaits the async form when server rules are enabled", async () => {
    const fetchMock = vi.fn(async (_url: unknown, init?: RequestInit) => {
      const body = JSON.parse(String(init?.body)) as { value: string };
      return new Response(JSON.stringify(body.value === "taken@example.com" ? { ok: false, issues: [{ code: "custom", message: "already taken" }] } : { ok: true }));
    });
    const user = createValidator(userSchema, { manifest: userManifest, onNonPortable: () => undefined, fetch: fetchMock as unknown as typeof fetch, debounceMs: 1 });
    const app = new Hono().post("/users", sValidator("json", user), (c) => c.json({ ok: true }));

    const free = await app.request("/users", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(validUser()) });
    expect(free.status).toBe(200);
    const taken = await app.request("/users", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ ...validUser(), email: "taken@example.com" }),
    });
    expect(taken.status).toBe(400);
    const payload = (await taken.json()) as { error: Array<{ message: string; path?: unknown[] }> };
    expect(payload.error).toEqual([{ message: "already taken", path: ["email"] }]);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
