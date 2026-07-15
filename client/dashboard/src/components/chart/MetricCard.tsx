import { SimpleTooltip } from "@/components/ui/tooltip";
import {
  StatCard,
  StatTile,
  type StatTileDelta,
  type StatTileTone,
} from "@/components/ui/stat-tile";
import { formatCompact } from "@/lib/format";
import { cn } from "@/lib/utils";
import { getValueColor, ThresholdConfig } from "./chartUtils";
import { ArrowRight, Info } from "lucide-react";
import { Link } from "react-router";
import type { IconName } from "@/components/ui/dynamic-icon";

export type MetricCardProps = {
  title: string;
  value: number;
  /** Renders in place of the formatted value (e.g. "-" when not applicable). */
  displayValue?: string;
  previousValue?: number;
  format?: "compact" | "number" | "currency" | "percent" | "ms" | "seconds";
  /** @deprecated The brand KPI strip carries no per-tile icon chrome. */
  icon?: IconName;
  /** @deprecated The brand KPI strip carries no per-tile accent tint. */
  accentColor?: "red" | "orange" | "yellow" | "green" | "blue" | "purple";
  invertDelta?: boolean;
  thresholds?: ThresholdConfig;
  comparisonLabel?: string;
  subtext?: string;
  tooltip?: string;
  link?: string;
  linkText?: string;
  /** Draw the hairline-bordered tile. Off when the row is a connected strip. */
  bordered?: boolean;
  className?: string;
};

export function MetricCard(props: MetricCardProps): JSX.Element {
  const {
    title,
    value,
    displayValue,
    previousValue = 0,
    format = "compact",
    invertDelta = false,
    thresholds,
    comparisonLabel,
    subtext,
    tooltip,
    link,
    linkText = "View",
    bordered = true,
    className,
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

  // Threshold coloring is expressed as a text-color class; feed it through
  // StatTile's escape hatch so alarming values keep their tone.
  const valueColor = getValueColor(value, thresholds);
  const tone: StatTileTone = "default";

  const label = tooltip ? (
    <span className="inline-flex items-center gap-1.5">
      {title}
      <SimpleTooltip tooltip={tooltip}>
        <button
          type="button"
          aria-label={`About ${title}`}
          className="hover:text-foreground inline-flex cursor-help items-center"
        >
          <Info className="size-3" />
        </button>
      </SimpleTooltip>
    </span>
  ) : (
    title
  );

  const statDelta: StatTileDelta | undefined =
    previousValue > 0 && delta !== 0
      ? {
          value: `${isPositive ? "+" : "-"}${delta.toFixed(0)}%`,
          tone: isGood ? "positive" : "negative",
        }
      : undefined;

  const caption =
    link || subtext || comparisonLabel ? (
      <span className="flex items-center justify-between gap-2">
        <span>{subtext ?? comparisonLabel}</span>
        {link && (
          <Link
            to={link}
            aria-label={`View ${title}`}
            className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 whitespace-nowrap no-underline"
          >
            {linkText}
            <ArrowRight className="size-3.5" />
          </Link>
        )}
      </span>
    ) : undefined;

  const shared = {
    label,
    value: displayValue ?? formatValue(value),
    delta: statDelta,
    caption,
    tone,
    valueClassName: valueColor || undefined,
  };

  if (bordered) {
    return <StatCard {...shared} cardClassName={className} />;
  }
  return <StatTile {...shared} className={cn("p-5", className)} />;
}
