import { Maximize2, Minimize2 } from "lucide-react";
import type { ReactNode } from "react";

export function ChartCard({
  title,
  chartId,
  expandedChart,
  onExpand,
  children,
}: {
  title: string;
  chartId: string;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
  children: ReactNode;
}) {
  const isExpanded = expandedChart === chartId;
  return (
    <div className="border-border bg-card space-y-4 rounded-lg border p-4">
      <div className="flex items-center justify-between">
        <h3 className="text font-semibold">{title}</h3>
        <button
          onClick={() => onExpand(isExpanded ? null : chartId)}
          className="text-muted-foreground hover:text-foreground rounded p-0.5 transition-colors"
          aria-label={isExpanded ? "Minimize chart" : "Expand chart"}
        >
          {isExpanded ? (
            <Minimize2 className="size-4" />
          ) : (
            <Maximize2 className="size-4" />
          )}
        </button>
      </div>
      {children}
    </div>
  );
}
