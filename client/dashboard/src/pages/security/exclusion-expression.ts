// The exclusion create/edit UI presents a single expression in a textarea
// (per the design mockups) which maps to the structured exclusion fields the
// API expects. This module is the single source of truth for that mapping.
//
// Grammar (clauses joined by `&&`):
//   match == "value"        -> matchType "exact"
//   match ~= "value"        -> matchType "regex"
//   rule_id == "value"      -> matchType "rule_id" (or ruleIdFilter when combined)
//   source == "value"       -> matchType "source"  (or sourceFilter when combined)
//   entity_type == "value"  -> matchType "entity_type"
//
// The first match/entity_type/rule_id/source clause is the primary match; an
// additional `rule_id ==` / `source ==` clause becomes the narrowing filter.

type ExclusionMatchType =
  | "exact"
  | "regex"
  | "rule_id"
  | "source"
  | "entity_type";

export interface ExclusionFields {
  matchType: ExclusionMatchType;
  matchValue: string;
  ruleIdFilter: string;
  sourceFilter: string;
}

export type ParseResult =
  | { ok: true; value: ExclusionFields }
  | { ok: false; error: string };

const REGEX_MAX_LENGTH = 512;

const CLAUSE_RE =
  /^(match|rule_id|source|entity_type)\s*(==|~=)\s*"((?:[^"\\]|\\.)*)"$/;

function unescape(value: string): string {
  return value.replace(/\\(["\\])/g, "$1");
}

function escape(value: string): string {
  return value.replace(/(["\\])/g, "\\$1");
}

// Split on `&&` that is not inside a double-quoted string.
function splitClauses(input: string): string[] {
  const clauses: string[] = [];
  let current = "";
  let inQuote = false;
  for (let i = 0; i < input.length; i++) {
    const ch = input[i];
    if (ch === '"' && input[i - 1] !== "\\") {
      inQuote = !inQuote;
    }
    if (!inQuote && ch === "&" && input[i + 1] === "&") {
      clauses.push(current);
      current = "";
      i++; // skip second &
      continue;
    }
    current += ch;
  }
  clauses.push(current);
  return clauses.map((c) => c.trim()).filter((c) => c.length > 0);
}

export function parseExclusionExpression(input: string): ParseResult {
  const clauses = splitClauses(input);
  if (clauses.length === 0) {
    return { ok: false, error: "Enter an exclusion criteria expression." };
  }

  let primary: { matchType: ExclusionMatchType; value: string } | null = null;
  let ruleIdFilter = "";
  let sourceFilter = "";

  for (const clause of clauses) {
    const m = CLAUSE_RE.exec(clause);
    if (!m) {
      return {
        ok: false,
        error: `Could not parse \`${clause}\`. Use e.g. match == "value".`,
      };
    }
    const [, lhs, op, rawValue] = m;
    const value = unescape(rawValue);

    if (op === "~=" && lhs !== "match") {
      return { ok: false, error: `\`~=\` is only valid with match.` };
    }
    if (value === "") {
      return { ok: false, error: "Match value must not be empty." };
    }

    switch (lhs) {
      case "match": {
        if (primary) {
          return { ok: false, error: "Only one match clause is allowed." };
        }
        primary = { matchType: op === "~=" ? "regex" : "exact", value };
        break;
      }
      case "entity_type": {
        if (primary) {
          return { ok: false, error: "Only one primary clause is allowed." };
        }
        primary = { matchType: "entity_type", value };
        break;
      }
      case "rule_id": {
        if (!primary) {
          primary = { matchType: "rule_id", value };
        } else if (ruleIdFilter) {
          return { ok: false, error: "Only one rule_id filter is allowed." };
        } else {
          ruleIdFilter = value;
        }
        break;
      }
      case "source": {
        if (!primary) {
          primary = { matchType: "source", value };
        } else if (sourceFilter) {
          return { ok: false, error: "Only one source filter is allowed." };
        } else {
          sourceFilter = value;
        }
        break;
      }
    }
  }

  if (!primary) {
    return { ok: false, error: "Enter an exclusion criteria expression." };
  }

  if (primary.matchType === "regex") {
    if (primary.value.length > REGEX_MAX_LENGTH) {
      return {
        ok: false,
        error: `Regex pattern too long (max ${REGEX_MAX_LENGTH} characters).`,
      };
    }
    try {
      new RegExp(primary.value);
    } catch {
      return { ok: false, error: "Invalid regex pattern." };
    }
  }

  return {
    ok: true,
    value: {
      matchType: primary.matchType,
      matchValue: primary.value,
      ruleIdFilter: primary.matchType === "rule_id" ? "" : ruleIdFilter,
      sourceFilter: primary.matchType === "source" ? "" : sourceFilter,
    },
  };
}

const PRIMARY_OPERATOR: Record<ExclusionMatchType, string> = {
  exact: 'match == "',
  regex: 'match ~= "',
  rule_id: 'rule_id == "',
  source: 'source == "',
  entity_type: 'entity_type == "',
};

export function serializeExclusionExpression(fields: {
  matchType: string;
  matchValue: string;
  ruleIdFilter?: string;
  sourceFilter?: string;
}): string {
  const matchType = fields.matchType as ExclusionMatchType;
  const prefix = PRIMARY_OPERATOR[matchType] ?? 'match == "';
  let expr = `${prefix}${escape(fields.matchValue)}"`;
  if (fields.ruleIdFilter && matchType !== "rule_id") {
    expr += ` && rule_id == "${escape(fields.ruleIdFilter)}"`;
  }
  if (fields.sourceFilter && matchType !== "source") {
    expr += ` && source == "${escape(fields.sourceFilter)}"`;
  }
  return expr;
}
