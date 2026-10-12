import type { EnforcementOutcome } from "@gram/client/models/components/riskresult.js";

const OUTCOME_LABELS: Record<EnforcementOutcome, string> = {
  logged: "Logged",
  denied: "Denied",
  withheld: "Withheld",
  warned_pending: "Warned · pending",
  warned_acknowledged: "Warned · acknowledged",
  warned_abandoned: "Warned · abandoned",
  quarantined: "Quarantined",
};

// Outcomes where the action was stopped or held, rendered with emphasis; the
// rest read as neutral text.
const BLOCKING_OUTCOMES = new Set<EnforcementOutcome>([
  "denied",
  "withheld",
  "quarantined",
]);

export function enforcementOutcomeLabel(
  outcome: EnforcementOutcome | undefined,
): string | null {
  return outcome ? OUTCOME_LABELS[outcome] : null;
}

export function isBlockingOutcome(
  outcome: EnforcementOutcome | undefined,
): boolean {
  return outcome != null && BLOCKING_OUTCOMES.has(outcome);
}

/** Label for a concrete MCP server id: display name, else slug, else short id. */
export function mcpServerDisplayName(server: {
  id: string;
  name?: string | undefined;
  slug?: string | undefined;
}): string {
  return server.name?.trim() || server.slug || server.id.slice(0, 8);
}
