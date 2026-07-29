import { formatBillingQuantity } from "@/components/billing/billing-format";
import { Type } from "@/components/ui/type";
import type { TierLimits } from "@gram/client/models/components/tierlimits.js";
import { Stack } from "@speakeasy-api/moonshine";

export function TierIncludedItems({
  tierLimits,
}: {
  tierLimits: TierLimits;
}): JSX.Element | null {
  const hasIncludedItems =
    tierLimits.includedBullets.length > 0 || tierLimits.includedCredits > 0;
  if (!hasIncludedItems) {
    return null;
  }

  return (
    <Stack gap={1}>
      <Type
        mono
        muted
        small
        variant="subheading"
        className="font-medium uppercase"
      >
        Included
      </Type>
      <ul className="list-inside space-y-1">
        {tierLimits.includedCredits > 0 ? (
          <li>
            <span className="text-muted-foreground/60">✓</span>{" "}
            {formatBillingQuantity(tierLimits.includedCredits, "chat credits")}{" "}
            / month
          </li>
        ) : null}
        {tierLimits.includedBullets.map((bullet) => (
          <li key={bullet}>
            <span className="text-muted-foreground/60">✓</span> {bullet}
          </li>
        ))}
      </ul>
    </Stack>
  );
}
