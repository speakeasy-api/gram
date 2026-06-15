import type { RiskMatchCondition } from "@gram/client/models/components";
import type { MatchCombine } from "./detection-rules-data";

type MatchTarget = RiskMatchCondition["target"];

/**
 * A Datadog-style query language for a rule's match_config. The operator is
 * inferred from the value syntax (no operator words):
 *
 *   field:value          equals            field:*value*   contains
 *   -field:value         not equals        field:value*    starts with
 *   field:/regex/        regex             field:*value    ends with
 *   field:(a OR b)       any of (in)       field:*         field present
 *   field:(*a* OR *b*)   contains any      -field:*value*  not contains
 *
 * Conditions join with a single AND or OR (the backend stores a flat list — NOT
 * and grouping parens are rejected; value unions still use balanced ( )).
 */

export type QueryCategory = "prompt" | "tool";

export type QueryTarget = {
  /** Field name as typed in the query, e.g. "tool_call.name". */
  name: string;
  backend: MatchTarget;
  category: QueryCategory;
  description: string;
  /** tool_call.args accepts an optional trailing JSON path. */
  hasPath?: boolean;
};

export const QUERY_TARGETS: QueryTarget[] = [
  {
    name: "user_prompt",
    backend: "user_prompt",
    category: "prompt",
    description: "The user's prompt text",
  },
  {
    name: "assistant_message",
    backend: "assistant_text",
    category: "prompt",
    description: "The assistant's reply text",
  },
  {
    name: "content",
    backend: "content",
    category: "prompt",
    description: "Whole message text (any type)",
  },
  {
    name: "tool_call.name",
    backend: "tool_name",
    category: "tool",
    description: "Raw tool-call name, e.g. mcp__mise__run_task",
  },
  {
    name: "tool_call.server",
    backend: "tool_server",
    category: "tool",
    description: "MCP server name; empty for native tools",
  },
  {
    name: "tool_call.function",
    backend: "tool_function",
    category: "tool",
    description: "Bare function name, e.g. run_task",
  },
  {
    name: "tool_call.args",
    backend: "tool_args",
    category: "tool",
    description: "Tool arguments; add .$.path for a field",
    hasPath: true,
  },
  {
    name: "tool_response",
    backend: "tool_result",
    category: "tool",
    description: "A tool call's result output",
  },
];

const BACKEND_TO_TARGET = new Map<MatchTarget, QueryTarget>(
  QUERY_TARGETS.map((t) => [t.backend, t]),
);

/** Worked examples shown under the editor as the syntax affordance. */
export const MATCH_QUERY_EXAMPLES: { query: string; meaning: string }[] = [
  { query: "content:*ssn*", meaning: "content contains “ssn”" },
  { query: "tool_call.name:bash", meaning: "the tool is exactly bash" },
  {
    query: '-tool_call.server:""',
    meaning: "an MCP tool (server is not empty)",
  },
  {
    query: "tool_call.args:(*rm* OR *curl*)",
    meaning: "arguments contain rm or curl",
  },
  { query: "content:/secret-\\d+/", meaning: "content matches a regex" },
  { query: "tool_call.name:bash AND content:sudo", meaning: "both must match" },
];

export type ParsedQuery = {
  conditions: RiskMatchCondition[];
  combine: MatchCombine;
  error: string | null;
};

export function parseMatchQuery(input: string): ParsedQuery {
  const trimmed = input.trim();
  if (!trimmed) {
    return {
      conditions: [],
      combine: "and",
      error: "Add at least one condition",
    };
  }
  if (/\bNOT\b/i.test(trimmed)) {
    return {
      conditions: [],
      combine: "and",
      error: "NOT isn't supported yet — use a single AND or OR",
    };
  }

  const split = splitClauses(trimmed);
  if (split.error) {
    return { conditions: [], combine: split.combine, error: split.error };
  }

  const conditions: RiskMatchCondition[] = [];
  for (const clause of split.clauses) {
    const parsed = parseClause(clause);
    if (typeof parsed === "string") {
      return { conditions: [], combine: split.combine, error: parsed };
    }
    conditions.push(parsed);
  }
  return { conditions, combine: split.combine, error: null };
}

