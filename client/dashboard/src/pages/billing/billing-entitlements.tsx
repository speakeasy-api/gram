import { formatBillingQuantity } from "@/components/billing/billing-format";
import { Type } from "@/components/ui/type";
import type { ProductTier } from "@/hooks/useProductTier";
import type { TierLimits } from "@gram/client/models/components/tierlimits.js";
import type { UsageTiers } from "@gram/client/models/components/usagetiers.js";
import { Stack } from "@speakeasy-api/moonshine";

export function getChatCreditEntitlement(
  productTier: ProductTier,
  usageTiers: UsageTiers | undefined,
): number | undefined {
  if (!usageTiers) {
    return undefined;
  }

  switch (productTier) {
    case "__deprecated__pro":
      return usageTiers.pro.includedCredits;
    case "enterprise":
      return usageTiers.enterprise.includedCredits;
    default:
      return usageTiers.free.includedCredits;
  }
}

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
