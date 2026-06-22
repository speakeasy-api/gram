import { useEffect, useMemo, useState } from "react";
import { useCelEngine } from "./use-cel-engine";

const DEBOUNCE_MS = 150;

export type CelStatus =
  | { kind: "idle" }
  | { kind: "validating" }
  | { kind: "ok" }
  | { kind: "error"; message: string };

/** Live, client-side type-check for a single CEL expression, run through the
 *  wasm engine — the same celenv the server compiles with, so a check here
 *  matches what the server accepts. Validation is instant (no round-trip); the
 *  server stays authoritative on the save path. If the engine fails to load,
 *  don't block: the server validates on save. */
export function useCelStatus(expr: string): CelStatus {
  const engine = useCelEngine();
  const trimmed = expr.trim();

  // Short debounce only to coalesce rapid typing (avoids flashing errors mid
  // token); the check itself is local and instant, not a network call.
  const [debounced, setDebounced] = useState(trimmed);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(trimmed), DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [trimmed]);

  const result = useMemo(() => {
    if (engine.status !== "ready" || !debounced) return null;
    // The engine asserts the expression is a bool predicate (celenv.Compile's
    // OutputType == bool check), so an error here already covers "must be true
    // or false" — no separate type assertion needed.
    return engine.engine.compile(debounced);
  }, [engine, debounced]);

  if (trimmed === "") return { kind: "idle" };
  // Can't check until the engine loads. If it failed, don't block the author —
  // the server validates authoritatively on save.
  if (engine.status === "error") return { kind: "ok" };
  if (engine.status === "loading") return { kind: "validating" };
  // Settling while the debounce catches up to the latest input.
  if (debounced !== trimmed || !result) return { kind: "validating" };

  if (!result.ok) return { kind: "error", message: result.error };
  return { kind: "ok" };
}
