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
//   prompt-injection.<heuristic> — L0 heuristic prompt injection match
//   prompt-injection.unknown     — L1 ML classifier binary verdict
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
    // Backend already emits `destructive.<category>.<name>` for cli
    // patterns, so this branch is a pass-through. Lowercase for safety.
    return id.toLowerCase();
  }
  if (src === "prompt_injection") {
    // The deberta classifier rule id in the policy form maps to
    // `prompt-injection.unknown` on findings — the binary model can't
    // pin a specific attack family. Other entries are heuristic rule
    // names that get a `prompt-injection.` prefix.
    if (id === "deberta-v3-classifier") return "prompt-injection.unknown";
    return "prompt-injection." + id.toLowerCase();
  }

  // Unknown source: pass through lowercased.
  return id.toLowerCase();
}

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
