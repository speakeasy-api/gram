import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { cn } from "@/lib/utils";
import { useExpandedChart } from "@/hooks/useExpandedChart";
import type { ExploreMetaResult } from "@gram/client/models/components/exploremetaresult.js";
import type { ExploreSavedQuery } from "@gram/client/models/components/exploresavedquery.js";
import { QueryWidget } from "./QueryWidget";

/**
 * The Explore dashboard tab: every saved query rendered as a live,
 * expandable widget. The grid is whatever the organization has saved from
 * the builder tab.
 */
export function ExploreDashboard({
  queries,
  loading,
  meta,
  onEdit,
  onDelete,
  onBuild,
}: {
  queries: ExploreSavedQuery[];
  loading: boolean;
  meta: ExploreMetaResult | undefined;
  onEdit: (query: ExploreSavedQuery) => void;
  onDelete: (query: ExploreSavedQuery) => void;
  onBuild: () => void;
}): JSX.Element {
  const { expandedChart, setExpandedChart } = useExpandedChart();

  if (loading) {
    return (
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Skeleton className="h-[320px]" />
        <Skeleton className="h-[320px]" />
      </div>
    );
  }

  if (queries.length === 0) {
    return (
      <InlineEmptyState
        icon="chart-column"
        heading="No saved charts yet"
        description="Build a query in Explore, then save it to this dashboard."
        action={<Button onClick={onBuild}>Build a query</Button>}
      />
    );
  }

  return (
    <div
      className={cn(
        "grid gap-4",
        expandedChart ? "grid-cols-1" : "grid-cols-1 lg:grid-cols-2",
      )}
    >
      {queries.map((query) => (
        <QueryWidget
          key={query.id}
          query={query}
          meta={meta}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
          onEdit={onEdit}
          onDelete={onDelete}
        />
      ))}
    </div>
  );
}
