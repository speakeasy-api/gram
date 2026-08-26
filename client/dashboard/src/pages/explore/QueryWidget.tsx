import { ChartButton } from "@/components/chart/ChartButton";
import { ChartCard } from "@/components/chart/ChartCard";
import { Icon } from "@/components/ui/Icon";
import type { ExploreMetaResult } from "@gram/client/models/components/exploremetaresult.js";
import type { ExploreSavedQuery } from "@gram/client/models/components/exploresavedquery.js";
import { useMemo } from "react";
import {
  completeCalcs,
  isTimeseries,
  queryBodyFromSpec,
  specFromSavedQuery,
} from "./exploreModel";
import { ExploreResultsBody } from "./ExploreResults";
import { resultErrorMessage, useRunQuery } from "./useRunQuery";

/**
 * One dashboard widget: a saved query run live against its relative window,
 * rendered in its saved chart shape inside an expandable ChartCard.
 */
export function QueryWidget({
  query,
  meta,
  expandedChart,
  onExpand,
  onEdit,
  onDelete,
}: {
  query: ExploreSavedQuery;
  meta: ExploreMetaResult | undefined;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
  onEdit: (query: ExploreSavedQuery) => void;
  onDelete: (query: ExploreSavedQuery) => void;
}): JSX.Element {
  const spec = useMemo(() => specFromSavedQuery(query), [query]);
  const body = useMemo(
    () =>
      queryBodyFromSpec(
        spec,
        isTimeseries(spec.chartType) ? "timeseries" : "summary",
      ),
    [spec],
  );
  const result = useRunQuery(body, completeCalcs(spec.calculations).length > 0);

  const expanded = expandedChart === query.id;
  const errorMessage = resultErrorMessage(result.error);
  const hasData = (result.data?.rows.length ?? 0) > 0;

  return (
    <ChartCard
      title={query.name}
      chartId={query.id}
      hasData={hasData}
      loading={result.isPending}
      expandedChart={expandedChart}
      onExpand={onExpand}
      actions={
        <>
          <ChartButton
            onClick={() => onEdit(query)}
            ariaLabel={`Edit ${query.name} in Explore`}
          >
            <Icon name="pencil" />
          </ChartButton>
          <ChartButton
            onClick={() => onDelete(query)}
            ariaLabel={`Delete ${query.name}`}
          >
            <Icon name="trash-2" />
          </ChartButton>
        </>
      }
    >
      <ExploreResultsBody
        result={result.data}
        meta={meta}
        chartType={query.chartType}
        loading={false}
        errorMessage={errorMessage}
        height={expanded ? 420 : 260}
      />
    </ChartCard>
  );
}
