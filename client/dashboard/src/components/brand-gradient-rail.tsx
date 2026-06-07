import { cn } from "@/lib/utils";

type BrandGradientRailProps = {
  className?: string;
};

/**
 * Vertical sibling of BrandGradientLine: the Speakeasy brand spectrum as a thin
 * vertical accent bar. Used as a left rail on the active nav item and the
 * workspace switcher now that the horizontal brand line is gone from the top bar.
 * Position it with className (e.g. absolute left-0). Pulls the gradient from
 * Moonshine so it stays in sync with brand updates.
 */
export function BrandGradientRail({ className }: BrandGradientRailProps) {
  return (
    <div
      aria-hidden
      className={cn("w-[3px] rounded-full", className)}
      style={{
        background:
          "linear-gradient(180deg, var(--gradient-brand-primary-colors))",
      }}
    />
  );
}