export function matchQueryFromConditions(
  conditions: RiskMatchCondition[],
  combine: MatchCombine,
): string {
  return conditions
    .map(serializeClause)
    .join(combine === "or" ? " OR " : " AND ");
}

function fieldName(c: RiskMatchCondition): string {
  const base = BACKEND_TO_TARGET.get(c.target)?.name ?? c.target;
  if (c.target === "tool_args" && c.path) return `${base}.${c.path}`;
  return base;
}

function operandList(c: RiskMatchCondition): string[] {
  const values = (c.values ?? []).filter((v) => v.trim());
  if (values.length > 0) return values;
  return c.value ? [c.value] : [];
}

export function serializeClause(c: RiskMatchCondition): string {
  const field = fieldName(c);
  switch (c.op) {
    case "exists":
      return `${field}:*`;
    case "regex":
      return `${field}:/${c.value ?? ""}/`;
    case "equals":
      return `${field}:${quoteValue(c.value ?? "")}`;
    case "not_equals":
      return `-${field}:${quoteValue(c.value ?? "")}`;
    case "starts_with":
      return `${field}:${quoteValue(c.value ?? "")}*`;
    case "ends_with":
      return `${field}:*${quoteValue(c.value ?? "")}`;
    case "contains":
    case "not_contains": {
      const prefix = c.op === "not_contains" ? "-" : "";
      const vals = operandList(c);
      const body =
        vals.length === 1
          ? `*${vals[0]}*`
          : `(${vals.map((v) => `*${v}*`).join(" OR ")})`;
      return `${prefix}${field}:${body}`;
    }
    case "in":
    case "keyword": {
      const vals = operandList(c);
      return `${field}:(${vals.join(" OR ")})`;
    }
    case "glob":
      return `${field}:${c.value ?? ""}`;
  }
}

function quoteValue(v: string): string {
  return v === "" || /\s/.test(v) ? `"${v}"` : v;
}

function splitClauses(input: string): {
  clauses: string[];
  combine: MatchCombine;
  error: string | null;
} {
  const clauses: string[] = [];
  const connectors: ("and" | "or")[] = [];
  let buf = "";
  let inQuotes = false;
  let depth = 0;
  let i = 0;
  while (i < input.length) {
    const ch = input.charAt(i);
    if (ch === '"') {
      inQuotes = !inQuotes;
      buf += ch;
      i++;
      continue;
    }
    if (!inQuotes && ch === "(") depth++;
    if (!inQuotes && ch === ")") depth--;
    if (!inQuotes && depth === 0 && /\s/.test(ch)) {
      const conn = connectorAt(input, i + 1);
      if (conn) {
        clauses.push(buf.trim());
        connectors.push(conn.connector);
        buf = "";
        i = conn.end;
        continue;
      }
    }
    buf += ch;
    i++;
  }
  clauses.push(buf.trim());

  if (clauses.some((c) => c === "")) {
    return { clauses: [], combine: "and", error: "Empty clause around AND/OR" };
  }
  if (new Set(connectors).size > 1) {
    return {
      clauses: [],
      combine: "and",
      error: "Use only AND or only OR (mixing isn't supported yet)",
    };
  }
  return {
    clauses,
    combine: connectors[0] === "or" ? "or" : "and",
    error: null,
  };
}

function connectorAt(
  s: string,
  i: number,
): { connector: "and" | "or"; end: number } | null {
  const m = /^(and|or)(\s+|$)/i.exec(s.slice(i));
  if (!m?.[1]) return null;
  return {
    connector: m[1].toLowerCase() as "and" | "or",
    end: i + m[0].length,
  };
}

function parseClause(clause: string): RiskMatchCondition | string {
  let s = clause.trim();
  let negate = false;
  if (s.startsWith("-")) {
    negate = true;
    s = s.slice(1);
  }
  const colon = s.indexOf(":");
  if (colon === -1) return `Use field:value in "${clause.trim()}"`;
  const resolved = resolveField(s.slice(0, colon));
  if (!resolved) return `Unknown field "${s.slice(0, colon)}"`;

  const val = s.slice(colon + 1).trim();
  return makeCondition(resolved, val, negate);
}

