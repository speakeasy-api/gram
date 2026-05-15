// ruleIdToPresidioEntity converts a canonical `pii.<kebab>` rule id back
// to the UPPER_SNAKE entity type Presidio's HTTP API speaks. Used at the
// policy-payload boundary so the dashboard can store canonical ids
// everywhere internally while still sending Presidio a compatible
// entities list.
export function ruleIdToPresidioEntity(ruleId: string): string {
  const stripped = ruleId.startsWith("pii.") ? ruleId.slice(4) : ruleId;
  return stripped.toUpperCase().replace(/-/g, "_");
}

// Humanize a kebab/dotted rule id we don't have catalog metadata for.
// "destructive.cli-rm-rf" -> "Destructive Cli Rm Rf"
// "pii.credit-card"       -> "Pii Credit Card"
// Used as a last-resort label so unknown findings render legibly instead of
// as raw kebab.
export function humanizeRuleId(ruleId: string): string {
  if (!ruleId) return "";
  return ruleId
    .split(/[.-]/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}
