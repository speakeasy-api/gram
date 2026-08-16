import {
  canEstimatePaygInvoice,
  crossedSpendCapThreshold,
  formatExactUsd,
  formatRecordedThrough,
  formatTokenCount,
  spendCapFillPercent,
  type SpendCapThreshold,
} from "@/components/billing/payg-billing-estimate";
import { formatBillingDate } from "@/components/billing/payg-plan-state";
import { useStripeSubscription } from "@/components/billing/use-stripe-subscription";
import { Page } from "@/components/page-layout";
import { MetricCard } from "@/components/ui/MetricCard";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { PaygBillingSummary } from "@gram/client/models/components/paygbillingsummary.js";
import { useGetCreditUsage } from "@gram/client/react-query/getCreditUsage.js";
import { useGetPaygBillingSummary } from "@gram/client/react-query/getPaygBillingSummary.js";

// The window Stripe leaves itself to close a cycle. Named here because it is
// the difference between an estimate the customer can trust and one that looks
// wrong for three days every month.
const INVOICE_FINALIZATION_NOTE =
  "This is an estimate, not a bill. Your invoice can finalize up to 72 hours after the cycle ends.";

const MISSING_FIGURE = "—";

// Hoisted so a formatter isn't constructed on every render. Cents throughout:
// the cap is set in whole dollars, and the spend against it is small enough
// that rounding it to dollars would show "$0" for real usage.
const usdMeter = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

/**
 * The pay-as-you-go organization's usage: what the live billing cycle has run
 * up so far and what it is on course to be invoiced, plus the separate
 * calendar-month chat spend meter.
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
          <ChatSpendCapMeter />
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
  const chatSpend = formatExactUsd(summary.chatSpendUsd);
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
        <MetricCard
          size="sm"
          tone="information"
          label="Chat spend"
          value={chatSpend ?? MISSING_FIGURE}
          description={chatSpendDescription(recordedThrough)}
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

// The cutoff is the whole point of this figure: chat spend only counts UTC days
// that have closed, so today's chat is not in the number and saying otherwise
// would make the estimate look stale to the customer who just used it.
function chatSpendDescription(recordedThrough: string | null): string {
  if (recordedThrough === null) {
    return "No completed day of chat spend has been recorded in this cycle yet.";
  }
  return `Completed days through ${recordedThrough}. Today's chat isn't counted yet.`;
}

// The total inherits the chat cutoff, so it names it too — an estimate that
// silently stops short of today is worse than one that says where it stops.
function estimatedTotalDescription(recordedThrough: string | null): string {
  if (recordedThrough === null) return INVOICE_FINALIZATION_NOTE;
  return `Chat spend counted through ${recordedThrough}. ${INVOICE_FINALIZATION_NOTE}`;
}

/**
 * This month's chat spend against the organization's monthly cap.
 *
 * Deliberately not part of the estimate above: the cap runs on the calendar
 * month while the invoice runs on the Stripe cycle, so the two windows overlap
 * without matching. The copy names the window on every line so the figure can't
 * be read as the cycle's chat spend, which is the number sitting beside it.
 */
function ChatSpendCapMeter(): JSX.Element | null {
  const { data, isError } = useGetCreditUsage(undefined, undefined, {
    throwOnError: false,
  });

  if (isError) return null;

  if (data === undefined) {
    return (
      <Stack gap={2}>
        <Skeleton className="h-4 w-64" />
        <Skeleton className="h-2 w-full" />
      </Stack>
    );
  }

  // Without a cap there is no meter to draw — the spend has nothing to be a
  // proportion of, and a full-width bar would read as a limit that was reached.
  if (!(data.monthlyCredits > 0)) return null;

  const threshold = crossedSpendCapThreshold(
    data.creditsUsed,
    data.monthlyCredits,
  );
  const percent = spendCapFillPercent(data.creditsUsed, data.monthlyCredits);
  const spent = usdMeter.format(data.creditsUsed);
  const cap = usdMeter.format(data.monthlyCredits);

  return (
    <Stack gap={2}>
      <Stack direction="horizontal" justify="space-between" gap={3}>
        <Text className="font-medium">Chat spend this calendar month</Text>
        <Text muted small className="tabular-nums">
          {spent} of {cap}
        </Text>
      </Stack>
      <div
        role="progressbar"
        aria-label={`Chat spend this calendar month: ${spent} of the ${cap} monthly cap`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(percent)}
        className="bg-muted h-2 w-full overflow-hidden"
      >
        <div
          className={cn(
            "h-full transition-all duration-300",
            meterFillClass(threshold),
          )}
          style={{ width: `${percent}%` }}
        />
      </div>
      <Text muted small>
        {capNote(threshold)} This is the monthly cap on chat and the other
        AI-powered dashboard experiences. It resets on the first of the month,
        so it doesn't line up with the billing cycle above and isn't part of the
        estimate.
      </Text>
    </Stack>
  );
}

// The same bands the threshold emails are sent on, so a customer who has had
// the 90% notice sees a bar that agrees with it.
function meterFillClass(threshold: SpendCapThreshold): string {
  switch (threshold) {
    case 100:
      return "bg-destructive-default";
    case 90:
    case 75:
      return "bg-warning-default";
    case 50:
    case 0:
      return "bg-success-default";
  }
}

function capNote(threshold: SpendCapThreshold): string {
  switch (threshold) {
    case 100:
      return "This month's cap is reached, so chat is stopped until the month rolls over or the cap is raised.";
    case 90:
      return "You've used over 90% of this month's cap.";
    case 75:
      return "You've used over 75% of this month's cap.";
    case 50:
      return "You've used over half of this month's cap.";
    case 0:
      return "";
  }
}
