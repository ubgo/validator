/**
 * The portability manifest, mirrored from Go's `jsonschema.Manifest`.
 *
 * The manifest is the runtime's contract with honesty (ADR-VAL-004): every
 * rule the JSON Schema could not carry arrives here with a class that says
 * what the browser must do about it. The runtime never ignores an entry —
 * it stubs and warns, round-trips to the server, or reports it — so the
 * frontend can never quietly accept a value the backend will reject.
 */

/** Why an element is absent from the JSON Schema. Mirrors Go's `Class`. */
export type PortabilityClass =
  /** A custom Go function: runs server-side only. */
  | "opaque"
  /** An async rule with backend state: the frontend round-trips. */
  | "server"
  /** A custom function the author ships as WASM (not yet supported here). */
  | "wasm"
  /** Needs CEL to cross; invisible to plain JSON Schema. */
  | "cel"
  /** The schema approximates or omits it; applied server-side. */
  | "lossy";

/** What sort of IR element an entry describes. Mirrors Go's `Kind`. */
export type ManifestKind = "rule" | "condition" | "transform" | "gate";

/** One element the schema does not carry. Mirrors Go's `Entry`. */
export interface ManifestEntry {
  /** JSONPath of the value the element applies to: `$.email`. */
  readonly path: string;
  readonly kind: ManifestKind;
  /** Rule/condition/transform/gate name: `custom`, `async`, `after`, `trim`. */
  readonly name: string;
  readonly class: PortabilityClass;
  /** Registered function name for custom/async elements. */
  readonly ref?: string;
  /** Round-trip URL for server rules, when declared. */
  readonly endpoint?: string;
  /** One-line human explanation. */
  readonly reason: string;
  /**
   * The executable form of an element JSON Schema could not express: the
   * condition tree in wire names, the branch fragments, and the code and
   * message Go reports. See `conditions.ts`; absent when the element can
   * only run on the server.
   */
  readonly cond?: unknown;
  readonly then?: Readonly<Record<string, unknown>>;
  readonly else?: Readonly<Record<string, unknown>>;
  readonly code?: string;
  readonly message?: string;
}

/** The per-schema manifest. Mirrors Go's `Manifest`. */
export interface Manifest {
  readonly entries: ReadonlyArray<ManifestEntry>;
}

/** The closed set of classes, for runtime validation of a loaded manifest. */
export const PORTABILITY_CLASSES: ReadonlySet<PortabilityClass> = new Set<PortabilityClass>([
  "opaque",
  "server",
  "wasm",
  "cel",
  "lossy",
]);

/**
 * Converts a manifest JSONPath (`$.tags[0].name`) into the path segments
 * Standard Schema issues use (`["tags", 0, "name"]`). Wildcards emitted for
 * array items (`[*]`) and map values (`.*`) are kept as the literal `*`.
 */
export function jsonPathToSegments(path: string): PropertyKey[] {
  const out: PropertyKey[] = [];
  let rest = path.startsWith("$") ? path.slice(1) : path;
  while (rest.length > 0) {
    if (rest.startsWith(".")) {
      rest = rest.slice(1);
      const end = nextBoundary(rest);
      out.push(rest.slice(0, end));
      rest = rest.slice(end);
    } else if (rest.startsWith("[")) {
      const close = rest.indexOf("]");
      const raw = close < 0 ? rest.slice(1) : rest.slice(1, close);
      out.push(/^\d+$/.test(raw) ? Number(raw) : raw);
      rest = close < 0 ? "" : rest.slice(close + 1);
    } else {
      // A bare segment (defensive: never produced by the emitter).
      const end = nextBoundary(rest);
      out.push(rest.slice(0, end));
      rest = rest.slice(end);
    }
  }
  return out;
}

/** Index of the next `.` or `[`, or the end of the string. */
function nextBoundary(s: string): number {
  const dot = s.indexOf(".");
  const bracket = s.indexOf("[");
  if (dot < 0 && bracket < 0) return s.length;
  if (dot < 0) return bracket;
  if (bracket < 0) return dot;
  return Math.min(dot, bracket);
}

/**
 * Converts Ajv's `instancePath` (a JSON Pointer such as `/tags/0/name`) into
 * path segments, unescaping `~1` and `~0` per RFC 6901 and turning numeric
 * segments into numbers.
 */
export function pointerToSegments(pointer: string): PropertyKey[] {
  if (pointer === "") return [];
  return pointer
    .split("/")
    .slice(1)
    .map((seg) => {
      const unescaped = seg.replaceAll("~1", "/").replaceAll("~0", "~");
      return /^\d+$/.test(unescaped) ? Number(unescaped) : unescaped;
    });
}

/** Renders path segments back to the JSONPath form Go uses in `verr.Issue`. */
export function segmentsToJSONPath(segments: ReadonlyArray<PropertyKey>): string {
  let out = "$";
  for (const seg of segments) {
    out += typeof seg === "number" ? `[${seg}]` : `.${String(seg)}`;
  }
  return out;
}
