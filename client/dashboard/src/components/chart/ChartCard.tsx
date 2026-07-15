import { cn } from "@/lib/utils";
import { Heading } from "@/components/ui/heading";
import { Maximize2, Minimize2, RotateCcw } from "lucide-react";
import type { ReactNode } from "react";
import { ChartButton } from "./ChartButton";

export type ChartCardProps = {
  title: string;
  chartId: string;
  hasData?: boolean;
  expandable?: boolean;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
  isZoomed?: boolean;
  onResetZoom?: () => void;
  children: ReactNode;
};

export function ChartCard({
  title,
  chartId,
  hasData = true,
  expandable = true,
  expandedChart,
  onExpand,
  isZoomed,
  onResetZoom,
  children,
}: ChartCardProps): ReactNode {
  const isExpanded = expandedChart === chartId;
  const showExpandButton = expandable && (hasData || isExpanded);
  return (
    <div
      className={cn(
        "border-border bg-card border p-4 transition-all duration-200 ease-in-out",
        expandedChart && !isExpanded && "hidden",
      )}
    >
      <div className="mb-4 flex items-center justify-between">
        <Heading variant="h4" className="leading-none normal-case">
          {title}
        </Heading>
        <div className="flex items-center gap-2">
          {isZoomed && onResetZoom && (
            <ChartButton onClick={onResetZoom} ariaLabel="Reset zoom">
              <RotateCcw className="size-4" />
              Reset zoom
            </ChartButton>
          )}
          {showExpandButton && (
            <ChartButton
              onClick={() => onExpand(isExpanded ? null : chartId)}
              ariaLabel={isExpanded ? "Minimize chart" : "Expand chart"}
            >
              {isExpanded ? (
                <Minimize2 className="size-4" />
              ) : (
                <Maximize2 className="size-4" />
              )}
            </ChartButton>
          )}
        </div>
      </div>
      {children}
    </div>
  );
}
