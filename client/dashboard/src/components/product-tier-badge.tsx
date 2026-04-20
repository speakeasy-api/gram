import { ProductTier, useProductTier } from "@/hooks/useProductTier";
import { productTierColors } from "@/components/product-tier-utils";

export const ProductTierBadge = ({
  tierOverride,
}: {
  tierOverride?: ProductTier;
}) => {
  const productTier = useProductTier();

  const finalTier = tierOverride ?? productTier;

  const name = {
    base: "Base",
    base_PAID: "Base",
    __deprecated__pro: "Pro",
    enterprise: "Enterprise",
  }[finalTier];

  const classes = productTierColors(finalTier);

  return (
    <div
      className={`text-muted-foreground w-fit rounded-sm px-1 py-0.5 text-xs ${classes.bg} ${classes.text}`}
    >
      {name}
    </div>
  );
};
