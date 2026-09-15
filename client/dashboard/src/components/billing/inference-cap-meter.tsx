import {
  crossedSpendCapThreshold,
  inferenceCapLabel,
  type SpendCapThreshold,
  spendCapFillPercent,
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
 * Shared by every surface that reports a cap, so none of them can disagree
 * about what has been spent, or about which band the spend is in. It never
 * names the cap: every caller sits under the control or label that already
 * does, and repeating the name directly below reads as two separate things.
 */
export function InferenceCapMeter({
  cap,
}: {
  cap: InferenceSpendCap;
}): JSX.Element {
  const label = inferenceCapLabel(cap.keyType);
  const spent = usdMeter.format(cap.creditsUsed);

  // Without a cap the spend has nothing to be a proportion of, and a full-width
  // bar would read as a limit that was reached. The figure still shows: it is
  // the only place this month's spend on this key appears.
  if (!(cap.monthlyCredits > 0)) {
    return (
      <Text muted small className="tabular-nums">
        {spent} spent this month. No cap is set.
      </Text>
    );
  }

  const threshold = crossedSpendCapThreshold(
    cap.creditsUsed,
    cap.monthlyCredits,
  );
  const percent = spendCapFillPercent(cap.creditsUsed, cap.monthlyCredits);
  const limit = usdMeter.format(cap.monthlyCredits);
  const footnote = capNote(threshold, cap.disabled);

  return (
    <Stack gap={2}>
      <Text muted small className="tabular-nums">
        {spent} of {limit}
      </Text>
      <div
        role="progressbar"
        aria-label={`${label}: ${spent} of the ${limit} monthly cap`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
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
      {/* Below the first threshold there is no note, and an empty line must
          not take up space. */}
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

// The cap is what stopped a key that is otherwise on, so that is the only case
// where the way back is the month rolling over or a higher cap. A key the
// platform has turned off is stopped by something this org can't move from
// here, and neither of those would bring it back.
const CAP_REACHED_NOTE =
  "This month's cap is reached, so this inference is stopped until the month rolls over or the cap is raised.";

const CAP_REACHED_DISABLED_NOTE =
  "This month's cap is reached, and this inference is turned off for this organization — neither the month rolling over nor a higher cap will resume it.";

// The bands are entered *at* their percentage, not past it: spend of exactly
// half the cap is already the 50 band. So the copy is inclusive — "over 50%" on
// a meter reading $50.00 of $100.00 contradicts the figure beside it.
function capNote(threshold: SpendCapThreshold, disabled: boolean): string {
  switch (threshold) {
    case 100:
      return disabled ? CAP_REACHED_DISABLED_NOTE : CAP_REACHED_NOTE;
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