function resolveField(
  raw: string,
): { target: QueryTarget; path: string } | null {
  let best: QueryTarget | null = null;
  for (const t of QUERY_TARGETS) {
    if (raw === t.name || (t.hasPath && raw.startsWith(`${t.name}.`))) {
      if (!best || t.name.length > best.name.length) best = t;
    }
  }
  if (!best) return null;
  const path =
    best.hasPath && raw.length > best.name.length
      ? raw.slice(best.name.length + 1)
      : "";
  return { target: best, path };
}

function makeCondition(
  resolved: { target: QueryTarget; path: string },
  rawVal: string,
  negate: boolean,
): RiskMatchCondition | string {
  const base: RiskMatchCondition = {
    target: resolved.target.backend,
    op: "equals",
  };
  if (resolved.path) base.path = resolved.path;

  // field:*  → exists
  if (rawVal === "*") {
    if (negate) return "Use -field:value to negate, not -field:*";
    return { ...base, op: "exists" };
  }
  // field:/regex/
  if (rawVal.length >= 2 && rawVal.startsWith("/") && rawVal.endsWith("/")) {
    if (negate) return "Negation isn't supported with /regex/";
    return { ...base, op: "regex", value: rawVal.slice(1, -1) };
  }
  // field:(a OR b)  → union
  if (rawVal.startsWith("(") && rawVal.endsWith(")")) {
    const terms = parseUnionTerms(rawVal);
    if (terms.every((t) => isWrappedStar(t))) {
      return {
        ...base,
        op: negate ? "not_contains" : "contains",
        values: terms.map(stripStars),
      };
    }
    if (negate) return "Negation isn't supported with (a OR b)";
    return { ...base, op: "in", values: terms.map(unquote) };
  }
  // single value with wildcards
  if (isWrappedStar(rawVal)) {
    return {
      ...base,
      op: negate ? "not_contains" : "contains",
      value: stripStars(rawVal),
    };
  }
  if (rawVal.endsWith("*") && !rawVal.startsWith("*")) {
    if (negate) return "Negation isn't supported with starts-with (value*)";
    return { ...base, op: "starts_with", value: unquote(rawVal.slice(0, -1)) };
  }
  if (rawVal.startsWith("*") && !rawVal.endsWith("*")) {
    if (negate) return "Negation isn't supported with ends-with (*value)";
    return { ...base, op: "ends_with", value: unquote(rawVal.slice(1)) };
  }
  return {
    ...base,
    op: negate ? "not_equals" : "equals",
    value: unquote(rawVal),
  };
}

function parseUnionTerms(raw: string): string[] {
  let s = raw.trim();
  if (s.startsWith("(") && s.endsWith(")")) s = s.slice(1, -1);
  return s
    .split(/\s+OR\s+|,/i)
    .map((v) => v.trim())
    .filter(Boolean);
}

function isWrappedStar(v: string): boolean {
  return v.length >= 2 && v.startsWith("*") && v.endsWith("*");
}

function stripStars(v: string): string {
  return unquote(v.replace(/^\*+/, "").replace(/\*+$/, ""));
}

function unquote(s: string): string {
  if (s.length >= 2 && s.startsWith('"') && s.endsWith('"'))
    return s.slice(1, -1);
  return s;
}

/* -------------------------------------------------------------------------- */
/*  Autocomplete                                                              */
/* -------------------------------------------------------------------------- */

export type QuerySuggestion = {
  label: string;
  description: string;
  /** right-aligned group label (Datadog facet style). */
  group?: string;
  /** Text inserted in place of the current partial token. */
  insert: string;
  /** Caret position relative to the inserted text (defaults to its end). */
  caretOffset?: number;
};

