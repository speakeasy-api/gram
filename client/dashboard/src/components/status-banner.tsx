import { cn } from "@speakeasy-api/moonshine";

// A gradient can't be transitioned, so every tone is painted as its own layer
// and the active one is faded in over the others. Banners whose tone changes in
// place (a server going from unpublished to published) cross-fade rather than
// snapping.
const BANNER_TONES = {
  warning:
    "bg-gradient-to-tr from-slate-50 via-slate-50 to-orange-100 dark:from-slate-950 dark:via-neutral-800 dark:to-amber-900/60",
  success:
    "bg-gradient-to-br from-slate-50/10 via-slate-50 to-emerald-100/50 dark:from-slate-950/60 dark:via-neutral-800 dark:to-emerald-900/30",
  destructive:
    "bg-gradient-to-tr from-slate-50 via-slate-50 to-red-100 dark:from-slate-950 dark:via-neutral-800 dark:to-red-900/60",
} as const;

export type StatusBannerTone = keyof typeof BANNER_TONES;

/**
 * Full-width banner heading a detail page, tinted to the state it reports.
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
        "border-border/70 relative overflow-hidden rounded-xl border shadow-sm",
        className,
      )}
    >
      {Object.entries(BANNER_TONES).map(([name, gradient]) => (
        <div
          key={name}
          aria-hidden="true"
          className={cn(
            "absolute inset-0 transition-opacity duration-700 ease-in-out",
            gradient,
            name === tone ? "opacity-100" : "opacity-0",
          )}
        />
      ))}
      <div className="relative flex flex-col">{children}</div>
    </div>
  );
}
