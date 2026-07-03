import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { JSX } from "react";
import {
  RULE_ACTION_LABELS,
  RULE_STATUS_LABELS,
  SPEND_EVENT_TYPE_LABELS,
  formatUsd,
  type RuleAction,
  type RuleStatus,
  type RuleUsage,
  type SpendEventType,
} from "./budgets-data";

/** The `warning` token maps to feedback-orange-100 in light mode (a badge
 *  background tint), which washes out on fills and dots. Use the same
 *  feedback-palette tier the neighboring status colors sit at — destructive
 *  and success-foreground are the 700 shades in light mode and 500 in dark —
 *  so all three status colors carry the same weight. */
const WARNING_ORANGE_FILL =
  "bg-[var(--color-feedback-orange-700)] dark:bg-[var(--color-feedback-orange-500)]";
const WARNING_ORANGE_TEXT =
  "text-[var(--color-feedback-orange-700)] dark:text-[var(--color-feedback-orange-500)]";

/** Colored spend-vs-limit bar. Green below the rule's warn threshold, orange
 *  approaching, red over the limit. */
export function UsageBar({
  usage,
  limitUsd,
  warnAtPct = 80,
}: {
  usage: RuleUsage;
  limitUsd: number;
  /** Percent of budget where the bar turns orange. */
  warnAtPct?: number;
}): JSX.Element {
  const pct = Math.min(100, usage.utilization * 100);
  const over = usage.currentSpendUsd > limitUsd;
  const near = !over && usage.utilization >= warnAtPct / 100;
  const barColor = over
    ? "bg-destructive"
    : near
      ? WARNING_ORANGE_FILL
      : "bg-success-foreground";

  return (
    <div className="space-y-1">
      {/* One statement, not a fraction: the bar already shows the ratio. */}
      <div className="text-xs whitespace-nowrap">
        <span className={cn(over && "text-destructive font-medium")}>
          {formatUsd(usage.currentSpendUsd)}
        </span>
        <span className="text-muted-foreground"> of {formatUsd(limitUsd)}</span>
      </div>
      <div className="bg-muted h-2.5 w-full overflow-hidden rounded-full">
        <div
          className={cn("h-full rounded-full transition-all", barColor)}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}

/** Mirrors the security policy ActionBadge: Flag is quiet, Block is loud. */
export function RuleActionBadge({
  action,
}: {
  action: RuleAction;
}): JSX.Element {
  const variant = action === "block" ? "destructive" : "secondary";
  return <Badge variant={variant}>{RULE_ACTION_LABELS[action]}</Badge>;
}

const STATUS_COLOR: Record<RuleStatus, { dot: string; text: string }> = {
  healthy: { dot: "bg-success-foreground", text: "text-success-foreground" },
  approaching: { dot: WARNING_ORANGE_FILL, text: WARNING_ORANGE_TEXT },
  flagging: { dot: "bg-destructive", text: "text-destructive" },
  blocking: { dot: "bg-destructive", text: "text-destructive" },
};

/** Live lifecycle state of a rule: a colored dot plus colored text. No badge
 *  background, so it never competes with the Action badge next to it. Null
 *  status (disabled / unevaluable rule) renders a muted dash. */
export function RuleStatusBadge({
  status,
}: {
  status: RuleStatus | null;
}): JSX.Element {
  if (status === null) {
    return <span className="text-muted-foreground text-sm">—</span>;
  }
  const color = STATUS_COLOR[status];
  return (
    <span
      className={cn("inline-flex items-center gap-1.5 text-sm", color.text)}
    >
      <span className={cn("size-2.5 shrink-0 rounded-full", color.dot)} />
      {RULE_STATUS_LABELS[status]}
    </span>
  );
}

const EVENT_COLOR: Record<SpendEventType, { dot: string; text: string }> = {
  warning: { dot: WARNING_ORANGE_FILL, text: WARNING_ORANGE_TEXT },
  breach: { dot: "bg-destructive", text: "text-destructive" },
};

/** Lifecycle event label — same visual language as RuleStatusBadge: a colored
 *  dot plus colored text, never a solid badge background. */
export function EventTypeBadge({
  type,
}: {
  type: SpendEventType;
}): JSX.Element {
  const color = EVENT_COLOR[type];
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 text-sm whitespace-nowrap",
        color.text,
      )}
    >
      <span className={cn("size-2.5 shrink-0 rounded-full", color.dot)} />
      {SPEND_EVENT_TYPE_LABELS[type]}
    </span>
  );
}
