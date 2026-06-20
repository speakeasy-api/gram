import { useRiskCompileCel } from "@gram/client/react-query";
import { useEffect, useState } from "react";

const DEBOUNCE_MS = 300;

export type CelStatus =
  | { kind: "idle" }
  | { kind: "validating" }
  | { kind: "ok" }
  | { kind: "error"; message: string };

/** Debounced, backend-authoritative compile check for a single CEL expression.
 *  Mirrors the rule/policy save gate (risk.compileCel -> celenv.Compile), so an
 *  expression that reads "Compiles" here also saves. Building a CEL parser in
 *  the browser would drift from the engine, so the backend stays authoritative. */
export function useCelStatus(expr: string): CelStatus {
  const trimmed = expr.trim();

  // Debounce so we don't fire an RPC on every keystroke mid-expression.
  const [debounced, setDebounced] = useState(trimmed);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(trimmed), DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [trimmed]);

  const { data, isFetching } = useRiskCompileCel(
    { cel: debounced },
    undefined,
    {
      enabled: debounced !== "",
    },
  );

  if (trimmed === "") return { kind: "idle" };
  // Settling: the debounce hasn't caught up, or the query is in flight.
  if (debounced !== trimmed || isFetching || !data) {
    return { kind: "validating" };
  }
  if (data.ok) return { kind: "ok" };
  return { kind: "error", message: data.error };
}
