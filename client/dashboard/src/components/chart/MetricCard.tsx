import { cn, Icon, type IconName } from "@speakeasy-api/moonshine";

type AccentColor = "red" | "orange" | "yellow" | "green" | "blue" | "purple";

export type MetricCardProps = {
  title: string;
  value: number;
  previousValue?: number;
  format?: "number" | "percent" | "ms" | "seconds";
  icon?: IconName;
  invertDelta?: boolean;
  thresholds?: ThresholdConfig;
  comparisonLabel?: string;
  accentColor?: AccentColor;
  subtext?: string;
};

export type ThresholdConfig = {
  red: number;
  amber: number;
  inverted?: boolean; // true if lower is better (like latency)
};

export function getValueColor(
  value: number,
  thresholds?: ThresholdConfig,
): string {
  if (!thresholds) return "";

  if (thresholds.inverted) {
    // Lower is better (e.g., latency)
    if (value > thresholds.red) return "text-red-500";
    if (value > thresholds.amber) return "text-amber-500";
    return "text-emerald-600";
  } else {
    // Higher is better (e.g., chats, resolution rate)
    if (value < thresholds.red) return "text-red-500";
    if (value < thresholds.amber) return "text-amber-500";
    return "text-emerald-600";
  }
}

const accentColorsMap: Record<AccentColor, string> = {
  red: "border-t-red-500",
  orange: "border-t-orange-500",
  yellow: "border-t-yellow-500",
  green: "border-t-green-500",
  blue: "border-t-blue-500",
  purple: "border-t-purple-500",
};

export function MetricCard({
  title,
  value,
  previousValue = 0,
  format = "number",
  icon,
  invertDelta = false,
  thresholds,
  comparisonLabel,
  accentColor,
  subtext,
}: MetricCardProps) {
  const formatValue = (v: number) => {
    switch (format) {
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

  const valueColor = getValueColor(value, thresholds);

  const accentColorClass = accentColor ? accentColorsMap[accentColor] : null;

  return (
    <div
      className={cn(
        "bg-card rounded-lg border p-5",
        accentColor ? `border-t-3 ${accentColorClass}` : "border-border",
      )}
    >
      <div className="mb-3 flex items-center justify-between">
        <span className="text-sm font-semibold">{title}</span>
        {icon && (
          <div className="bg-muted/50 rounded-lg p-2">
            <Icon name={icon} className="text-muted-foreground size-4" />
          </div>
        )}
      </div>
      <div className="flex items-end justify-between">
        <span className={`text-3xl font-semibold tracking-tight ${valueColor}`}>
          {formatValue(value)}
        </span>
        {previousValue > 0 && delta !== 0 && (
          <div className="flex flex-col items-end gap-0.5">
            <div
              className={`flex items-center gap-1 text-xs font-medium ${
                isGood ? "text-emerald-600" : "text-red-500"
              }`}
            >
              <Icon
                name={isPositive ? "trending-up" : "trending-down"}
                className="size-3"
              />
              <span>{delta.toFixed(1)}%</span>
            </div>
            {comparisonLabel && (
              <span className="text-muted-foreground text-[10px]">
                {comparisonLabel}
              </span>
            )}
          </div>
        )}
      </div>
      {subtext && (
        <span className="text-muted-foreground mt-1 block text-xs">
          {subtext}
        </span>
      )}
    </div>
  );
}
