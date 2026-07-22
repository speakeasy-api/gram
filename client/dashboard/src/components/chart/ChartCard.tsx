import { Skeleton } from "@/components/ui/skeleton";
import { cn, Icon } from "@speakeasy-api/moonshine";
import type { ReactNode } from "react";
import { ChartButton } from "./ChartButton";

export type ChartCardProps = {
  title: string;
  chartId: string;
  hasData?: boolean;
  expandable?: boolean;
  /**
   * When true the card renders a skeleton in place of its body. Lets a panel
   * show its own loading state so dashboards can render each card as its data
   * arrives instead of blocking on the slowest query.
   */
  loading?: boolean;
  /**
   * When true the card renders an error state in place of its body. Symmetric
   * with `loading` so a panel whose own query failed reads as failed rather
   * than as empty ("no data"). Ignored while `loading` is true.
   */
  error?: boolean;
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
  loading = false,
  error = false,
  expandedChart,
  onExpand,
  isZoomed,
  onResetZoom,
  children,
}: ChartCardProps): ReactNode {
  const isExpanded = expandedChart === chartId;
  const showExpandButton =
    expandable && !loading && !error && (hasData || isExpanded);
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
      {loading ? (
        <Skeleton className="h-[240px] w-full rounded-md" />
      ) : error ? (
        <div className="text-muted-foreground flex h-[240px] w-full flex-col items-center justify-center gap-2 text-sm">
          <Icon name="triangle-alert" className="size-5" />
          <span>Couldn&apos;t load this data</span>
        </div>
      ) : (
        children
      )}
    </div>
  );
}
