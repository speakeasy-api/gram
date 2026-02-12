import { ProductTier, useProductTier } from "@/hooks/useProductTier";

export const ProductTierBadge = ({ tierOverride }: { tierOverride?: ProductTier }) => {
  const productTier = useProductTier();

  const finalTier = tierOverride ?? productTier;

  const name = {
    base: "Base",
    base_PAID: "Base",
    __deprecated__pro: "Pro (Deprecated)",
    enterprise: "Enterprise",
  }[finalTier];

  const classes = productTierColors(finalTier);

  return (
    <div
      className={`text-xs text-muted-foreground px-1 py-0.5 rounded-sm ${classes.bg} ${classes.text}`}
    >
      {name}
    </div>
  );
};

export const productTierColors = (tier: ProductTier) => {
  return {
    base: {
      bg: "bg-neutral-600",
      text: "text-white",
      ring: "ring-neutral-600/50",
    },
    base_PAID: {
      bg: "bg-violet-500",
      text: "text-white",
      ring: "ring-violet-500/50",
    },
    __deprecated__pro: {
      bg: "bg-violet-500",
      text: "text-white",
      ring: "ring-violet-500/50",
    },
    enterprise: {
      bg: "bg-success-foreground dark:bg-success",
      text: "text-success dark:text-white",
      ring: "ring-success-foreground/50 dark:ring-success/50",
    },
  }[tier];
};
