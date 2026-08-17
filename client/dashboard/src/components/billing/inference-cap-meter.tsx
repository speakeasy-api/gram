import {
  crossedSpendCapThreshold,
  type SpendCapThreshold,
  spendCapFillPercent,
} from "@/components/billing/payg-billing-estimate";
import {
  inferenceCapBillingNote,
  inferenceCapInvoiceNote,
  inferenceCapLabel,
} from "@/components/billing/inference-caps";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { InferenceSpendCap } from "@gram/client/models/components/inferencespendcap.js";

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
 * This calendar month's spend against one Gram-managed inference cap.
 *
 * Shared by the usage section and the cap's own control so the two can't
 * disagree about what has been spent, or about which band the spend is in.
 *
 * `title` is what the usage section needs and the control doesn't: the control
 * already names the cap in the label of the field that sets it, and repeating
 * it directly below reads as two separate things. `billingNote` is the same
 * split: the usage section sits under the invoice estimate and has to say how
 * these figures relate to it, while the control is nowhere near one.
 *
 * The note is picked here rather than passed in because which half of it is
 * true depends on the branch below — copy that talks about the cap's month is
 * a contradiction on a key that has no cap.
 */
export function InferenceCapMeter({
  cap,
  title = true,
  billingNote = false,
}: {
  cap: InferenceSpendCap;
  title?: boolean;
  billingNote?: boolean;
}): JSX.Element {
  const label = inferenceCapLabel(cap.keyType);
  const spent = usdMeter.format(cap.creditsUsed);
  const heading = title ? <Text className="font-medium">{label}</Text> : null;

  // Without a cap the spend has nothing to be a proportion of, and a full-width
  // bar would read as a limit that was reached. The figure still shows: it is
  // the only place this month's spend on this key appears. So does the invoice
  // half of the billing note — an uncapped key still spends money, and whether
  // that money reaches the invoice is exactly what the note is there to say.
  // Only that half: the rest of it is about the cap's month rolling over, which
  // would contradict the "No cap is set." printed directly above it.
  if (!(cap.monthlyCredits > 0)) {
    return (
      <Stack gap={1}>
        {heading}
        <Text muted small className="tabular-nums">
          {spent} spent this month. No cap is set.
        </Text>
        <Footnote
          text={billingNote ? inferenceCapInvoiceNote(cap.keyType) : ""}
        />
      </Stack>
    );
  }

  const threshold = crossedSpendCapThreshold(
    cap.creditsUsed,
    cap.monthlyCredits,
  );
  const percent = spendCapFillPercent(cap.creditsUsed, cap.monthlyCredits);
  const limit = usdMeter.format(cap.monthlyCredits);
  const thresholdNote = capNote(threshold);
  const footnote = [
    thresholdNote,
    billingNote ? inferenceCapBillingNote(cap.keyType) : "",
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <Stack gap={2}>
      <Stack direction="horizontal" justify="space-between" gap={3}>
        {heading}
        <Text muted small className="tabular-nums">
          {spent} of {limit}
        </Text>
      </Stack>
      <div
        role="progressbar"
        aria-label={`${label}: ${spent} of the ${limit} monthly cap`}
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
      <Footnote text={footnote} />
    </Stack>
  );
}

/**
 * The line under a meter, dropped entirely when there is nothing to say.
 *
 * Both branches render one, and both can end up with an empty string — below
 * the first threshold there is no threshold note, and the cap's own control
 * asks for no billing note at all — so an empty line must not take up space.
 */
function Footnote({ text }: { text: string }): JSX.Element | null {
  if (text === "") return null;
  return (
    <Text muted small>
      {text}
    </Text>
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

// The bands are entered *at* their percentage, not past it: spend of exactly
// half the cap is already the 50 band. So the copy is inclusive — "over 50%" on
// a meter reading $50.00 of $100.00 contradicts the figure beside it.
function capNote(threshold: SpendCapThreshold): string {
  switch (threshold) {
    case 100:
      return "This month's cap is reached, so this inference is stopped until the month rolls over or the cap is raised.";
    case 90:
      return "You've used at least 90% of this month's cap.";
    case 75:
      return "You've used at least 75% of this month's cap.";
    case 50:
      return "You've used at least half of this month's cap.";
    case 0:
      return "";
  }
}
