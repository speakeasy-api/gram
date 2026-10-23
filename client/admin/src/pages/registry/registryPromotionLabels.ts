export const PROMOTION_REASONS: Record<string, string> = {
  eligible: "Ready for staff review",
  insufficient_evidence: "More successful usage is needed",
  errors_observed: "Recent failures need investigation",
  logging_disabled: "Tool call logging is disabled",
  telemetry_unavailable: "Health evidence is unavailable",
  source_changed: "The source changed; refresh the review",
  disabled: "The source server is disabled",
  already_promoted: "Already promoted",
  already_catalogued: "This endpoint is already in the catalog",
  unsupported_configuration: "This server requires custom forwarding headers",
  authentication_required: "Reusable authentication is not supported yet",
};

export function promotionReason(reason: string): string {
  return PROMOTION_REASONS[reason] ?? "This server is not ready for promotion";
}

export function deliveryStatus(status: string | undefined): string {
  const labels: Record<string, string> = {
    pending: "Pending",
    retrying: "Retry scheduled",
    queued: "Guide requested",
    needs_attention: "Needs attention",
  };
  return labels[status ?? ""] ?? "Pending";
}

export function deliveryErrorMessage(code: string | undefined): string | null {
  const labels: Record<string, string> = {
    docs_transient:
      "Delivery encountered a temporary issue and will retry automatically.",
    docs_permanent:
      "The guide request was rejected. Resolve the delivery issue before retrying.",
    docs_attempts_exhausted:
      "Automatic retries are exhausted. Resolve the delivery issue, then retry.",
    docs_not_configured: "Guide delivery needs configuration before retrying.",
  };
  return code ? (labels[code] ?? "Guide delivery needs attention.") : null;
}

export function promotionTimestamp(value: string | undefined): string | null {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? null
    : date.toLocaleString(undefined, {
        month: "short",
        day: "numeric",
        hour: "numeric",
        minute: "2-digit",
      });
}
