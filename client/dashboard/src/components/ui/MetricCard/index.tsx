import { cva, type VariantProps } from "class-variance-authority";
import type { HTMLAttributes, ReactNode } from "react";

import { cn } from "../lib/utils";

/**
 * MetricCard — a three-pane stat tile in the Risk Watchdog idiom:
 *
 *   ┌────────────────────────────┐
 *   │ ORG RISK SCORE             │  label pane: mono, uppercase, tracked
 *   │ 78 +6                      │  value pane: thin display serif + mono delta
 *   │ High — driven by 2 signals │  description pane: small muted sans
 *   └────────────────────────────┘
 *
 * Cards are usually laid side by side inside a `MetricCard.Group`, which
 * draws the shared outer border and the hairline dividers between cards.
 */

const valueTone = cva("font-display font-thin", {
  variants: {
    tone: {
      neutral: "text-highlight",
      information: "text-default-information",
      destructive: "text-default-destructive",
      success: "text-default-success",
      warning: "text-default-warning",
    },
    size: {
      // For a strip sharing a row rather than owning one: at half width the
      // sm figures crowd their labels and each other.
      xs: "text-[1.75rem] leading-[0.95]",
      sm: "text-[2.5rem] leading-[0.85]",
      md: "text-[3.625rem] leading-[0.82]",
      lg: "text-[4.75rem] leading-[0.8]",
    },
  },
  defaultVariants: {
    size: "md",
  },
});

const deltaTone = cva("font-mono text-xs leading-none", {
  variants: {
    tone: {
      neutral: "text-muted",
      information: "text-default-information",
      destructive: "text-default-destructive",
      success: "text-default-success",
      warning: "text-default-warning",
    },
  },
  defaultVariants: {
    tone: "destructive",
  },
});

type Tone = NonNullable<VariantProps<typeof valueTone>["tone"]>;

export interface MetricCardProps extends HTMLAttributes<HTMLDivElement> {
  /** Top pane: the metric name, rendered mono/uppercase/tracked. */
  label: ReactNode;
  /** Middle pane: the headline figure, rendered in the thin display serif. */
  value: ReactNode;
  /** Small mono annotation sitting beside the value (e.g. "+6", "2 crit"). */
  delta?: ReactNode;
  /** Bottom pane: one-line qualifier under the value. */
  description?: ReactNode;
  /**
   * Colors the value. Required — every metric declares how it should read
   * (neutral ink, or a semantic tone from a threshold/delta heuristic), so no
   * tile silently falls back to an unconsidered default.
   */
  tone: Tone;
  /** Colors the delta. Defaults to destructive — the accent that flags attention. */
  deltaTone?: Tone;
  size?: VariantProps<typeof valueTone>["size"];
}

export function MetricCard({
  label,
  value,
  delta,
  description,
  tone,
  deltaTone: deltaToneProp,
  size,
  className,
  ...rest
}: MetricCardProps): JSX.Element {
  return (
    <div
      className={cn(
        "bg-card flex min-w-0 flex-1 flex-col gap-4 p-6",
        className,
      )}
      {...rest}
    >
      <div className="text-eyebrow">{label}</div>
      <div className="flex flex-1 items-baseline gap-2">
        <span className={valueTone({ tone, size })}>{value}</span>
        {delta != null && (
          <span className={deltaTone({ tone: deltaToneProp })}>{delta}</span>
        )}
      </div>
      {description != null && (
        <div className="text-default text-[13px] leading-snug">
          {description}
        </div>
      )}
    </div>
  );
}

export interface MetricCardGroupProps extends HTMLAttributes<HTMLDivElement> {
  children: ReactNode;
}

/**
 * Lays MetricCards side by side in one bordered strip, separated by hairline
 * dividers — the group owns the border, the cards stay borderless.
 */
function MetricCardGroup({
  children,
  className,
  ...rest
}: MetricCardGroupProps): JSX.Element {
  return (
    <div
      className={cn("bg-card divide-border flex divide-x border", className)}
      {...rest}
    >
      {children}
    </div>
  );
}

MetricCard.Group = MetricCardGroup;
