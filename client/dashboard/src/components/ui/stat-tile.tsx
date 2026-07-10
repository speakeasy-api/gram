import * as React from "react";

import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

export type StatTileTone = "default" | "destructive" | "warning";
export type StatTileDeltaTone = "positive" | "negative" | "neutral";

export interface StatTileDelta {
  value: string;
  tone?: StatTileDeltaTone;
}

export interface StatTileProps {
  /** Rendered as the mono uppercase eyebrow label above the value. */
  label: React.ReactNode;
  /** The headline number — rendered huge, in the display serif. */
  value: React.ReactNode;
  /** Small colored delta shown beside the value (e.g. "+12%"). */
  delta?: StatTileDelta;
  /** Sans caption rendered below the value. */
  caption?: React.ReactNode;
  /** Colors the value for alarming metrics. Defaults to "default". */
  tone?: StatTileTone;
  isLoading?: boolean;
  className?: string;
  /** Escape hatch for callers that need a different value size/weight. */
  valueClassName?: string;
  /** Optional inline graphic (typically a chart/Sparkline) rendered beside
   * the value/delta row — e.g. a trend line backing up the headline number.
   * Hidden while `isLoading`. */
  sparkline?: React.ReactNode;
}

// `text-default-{success,warning}` (not the bare `text-success`/`text-warning`
// utilities) — the bare tokens pair a pale "-100" swatch in light mode with a
// chip background (see Badge), which reads as near-invisible text on its
// own. `text-destructive` bare is fine as-is; it's already the established
// convention for error text throughout the app and stays legible in both
// themes.
const valueToneClasses: Record<StatTileTone, string> = {
  default: "text-foreground",
  destructive: "text-destructive",
  warning: "text-default-warning",
};

const deltaToneClasses: Record<StatTileDeltaTone, string> = {
  positive: "text-default-success",
  negative: "text-destructive",
  neutral: "text-muted-foreground",
};

/**
 * Chrome-less stat: mono eyebrow label, huge serif value with an optional
 * colored delta, and an optional sans caption. Meant to be embedded directly
 * in a page section or a table cell — wrap in `StatCard` for the
 * hairline-bordered tile treatment.
 */
export function StatTile({
  label,
  value,
  delta,
  caption,
  tone = "default",
  isLoading = false,
  className,
  valueClassName,
  sparkline,
}: StatTileProps): React.JSX.Element {
  let captionNode: React.ReactNode = null;
  if (isLoading) {
    if (caption !== undefined) {
      captionNode = <Skeleton className="h-4 w-32" />;
    }
  } else if (caption) {
    captionNode = (
      <div className="font-sans text-sm text-muted-foreground">{caption}</div>
    );
  }

  return (
    <div className={cn("flex flex-col gap-2", className)}>
      <div className="font-mono text-xs uppercase tracking-[0.08em] text-muted">
        {isLoading ? <Skeleton className="h-3 w-20" /> : label}
      </div>
      <div className="flex items-baseline justify-between gap-2">
        <div className="flex items-baseline gap-2">
          {isLoading ? (
            <Skeleton className="h-10 w-24" />
          ) : (
            <span
              className={cn(
                "font-display text-4xl font-thin tabular-nums sm:text-5xl",
                valueToneClasses[tone],
                valueClassName,
              )}
            >
              {value}
            </span>
          )}
          {!isLoading && delta && (
            <span
              className={cn(
                "font-sans text-sm font-light",
                deltaToneClasses[delta.tone ?? "neutral"],
              )}
            >
              {delta.value}
            </span>
          )}
        </div>
        {!isLoading && sparkline && <div className="shrink-0">{sparkline}</div>}
      </div>
      {captionNode}
    </div>
  );
}

export interface StatCardProps extends StatTileProps {
  /** Extra classes for the bordered wrapper itself, distinct from `className` (applied to the inner StatTile). */
  cardClassName?: string;
}

/**
 * Hairline-bordered card wrapper around `StatTile` — the north-star stat
 * tile: squared corners, hairline border, no shadow.
 */
export function StatCard({
  cardClassName,
  ...statTileProps
}: StatCardProps): React.JSX.Element {
  return (
    <div className={cn("border-neutral-softest border p-5", cardClassName)}>
      <StatTile {...statTileProps} />
    </div>
  );
}