export function matchQuerySuggestions(
  input: string,
  caret: number,
): { from: number; suggestions: QuerySuggestion[] } {
  const before = input.slice(0, caret);
  const clauseStart = lastConnectorEnd(before);
  const clause = before.slice(clauseStart);
  const colon = clause.indexOf(":");

  // No colon yet → typing the field.
  if (colon === -1) {
    const dash = clause.startsWith("-") ? 1 : 0;
    return {
      from: clauseStart + dash,
      suggestions: targetSuggestions(
        clause.slice(dash),
        clause.startsWith("-"),
      ),
    };
  }

  const valuePart = clause.slice(colon + 1);
  const ws = lastTopLevelSpace(valuePart);
  if (ws === -1) {
    // Still inside the value (no top-level space after the colon).
    return {
      from: clauseStart + colon + 1,
      suggestions: valueSuggestions(valuePart),
    };
  }
  // Past the value (a top-level space) → chain with AND/OR.
  const connectorPartial = valuePart.slice(ws + 1);
  return {
    from: clauseStart + colon + 1 + ws + 1,
    suggestions: connectorSuggestions(connectorPartial),
  };
}

/** Index of the last whitespace at quote/paren depth 0, or -1. Spaces inside a
 *  quoted value or a (a OR b) union don't separate the value from a connector. */
function lastTopLevelSpace(s: string): number {
  let inQuotes = false;
  let depth = 0;
  let last = -1;
  for (let i = 0; i < s.length; i++) {
    const ch = s.charAt(i);
    if (ch === '"') inQuotes = !inQuotes;
    else if (!inQuotes && ch === "(") depth++;
    else if (!inQuotes && ch === ")") depth--;
    else if (!inQuotes && depth === 0 && /\s/.test(ch)) last = i;
  }
  return last;
}

function connectorSuggestions(partial: string): QuerySuggestion[] {
  const lower = partial.toLowerCase();
  return [
    {
      label: "AND",
      description: "all conditions must match",
      group: "Chain",
      insert: "AND ",
    },
    {
      label: "OR",
      description: "any condition may match",
      group: "Chain",
      insert: "OR ",
    },
  ].filter((s) => s.label.toLowerCase().startsWith(lower));
}

function targetSuggestions(
  fieldPart: string,
  negated: boolean,
): QuerySuggestion[] {
  const lower = fieldPart.toLowerCase();
  const negPrefix = negated ? "-" : "";
  return QUERY_TARGETS.filter((t) => t.name.startsWith(lower)).map((t) => ({
    label: `${negPrefix}${t.name}`,
    description: t.description,
    group: t.category === "tool" ? "Tool" : "Prompt",
    insert:
      t.hasPath && t.name === lower
        ? `${negPrefix}${t.name}.`
        : `${negPrefix}${t.name}:`,
  }));
}

function valueSuggestions(typed: string): QuerySuggestion[] {
  // Only offer scaffolds before the user starts typing the value.
  if (typed.trim() !== "") return [];
  return [
    { label: "value", description: "equals", group: "Value", insert: "" },
    {
      label: "*value*",
      description: "contains",
      group: "Value",
      insert: "**",
      caretOffset: 1,
    },
    { label: "value*", description: "starts with", group: "Value", insert: "" },
    {
      label: "/regex/",
      description: "regex",
      group: "Value",
      insert: "//",
      caretOffset: 1,
    },
    {
      label: "(a OR b)",
      description: "any of",
      group: "Value",
      insert: "()",
      caretOffset: 1,
    },
    {
      label: "*",
      description: "field is present",
      group: "Value",
      insert: "*",
    },
  ].filter((s) => s.insert !== "");
}

function lastConnectorEnd(before: string): number {
  let last = 0;
  let inQuotes = false;
  let depth = 0;
  for (let i = 0; i < before.length; i++) {
    const ch = before.charAt(i);
    if (ch === '"') inQuotes = !inQuotes;
    else if (!inQuotes && ch === "(") depth++;
    else if (!inQuotes && ch === ")") depth--;
    else if (!inQuotes && depth === 0 && /\s/.test(ch)) {
      const conn = connectorAt(before, i + 1);
      if (conn) last = conn.end;
    }
  }
  return last;
}
