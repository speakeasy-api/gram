import { useRiskDetectionDescriptor } from "@gram/client/react-query";
import { useEffect, useMemo, useState } from "react";
import { buildCelEnv, checkExpr } from "./cel-env";

const DEBOUNCE_MS = 150;

export type CelStatus =
  | { kind: "idle" }
  | { kind: "validating" }
  | { kind: "ok" }
  | {
      kind: "error";
      message: string;
      range?: { start: number; end: number };
    };

/** Live, client-side type-check for a single CEL expression. Builds a cel-js
 *  environment from the backend descriptor — the same source the Go engine
 *  compiles from — so a check here matches what the server accepts. Validation
 *  is instant (no round-trip); the server stays authoritative on the save path.
 */
export function useCelStatus(expr: string): CelStatus {
  const trimmed = expr.trim();

  // Short debounce only to coalesce rapid typing (avoids flashing errors mid
  // token); the check itself is local and instant, not a network call.
  const [debounced, setDebounced] = useState(trimmed);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(trimmed), DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [trimmed]);

  const { data: descriptor, isError } = useRiskDetectionDescriptor();
  const env = useMemo(
    () => (descriptor ? buildCelEnv(descriptor) : null),
    [descriptor],
  );
  const checked = useMemo(
    () => (env && debounced ? checkExpr(env, debounced) : null),
    [env, debounced],
  );

  if (trimmed === "") return { kind: "idle" };
  // Can't check until the descriptor loads. If it failed to load, don't block —
  // the server validates authoritatively on save.
  if (!env) return isError ? { kind: "ok" } : { kind: "validating" };
  // Settling while the debounce catches up to the latest input.
  if (debounced !== trimmed || !checked) return { kind: "validating" };

  if (!checked.valid) {
    return { kind: "error", message: checked.message, range: checked.range };
  }
  // A detection/scope expression must be a predicate — mirrors celenv.Compile's
  // OutputType == bool assertion so the editor and the save gate agree.
  if (checked.type !== "bool") {
    return {
      kind: "error",
      message: `Expression must be true or false, but it's a ${checked.type}.`,
    };
  }
  return { kind: "ok" };
}
