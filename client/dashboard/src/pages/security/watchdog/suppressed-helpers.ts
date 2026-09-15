import type { RiskExclusion } from "@gram/client/models/components/riskexclusion.js";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";

/** How a finding came to be suppressed, mirroring the API's suppressed_reason. */
export type SuppressionReason = "rule" | "manual" | "automated";

export const SUPPRESSION_REASON_LABEL: Record<SuppressionReason, string> = {
  rule: "Suppressed by rule",
  manual: "Suppressed manually",
  automated: "Suppressed automatically",
};

export function suppressionReason(result: RiskResult): SuppressionReason {
  if (result.suppressedReason) return result.suppressedReason;
  // Legacy pre-convergence rows can still reach the client without a reason.
  // Derive it the way ListDismissedRiskResults does: an exclusion id means a
  // rule suppressed the finding, anything else can only have come from a
  // manual dismissal.
  return result.exclusionId ? "rule" : "manual";
}

/**
 * Rule suppressions have no per-finding restore: unmarking one would only put
 * it back until the exclusion matched it again. Those rows link to the
 * exclusion instead.
 */
export function isRestorable(result: RiskResult): boolean {
  return suppressionReason(result) !== "rule";
}

/**
 * The provenance line above the reason label — what the suppression points at.
 * For a rule that is the exclusion's match value, resolved client-side against
 * the exclusion listing (an exclusion deleted since the finding was suppressed
 * no longer resolves, hence the generic fallback). For a manual or automated
 * suppression it is the user's dismissal note or the sweep's catalog reason,
 * both of which are often absent — the reason label then stands on its own.
 */
export function suppressionDetail(
  result: RiskResult,
  exclusion: RiskExclusion | undefined,
): string | null {
  if (suppressionReason(result) === "rule") {
    return exclusion?.matchValue ?? "Exclusion rule";
  }
  return result.suppressedDetail?.trim() || null;
}
