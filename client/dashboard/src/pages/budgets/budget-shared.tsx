import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { JSX } from "react";
import {
  BREACH_ACTION_LABELS,
  formatUsd,
  type BreachAction,
  type RuleUsage,
} from "./budgets-data";

/** Colored spend-vs-limit bar. Green under 80%, amber approaching, red over. */
export function UsageBar({
  usage,
  limitUsd,
}: {
  usage: RuleUsage;
  limitUsd: number;
}): JSX.Element {
  const pct = Math.min(100, usage.utilization * 100);
  const over = usage.currentSpendUsd > limitUsd;
  const near = !over && usage.utilization >= 0.8;
  const barColor = over
    ? "bg-destructive"
    : near
      ? "bg-yellow-500"
      : "bg-success-foreground";

  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between text-xs">
        <span className={cn(over && "text-destructive font-medium")}>
          {formatUsd(usage.currentSpendUsd)}
          <span className="text-muted-foreground">
            {" "}
            / {formatUsd(limitUsd)}
          </span>
        </span>
        <span className="text-muted-foreground">
          {Math.round(usage.utilization * 100)}%
        </span>
      </div>
      <div className="bg-muted h-2 w-full overflow-hidden rounded-full">
        <div
          className={cn("h-full rounded-full transition-all", barColor)}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}

export function BreachActionBadge({
  action,
}: {
  action: BreachAction;
}): JSX.Element {
  const variant =
    action === "block"
      ? "destructive"
      : action === "route_fallback"
        ? "outline"
        : "secondary";
  return <Badge variant={variant}>{BREACH_ACTION_LABELS[action]}</Badge>;
}
