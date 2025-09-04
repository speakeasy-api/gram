import { useSession } from "@/contexts/Auth";

export type ProductTier = "free" | "pro" | "enterprise";

export const ProductTierBadge = ({ tier }: { tier?: ProductTier }) => {
  const session = useSession();

  const finalTier = tier ?? (session.gramAccountType as ProductTier);

  const name = {
    free: "Free",
    pro: "Pro",
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
    free: {
      bg: "bg-neutral-600",
      text: "text-white",
      ring: "ring-neutral-600/50",
    },
    pro: {
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
