import { cva, type VariantProps } from "class-variance-authority";
import { useSession } from "@/contexts/Auth";
import { cn } from "@/lib/utils";

export type ProductTier = "free" | "pro" | "enterprise";

const productTierBadgeVariants = cva(
  "inline-flex items-center text-xs uppercase font-mono px-1 py-0.5 rounded-xs w-fit",
  {
    variants: {
      tier: {
        free: "border border-neutral-600 text-neutral-600 dark:border-neutral-400 dark:text-neutral-400",
        pro: "border border-success-foreground text-success-foreground dark:border-success dark:text-success",
        enterprise: "text-foreground", // Enterprise uses gradient wrapper, so different styling
      },
    },
    defaultVariants: {
      tier: "free",
    },
  }
);

const productTierRingVariants = cva("", {
  variants: {
    tier: {
      free: "ring-neutral-600/50",
      pro: "ring-success-foreground/50 dark:ring-success/50",
      enterprise: "ring-brand-gradient-end/50",
    },
  },
  defaultVariants: {
    tier: "free",
  },
});

type ProductTierBadgeProps = VariantProps<typeof productTierBadgeVariants> & {
  tier?: ProductTier;
  className?: string;
};

export const ProductTierBadge = ({ tier, className }: ProductTierBadgeProps) => {
  const session = useSession();

  const finalTier = tier ?? (session.gramAccountType as ProductTier);

  const name = {
    free: "Free",
    pro: "Pro",
    enterprise: "Enterprise",
  }[finalTier];

  // Enterprise tier uses gradient border technique
  if (finalTier === "enterprise") {
    return (
      <div className={cn("inline-flex rounded-xs p-[1px] bg-gradient-primary w-fit", className)}>
        <div className="inline-flex items-center text-xs uppercase font-mono px-1 py-0.5 rounded-[3px] bg-background text-foreground">
          {name}
        </div>
      </div>
    );
  }

  // Free and Pro use regular borders
  return (
    <div
      className={cn(
        productTierBadgeVariants({ tier: finalTier }),
        className
      )}
    >
      {name}
    </div>
  );
};

export const productTierColors = (tier: ProductTier) => {
  // Return classes that can be used for other components
  if (tier === "enterprise") {
    return {
      bg: '', // No background color for enterprise (uses gradient wrapper)
      border: '', // No simple border for enterprise
      text: 'text-foreground',
      ring: productTierRingVariants({ tier }),
    };
  }
  
  const variantClasses = productTierBadgeVariants({ tier }).split(' ');
  return {
    bg: '', // No background color anymore
    border: variantClasses.filter(c => c.startsWith('border-')).join(' '),
    text: variantClasses.find(c => c.startsWith('text-')) || '',
    ring: productTierRingVariants({ tier }),
  };
};