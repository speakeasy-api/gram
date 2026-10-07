import { InlineEmptyState } from "@/components/inline-empty-state";
import { Skeleton } from "@/components/ui/Skeleton";
import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import type { AnalyticsQueryResult } from "@gram/client/models/components/analyticsqueryresult.js";
import type { UseQueryResult } from "@tanstack/react-query";
import type { JSX } from "react";
import {
  additiveOp,
  completeMeasures,
  drawnChart,
  hasChartShape,
  isRowsMode,
  isStacked,
  queryDimensions,
  specGrain,
  type ExploreSpec,
  type MeasureDraft,
} from "./exploreModel";
import { CHART_HEIGHT, ResultChart } from "./ResultChart";
import { ResultNumbers } from "./ResultNumbers";
import { ResultRanked } from "./ResultRanked";
import { seriesFromRows, sharedUnit, type SeriesSet } from "./resultSeries";
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
  onRangeSelect,
}: {
  dataset: AnalyticsDataset | undefined;
  spec: ExploreSpec;
  rows: AnalyticsQueryResult["rows"];
  /** A timeseries chart's height; without one it fills its container. */
  chartHeight?: number;
  /** Dragging across a timeseries selects that range. */
  onRangeSelect?: ((from: Date, to: Date) => void) | undefined;
}): JSX.Element {
  if (hasChartShape(spec)) {
    return (
      <ChartOrReason
        dataset={dataset}
        spec={spec}
        rows={rows}
        height={chartHeight}
        onRangeSelect={onRangeSelect}
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
  onRangeSelect,
}: {
  dataset: AnalyticsDataset | undefined;
  spec: ExploreSpec;
  rows: AnalyticsQueryResult["rows"];
  height: number | undefined;
  onRangeSelect: ((from: Date, to: Date) => void) | undefined;
}): JSX.Element {
  const measures = completeMeasures(spec.measures);
  // Checked before the unit, so what is offered is what lets the stack
  // draw: several measures of one unit still cannot stack.
  const stacking = isStacked(spec.chartType) ? stackProblem(measures) : null;
  if (stacking) {
    return (
      <InlineEmptyState
        icon="chart-line"
        heading={stacking.heading}
        description={stacking.description}
      />
    );
  }
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
  const drawn = drawnChart(spec);
  const seriesSet = seriesFromRows(
    rows,
    queryDimensions(spec),
    measures,
    dataset,
    isStacked(drawn),
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
          chartType={drawn}
          grain={specGrain(spec)}
          height={height}
          onRangeSelect={onRangeSelect}
        />
      </div>
      <ChartNote spec={spec} drawn={drawn} seriesSet={seriesSet} />
    </div>
  );
}

/**
 * Why a stack cannot draw these measures, or null when it can. A stack is
 * one measure's composition, and its bands and its Other band add up: a
 * second measure has no place in it, and an average or a percentile does
 * not add. The server refuses to save the same shapes.
 */
function stackProblem(
  measures: MeasureDraft[],
): { heading: string; description: string } | null {
  if (measures.length !== 1) {
    return {
      heading: "A stacked chart stacks one measure",
      description:
        "Keep one measure to see what made up its total, or switch to Line.",
    };
  }
  const [measure] = measures;
  if (measure && !additiveOp(measure.op)) {
    return {
      heading: "A stacked chart adds its bands up",
      description:
        "Stack a count or a sum, or switch to Line to compare the groups.",
    };
  }
  return null;
}

/**
 * The line under a chart that says what the drawing left out: a stack with
 * nothing to stack by drawn as the plain chart it is, series folded into
 * Other, or series dropped past the cap.
 */
function ChartNote({
  spec,
  drawn,
  seriesSet,
}: {
  spec: ExploreSpec;
  drawn: ExploreSpec["chartType"];
  seriesSet: SeriesSet;
}): JSX.Element | null {
  const note = chartNote(spec, drawn, seriesSet);
  return note === "" ? null : (
    <p className="text-muted-foreground text-xs">{note}</p>
  );
}

function chartNote(
  spec: ExploreSpec,
  drawn: ExploreSpec["chartType"],
  seriesSet: SeriesSet,
): string {
  if (drawn !== spec.chartType && isStacked(spec.chartType)) {
    return `Stacking needs a Group by, so this is drawn as ${drawn === "bar" ? "a bar" : "an area"} chart.`;
  }
  if (seriesSet.hidden === 0) return "";
  const shown = seriesSet.series.length;
  if (seriesSet.series.at(-1)?.other) {
    const named = shown - 1;
    return `Showing the ${named} largest of ${named + seriesSet.hidden} series; the other ${seriesSet.hidden} are stacked as Other.`;
  }
  return `Showing the ${shown} largest of ${shown + seriesSet.hidden} series.`;
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
