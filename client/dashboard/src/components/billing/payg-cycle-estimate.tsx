import {
  canEstimatePaygInvoice,
  formatExactUsd,
  formatRecordedThrough,
  formatTokenCount,
} from "@/components/billing/payg-billing-estimate";
import { formatBillingDate } from "@/components/billing/payg-plan-state";
import { useStripeSubscription } from "@/components/billing/use-stripe-subscription";
import { StatRow, type StatRowMetric } from "@/components/stat-row";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { PaygBillingSummary } from "@gram/client/models/components/paygbillingsummary.js";
import { useGetPaygBillingSummary } from "@gram/client/react-query/getPaygBillingSummary.js";
import { useEffect, useRef } from "react";

// The window Stripe leaves itself to close a cycle. Named here because it is
// the difference between an estimate the customer can trust and one that looks
// wrong for three days every month.
const INVOICE_FINALIZATION_NOTE =
  "This is an estimate, not a bill. Your invoice can finalize up to 72 hours after the cycle ends.";

const MISSING_FIGURE = "—";

/**
 * The pay-as-you-go cycle's billable usage and estimated invoice, shown at the
 * head of the shared usage section.
 *
 * Nothing renders until the live Stripe subscription says there is a cycle to
 * report on. A trial, a subscription that never started, and the moments before
 * the period anchor all have no billable period — the endpoint answers those
 * with a conflict — so the gate keeps the request from being made rather than
 * turning its refusal into an error message on the billing page.
 */
export function PaygCycleEstimate(): JSX.Element | null {
  const { data: subscription } = useStripeSubscription();
  const billing = canEstimatePaygInvoice(subscription, new Date());

  // The shared query client throws everything but a 401/403 to the app error
  // boundary, which would take the whole billing page down over one estimate.
  const { data, isError, refetch } = useGetPaygBillingSummary(
    undefined,
    undefined,
    {
      enabled: billing,
      throwOnError: false,
    },
  );

  // A cached summary can outlive the cycle it described: at a cycle boundary
  // the live subscription moves onto the new period while the cache still
  // holds the prior cycle's summary. The summary's period start is the
  // subscription's own anchor (the server copies it verbatim), so a mismatch
  // identifies a stale summary — kept loading, and refetched once per new
  // anchor. Once per anchor rather than while mismatched: the mismatch can
  // also mean the SUBSCRIPTION read is the stale one, and refetching the
  // summary until they agree would poll the endpoint in a loop.
  const anchorMs =
    subscription?.currentPeriodStart instanceof Date
      ? subscription.currentPeriodStart.getTime()
      : null;
  const stale =
    data !== undefined &&
    anchorMs !== null &&
    (!(data.periodStart instanceof Date) ||
      data.periodStart.getTime() !== anchorMs);
  const refetchedForAnchor = useRef<number | null>(null);
  useEffect(() => {
    // `refetch` runs even on a disabled query, so the billing gate has to be
    // re-asserted here — a trial or canceled subscription holding a stale
    // cached summary must not request the billing endpoint.
    if (!billing || !stale || anchorMs === null) return;
    if (refetchedForAnchor.current === anchorMs) return;
    refetchedForAnchor.current = anchorMs;
    void refetch();
  }, [billing, stale, anchorMs, refetch]);

  if (!billing) return null;

  // An estimate that can't be loaded is left out entirely rather than reported.
  // The plan section below carries the subscription's real state, and a failure
  // banner here would only add noise to it — the figures are informational, and
  // a missing one costs the customer nothing.
  if (isError) return null;

  const summary = stale ? undefined : data;

  return (
    <Stack gap={3}>
      <CycleCaption summary={summary} />
      <StatRow
        isLoading={summary === undefined}
        metrics={estimateMetrics(summary)}
      />
    </Stack>
  );
}

function CycleCaption({
  summary,
}: {
  summary: PaygBillingSummary | undefined;
}): JSX.Element {
  if (summary === undefined) return <Skeleton className="h-4 w-72" />;
  return (
    <Text muted small>
      Current billing cycle · {cyclePeriodLabel(summary)}
    </Text>
  );
}

// The tile labels are known before the summary is, so the loading skeletons
// keep the loaded row's shape.
function estimateMetrics(
  summary: PaygBillingSummary | undefined,
): StatRowMetric[] {
  const recordedThrough = summary
    ? formatRecordedThrough(summary.recordedThrough)
    : null;

  return [
    {
      size: "sm",
      tone: "information",
      label: "Tokens under management",
      value: (summary && formatTokenCount(summary.tumTokens)) ?? MISSING_FIGURE,
      description: tumCostDescription(
        summary ? formatExactUsd(summary.tumUnitPriceUsd) : null,
        summary ? formatExactUsd(summary.tumCostUsd) : null,
      ),
    },
    // All Gram-managed inference is customer-billable on PAYG. The estimate
    // combines both key types through the latest completed UTC day.
    {
      size: "sm",
      tone: "information",
      label: "Inference spend",
      value:
        (summary && formatExactUsd(summary.otherInferenceSpendUsd)) ??
        MISSING_FIGURE,
      description: inferenceSpendDescription(recordedThrough),
    },
    {
      size: "sm",
      tone: "neutral",
      label: "Estimated total",
      value:
        (summary && formatExactUsd(summary.estimatedTotalUsd)) ??
        MISSING_FIGURE,
      description: estimatedTotalDescription(recordedThrough),
    },
  ];
}

/** The cycle's own dates, or nothing when Stripe gave nothing usable. */
function cyclePeriodLabel(summary: PaygBillingSummary): string {
  const start = formatBillingDate(summary.periodStart);
  const end = formatBillingDate(summary.periodEnd);
  if (start === null || end === null) return "dates unavailable";
  return `${start} to ${end}`;
}

function tumCostDescription(
  unitPrice: string | null,
  cost: string | null,
): string {
  if (unitPrice === null || cost === null) {
    return "Billed at a flat rate per token under management.";
  }
  return `${cost} at a flat ${unitPrice} per token.`;
}

// The cutoff is the whole point of this figure: the spend only counts UTC days
// that have closed, so today's usage is not in the number and saying otherwise
// would make the estimate look stale to the customer who just used it.
function inferenceSpendDescription(recordedThrough: string | null): string {
  if (recordedThrough === null) {
    return "Includes customer-facing and platform-initiated inference. No completed day has been recorded in this cycle yet.";
  }
  return `Includes customer-facing and platform-initiated inference. Completed days through ${recordedThrough}; today isn't counted yet.`;
}

// The total inherits that cutoff, so it names it too — an estimate that
// silently stops short of today is worse than one that says where it stops.
function estimatedTotalDescription(recordedThrough: string | null): string {
  if (recordedThrough === null) return INVOICE_FINALIZATION_NOTE;
  return `Inference spend counted through ${recordedThrough}. ${INVOICE_FINALIZATION_NOTE}`;
}
