import * as React from "react";

import { cn } from "@/lib/utils";

export type ProgressTone =
  | "neutral"
  | "success"
  | "warning"
  | "destructive"
  | "information";

// "neutral" renders as the ink fill (bg-btn-primary) per the global "ink
// primary fills" rule — the other tones map onto their matching semantic
// solid ("-default" tier, not the paler "-softest" badge-chip tier, so the
// fill stays legible against the bg-muted track).
const fillToneClass: Record<ProgressTone, string> = {
  neutral: "bg-btn-primary",
  success: "bg-success-default",
  warning: "bg-warning-default",
  destructive: "bg-destructive-default",
  information: "bg-information-default",
};

function clampPercent(value: number): number {
  if (Number.isNaN(value)) return 0;
  return Math.min(100, Math.max(0, value));
}

export interface ProgressProps extends Omit<
  React.ComponentProps<"div">,
  "children"
> {
  value: number;
  max?: number;
  tone?: ProgressTone;
}

/** Single-segment track/fill bar — squared corners, no shadow, `h-2` by default. */
export function Progress({
  value,
  max = 100,
  tone = "neutral",
  className,
  ...props
}: ProgressProps): React.JSX.Element {
  const percent = clampPercent(max > 0 ? (value / max) * 100 : 0);

  return (
    <div
      role="progressbar"
      aria-valuenow={value}
      aria-valuemin={0}
      aria-valuemax={max}
      className={cn("bg-muted h-2 w-full overflow-hidden", className)}
      {...props}
    >
      <div
        className={cn("h-full", fillToneClass[tone])}
        style={{ width: `${percent}%` }}
      />
    </div>
  );
}

export interface UsageMeterLabels {
  /** Left-aligned mono caption, e.g. "420 used". */
  primary?: React.ReactNode;
  /** Right-aligned mono caption, e.g. "1,000 included". */
  secondary?: React.ReactNode;
}

export interface UsageMeterProps {
  used: number;
  included: number;
  overageUsed?: number;
  overageLimit?: number;
  labels?: UsageMeterLabels;
  className?: string;
}

/**
 * Two-segment included/overage meter on a single track: an ink-filled
 * segment for usage within `included`, followed by a warning-filled segment
 * for usage beyond it.
 *
 * The track is scaled to `included + (overageLimit ?? overageUsed ?? 0)` so
 * the included region's width reflects its real share of total capacity —
 * when there's no `overageLimit`, the overage region's width scales to the
 * overage actually consumed (so it always renders full, since there's no
 * known ceiling to measure it against).
 */
export function UsageMeter({
  used,
  included,
  overageUsed = 0,
  overageLimit,
  labels,
  className,
}: UsageMeterProps): React.JSX.Element {
  const overageCapacity = overageLimit ?? overageUsed;
  const totalScale = included + overageCapacity;

  const includedFillPercent =
    totalScale > 0
      ? clampPercent((Math.min(used, included) / totalScale) * 100)
      : 0;
  const overageTrackPercent =
    totalScale > 0 ? clampPercent((overageCapacity / totalScale) * 100) : 0;
  const overageFillFraction =
    overageCapacity > 0
      ? clampPercent(
          (Math.min(overageUsed, overageCapacity) / overageCapacity) * 100,
        ) / 100
      : 0;
  const overageFillPercent = overageTrackPercent * overageFillFraction;

  return (
    <div className={cn("flex flex-col gap-1.5", className)}>
      {labels && (
        <div className="text-muted-foreground flex items-center justify-between font-mono text-xs tracking-[0.08em] uppercase">
          <span>{labels.primary}</span>
          <span>{labels.secondary}</span>
        </div>
      )}
      <div
        role="progressbar"
        aria-valuenow={used}
        aria-valuemin={0}
        aria-valuemax={included + (overageLimit ?? 0)}
        className="bg-muted flex h-2 w-full overflow-hidden"
      >
        <div
          className="bg-btn-primary h-full"
          style={{ width: `${includedFillPercent}%` }}
        />
        <div
          className="bg-warning-default h-full"
          style={{ width: `${overageFillPercent}%` }}
        />
      </div>
    </div>
  );
}
