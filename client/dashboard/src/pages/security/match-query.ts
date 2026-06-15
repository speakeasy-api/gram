import type { RiskMatchCondition } from "@gram/client/models/components";
import type { MatchCombine } from "./detection-rules-data";

type MatchTarget = RiskMatchCondition["target"];
type MatchOp = RiskMatchCondition["op"];

/**
 * A Datadog-style colon query language for a rule's match_config. Conditions
 * read `field:value` (equals shorthand) or `field:op:value`, joined by a single
 * `AND` or `OR` (the backend stores a flat list with one combine — nesting,
 * NOT, and parentheses are rejected pending a backend grammar change).
 *
 *   tool_call.name:bash AND tool_call.args:contains:(rm OR curl OR wget)
 *   content:matches:/secret-\d+/
 *   tool_call.server:""              (empty value matches native tools)
 *   tool_call.args.$.scope:in:(all OR everything)
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

export type QueryOp = { token: string; op: MatchOp; description: string };

export const QUERY_OPS: QueryOp[] = [
  {
    token: "equals",
    op: "equals",
    description: "exact value (empty matches native)",
  },
  {
    token: "not_equals",
    op: "not_equals",
    description: "any value except this",
  },
  {
    token: "contains",
    op: "contains",
    description: "substring; (a OR b) matches any",
  },
  {
    token: "not_contains",
    op: "not_contains",
    description: "matches none of the substrings",
  },
  { token: "in", op: "in", description: "exactly one of (a OR b)" },
  { token: "starts_with", op: "starts_with", description: "value is a prefix" },
  { token: "ends_with", op: "ends_with", description: "value is a suffix" },
  { token: "matches", op: "regex", description: "/RE2 regex/" },
  { token: "exists", op: "exists", description: "field is present (no value)" },
];

const TOKEN_BY_OP = new Map<MatchOp, string>(
  QUERY_OPS.map((o) => [o.op, o.token]),
);
const OP_BY_TOKEN = new Map<string, MatchOp>(
  QUERY_OPS.map((o) => [o.token, o.op]),
);
const BACKEND_TO_TARGET = new Map<MatchTarget, QueryTarget>(
  QUERY_TARGETS.map((t) => [t.backend, t]),
);
const UNION_OPS = new Set<MatchOp>(["contains", "not_contains", "in"]);

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
  // NOT and grouping parens aren't supported yet (the backend stores a flat
  // list); value unions still use balanced ( ).
  if (/\bNOT\b/i.test(trimmed)) {
    return {
      conditions: [],
      combine: "and",
      error: "NOT isn't supported yet — use a single AND or OR",
    };
  }

  const split = splitClauses(trimmed);
  if (split.error)
    return { conditions: [], combine: split.combine, error: split.error };

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
  const target = BACKEND_TO_TARGET.get(c.target);
  const base = target?.name ?? c.target;
  if (c.target === "tool_args" && c.path) return `${base}.${c.path}`;
  return base;
}

function serializeClause(c: RiskMatchCondition): string {
  const field = fieldName(c);
  if (c.op === "exists") return `${field}:exists`;

  const values = (c.values ?? []).filter((v) => v.trim());
  if (UNION_OPS.has(c.op) && values.length > 0) {
    const body = values.length === 1 ? values[0] : `(${values.join(" OR ")})`;
    return `${field}:${TOKEN_BY_OP.get(c.op)}:${body}`;
  }
  if (c.op === "regex") return `${field}:matches:/${c.value ?? ""}/`;
  if (c.op === "equals") return `${field}:${quoteValue(c.value ?? "")}`;
  return `${field}:${TOKEN_BY_OP.get(c.op)}:${quoteValue(c.value ?? "")}`;
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
  const trimmed = clause.trim();
  const colon = trimmed.indexOf(":");
  if (colon === -1) {
    return `Use field:value in "${trimmed}"`;
  }
  const fieldRaw = trimmed.slice(0, colon);
  const resolved = resolveField(fieldRaw);
  if (!resolved) return `Unknown field "${fieldRaw}"`;

  const rest = trimmed.slice(colon + 1);

  // field:exists
  if (rest.toLowerCase() === "exists") {
    return makeCondition(resolved, "exists", "");
  }

  // Does `rest` start with an explicit operator token (`op:...`)?
  const opColon = rest.indexOf(":");
  if (opColon !== -1) {
    const maybeOp = rest.slice(0, opColon);
    const op = OP_BY_TOKEN.get(maybeOp.toLowerCase());
    if (op) {
      return makeCondition(resolved, op, rest.slice(opColon + 1));
    }
  }
  // Otherwise treat the whole remainder as an equals value (Datadog shorthand).
  return makeCondition(resolved, "equals", rest);
}

function resolveField(
  raw: string,
): { target: QueryTarget; path: string } | null {
  // Longest-prefix match so tool_call.args.$.x resolves to tool_call.args.
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
  op: MatchOp,
  rawValue: string,
): RiskMatchCondition {
  const condition: RiskMatchCondition = { target: resolved.target.backend, op };
  if (resolved.path) condition.path = resolved.path;
  if (op === "exists") return condition;

  if (op === "regex") {
    condition.value = stripRegexSlashes(rawValue.trim());
    return condition;
  }
  if (UNION_OPS.has(op)) {
    condition.values = parseUnion(rawValue);
    return condition;
  }
  condition.value = unquote(rawValue.trim());
  return condition;
}

function parseUnion(raw: string): string[] {
  let s = raw.trim();
  if (s.startsWith("(") && s.endsWith(")")) s = s.slice(1, -1);
  return s
    .split(/\s+OR\s+|,/i)
    .map((v) => unquote(v.trim()))
    .filter(Boolean);
}

function stripRegexSlashes(v: string): string {
  if (v.length >= 2 && v.startsWith("/") && v.endsWith("/"))
    return v.slice(1, -1);
  return v;
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
  /** right-aligned category/group label (Datadog facet style). */
  group?: string;
  /** Text inserted in place of the current partial token. */
  insert: string;
};

