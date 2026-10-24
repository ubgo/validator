import { describe, expectTypeOf, it } from "vitest";

import { createValidator, type Infer } from "../src/index.js";
import { userSchema, userManifest } from "./fixtures/user.schema.js";

// Type-level proof: the `--ts` export carries enough literal information
// for json-schema-to-ts to type the validated value. These assertions are
// checked by `tsc` (and vitest's expectTypeOf); a schema shape the inference
// cannot follow would surface here as a type error, not at runtime.
describe("type inference from the exported schema", () => {
  it("infers required scalars, optionals, arrays, refs and nullables", () => {
    type User = Infer<typeof userSchema>;
    expectTypeOf<User["email"]>().toEqualTypeOf<string>();
    expectTypeOf<User["age"]>().toEqualTypeOf<number>();
    expectTypeOf<User["active"]>().toEqualTypeOf<boolean>();
    expectTypeOf<User["tags"]>().toEqualTypeOf<("a" | "b")[]>();
    // Nested objects without additionalProperties:false keep an index
    // signature, so assert on the properties rather than the whole shape.
    expectTypeOf<User["home"]["street"]>().toEqualTypeOf<string>();
    expectTypeOf<User["home"]["zip"]>().toEqualTypeOf<string>();
    expectTypeOf<User["avatar"]>().toEqualTypeOf<string | null | undefined>();
    expectTypeOf<User["type"]>().toEqualTypeOf<"online" | "offline">();

    const v = createValidator(userSchema, { manifest: userManifest, onNonPortable: () => undefined });
    const r = v.validate({});
    if (r.ok) {
      expectTypeOf(r.value.email).toEqualTypeOf<string>();
      expectTypeOf(r.value.url).toEqualTypeOf<string | undefined>();
    }
  });

  it("falls back to unknown for a widened schema, and honors an explicit type", () => {
    const widened: Record<string, unknown> = { type: "object" };
    const u = createValidator(widened);
    const r = u.validate({});
    if (r.ok) expectTypeOf(r.value).toEqualTypeOf<unknown>();
    const e = createValidator<{ id: number }>({ type: "object" });
    const re = e.validate({ id: 1 });
    if (re.ok) expectTypeOf(re.value.id).toEqualTypeOf<number>();
  });
});
