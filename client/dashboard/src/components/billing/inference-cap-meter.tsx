import {
  crossedSpendCapThreshold,
  type SpendCapThreshold,
  spendCapFillPercent,
} from "@/components/billing/payg-billing-estimate";
import { inferenceCapLabel } from "@/components/billing/inference-caps";
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
 * it directly below reads as two separate things.
 */
export function InferenceCapMeter({
  cap,
  title = true,
  note,
}: {
  cap: InferenceSpendCap;
  title?: boolean;
  note?: string;
}): JSX.Element {
  const label = inferenceCapLabel(cap.keyType);
  const spent = usdMeter.format(cap.creditsUsed);
  const heading = title ? <Text className="font-medium">{label}</Text> : null;

  // Without a cap the spend has nothing to be a proportion of, and a full-width
  // bar would read as a limit that was reached. The figure still shows: it is
  // the only place this month's spend on this key appears.
  if (!(cap.monthlyCredits > 0)) {
    return (
      <Stack gap={1}>
        {heading}
        <Text muted small className="tabular-nums">
          {spent} spent this month. No cap is set.
        </Text>
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
  const footnote = [thresholdNote, note].filter(Boolean).join(" ");

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
      {footnote !== "" && (
        <Text muted small>
          {footnote}
        </Text>
      )}
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
      return "This month's cap is reached, so this inference is stopped until the month rolls over or the cap is raised.";
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
