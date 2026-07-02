// Non-component helpers for the estimated-cost affordances. Kept in a separate
// module from estimated-cost.tsx so that file can satisfy the react-refresh
// "only export components" rule.
//
// Cost shown across the telemetry surfaces (Costs pages, insights) is derived
// from Claude Code's `claude_code.cost.usage` metric, which is computed as
// `tokens × published API price`. That equals real spend ONLY for metered
// (pay-per-token) accounts. For subscription accounts — Claude Max/Pro
// (personal) and Team/Enterprise seats — usage is included in a flat fee, so the
// figure is only an estimate and overstates real cost.
//
// We therefore label the measure conditionally: a scope we positively know is
// metered is real "Cost"; anything else is "Est. cost" with a disclaimer.
// Claude exposes no plan/billing signal in telemetry, so metered status can't be
// inferred from a session — it comes from an out-of-band, admin-declared
// billing_mode (ai_integration_configs / user_accounts). Until that signal
// exists, no scope is known-metered and every surface shows the estimate. When
// it lands, metered scopes render a plain, confident "Cost" with no asterisk.
export const ESTIMATED_COST_TOOLTIP =
  "Estimated from token usage at standard API rates. Actual billing depends on the plan — subscription seats (Claude Max, Pro, Team, Enterprise) include usage in a flat fee, so this can overstate real cost for non-metered accounts.";

// Known billing modes. Deliberately a widening string, not a closed enum, so new
// modes can be added without a breaking type change (mirrors the backend, which
// stores billing_mode as free-form text). Current well-known values: "metered",
// "flat_rate", "unknown".
export type BillingMode = "metered" | "flat_rate" | "unknown" | (string & {});

/**
 * Whether a scope is confidently billed per token. Only then is the cost figure
 * real spend; every other value (flat_rate, unknown, or absent) means we fall
 * back to the estimate treatment.
 */
export function isMeteredBilling(billingMode?: string | null): boolean {
  return billingMode === "metered";
}

/**
 * The headline label for the cost measure: a plain "Cost" for a confidently
 * metered scope, otherwise "Est. cost". Pass the scope's billing mode when
 * known; omit it (aggregates over mixed/unknown accounts) to get the estimate
 * label.
 */
export function costMeasureLabel(billingMode?: string | null): string {
  return isMeteredBilling(billingMode) ? "Cost" : "Est. cost";
}

/**
 * Resolve a displayed scope's billing mode from the distinct `billing_mode`
 * values observed within it (e.g. a cost view's `dimensionValues.billing_mode`,
 * or the union across a view's rows). A scope is only confidently "metered" — and
 * therefore shows real cost rather than an estimate — when every contributing row
 * is metered, i.e. the single distinct value is "metered". Anything mixed,
 * flat_rate, unknown, or unclassified stays an estimate. Unlike other
 * dimensions, the server keeps empty strings in billing_mode dimension values,
 * so unclassified contributors surface as "" and a mixed metered+unclassified
 * scope correctly fails the single-value check.
 */
export function resolveScopeBillingMode(
  billingModeValues: string[] | undefined,
): string | undefined {
  if (
    billingModeValues &&
    billingModeValues.length === 1 &&
    billingModeValues[0] === "metered"
  ) {
    return "metered";
  }
  return undefined;
}
