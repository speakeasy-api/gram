import { Op } from "@gram/client/models/components/attributefilter";

export type { Op };

export interface ActiveAttributeFilter {
  id: string;
  path: string;
  op: Op;
  value?: string;
}

export const OP_LABELS: Record<Op, string> = {
  eq: "=",
  not_eq: "!=",
  contains: "~",
  exists: "exists",
  not_exists: "∄",
};

// Ordered longest-first so `!=` is checked before `=`.
const SYMBOL_TO_OP: [string, Op][] = [
  ["!=", Op.NotEq],
  ["=", Op.Eq],
  ["~", Op.Contains],
];

/** Match an operator symbol (e.g. `!=`, `=`, `~`) to its Op enum value. */
export function parseOperatorSymbol(input: string): Op | null {
  for (const [symbol, op] of SYMBOL_TO_OP) {
    if (input === symbol) return op;
  }
  return null;
}

/**
 * Try to parse a freeform filter expression like `http.status != 200`.
 * Returns an `{ key, op, value }` triple on success, or `null` when the input
 * doesn't look like a filter expression (so the caller can fall through to
 * plain-text search).
 */
export function tryParseFilterExpression(
  input: string,
): { key: string; op: Op; value: string } | null {
  let best: { key: string; op: Op; value: string; idx: number } | null = null;
  for (const [symbol, op] of SYMBOL_TO_OP) {
    const idx = input.indexOf(symbol);
    if (idx === -1) continue;

    const key = input.slice(0, idx).trim();
    const value = input.slice(idx + symbol.length).trim();
    if (!key || !value) continue;

    // Pick the earliest match. At the same position, the iteration order
    // already prefers longer symbols (`!=` before `=`).
    if (!best || idx < best.idx) {
      best = { key, op, value, idx };
    }
  }
  return best ? { key: best.key, op: best.op, value: best.value } : null;
}
