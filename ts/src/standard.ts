/**
 * Standard Schema and Standard JSON Schema interfaces.
 *
 * Why they are copied here instead of imported: the specs are deliberately
 * dependency-free — every library re-declares the ~60-line interface so
 * consumers (tRPC, TanStack Form, React Hook Form, Hono) can type against a
 * structural contract with no shared package. Keeping our copy verbatim to
 * the spec is what makes `@ubgo/validator` drop into those adapters
 * (docsi/MISSION.md ADR-VAL-002).
 *
 * Spec: https://standardschema.dev — `StandardSchemaV1`; and the Standard
 * JSON Schema extension `StandardJSONSchemaV1` for `~standard.jsonSchema`.
 */

/** The Standard Schema interface (v1). */
export interface StandardSchemaV1<Input = unknown, Output = Input> {
  readonly "~standard": StandardSchemaV1.Props<Input, Output>;
}

export declare namespace StandardSchemaV1 {
  /** The Standard Schema properties interface. */
  export interface Props<Input = unknown, Output = Input> {
    /** The version number of the standard. */
    readonly version: 1;
    /** The vendor name of the schema library. */
    readonly vendor: string;
    /** Validates unknown input values. */
    readonly validate: (
      value: unknown,
    ) => Result<Output> | Promise<Result<Output>>;
    /** Inferred types associated with the schema. */
    readonly types?: Types<Input, Output> | undefined;
    /** Standard JSON Schema extension: the schema as JSON Schema. */
    readonly jsonSchema?: JSONSchemaProps | undefined;
  }

  /** The result interface of the validate function. */
  export type Result<Output> = SuccessResult<Output> | FailureResult;

  /** The result interface if validation succeeds. */
  export interface SuccessResult<Output> {
    /** The typed output value. */
    readonly value: Output;
    /** The non-existent issues. */
    readonly issues?: undefined;
  }

  /** The result interface if validation fails. */
  export interface FailureResult {
    /** The issues of failed validation. */
    readonly issues: ReadonlyArray<Issue>;
  }

  /** The issue interface of the failure output. */
  export interface Issue {
    /** The error message of the issue. */
    readonly message: string;
    /** The path of the issue, if any. */
    readonly path?: ReadonlyArray<PropertyKey | PathSegment> | undefined;
  }

  /** The path segment interface of the issue. */
  export interface PathSegment {
    /** The key representing a path segment. */
    readonly key: PropertyKey;
  }

  /** The Standard Schema types interface. */
  export interface Types<Input = unknown, Output = Input> {
    /** The input type of the schema. */
    readonly input: Input;
    /** The output type of the schema. */
    readonly output: Output;
  }

  /** Infers the input type of a Standard Schema. */
  export type InferInput<Schema extends StandardSchemaV1> = NonNullable<
    Schema["~standard"]["types"]
  >["input"];

  /** Infers the output type of a Standard Schema. */
  export type InferOutput<Schema extends StandardSchemaV1> = NonNullable<
    Schema["~standard"]["types"]
  >["output"];

  /** JSON Schema dialects a consumer may request. */
  export type JSONSchemaTarget = "draft-2020-12" | "draft-07" | "openapi-3.0";

  /** Options for the JSON Schema conversion methods. */
  export interface JSONSchemaOptions {
    /** The target dialect. */
    readonly target: JSONSchemaTarget;
  }

  /** The Standard JSON Schema properties interface. */
  export interface JSONSchemaProps {
    /** Converts the schema's input type to JSON Schema. */
    readonly input: (options: JSONSchemaOptions) => Record<string, unknown>;
    /** Converts the schema's output type to JSON Schema. */
    readonly output: (options: JSONSchemaOptions) => Record<string, unknown>;
  }
}
