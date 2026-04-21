import { Operator } from "@gram/client/models/components/logfilter";

export type Op = Operator;

export interface ActiveLogFilter {
  id: string;
  path: string;
  op: Op;
  value?: string;
}

export const OP_LABELS: Partial<Record<Op, string>> = {
  eq: "=",
  not_eq: "!=",
  contains: "~",
  in: "in",
};

// Ordered longest-first so `!=` is checked before `=`.
const SYMBOL_TO_OP: [string, Op][] = [
  ["!=", Operator.NotEq],
  ["=", Operator.Eq],
  ["~", Operator.Contains],
];

/** Match an operator symbol or keyword to its Op enum value. */
export function parseOperatorSymbol(input: string): Op | null {
  if (input === "in") return Operator.In;
  for (const [symbol, op] of SYMBOL_TO_OP) {
    if (input === symbol) return op;
  }
  return null;
}

/**
 * Add a filter to a list, applying dedup rules:
 * - For eq/in: replaces any existing filter on the same path+op
 * - For not_eq/contains: appends (stacking is valid)
 */
export function applyFilterAdd(
  current: ActiveLogFilter[],
  next: { path: string; op: Op; value?: string },
): ActiveLogFilter[] {
  const rest =
    next.op === Operator.Eq || next.op === Operator.In
      ? current.filter((f) => !(f.path === next.path && f.op === next.op))
      : current;
  return [
    ...rest,
    {
      id: crypto.randomUUID(),
      path: next.path,
      op: next.op,
      value: next.value,
    },
  ];
}

/**
 * Try to parse a freeform filter expression like `http.response.status_code != 200`.
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

    // Skip `=` when it's actually part of a `!=` token to avoid phantom
    // matches like key="!" from input "!= 200".
    if (symbol === "=" && idx > 0 && input[idx - 1] === "!") continue;

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
