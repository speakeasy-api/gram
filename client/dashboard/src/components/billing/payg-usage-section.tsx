import { InferenceCapMeter } from "@/components/billing/inference-cap-meter";
import { sortInferenceCaps } from "@/components/billing/inference-caps";
import {
  canEstimatePaygInvoice,
  formatExactUsd,
  formatRecordedThrough,
  formatTokenCount,
} from "@/components/billing/payg-billing-estimate";
import { formatBillingDate } from "@/components/billing/payg-plan-state";
import { useStripeSubscription } from "@/components/billing/use-stripe-subscription";
import { Page } from "@/components/page-layout";
import { MetricCard } from "@/components/ui/MetricCard";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { PaygBillingSummary } from "@gram/client/models/components/paygbillingsummary.js";
import { useGetInferenceSpendCaps } from "@gram/client/react-query/getInferenceSpendCaps.js";
import { useGetPaygBillingSummary } from "@gram/client/react-query/getPaygBillingSummary.js";

// The window Stripe leaves itself to close a cycle. Named here because it is
// the difference between an estimate the customer can trust and one that looks
// wrong for three days every month.
const INVOICE_FINALIZATION_NOTE =
  "This is an estimate, not a bill. Your invoice can finalize up to 72 hours after the cycle ends.";

const MISSING_FIGURE = "—";

/**
 * The pay-as-you-go organization's usage: what the live billing cycle has run
 * up so far and what it is on course to be invoiced, plus the separate
 * calendar-month meter for every inference cap it has.
 *
 * This is what a PAYG organization gets in place of the self-serve usage
 * meters. Those read Polar period usage, which bills nothing for this tier —
 * showing them beside a Stripe estimate would put two disagreeing totals on one
 * page — so the tier branch swaps the whole section rather than adding to it.
 */
export function PaygUsageSection(): JSX.Element {
  return (
    <Page.Section>
      <Page.Section.Title>Usage</Page.Section.Title>
      <Page.Section.Description>
        What this organization has used in the current pay-as-you-go billing
        cycle, and what it is on course to be invoiced.
      </Page.Section.Description>
      <Page.Section.Body>
        <Stack gap={8}>
          <BillingCycleEstimate />
          <InferenceCapMeters />
        </Stack>
      </Page.Section.Body>
    </Page.Section>
  );
}

/**
 * The current cycle's billable usage and estimated invoice.
 *
 * Nothing renders until the live Stripe subscription says there is a cycle to
 * report on. A trial, a subscription that never started, and the moments before
 * the period anchor all have no billable period — the endpoint answers those
 * with a conflict — so the gate keeps the request from being made rather than
 * turning its refusal into an error message on the billing page.
 */
function BillingCycleEstimate(): JSX.Element | null {
  const { data: subscription } = useStripeSubscription();
  const billing = canEstimatePaygInvoice(subscription, new Date());

  // The shared query client throws everything but a 401/403 to the app error
  // boundary, which would take the whole billing page down over one estimate.
  const { data, isError } = useGetPaygBillingSummary(undefined, undefined, {
    enabled: billing,
    throwOnError: false,
  });

  if (!billing) return null;

  // An estimate that can't be loaded is left out entirely rather than reported.
  // The plan section directly below carries the subscription's real state, and
  // a failure banner here would only add noise to it — the figures are
  // informational, and a missing one costs the customer nothing.
  if (isError) return null;

  if (data === undefined) {
    return (
      <Stack gap={4}>
        <Skeleton className="h-4 w-72" />
        <Skeleton className="h-28 w-full" />
      </Stack>
    );
  }

  return <BillingCycleFigures summary={data} />;
}

function BillingCycleFigures({
  summary,
}: {
  summary: PaygBillingSummary;
}): JSX.Element {
  const tokens = formatTokenCount(summary.tumTokens);
  const unitPrice = formatExactUsd(summary.tumUnitPriceUsd);
  const tumCost = formatExactUsd(summary.tumCostUsd);
  const otherInferenceSpend = formatExactUsd(summary.otherInferenceSpendUsd);
  const estimatedTotal = formatExactUsd(summary.estimatedTotalUsd);
  const recordedThrough = formatRecordedThrough(summary.recordedThrough);

  return (
    <Stack gap={3}>
      <Text muted small>
        Current billing cycle · {cyclePeriodLabel(summary)}
      </Text>
      <MetricCard.Group>
        <MetricCard
          size="sm"
          tone="information"
          label="Tokens under management"
          value={tokens ?? MISSING_FIGURE}
          description={tumCostDescription(unitPrice, tumCost)}
        />
        {/* The invoiced half of the Gram-managed inference. The analysis
            Gram runs for its own features is funded by Gram, so it is not a
            line here and is not in the total below. */}
        <MetricCard
          size="sm"
          tone="information"
          label="Other inference spend"
          value={otherInferenceSpend ?? MISSING_FIGURE}
          description={inferenceSpendDescription(recordedThrough)}
        />
        <MetricCard
          size="sm"
          tone="neutral"
          label="Estimated total"
          value={estimatedTotal ?? MISSING_FIGURE}
          description={estimatedTotalDescription(recordedThrough)}
        />
      </MetricCard.Group>
    </Stack>
  );
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
    return "Billed to you as its own line. No completed day has been recorded in this cycle yet.";
  }
  return `Billed to you as its own line. Completed days through ${recordedThrough}; today isn't counted yet.`;
}

// The total inherits that cutoff, so it names it too — an estimate that
// silently stops short of today is worse than one that says where it stops.
function estimatedTotalDescription(recordedThrough: string | null): string {
  if (recordedThrough === null) return INVOICE_FINALIZATION_NOTE;
  return `Inference spend counted through ${recordedThrough}. ${INVOICE_FINALIZATION_NOTE}`;
}

/**
 * This calendar month's spend against every inference cap this organization
 * has, one meter each.
 *
 * Deliberately not part of the estimate above: the caps run on the calendar
 * month while the invoice runs on the Stripe cycle, so the two windows overlap
 * without matching. Each meter carries its own billing note, which is also
 * where the invoiced inference is told apart from the analysis Gram funds and
 * never puts on an invoice.
 */
function InferenceCapMeters(): JSX.Element | null {
  const { data, isError } = useGetInferenceSpendCaps(undefined, undefined, {
    throwOnError: false,
  });

  // A failing read leaves the meters out entirely rather than reporting the
  // outage here: the figures are informational, and the caps section below
  // carries its own explicit failure state and its own way back from it.
  if (isError) return null;

  if (data === undefined) {
    return (
      <Stack gap={2}>
        <Skeleton className="h-4 w-64" />
        <Skeleton className="h-2 w-full" />
      </Stack>
    );
  }

  // No Gram-managed keys have been materialized for this organization yet, so
  // there is nothing here to meter.
  if (data.length === 0) return null;

  return (
    <Stack gap={6}>
      {sortInferenceCaps(data).map((cap) => (
        <InferenceCapMeter key={cap.keyType} cap={cap} billingNote />
      ))}
    </Stack>
  );
}
