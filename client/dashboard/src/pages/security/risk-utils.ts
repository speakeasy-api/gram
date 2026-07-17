import { DETECTION_RULES, type RuleCategory } from "./policy-data";
import { humanizeRuleId } from "./rule-ids";

const SOURCE_TO_CATEGORY: ReadonlyMap<string, RuleCategory> = new Map<
  string,
  RuleCategory
>([
  ["destructive_tool", "destructive_tool"],
  ["shadow_mcp", "shadow_mcp"],
  ["prompt_injection", "prompt_injection"],
  ["llm_judge", "prompt_policy"],
  ["cli_destructive", "cli_destructive"],
  ["account_identity", "account_identity"],
  // Scanner-source fallbacks: when a rule_id doesn't carry its category
  // prefix (e.g. gitleaks' bare "generic-api-key"), classify by source so
  // we never leak the scanner name to the UI. Keep in sync with the Go
  // classifier in server/internal/risk/categories.
  ["gitleaks", "secrets"],
  ["presidio", "pii"],
]);

const ruleIdToCategory = new Map<string, RuleCategory>();
const ruleIdToTitle = new Map<string, string>();

for (const [category, rules] of Object.entries(DETECTION_RULES)) {
  for (const rule of rules) {
    ruleIdToCategory.set(rule.id, category as RuleCategory);
    ruleIdToTitle.set(rule.id, rule.title);
  }
}

// DETECTION_RULES.id is the canonical rule_id the backend writes to
// risk_results, so lookup maps key by it directly.
const RULE_ID_TO_CATEGORY: ReadonlyMap<string, RuleCategory> = ruleIdToCategory;
const RULE_ID_TO_TITLE: ReadonlyMap<string, string> = ruleIdToTitle;

export function getRuleTitleFallback(ruleId: string | undefined): string {
  if (!ruleId) return "-";
  return RULE_ID_TO_TITLE.get(ruleId) ?? humanizeRuleId(ruleId);
}

// ── Severity (CVSS-style) ────────────────────────────────────────────────────
// A policy carries a decimal severity score (0.1–10). Findings resolve their
// rating from the owning policy's score at read time. Bands mirror the CVSS
// qualitative scale, minus the "None" band — the lowest rating is Low.
export type SeverityRating = "low" | "medium" | "high" | "critical";

const SEVERITY_BANDS: { min: number; rating: SeverityRating }[] = [
  { min: 9.0, rating: "critical" },
  { min: 7.0, rating: "high" },
  { min: 4.0, rating: "medium" },
  { min: 0, rating: "low" },
];

export function scoreToRating(score: number): SeverityRating {
  for (const band of SEVERITY_BANDS) {
    if (score >= band.min) return band.rating;
  }
  return "low";
}

export const SEVERITY_RATING_LABEL: Record<SeverityRating, string> = {
  low: "Low",
  medium: "Medium",
  high: "High",
  critical: "Critical",
};

export function getCategoryForFinding(
  source?: string,
  ruleId?: string,
): RuleCategory | null {
  if (ruleId) {
    const byRule = RULE_ID_TO_CATEGORY.get(ruleId);
    if (byRule) return byRule;
  }
  if (source) {
    return SOURCE_TO_CATEGORY.get(source) ?? null;
  }
  return null;
}

// getCategoryCodeForFinding resolves a finding to a short, uppercase,
// customer-safe category code (e.g. "SECRETS", "PII") for use as compact
// fine-print or badge text. It NEVER returns the raw scanner `source`
// (`gitleaks`, `presidio`), which is an implementation detail we don't
// expose. Falls back to "FLAGGED" when nothing classifies.
export function getCategoryCodeForFinding(
  source?: string,
  ruleId?: string,
): string {
  const category = getCategoryForFinding(source, ruleId);
  return category ? category.toUpperCase() : "FLAGGED";
}
