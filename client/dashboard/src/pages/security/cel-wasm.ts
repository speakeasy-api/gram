// Loads the risk CEL engine compiled to WebAssembly (the same celenv the server
// runs). The asset is built by `mise gen:celwasm` into public/cel/; its
// wasm_exec.js shim defines globalThis.Go, and go.run() sets the __cel* globals
// this module wraps. Mirrors the celenv Go types.

interface CelVarRef {
  name: string;
  type: string;
  description: string;
  matchable: boolean;
  fields?: CelVarRef[];
}

interface CelFuncRef {
  name: string;
  signature: string;
  description: string;
  returnsField: boolean;
}

interface CelMacroRef {
  name: string;
  signature: string;
  description: string;
  returnsBool: boolean;
}

export interface CelReferenceData {
  variables: CelVarRef[];
  matchers: CelFuncRef[];
  macros: CelMacroRef[];
}

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

export interface CelSpan {
  Target: string;
  ToolCallID: string;
  Path: string;
  Start: number;
  End: number;
  Value: string;
}

type CelCompileResult = { ok: true } | { ok: false; error: string };
type CelEvalResult =
  | { ok: true; matched: boolean; spans: CelSpan[] }
  | { ok: false; error: string };

export interface CelCompletionItem {
  label: string;
  detail: string;
  doc: string;
  category: "matcher" | "macro" | "globalMacro" | "field" | "variable" | "bind";
}

interface CelCompletion {
  context: "member" | "name";
  items: CelCompletionItem[];
}

// The loaded engine: the same compile/complete/eval the server runs, in-browser.
export interface CelEngine {
  reference: CelReferenceData;
  compile(expr: string): CelCompileResult;
  complete(srcUpToCursor: string): CelCompletion;
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

// wasm_exec.js is a classic script (defines globalThis.Go), so it's loaded via a
// tag rather than a static ESM import.
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
    // Clear the cache on failure so a later call can retry a transient error.
    runtimePromise.catch(() => {
      runtimePromise = null;
    });
  }
  return runtimePromise;
}

async function instantiate(
  go: InstanceType<NonNullable<CelGlobals["Go"]>>,
): Promise<WebAssembly.Instance> {
  // Prefer streaming; fall back to arrayBuffer for hosts that don't serve wasm
  // with the application/wasm content type. Fetch once and clone so the fallback
  // reuses the response rather than issuing a second request.
  const response = await fetch(WASM_URL);
  try {
    const res = await WebAssembly.instantiateStreaming(
      response.clone(),
      go.importObject,
    );
    return res.instance;
  } catch {
    const bytes = await response.arrayBuffer();
    const res = await WebAssembly.instantiate(bytes, go.importObject);
    return res.instance;
  }
}

// Go main installs the exported funcs synchronously before parking on select{},
// so we don't await go.run (it never resolves).
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

// Loaded at most once per session (the wasm asset is large).
let enginePromise: Promise<CelEngine> | null = null;

// Rejects if the asset is missing or the runtime fails; callers then fall back
// to server-side validation. The cache is cleared on failure so a later call can
// retry a transient error.
export function loadCelEngine(): Promise<CelEngine> {
  if (!enginePromise) {
    enginePromise = start();
    enginePromise.catch(() => {
      enginePromise = null;
    });
  }
  return enginePromise;
}
