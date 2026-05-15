// Mirrors the Go `risk_analysis.CanonicalRuleID` helper. Both layers must
// agree so that policy form ids (UPPER_SNAKE Presidio entity types,
// dotted-prefixed scanner ids) line up with the kebab-case rule_id the
// backend writes to risk_results.
const KNOWN_SOURCE_PREFIXES = [
  "presidio.",
  "shadow_mcp.",
  "destructive_tool.",
  "cli_destructive.",
  "gitleaks.",
  "prompt_injection.",
  "pi.",
] as const;

export function canonicalizeRuleId(
  ruleId: string,
  source?: string | null,
): string {
  let id = ruleId.trim().toLowerCase();
  if (!id) return "";

  if (source) {
    const sourcePrefix = source.toLowerCase() + ".";
    if (id.startsWith(sourcePrefix)) {
      id = id.slice(sourcePrefix.length);
    }
  }
  for (const prefix of KNOWN_SOURCE_PREFIXES) {
    if (id.startsWith(prefix)) {
      id = id.slice(prefix.length);
      break;
    }
  }

  return id.replace(/[._/]/g, "-");
}

// Humanize a kebab-case rule id we don't have catalog metadata for.
// "shell-rm-rf" -> "Shell Rm Rf". Used as a last-resort label so unknown
// findings render legibly instead of as raw kebab.
export function humanizeRuleId(ruleId: string): string {
  if (!ruleId) return "";
  return ruleId
    .split("-")
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}
