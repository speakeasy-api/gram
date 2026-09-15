import { InlineEmptyState } from "@/components/inline-empty-state";
import { Skeleton, SkeletonTable } from "@/components/ui/Skeleton";
import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import type { AnalyticsQueryResult } from "@gram/client/models/components/analyticsqueryresult.js";
import type { UseQueryResult } from "@tanstack/react-query";
import type { JSX } from "react";
import {
  autoGrain,
  completeMeasures,
  hasChartShape,
  queryDimensions,
  type ExploreSpec,
} from "./exploreModel";
import { CHART_HEIGHT, ResultChart } from "./ResultChart";
import { ResultNumbers } from "./ResultNumbers";
import { seriesFromRows, sharedUnit } from "./resultSeries";
import { ResultTable } from "./ResultTable";

export type RunQuery = UseQueryResult<AnalyticsQueryResult, Error>;

/**
 * The results panel under the builder. A timeseries chart draws the chart
 * query with the summary query tabled beneath it; a table or number chart
 * draws the summary query alone. One generic empty state covers every reason
 * there is nothing to draw.
 */
export function ExploreResults({
  dataset,
  spec,
  chart,
  summary,
}: {
  dataset: AnalyticsDataset | undefined;
  /** The spec the queries answer, which trails the builder by the debounce. */
  spec: ExploreSpec;
  chart: RunQuery;
  summary: RunQuery;
}): JSX.Element {
  const drawsChart = hasChartShape(spec);
  const primary = drawsChart ? chart : summary;
  // Refining an existing result keeps it on screen; only the first result
  // for a query shape blanks the panel.
  const refining = primary.isFetching && primary.data !== undefined;

  return (
    <section
      className="border-border bg-card flex flex-col gap-4 border p-5"
      aria-busy={primary.isFetching}
    >
      <div className="flex items-center justify-between gap-4">
        <span className="text-eyebrow">Results</span>
        <span className="text-muted-foreground flex items-center gap-3 font-mono text-xs">
          {refining ? <span>refining…</span> : null}
          <span>{spec.dataset}</span>
        </span>
      </div>
      <ResultsBody
        dataset={dataset}
        spec={spec}
        primary={primary}
        summary={summary}
        drawsChart={drawsChart}
      />
    </section>
  );
}

function ResultsBody({
  dataset,
  spec,
  primary,
  summary,
  drawsChart,
}: {
  dataset: AnalyticsDataset | undefined;
  spec: ExploreSpec;
  primary: RunQuery;
  summary: RunQuery;
  drawsChart: boolean;
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
  if (drawsChart) {
    return (
      <>
        <ChartOrReason dataset={dataset} spec={spec} rows={primary.data.rows} />
        <SummarySection dataset={dataset} spec={spec} summary={summary} />
      </>
    );
  }
  if (spec.chartType === "number") {
    return (
      <ResultNumbers dataset={dataset} spec={spec} row={primary.data.rows[0]} />
    );
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

// The table under a chart answers the same question over the whole window,
// with the builder's order and limit, so it loads and fails on its own.
function SummarySection({
  dataset,
  spec,
  summary,
}: {
  dataset: AnalyticsDataset | undefined;
  spec: ExploreSpec;
  summary: RunQuery;
}): JSX.Element {
  let body: JSX.Element;
  if (summary.isError) {
    body = <QueryFailed error={summary.error} />;
  } else if (summary.data === undefined) {
    body = <SkeletonTable />;
  } else {
    body = (
      <ResultTable dataset={dataset} spec={spec} rows={summary.data.rows} />
    );
  }
  return (
    <div className="flex flex-col gap-2">
      <span className="text-eyebrow">Summary</span>
      {body}
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
