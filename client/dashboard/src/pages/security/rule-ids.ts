// Mirrors the Go canonical rule id helpers (`CanonicalGitleaksRuleID`,
// `CanonicalPresidioRuleID`, etc.) so the dashboard's DETECTION_RULES
// lookups match the rule_id the backend writes to risk_results.
//
// Convention: rule ids are category-prefixed:
//   secret.<gitleaks-rule>       — credentials / secrets
//   pii.<presidio-entity>        — personal / financial / medical data
//   shadow-mcp                   — unverified MCP tool call
//   destructive.tool             — MCP tool annotated as destructive
//   destructive.cli-<command>    — destructive shell / git / db / cloud command
//   pi                           — ML classifier prompt injection verdict
//   pi.<heuristic>               — L0 heuristic prompt injection match
export function canonicalizeRuleId(
  ruleId: string,
  source?: string | null,
): string {
  const id = ruleId.trim();
  if (!id) return "";

  const src = source?.toLowerCase() ?? "";

  if (src === "gitleaks") {
    return "secret." + id.toLowerCase();
  }
  if (src === "presidio") {
    return "pii." + id.toLowerCase().replace(/_/g, "-");
  }
  if (src === "shadow_mcp") {
    return "shadow-mcp";
  }
  if (src === "destructive_tool") {
    return "destructive.tool";
  }
  if (src === "cli_destructive") {
    return "destructive.cli-" + id.toLowerCase().replace(/[._/]/g, "-");
  }
  if (src === "prompt_injection") {
    // The deberta classifier rule id in the policy form maps to `pi` on
    // findings — the model is implementation detail. Other entries are
    // heuristic rule names that get a `pi.` prefix.
    if (id === "deberta-v3-classifier") return "pi";
    return "pi." + id.toLowerCase();
  }

  // Unknown source: pass through lowercased.
  return id.toLowerCase();
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
