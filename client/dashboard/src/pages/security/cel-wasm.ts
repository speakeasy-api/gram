// Loads the risk CEL engine compiled to WebAssembly. It is the exact same
// celenv the server compiles and evaluates with, so the editor type-checks
// expressions and previews matched spans against real engine semantics — no
// round-trip, no drift from save-time validation.
//
// The asset is produced by `mise gen:celwasm` into public/cel/ (cel.wasm +
// wasm_exec.js, the Go runtime shim from the same toolchain). The shim is a
// classic script that defines globalThis.Go; go.run() then sets the __cel*
// globals this module wraps.

/** One author-visible variable. `fields` is set only on `tool_calls`, whose
 *  elements expose name/server/function/args. `matchable` marks the field-typed
 *  variables the matcher methods apply to. */
export interface CelVarRef {
  name: string;
  type: string; // human label: "field", "string", "list(tool)"
  description: string;
  matchable: boolean;
  fields?: CelVarRef[];
}

/** One member method on a field. `returnsField` marks get(), which yields a
 *  field the matchers chain off; the rest return bool. */
export interface CelFuncRef {
  name: string;
  signature: string;
  description: string;
  returnsField: boolean;
}

/** One CEL macro. `returnsBool` marks the predicate macros that can stand as a
 *  whole rule, versus the list-producing ones (map/filter). */
export interface CelMacroRef {
  name: string;
  signature: string;
  description: string;
  returnsBool: boolean;
}

/** The editor-facing catalog (celenv.Describe()), used to drive autocomplete and
 *  the reference panel. */
export interface CelReferenceData {
  variables: CelVarRef[];
  matchers: CelFuncRef[];
  macros: CelMacroRef[];
}

/** A message to evaluate a detection expression against. Mirrors celenv.Message. */
export interface CelMessage {
  type: string;
  content: string;
  tools?: {
    name: string;
    server: string;
    function: string;
    args: string;
  }[];
}

/** One matched substring, attributed to the field it matched in. Mirrors
 *  celenv.Span. */
export interface CelSpan {
  Target: string;
  ToolCallID: string;
  Path: string;
  Start: number;
  End: number;
  Value: string;
}

export type CelCompileResult = { ok: true } | { ok: false; error: string };
export type CelEvalResult =
  | { ok: true; matched: boolean; spans: CelSpan[] }
  | { ok: false; error: string };

/** One completion suggestion. `category` lets the editor pick an icon and an
 *  insertion snippet without re-deriving what the item is. */
export interface CelCompletionItem {
  label: string;
  detail: string;
  doc: string;
  category: "matcher" | "macro" | "globalMacro" | "field" | "variable" | "bind";
}

/** The engine's answer for a cursor position: whether we're after a dot
 *  ("member") or at a name, and the items valid there. */
export interface CelCompletion {
  context: "member" | "name";
  items: CelCompletionItem[];
}

/** The loaded engine: the same compile/eval the server runs, callable in-browser. */
export interface CelEngine {
  reference: CelReferenceData;
  /** Type-check an expression; mirrors the server's save-time gate exactly. */
  compile(expr: string): CelCompileResult;
  /** Type-directed completion for the text up to the cursor. The receiver's real
   *  CEL type decides the offering, so the editor needs no completion heuristics. */
  complete(srcUpToCursor: string): CelCompletion;
  /** Evaluate a detection predicate against a message, returning matched spans. */
  evalDetection(expr: string, message: CelMessage): CelEvalResult;
}

// Raw shapes of the wasm globals (set by server/cmd/celwasm).
interface CelGlobals {
  Go?: new () => {
    importObject: WebAssembly.Imports;
    run(instance: WebAssembly.Instance): Promise<void>;
  };
  __celEngineReady?: boolean;
  __celInitError?: string;
  __celReference?: () => string;
  __celComplete?: (src: string) => string;
  __celCompile?: (expr: string) => CelCompileResult;
  __celEval?: (
    expr: string,
    messageJSON: string,
  ) => { ok: boolean; matched?: boolean; spans?: string; error?: string };
}

const WASM_URL = "/cel/cel.wasm";
const RUNTIME_URL = "/cel/wasm_exec.js";

function globals(): CelGlobals {
  return globalThis as unknown as CelGlobals;
}

// loadGoRuntime injects wasm_exec.js once. It's a classic script (defines
// globalThis.Go), so it can't be a static ESM import; a tag load is the simplest
// way to evaluate it without a bundler shim.
let runtimePromise: Promise<void> | null = null;
function loadGoRuntime(): Promise<void> {
  if (globals().Go) return Promise.resolve();
  if (!runtimePromise) {
    runtimePromise = new Promise<void>((resolve, reject) => {
      const script = document.createElement("script");
      script.src = RUNTIME_URL;
      script.onload = () => resolve();
      script.onerror = () =>
        reject(new Error("failed to load the CEL wasm runtime"));
      document.head.appendChild(script);
    });
  }
  return runtimePromise;
}

async function instantiate(
  go: InstanceType<NonNullable<CelGlobals["Go"]>>,
): Promise<WebAssembly.Instance> {
  // Prefer streaming; fall back to arrayBuffer for hosts that don't serve wasm
  // with the application/wasm content type streaming requires.
  try {
    const res = await WebAssembly.instantiateStreaming(
      fetch(WASM_URL),
      go.importObject,
    );
    return res.instance;
  } catch {
    const bytes = await fetch(WASM_URL).then((r) => r.arrayBuffer());
    const res = await WebAssembly.instantiate(bytes, go.importObject);
    return res.instance;
  }
}

// start loads the runtime and module, then reads the globals the Go main sets
// synchronously before it parks on select{}. We deliberately do NOT await
// go.run — with select{} it never resolves; its synchronous prologue has already
// installed the exported funcs by the time run() returns control to the loop.
async function start(): Promise<CelEngine> {
  await loadGoRuntime();
  const Go = globals().Go;
  if (!Go) throw new Error("CEL wasm runtime did not load");

  const go = new Go();
  const instance = await instantiate(go);
  void go.run(instance);

  const g = globals();
  if (g.__celInitError) {
    throw new Error(`CEL engine failed to initialize: ${g.__celInitError}`);
  }
  if (
    !g.__celEngineReady ||
    !g.__celReference ||
    !g.__celComplete ||
    !g.__celCompile ||
    !g.__celEval
  ) {
    throw new Error("CEL engine did not expose its interface");
  }

  const reference = JSON.parse(g.__celReference()) as CelReferenceData;
  const compile = g.__celCompile;
  const rawComplete = g.__celComplete;
  const rawEval = g.__celEval;

  return {
    reference,
    compile: (expr) => compile(expr),
    complete: (src) => JSON.parse(rawComplete(src)) as CelCompletion,
    evalDetection: (expr, message) => {
      const r = rawEval(expr, JSON.stringify(message));
      if (!r.ok) return { ok: false, error: r.error ?? "evaluation failed" };
      return {
        ok: true,
        matched: r.matched ?? false,
        spans: r.spans ? (JSON.parse(r.spans) as CelSpan[]) : [],
      };
    },
  };
}

// Single in-flight load shared across every editor instance; the wasm asset is
// large, so it must load at most once per session.
let enginePromise: Promise<CelEngine> | null = null;

/** Load the CEL engine, reusing the in-flight/resolved instance. Rejects if the
 *  asset is missing or the runtime fails; callers fall back to server-side
 *  validation (risk.compileExpr) in that case. */
export function loadCelEngine(): Promise<CelEngine> {
  if (!enginePromise) enginePromise = start();
  return enginePromise;
}
