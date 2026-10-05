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
  return (
    <ResultDrawing
      dataset={dataset}
      spec={spec}
      rows={primary.data.rows}
      chartHeight={CHART_HEIGHT}
    />
  );
}

/**
 * A result that came back with rows, drawn as the spec's chart: the body
 * the Explore panel and a standalone widget share. Loading, failure and
 * empty results are the caller's, sized to wherever it sits.
 */
export function ResultDrawing({
  dataset,
  spec,
  rows,
  chartHeight,
}: {
  dataset: AnalyticsDataset | undefined;
  spec: ExploreSpec;
  rows: AnalyticsQueryResult["rows"];
  /** A timeseries chart's height; without one it fills its container. */
  chartHeight?: number;
}): JSX.Element {
  if (hasChartShape(spec)) {
    return (
      <ChartOrReason
        dataset={dataset}
        spec={spec}
        rows={rows}
        height={chartHeight}
      />
    );
  }
  // Nothing measured means rows, so a number spec that lost its last measure
  // tables what came back instead of drawing an empty group.
  if (
    spec.chartType === "number" &&
    completeMeasures(spec.measures).length > 0
  ) {
    return <ResultNumbers dataset={dataset} spec={spec} row={rows[0]} />;
  }
  if (spec.chartType === "ranked" && !isRowsMode(spec)) {
    return <ResultRanked spec={spec} rows={rows} />;
  }
  return <ResultTable dataset={dataset} spec={spec} rows={rows} />;
}

function ChartOrReason({
  dataset,
  spec,
  rows,
  height,
}: {
  dataset: AnalyticsDataset | undefined;
  spec: ExploreSpec;
  rows: AnalyticsQueryResult["rows"];
  height: number | undefined;
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
    <div
      className={
        height === undefined
          ? "flex h-full min-h-0 flex-col gap-2"
          : "flex flex-col gap-2"
      }
    >
      <div className={height === undefined ? "min-h-0 flex-1" : undefined}>
        <ResultChart
          seriesSet={seriesSet}
          unit={unit}
          chartType={spec.chartType}
          grain={autoGrain(spec.window)}
          height={height}
        />
      </div>
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
