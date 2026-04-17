import { ProductTier } from "@/hooks/useProductTier";

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
