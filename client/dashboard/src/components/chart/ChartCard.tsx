import { Skeleton } from "@/components/ui/Skeleton";
import { Icon } from "@/components/ui/Icon";
import { cn } from "@/lib/utils";
import type { ReactNode } from "react";
import { Link } from "react-router";
import { ChartButton } from "./ChartButton";

export type ChartCardProps = {
  title: string;
  /** Renders the title as a link to the card's fuller page. */
  titleHref?: string;
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
  titleHref,
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
  // Always keep the button on an expanded card so its Minimize (collapse) escape
  // survives the card later entering loading/error; on a collapsed card, only
  // offer Expand once it has data and is neither loading nor erroring.
  const showExpandButton =
    expandable && (isExpanded || (!loading && !error && hasData));
  return (
    <div
      className={cn(
        "border-border bg-card border p-4 transition-all duration-200 ease-in-out",
        expandedChart && !isExpanded && "hidden",
      )}
    >
      <div className="mb-4 flex items-center justify-between">
        <h3 className="text-eyebrow">
          {titleHref ? (
            <Link
              to={titleHref}
              className="hover:text-foreground inline-flex items-center gap-1 no-underline hover:underline"
            >
              {title}
              <Icon name="arrow-up-right" className="size-3" />
            </Link>
          ) : (
            title
          )}
        </h3>
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
        <Skeleton className="h-[240px] w-full" />
      ) : error ? (
        <div
          role="alert"
          className="text-muted-foreground flex h-[240px] w-full flex-col items-center justify-center gap-2 text-sm"
        >
          <Icon name="triangle-alert" className="size-5" />
          <span>Couldn&apos;t load this data</span>
        </div>
      ) : (
        children
      )}
    </div>
  );
}