export function matchQuerySuggestions(
  input: string,
  caret: number,
): { from: number; suggestions: QuerySuggestion[] } {
  const before = input.slice(0, caret);
  const clauseStart = lastConnectorEnd(before);
  const clause = before.slice(clauseStart);
  // The token currently under edit (no whitespace, respecting an open union paren).
  const partial = /(\S*)$/.exec(before)?.[1] ?? "";
  const from = caret - partial.length;

  const colon = clause.indexOf(":");
  if (colon === -1 || clause.length - partial.length < colon + 1) {
    // Still typing the field (no committed colon yet).
    return { from, suggestions: targetSuggestions(partial) };
  }

  const fieldRaw = clause.slice(0, colon);
  const afterField = clause.slice(colon + 1);
  const opColon = afterField.indexOf(":");
  if (opColon === -1 || afterField.length - partial.length < opColon + 1) {
    // After `field:` — suggest operators (or type a value for equals).
    return { from, suggestions: opSuggestions(partial) };
  }
  // After `field:op:` — value hints.
  return {
    from,
    suggestions: valueSuggestions(
      fieldRaw,
      afterField.slice(0, opColon),
      partial,
    ),
  };
}

function targetSuggestions(partial: string): QuerySuggestion[] {
  const lower = partial.toLowerCase();
  return QUERY_TARGETS.filter((t) => t.name.startsWith(lower)).map((t) => ({
    label: t.name,
    description: t.description,
    group: t.category === "tool" ? "Tool" : "Prompt",
    insert: t.hasPath && t.name === lower ? `${t.name}.` : `${t.name}:`,
  }));
}

function opSuggestions(partial: string): QuerySuggestion[] {
  const lower = partial.toLowerCase();
  return QUERY_OPS.filter((o) => o.token.startsWith(lower)).map((o) => ({
    label: o.token,
    description: o.description,
    group: "Operator",
    insert: o.op === "exists" ? `${o.token} ` : `${o.token}:`,
  }));
}

function valueSuggestions(
  _field: string,
  opToken: string,
  partial: string,
): QuerySuggestion[] {
  const op = OP_BY_TOKEN.get(opToken.toLowerCase());
  const hints: QuerySuggestion[] = [];
  if (op && UNION_OPS.has(op)) {
    hints.push({
      label: "(a OR b)",
      description: "match any of a list",
      group: "Value",
      insert: "(",
    });
  }
  if (op === "regex") {
    hints.push({
      label: "/regex/",
      description: "RE2 pattern between slashes",
      group: "Value",
      insert: "/",
    });
  }
  if (!partial)
    hints.push({
      label: "…",
      description: "type a value",
      group: "Value",
      insert: "",
    });
  return hints;
}

function lastConnectorEnd(before: string): number {
  let last = 0;
  let inQuotes = false;
  for (let i = 0; i < before.length; i++) {
    if (before.charAt(i) === '"') inQuotes = !inQuotes;
    if (!inQuotes && /\s/.test(before.charAt(i))) {
      const conn = connectorAt(before, i + 1);
      if (conn) last = conn.end;
    }
  }
  return last;
}
