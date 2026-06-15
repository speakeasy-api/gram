import type { RiskMatchCondition } from "@gram/client/models/components";
import {
  MATCH_OPS,
  MATCH_TARGETS,
  OP_DESCRIPTIONS,
  OP_LABELS,
  TARGET_DESCRIPTIONS,
  TARGET_LABELS,
  type MatchCombine,
} from "./detection-rules-data";

type MatchTarget = RiskMatchCondition["target"];
type MatchOp = RiskMatchCondition["op"];

/**
 * A Datadog-style query language for a rule's match_config. Clauses read
 * `<field> <op> <value>` and are joined by a single `AND` or `OR` (the backend
 * stores a flat condition list with one combine — no nested grouping yet).
 *
 *   tool_server is mise AND tool_function matches ^delete_
 *   content contains ssn, tax id
 *   tool_args.$.scope is all
 *   tool_server is ""            (empty value matches native tools)
 *
 * `field:value` is accepted as shorthand for `field is value`.
 */

const OP_KEYWORDS: { keyword: string; op: MatchOp }[] = [
  { keyword: "is not", op: "not_equals" },
  { keyword: "is", op: "equals" },
  { keyword: "matches", op: "regex" },
  { keyword: "like", op: "glob" },
  { keyword: "contains", op: "keyword" },
  { keyword: "exists", op: "exists" },
];

const OP_TO_KEYWORD: Record<MatchOp, string> = {
  equals: "is",
  not_equals: "is not",
  regex: "matches",
  glob: "like",
  keyword: "contains",
  exists: "exists",
};

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
  if (trimmed.includes("(") || trimmed.includes(")")) {
    return {
      conditions: [],
      combine: "and",
      error: "Grouping with ( ) isn't supported yet — use a single AND or OR",
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

function serializeClause(c: RiskMatchCondition): string {
  const field =
    c.target === "tool_args"
      ? `tool_args${c.path ? `.${c.path}` : ""}`
      : c.target;
  const keyword = OP_TO_KEYWORD[c.op];
  if (c.op === "exists") return `${field} exists`;
  if (c.op === "keyword") {
    return `${field} contains ${(c.values ?? []).join(", ")}`;
  }
  const value = c.value ?? "";
  const needsQuote =
    value === "" || /\s/.test(value) || /^(and|or)$/i.test(value);
  return `${field} ${keyword} ${needsQuote ? `"${value}"` : value}`;
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
  let i = 0;
  while (i < input.length) {
    const ch = input.charAt(i);
    if (ch === '"') {
      inQuotes = !inQuotes;
      buf += ch;
      i++;
      continue;
    }
    if (!inQuotes && /\s/.test(ch)) {
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
  const fieldMatch = /^([^\s:]+)/.exec(trimmed);
  const fieldRaw = fieldMatch?.[1];
  if (!fieldRaw) return "Expected a field";

  let target: MatchTarget;
  let path: string | undefined;
  if (fieldRaw === "tool_args" || fieldRaw.startsWith("tool_args.")) {
    target = "tool_args";
    path = fieldRaw === "tool_args" ? "" : fieldRaw.slice("tool_args.".length);
  } else if ((MATCH_TARGETS as readonly string[]).includes(fieldRaw)) {
    target = fieldRaw as MatchTarget;
  } else {
    return `Unknown field "${fieldRaw}"`;
  }

  let op: MatchOp;
  let valueStr: string;
  if (trimmed.charAt(fieldRaw.length) === ":") {
    op = "equals";
    valueStr = trimmed.slice(fieldRaw.length + 1).trim();
  } else {
    const afterField = trimmed.slice(fieldRaw.length).trim();
    const opMatch = matchOpKeyword(afterField);
    if (!opMatch) return `Expected an operator after "${fieldRaw}"`;
    op = opMatch.op;
    valueStr = afterField.slice(opMatch.length).trim();
  }

  if (target === "tool_args" && !path) {
    return "tool_args needs a path, e.g. tool_args.$.field";
  }

  const condition: RiskMatchCondition = { target, op };
  if (path) condition.path = path;
  if (op === "keyword") {
    condition.values = valueStr
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
  } else if (op !== "exists") {
    condition.value = unquote(valueStr);
  }
  return condition;
}

function matchOpKeyword(s: string): { op: MatchOp; length: number } | null {
  const lower = s.toLowerCase();
  for (const { keyword, op } of OP_KEYWORDS) {
    if (lower === keyword || lower.startsWith(`${keyword} `)) {
      return { op, length: keyword.length };
    }
  }
  return null;
}

function unquote(s: string): string {
  if (s.length >= 2 && s.startsWith('"') && s.endsWith('"')) {
    return s.slice(1, -1);
  }
  return s;
}

/* -------------------------------------------------------------------------- */
/*  Autocomplete                                                              */
/* -------------------------------------------------------------------------- */

export type QuerySuggestion = {
  /** The token shown as the primary label. */
  label: string;
  /** Affordance copy explaining the token. */
  description: string;
  /** Text inserted (with a trailing space) in place of the current partial. */
  insert: string;
};

/** Suggestions for the caret position: the field, operator, or connector the
 *  user is most likely typing next, filtered by the partial token. `from` is
 *  the index the partial token starts at, so callers can replace it. */
export function matchQuerySuggestions(
  input: string,
  caret: number,
): { from: number; suggestions: QuerySuggestion[] } {
  const before = input.slice(0, caret);
  const partial = /(\S*)$/.exec(before)?.[1] ?? "";
  const from = caret - partial.length;

  const clauseStart = lastConnectorEnd(before);
  const clause = before.slice(clauseStart);
  const completed = clause.slice(0, clause.length - partial.length).trim();

  let suggestions: QuerySuggestion[];
  if (completed === "") {
    suggestions = targetSuggestions(partial);
  } else if (!completed.includes(" ") && !completed.includes(":")) {
    suggestions = opSuggestions(partial);
  } else if (!matchOpKeyword(clauseAfterField(completed))) {
    suggestions = opSuggestions(partial);
  } else {
    suggestions = connectorSuggestions(partial);
  }
  return { from, suggestions };
}

function clauseAfterField(completed: string): string {
  const space = completed.indexOf(" ");
  return space === -1 ? "" : completed.slice(space).trim();
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

function targetSuggestions(partial: string): QuerySuggestion[] {
  const lower = partial.toLowerCase();
  return MATCH_TARGETS.filter((t) => t.startsWith(lower)).map((t) => ({
    label: t,
    description: `${TARGET_LABELS[t]} — ${TARGET_DESCRIPTIONS[t]}`,
    insert: t === "tool_args" ? "tool_args." : `${t} `,
  }));
}

function opSuggestions(partial: string): QuerySuggestion[] {
  const lower = partial.toLowerCase();
  return MATCH_OPS.map((op) => ({ op, keyword: OP_TO_KEYWORD[op] }))
    .filter(({ keyword }) => keyword.startsWith(lower))
    .map(({ op, keyword }) => ({
      label: keyword,
      description: `${OP_LABELS[op]} — ${OP_DESCRIPTIONS[op]}`,
      insert: `${keyword} `,
    }));
}

function connectorSuggestions(partial: string): QuerySuggestion[] {
  const lower = partial.toLowerCase();
  return [
    { label: "AND", description: "All conditions must match", insert: "AND " },
    { label: "OR", description: "Any condition may match", insert: "OR " },
  ].filter((s) => s.label.toLowerCase().startsWith(lower));
}
