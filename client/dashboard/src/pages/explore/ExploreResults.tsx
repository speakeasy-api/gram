import { InlineEmptyState } from "@/components/inline-empty-state";
import { Skeleton } from "@/components/ui/Skeleton";
import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import type { AnalyticsQueryResult } from "@gram/client/models/components/analyticsqueryresult.js";
import type { UseQueryResult } from "@tanstack/react-query";
import type { JSX } from "react";
import {
  autoGrain,
  completeMeasures,
  hasChartShape,
  isRowsMode,
  queryDimensions,
  type ExploreSpec,
} from "./exploreModel";
import { CHART_HEIGHT, ResultChart } from "./ResultChart";
import { ResultNumbers } from "./ResultNumbers";
import { ResultRanked } from "./ResultRanked";
import { seriesFromRows, sharedUnit } from "./resultSeries";
import { ResultTable } from "./ResultTable";

export type RunQuery = UseQueryResult<AnalyticsQueryResult, Error>;

/**
 * The results panel under the builder: the one query the spec runs, drawn as
 * its chart. One generic empty state covers every reason there is nothing to
 * draw.
 */
export function ExploreResults({
  dataset,
  spec,
  result,
}: {
  dataset: AnalyticsDataset | undefined;
  /** The spec the last run answered, which the builder may have moved past. */
  spec: ExploreSpec;
  result: RunQuery;
}): JSX.Element {
  return (
    <section
      className="border-border bg-card flex flex-col gap-4 border p-5"
      aria-busy={result.isFetching}
    >
      <span className="text-eyebrow">Results</span>
      <ResultsBody dataset={dataset} spec={spec} primary={result} />
    </section>
  );
}

function ResultsBody({
  dataset,
  spec,
  primary,
}: {
  dataset: AnalyticsDataset | undefined;
  spec: ExploreSpec;
  primary: RunQuery;
}): JSX.Element {
  if (primary.isError) {
    return <QueryFailed error={primary.error} />;
  }
  if (primary.data === undefined) {
    return (
      <div style={{ height: CHART_HEIGHT }}>
        <Skeleton className="h-full w-full" />
      </div>
    );
  }
  if (primary.data.rows.length === 0) {
    return (
      <InlineEmptyState
        icon="telescope"
        heading="No rows to show"
        description="Nothing in this window matches the query."
      />
    );
  }
  if (hasChartShape(spec)) {
    return (
      <ChartOrReason dataset={dataset} spec={spec} rows={primary.data.rows} />
    );
  }
  // Nothing measured means rows, so a number spec that lost its last measure
  // tables what came back instead of drawing an empty group.
  if (
    spec.chartType === "number" &&
    completeMeasures(spec.measures).length > 0
  ) {
    return (
      <ResultNumbers dataset={dataset} spec={spec} row={primary.data.rows[0]} />
    );
  }
  if (spec.chartType === "ranked" && !isRowsMode(spec)) {
    return <ResultRanked spec={spec} rows={primary.data.rows} />;
  }
  return <ResultTable dataset={dataset} spec={spec} rows={primary.data.rows} />;
}

function ChartOrReason({
  dataset,
  spec,
  rows,
}: {
  dataset: AnalyticsDataset | undefined;
  spec: ExploreSpec;
  rows: AnalyticsQueryResult["rows"];
}): JSX.Element {
  const measures = completeMeasures(spec.measures);
  const unit = sharedUnit(dataset, measures);
  if (unit === null) {
    return (
      <InlineEmptyState
        icon="chart-line"
        heading="These measures do not share a unit"
        description="A chart needs one axis. Keep measures with one unit, or switch to a table."
      />
    );
  }
  const seriesSet = seriesFromRows(
    rows,
    queryDimensions(spec),
    measures,
    dataset,
  );
  return (
    <div className="flex flex-col gap-2">
      <ResultChart
        seriesSet={seriesSet}
        unit={unit}
        chartType={spec.chartType}
        grain={autoGrain(spec.window)}
      />
      {seriesSet.hidden > 0 ? (
        <p className="text-muted-foreground text-xs">
          Showing the {seriesSet.series.length} largest of{" "}
          {seriesSet.series.length + seriesSet.hidden} series.
        </p>
      ) : null}
    </div>
  );
}

// The server names what it refused and where in the request it sat, so its
// message is the description.
function QueryFailed({ error }: { error: Error | null }): JSX.Element {
  return (
    <InlineEmptyState
      icon="triangle-alert"
      heading="The query did not run"
      description={error?.message || "Something went wrong."}
    />
  );
}
