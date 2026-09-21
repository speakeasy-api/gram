// ruleIdToPresidioEntity converts a canonical `pii.<snake_case>` rule id back
// to the UPPER_SNAKE entity type Presidio's HTTP API speaks. Used at the
// policy-payload boundary so the dashboard can store canonical ids
// everywhere internally while still sending Presidio a compatible
// entities list.
export function ruleIdToPresidioEntity(ruleId: string): string {
  const stripped = ruleId.startsWith("pii.") ? ruleId.slice(4) : ruleId;
  return stripped.toUpperCase();
}

// Acronyms we want to render uppercase rather than title-cased. Anything
// that wouldn't read right as "Pii" or "Cli" goes here.
const ACRONYMS = new Set([
  "pii",
  "cli",
  "mcp",
  "api",
  "url",
  "ip",
  "mac",
  "ssn",
  "nric",
  "fin",
  "nhs",
  "nino",
  "nif",
  "tfn",
  "pan",
  "mbi",
  "npi",
  "itin",
  "iban",
  "ml",
  "us",
  "uk",
  "es",
  "it",
  "au",
  "in",
  "sg",
  "aws",
  "gcp",
  "id",
]);

// The rule ids the LLM risk analyzer writes. One per risk it evaluates, plus
// the sentinel its dead-letter path records when the model could not be
// reached or its verdict could not be parsed. Unlike the scanner catalogs,
// these carry no per-rule granularity: the category *is* the rule, and the
// finding's `description` holds the model's reasoning for that one finding.
//
// The labels are what end users see. Nothing in them names the engine — the
// analyzer is an implementation detail, exactly like `gitleaks` / `presidio`
// are for the scanner-backed rules, so the raw id must never be the label.
export const LLM_ANALYZER_DEAD_LETTER_RULE_ID = "llm_analyzer.dead_letter";

const LLM_ANALYZER_RULE_LABEL: ReadonlyMap<string, string> = new Map([
  ["secret.llm", "Secret"],
  ["pii.llm", "PII"],
  ["prompt_injection.llm", "Prompt injection"],
  ["destructive_tool.llm", "Destructive tool"],
  ["cli_destructive.llm", "Destructive command"],
  [LLM_ANALYZER_DEAD_LETTER_RULE_ID, "Analysis unavailable"],
]);

// llmAnalyzerRuleLabel returns the user-facing label for one of the LLM
// analyzer's rule ids, or undefined for any other id.
export function llmAnalyzerRuleLabel(
  ruleId: string | undefined | null,
): string | undefined {
  if (!ruleId) return undefined;
  return LLM_ANALYZER_RULE_LABEL.get(ruleId);
}

// isLlmAnalyzerRuleId reports whether a rule id is one the LLM analyzer
// writes (see LLM_ANALYZER_RULE_LABEL).
export function isLlmAnalyzerRuleId(
  ruleId: string | undefined | null,
): boolean {
  return ruleId != null && LLM_ANALYZER_RULE_LABEL.has(ruleId);
}

// ruleIdCategoryLabel returns the uppercase category-prefix label for a
// rule id (`pii.credit_card` → `PII`, `secret.anthropic_api_key` →
// `SECRET`, `destructive.shell.rm_rf` → `DESTRUCTIVE`,
// `shadow_mcp` → `SHADOW_MCP`, `prompt_injection` → `PROMPT_INJECTION`).
// Use this for category badges instead of the raw `source`, which leaks
// implementation detail (`presidio`, `gitleaks`) the policy author never
// thinks in.
//
// For dotted canonical ids the category is the first dot-delimited
// segment. For undotted canonical ids (`shadow_mcp`, `prompt_injection`)
// the whole id is the category. Legacy non-canonical ids fall through to
// the raw id uppercased — those rows will look noisy, which signals
// "this row predates normalization."
//
// The LLM analyzer's `<category>.llm` ids fall out of the dotted rule
// naturally (`secret.llm` → `SECRET`). Its dead-letter sentinel does not —
// its prefix is the engine's name — so it gets the same label the rule
// renders under, uppercased into badge form.
export function ruleIdCategoryLabel(ruleId: string | undefined | null): string {
  if (!ruleId) return "";
  if (ruleId === LLM_ANALYZER_DEAD_LETTER_RULE_ID) {
    return llmAnalyzerRuleLabel(ruleId)!.toUpperCase().replace(/ /g, "_");
  }
  const dot = ruleId.indexOf(".");
  return (dot >= 0 ? ruleId.slice(0, dot) : ruleId).toUpperCase();
}

// Humanize a snake_case / dotted rule id we don't have catalog metadata for.
// Splits on every separator the dashboard might encounter — dots, hyphens,
// underscores, and forward slashes — so canonical, UPPER_SNAKE Presidio,
// and legacy slash-bearing cli_destructive ids all render legibly.
// "destructive.cli_rm_rf"            -> "Destructive CLI Rm Rf"
// "pii.credit_card"                  -> "PII Credit Card"
// "pii.us_ssn"                       -> "PII US SSN"
// "MEDICAL_LICENSE" (legacy)         -> "Medical License"
// "cli_destructive.shell/rm-rf"      -> "CLI Destructive Shell Rm Rf"
// "shadow_mcp.unverified_call"       -> "Shadow MCP Unverified Call"
// "secret.llm"                       -> "Secret"      (LLM analyzer; see above)
export function humanizeRuleId(ruleId: string): string {
  if (!ruleId) return "";
  const llm = llmAnalyzerRuleLabel(ruleId);
  if (llm) return llm;
  return ruleId
    .split(/[._/-]/)
    .filter(Boolean)
    .map((part) => {
      const lower = part.toLowerCase();
      if (ACRONYMS.has(lower)) return lower.toUpperCase();
      return lower.charAt(0).toUpperCase() + lower.slice(1);
    })
    .join(" ");
}
