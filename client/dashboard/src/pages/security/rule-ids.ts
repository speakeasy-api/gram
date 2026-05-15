// ruleIdToPresidioEntity converts a canonical `pii.<kebab>` rule id back
// to the UPPER_SNAKE entity type Presidio's HTTP API speaks. Used at the
// policy-payload boundary so the dashboard can store canonical ids
// everywhere internally while still sending Presidio a compatible
// entities list.
export function ruleIdToPresidioEntity(ruleId: string): string {
  const stripped = ruleId.startsWith("pii.") ? ruleId.slice(4) : ruleId;
  return stripped.toUpperCase().replace(/-/g, "_");
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

// ruleIdCategoryLabel returns the uppercase category-prefix label for a
// rule id (`pii.credit-card` → `PII`, `secret.anthropic-api-key` →
// `SECRET`, `destructive.shell.rm-rf` → `DESTRUCTIVE`,
// `shadow-mcp` → `SHADOW-MCP`, `prompt-injection` → `PROMPT-INJECTION`).
// Use this for category badges instead of the raw `source`, which leaks
// implementation detail (`presidio`, `gitleaks`) the policy author never
// thinks in.
//
// For dotted canonical ids the category is the first dot-delimited
// segment. For undotted canonical ids (`shadow-mcp`, `prompt-injection`)
// the whole id is the category. Legacy non-canonical ids fall through to
// the raw id uppercased — those rows will look noisy, which signals
// "this row predates normalization."
export function ruleIdCategoryLabel(ruleId: string | undefined | null): string {
  if (!ruleId) return "";
  const dot = ruleId.indexOf(".");
  return (dot >= 0 ? ruleId.slice(0, dot) : ruleId).toUpperCase();
}

// Humanize a kebab/dotted rule id we don't have catalog metadata for.
// Splits on every separator the dashboard might encounter — dots, hyphens,
// underscores, and forward slashes — so canonical, UPPER_SNAKE Presidio,
// and legacy slash-bearing cli_destructive ids all render legibly.
// "destructive.cli-rm-rf"            -> "Destructive CLI Rm Rf"
// "pii.credit-card"                  -> "PII Credit Card"
// "pii.us-ssn"                       -> "PII US SSN"
// "MEDICAL_LICENSE" (legacy)         -> "Medical License"
// "cli_destructive.shell/rm-rf"      -> "CLI Destructive Shell Rm Rf"
// "shadow_mcp.unverified_call"       -> "Shadow MCP Unverified Call"
export function humanizeRuleId(ruleId: string): string {
  if (!ruleId) return "";
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
