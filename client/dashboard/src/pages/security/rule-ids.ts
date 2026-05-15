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

// Humanize a kebab/dotted rule id we don't have catalog metadata for.
// "destructive.cli-rm-rf" -> "Destructive CLI Rm Rf"
// "pii.credit-card"       -> "PII Credit Card"
// "pii.us-ssn"            -> "PII US SSN"
// Used as a last-resort label so unknown findings render legibly instead
// of as raw kebab.
export function humanizeRuleId(ruleId: string): string {
  if (!ruleId) return "";
  return ruleId
    .split(/[.-]/)
    .filter(Boolean)
    .map((part) => {
      const lower = part.toLowerCase();
      if (ACRONYMS.has(lower)) return lower.toUpperCase();
      return lower.charAt(0).toUpperCase() + lower.slice(1);
    })
    .join(" ");
}
