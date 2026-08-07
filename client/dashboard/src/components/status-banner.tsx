import { cn } from "@/lib/utils";

// The tone is carried by a hairline left strip in the matching semantic
// color; the surface itself stays flat. Banners whose tone changes in place
// (a server going from unpublished to published) transition the strip color
// rather than snapping.
const BANNER_TONES = {
  warning: "bg-warning-default",
  success: "bg-success-default",
  destructive: "bg-destructive-default",
} as const;

export type StatusBannerTone = keyof typeof BANNER_TONES;

/**
 * Full-width banner heading a detail page, marked with the state it reports.
 *
 * Supplies only the frame: callers lay out their own headline, copy and
 * actions inside it.
 */
export function StatusBanner({
  tone,
  className,
  children,
}: {
  tone: StatusBannerTone;
  className?: string;
  children: React.ReactNode;
}): React.JSX.Element {
  return (
    <div
      className={cn(
        "border-border bg-card relative overflow-hidden border",
        className,
      )}
    >
      <div
        aria-hidden="true"
        className={cn(
          "absolute inset-y-0 left-0 w-0.5 transition-colors duration-700 ease-in-out",
          BANNER_TONES[tone],
        )}
      />
      <div className="relative flex flex-col">{children}</div>
    </div>
  );
}
