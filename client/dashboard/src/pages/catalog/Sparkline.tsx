import { cn } from "@/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";

interface SparklineProps {
  /** Array of values representing the last 4 weeks */
  data: number[];
  /** Height of the sparkline in pixels */
  height?: number;
  /** Width of individual bars */
  barWidth?: number;
  /** Gap between bars */
  gap?: number;
  /** Custom class name */
  className?: string;
  /** Whether to show tooltip with values */
  showTooltip?: boolean;
}

/**
 * 4-bar sparkline visualization for showing usage trends.
 *
 * Renders a mini bar chart showing the last 4 weeks of data.
 * Uses semantic colors:
 * - Upward trend: success-subtle green
 * - Flat/downward: muted gray
 */
export function Sparkline({
  data,
  height = 16,
  barWidth = 4,
  gap = 2,
  className,
  showTooltip = true,
}: SparklineProps) {
  // Ensure we have exactly 4 data points
  const normalizedData = [...data, 0, 0, 0, 0].slice(0, 4);

  // Calculate max for normalization
  const max = Math.max(...normalizedData, 1);

  // Determine if trend is upward (last value > average of first 3)
  const avgFirst3 = normalizedData.slice(0, 3).reduce((a, b) => a + b, 0) / 3;
  const isUptrend = normalizedData[3] > avgFirst3;

  // Calculate bar heights as percentages
  const bars = normalizedData.map((value) => ({
    value,
    heightPercent: Math.max((value / max) * 100, 8), // Min 8% for visibility
  }));

  const totalWidth = barWidth * 4 + gap * 3;

  const sparklineContent = (
    <div
      className={cn("inline-flex items-end", className)}
      style={{ height, width: totalWidth, gap }}
      aria-label={`Usage trend: ${normalizedData.join(", ")} users per week`}
      role="img"
    >
      {bars.map((bar, index) => (
        <div
          key={index}
          className={cn(
            "rounded-sm transition-all",
            isUptrend ? "bg-success/60" : "bg-muted-foreground/30",
            // Highlight the most recent bar
            index === 3 && isUptrend && "bg-success",
            index === 3 && !isUptrend && "bg-muted-foreground/50",
          )}
          style={{
            width: barWidth,
            height: `${bar.heightPercent}%`,
            minHeight: 2,
          }}
        />
      ))}
    </div>
  );

  if (!showTooltip) {
    return sparklineContent;
  }

  const weekLabels = ["4 weeks ago", "3 weeks ago", "2 weeks ago", "Last week"];

  return (
    <Tooltip>
      <TooltipTrigger asChild>{sparklineContent}</TooltipTrigger>
      <TooltipContent side="top" className="text-xs">
        <div className="space-y-0.5">
          {normalizedData.map((value, i) => (
            <div key={i} className="flex justify-between gap-4">
              <span className="text-muted-foreground">{weekLabels[i]}:</span>
              <span className="font-medium">{value.toLocaleString()}</span>
            </div>
          ))}
        </div>
      </TooltipContent>
    </Tooltip>
  );
}

/**
 * Placeholder shown when no usage data is available.
 */
export function SparklinePlaceholder({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        "inline-flex items-center px-1.5 py-0.5 rounded text-xs",
        "bg-muted text-muted-foreground",
        className,
      )}
    >
      New
    </span>
  );
}
