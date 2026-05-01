import { cn } from "@/lib/utils";

type BrandGradientLineProps = {
  className?: string;
};

/**
 * Speakeasy brand signature: a thin full-spectrum gradient bar that runs
 * across the 9 language colors. Use exactly once per surface. Pulls the
 * gradient from Moonshine so it stays in sync with brand updates.
 */
export function BrandGradientLine({ className }: BrandGradientLineProps) {
  return (
    <div
      aria-hidden
      className={cn("h-[2px] w-full shrink-0", className)}
      style={{
        background:
          "linear-gradient(90deg, var(--gradient-brand-primary-colors))",
      }}
    />
  );
}
