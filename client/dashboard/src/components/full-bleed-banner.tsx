import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";

/**
 * The edge-to-edge strip that sits between the page header and the page
 * content: a bordered icon tile, a headline over one line of supporting copy,
 * and the actions that answer it.
 *
 * Every banner in this slot shares the frame so they stack without seams — an
 * organization can be shown more than one at a time, and strips of different
 * heights or gutters read as one broken layout.
 *
 * `className` and `contentClassName` exist for the view-transition names the
 * onboarding banner carries across route changes; nothing else should be
 * reaching for them.
 */
export function FullBleedBanner({
  icon: BannerIcon,
  role,
  title,
  description,
  descriptionClassName = "max-w-10/12",
  actions,
  className,
  contentClassName,
}: {
  icon: React.ComponentType<{ className?: string; strokeWidth?: number }>;
  /** Left unset for a banner that is page furniture rather than a report. */
  role?: "alert" | "status";
  title: string;
  description: string;
  descriptionClassName?: string;
  actions: React.ReactNode;
  className?: string;
  contentClassName?: string;
}): JSX.Element {
  return (
    <div
      role={role}
      className={cn(
        "border-border/60 bg-muted/20 dark:bg-white/[0.03] w-full border-b",
        className,
      )}
    >
      <div
        className={cn(
          "mx-auto flex max-w-7xl items-center gap-4 px-8 py-5",
          contentClassName,
        )}
      >
        <div className="bg-background border-border/60 flex size-10 shrink-0 items-center justify-center border">
          <BannerIcon className="text-foreground size-5" strokeWidth={1.75} />
        </div>

        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <Text
            variant="small"
            as="span"
            className="text-foreground leading-tight font-semibold"
          >
            {title}
          </Text>
          <Text variant="small" muted className={descriptionClassName}>
            {description}
          </Text>
        </div>

        <div className="flex shrink-0 items-center gap-1">{actions}</div>
      </div>
    </div>
  );
}
