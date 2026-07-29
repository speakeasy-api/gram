import type { ProductTier } from "@/hooks/useProductTier";
import type { UsageTiers } from "@gram/client/models/components/usagetiers.js";

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
    case "base":
    case "base_PAID":
      return usageTiers.free.includedCredits;
  }
}
