import { ProductTier } from "@/hooks/useProductTier";

// Editorial tier chips: hairline border on the card surface, tier expressed
// through text color only — no filled washes.
export const productTierColors = (
  tier: ProductTier,
): { bg: string; text: string; ring: string } => {
  return {
    base: {
      bg: "border-border bg-card border",
      text: "text-muted-foreground",
      ring: "ring-border/50",
    },
    base_PAID: {
      bg: "border-border bg-card border",
      text: "text-default-information",
      ring: "ring-border/50",
    },
    __deprecated__pro: {
      bg: "border-border bg-card border",
      text: "text-default-information",
      ring: "ring-border/50",
    },
    enterprise: {
      bg: "border-border bg-card border",
      text: "text-default-success",
      ring: "ring-border/50",
    },
  }[tier];
};
