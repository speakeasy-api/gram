import { useEffect, useMemo, useState } from "react";
import { useCelEngine } from "./use-cel-engine";

const DEBOUNCE_MS = 150;

export type CelStatus =
  | { kind: "idle" }
  | { kind: "validating" }
  | { kind: "ok" }
  | { kind: "unavailable" } // engine failed to load; server validates on save
  | { kind: "error"; message: string };

/** Live, client-side type-check for a single CEL expression via the wasm engine
 *  (the same celenv the server compiles with). The server stays authoritative on
 *  save; if the engine can't load, status is "unavailable" — never a false ok. */
export function useCelStatus(expr: string): CelStatus {
  const engine = useCelEngine();
  const trimmed = expr.trim();

  // Short debounce to coalesce rapid typing; the check itself is local/instant.
  const [debounced, setDebounced] = useState(trimmed);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(trimmed), DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [trimmed]);

  const result = useMemo(() => {
    if (engine.status !== "ready" || !debounced) return null;
    // compile asserts a bool predicate, so its error already covers "must be
    // true or false" — no separate type check needed.
    return engine.engine.compile(debounced);
  }, [engine, debounced]);

  if (trimmed === "") return { kind: "idle" };
  if (engine.status === "error") return { kind: "unavailable" };
  if (engine.status === "loading") return { kind: "validating" };
  if (debounced !== trimmed || !result) return { kind: "validating" };

  if (!result.ok) return { kind: "error", message: result.error };
  return { kind: "ok" };
}
