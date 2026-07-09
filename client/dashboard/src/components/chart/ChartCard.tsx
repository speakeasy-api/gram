import { cn, Icon } from "@/components/ui/moonshine";
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
        "border-border bg-card rounded-lg border p-4 transition-all duration-200 ease-in-out",
        expandedChart && !isExpanded && "hidden",
      )}
    >
      <div className="mb-4 flex items-center justify-between">
        <h3 className="text font-semibold">{title}</h3>
        <div className="flex items-center gap-2">
          {isZoomed && onResetZoom && (
            <ChartButton onClick={onResetZoom} ariaLabel="Reset zoom">
              <Icon name="rotate-ccw" />
              Reset zoom
            </ChartButton>
          )}
          {showExpandButton && (
            <ChartButton
              onClick={() => onExpand(isExpanded ? null : chartId)}
              ariaLabel={isExpanded ? "Minimize chart" : "Expand chart"}
            >
              {isExpanded ? (
                <Icon name="minimize-2" />
              ) : (
                <Icon name="maximize-2" />
              )}
            </ChartButton>
          )}
        </div>
      </div>
      {children}
    </div>
  );
}
