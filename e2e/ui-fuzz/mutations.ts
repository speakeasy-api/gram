import type { ScreenState } from "./questions.ts";

// Shape-agnostic mutators. Each rewrites one recorded API response the
// screen depends on, without knowing the endpoint: arrays are arrays and
// names are names whatever the service. The pure part lives here so it is
// unit tested; the spec wires it into `page.route`. `expected` is the set of
// states a correct UI may land in. A mutator that finds nothing to change in
// a payload returns `skip`, so the case is recorded as skipped rather than
// judged against an unmutated screen.

export type MockedResponse = {
  status: number;
  contentType: string;
  body: string;
};

export type Applied =
  | { kind: "respond"; response: MockedResponse }
  | { kind: "skip"; reason: string };

export type Mutator = {
  name: string;
  expected: ReadonlySet<ScreenState>;
  // Whether `apply` needs the real response; the spec fetches it first.
  needsOriginal: boolean;
  apply(original: string | undefined): Applied;
};

const json = (status: number, value: unknown): Applied => ({
  kind: "respond",
  response: {
    status,
    contentType: "application/json",
    body: JSON.stringify(value),
  },
});

// A name long enough to overflow any column, with no spaces so the browser
// cannot wrap it. Realistic for a slug pasted from a script.
export const LONG_NAME = "name-" + "x".repeat(4995);

// Keys whose string values a person reads as a label. Identifiers, slugs,
// URLs and enums are left alone: stretching those breaks links and routing,
// which is a different bug from a label that does not fit.
export const NAME_KEYS = new Set([
  "name",
  "title",
  "description",
  "summary",
  "label",
  "display_name",
  "displayName",
]);

type Walk = { value: unknown; changed: number };

function emptyArrays(v: unknown): Walk {
  let changed = 0;
  const walk = (x: unknown): unknown => {
    if (Array.isArray(x)) {
      changed += 1;
      return [];
    }
    if (typeof x === "object" && x !== null) {
      return Object.fromEntries(
        Object.entries(x).map(([k, y]) => [k, walk(y)]),
      );
    }
    return x;
  };
  return { value: walk(v), changed };
}

function longNames(v: unknown): Walk {
  let changed = 0;
  const walk = (x: unknown): unknown => {
    if (Array.isArray(x)) return x.map(walk);
    if (typeof x === "object" && x !== null) {
      return Object.fromEntries(
        Object.entries(x).map(([k, y]) => {
          if (NAME_KEYS.has(k) && typeof y === "string" && y !== "") {
            changed += 1;
            return [k, LONG_NAME];
          }
          return [k, walk(y)];
        }),
      );
    }
    return x;
  };
  return { value: walk(v), changed };
}

function parse(original: string | undefined): unknown {
  if (original === undefined) return undefined;
  try {
    return JSON.parse(original) as unknown;
  } catch {
    return undefined;
  }
}

function rewrite(
  name: string,
  original: string | undefined,
  fn: (v: unknown) => Walk,
): Applied {
  const parsed = parse(original);
  if (parsed === undefined)
    return { kind: "skip", reason: `${name}: response is not JSON` };
  const { value, changed } = fn(parsed);
  if (changed === 0)
    return { kind: "skip", reason: `${name}: nothing to change` };
  return json(200, value);
}

// Goa's error envelope, so the SDK decodes it the way it decodes a real one.
const goaError = (name: string, message: string) => ({
  name,
  id: "ui-fuzz",
  message,
  temporary: false,
  timeout: false,
  fault: name === "unexpected",
});

export const mutators: Mutator[] = [
  {
    name: "empty-arrays",
    // A page whose lists are all empty is an empty state; a page that also
    // shows scalar data is still loaded. Both are correct.
    expected: new Set(["empty", "loaded"]),
    needsOriginal: true,
    apply: (o) => rewrite("empty-arrays", o, emptyArrays),
  },
  {
    name: "long-names",
    expected: new Set(["loaded", "empty"]),
    needsOriginal: true,
    apply: (o) => rewrite("long-names", o, longNames),
  },
  {
    name: "server-error",
    expected: new Set(["error"]),
    needsOriginal: false,
    apply: () => json(500, goaError("unexpected", "internal server error")),
  },
  {
    name: "forbidden",
    expected: new Set(["error"]),
    needsOriginal: false,
    apply: () =>
      json(
        403,
        goaError("forbidden", "you do not have access to this resource"),
      ),
  },
];

// The unmutated visit. Not a mutator: it runs once per route rather than
// once per dependency, and its records are the calibration set.
export const BASELINE_EXPECTED: ReadonlySet<ScreenState> = new Set([
  "loaded",
  "empty",
]);
