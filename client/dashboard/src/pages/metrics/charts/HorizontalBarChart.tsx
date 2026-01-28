import { cn } from "@/lib/utils";
import { formatNumber } from "./utils";

export interface Bar {
  label: string;
  value: number;
  /** Percentage width relative to the largest bar (0–100). */
  pct: number;
  /** Tailwind background class (e.g. "bg-chart-4"). */
  colorClass: string;
}

interface HorizontalBarChartProps {
  bars: Bar[];
  /** Width class for the label column. Defaults to "w-[140px]". */
  labelWidth?: string;
}

export function HorizontalBarChart({
  bars,
  labelWidth = "w-[140px]",
}: HorizontalBarChartProps) {
  return (
    <div className="flex flex-col gap-2">
      {bars.map((bar) => (
        <div key={bar.label} className="group flex items-center gap-3">
          <span
            className={cn(
              "text-xs text-muted-foreground truncate text-right shrink-0",
              labelWidth,
            )}
          >
            {bar.label}
          </span>
          <div className="flex-1 h-6 rounded bg-muted/30 relative overflow-hidden">
            <div
              className={cn(
                "h-full rounded transition-opacity group-hover:opacity-100 opacity-80",
                bar.colorClass,
              )}
              style={{ width: `${bar.pct}%` }}
            />
          </div>
          <span className="text-xs text-muted-foreground w-12 tabular-nums shrink-0">
            {formatNumber(bar.value)}
          </span>
        </div>
      ))}
    </div>
  );
}
