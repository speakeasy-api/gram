import { Icon } from "@/components/ui/Icon";
import { type IconName } from "@/components/ui/Icon/names";
import { MetricCard as UiMetricCard } from "@/components/ui/MetricCard";

/**
 * Lays MetricCards flush in one bordered strip with hairline dividers — the
 * prototype's stat-row idiom. Wrap every row of MetricCards in this (a single
 * card still gets its border from the group).
 */
export const MetricCardGroup = UiMetricCard.Group;
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { formatCompact } from "@/lib/format";
import { ThresholdConfig } from "./chartUtils";
import { Loader2 } from "lucide-react";
import { Link } from "react-router";

type AccentColor = "red" | "orange" | "yellow" | "green" | "blue" | "purple";

export type MetricCardProps = {
  title: string;
  value: number;
  /** Renders in place of the formatted value (e.g. "-" when not applicable). */
  displayValue?: string;
  previousValue?: number;
  format?: "compact" | "number" | "currency" | "percent" | "ms" | "seconds";
  icon?: IconName;
  /** Replaces the icon with a spinner while cached data refreshes in the background. */
  isRefreshing?: boolean;
  invertDelta?: boolean;
  thresholds?: ThresholdConfig;
  comparisonLabel?: string;
  accentColor?: AccentColor;
  subtext?: string;
  tooltip?: string;
  link?: string;
  linkText?: string;
  /**
   * Explicit value tone. Overrides the threshold heuristic — use for metrics
   * without thresholds so tiles don't sit in unconsidered black ink
   * (e.g. volume counts = "information", spend = "neutral").
   */
  tone?: Tone;
};

type Tone = "neutral" | "information" | "destructive" | "success" | "warning";

function getValueTone(value: number, thresholds?: ThresholdConfig): Tone {
  if (!thresholds) return "neutral";

  if (thresholds.inverted) {
    // Lower is better (e.g., latency)
    if (value > thresholds.red) return "destructive";
    if (value > thresholds.amber) return "warning";
    return "success";
  } else {
    // Higher is better (e.g., chats, resolution rate)
    if (value < thresholds.red) return "destructive";
    if (value < thresholds.amber) return "warning";
    return "success";
  }
}

export function MetricCard(props: MetricCardProps): JSX.Element {
  const {
    title,
    value,
    displayValue,
    previousValue = 0,
    format = "compact",
    icon,
    isRefreshing = false,
    invertDelta = false,
    thresholds,
    comparisonLabel,
    subtext,
    tooltip,
    link,
    linkText = "View",
    tone,
  } = props;
  const formatValue = (v: number) => {
    switch (format) {
      case "compact":
        return formatCompact(v);
      case "percent":
        return `${v.toFixed(1)}%`;
      case "ms":
        return `${v.toFixed(0)}ms`;
      case "seconds":
        if (v >= 60) {
          const mins = Math.floor(v / 60);
          const secs = Math.round(v % 60);
          return secs > 0 ? `${mins}m ${secs}s` : `${mins}m`;
        }
        return `${v.toFixed(1)}s`;
      case "currency":
        if (v >= 1) return `$${v.toFixed(2)}`;
        if (v >= 0.01) return `$${v.toFixed(3)}`;
        if (v > 0) return `$${v.toFixed(4)}`;
        return "$0.00";
      case "number":
      default:
        return v.toLocaleString();
    }
  };

  const rawDelta =
    previousValue > 0 ? ((value - previousValue) / previousValue) * 100 : 0;
  // Cap delta display at 999% to avoid absurd numbers
  const delta = Math.min(Math.abs(rawDelta), 999);
  const isPositive = rawDelta > 0;
  const isGood = invertDelta ? !isPositive : isPositive;
  const showDelta = previousValue > 0 && delta !== 0;

  const label = (
    <span className="inline-flex items-center gap-1.5">
      {title}
      {tooltip && (
        <SimpleTooltip tooltip={tooltip}>
          <button
            type="button"
            aria-label={`About ${title}`}
            className="text-muted-foreground hover:text-foreground inline-flex cursor-help items-center"
          >
            <Icon name="info" className="size-3" />
          </button>
        </SimpleTooltip>
      )}
      {icon &&
        (isRefreshing ? (
          <Loader2
            aria-label={`Refreshing ${title}`}
            className="text-muted-foreground size-3 animate-spin"
          />
        ) : (
          <Icon name={icon} className="text-muted-foreground size-3" />
        ))}
    </span>
  );

  const deltaNode = showDelta ? (
    <>
      {isPositive ? "+" : "-"}
      {delta.toFixed(1)}%
      {comparisonLabel && (
        <span className="text-muted"> {comparisonLabel}</span>
      )}
    </>
  ) : undefined;

  const description =
    subtext || link ? (
      <span className="flex items-center justify-between gap-2">
        <span className="text-muted-foreground">{subtext}</span>
        {link && (
          <Link
            to={link}
            aria-label={`View ${title}`}
            className="text-primary/70 hover:text-primary flex shrink-0 items-center gap-1 text-xs no-underline"
          >
            {linkText}
            <Icon name="arrow-right" />
          </Link>
        )}
      </span>
    ) : undefined;

  return (
    <UiMetricCard
      size="sm"
      label={label}
      value={displayValue ?? formatValue(value)}
      tone={tone ?? getValueTone(value, thresholds)}
      delta={deltaNode}
      deltaTone={isGood ? "success" : "destructive"}
      description={description}
    />
  );
}
