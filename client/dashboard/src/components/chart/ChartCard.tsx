import { cn } from "@/lib/utils";
import { Maximize2, Minimize2 } from "lucide-react";
import type { ReactNode } from "react";

export function ChartCard({
  title,
  chartId,
  expandedChart,
  onExpand,
  hasData = true,
  children,
}: {
  title: string;
  chartId: string;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
  hasData?: boolean;
  children: ReactNode;
}) {
  const isExpanded = expandedChart === chartId;
  const showExpandButton = hasData || (isExpanded && !hasData);
  return (
    <div
      className={cn(
        "border-border bg-card space-y-4 rounded-lg border p-4 transition-all duration-200 ease-in-out",
        expandedChart && !isExpanded && "hidden",
      )}
    >
      <div className="flex items-center justify-between">
        <h3 className="text font-semibold">{title}</h3>
        {showExpandButton && (
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
        )}
      </div>
      {children}
    </div>
  );
}
