import { type Dimension } from "@gram/client/models/components/queryfilter.js";
import { type QueryResult } from "@gram/client/models/components/queryresult.js";
import { useMemo } from "react";
import {
  OTHER_STACK_LABEL,
  type TimeSeriesStack,
  unixNanoToMs,
} from "@/components/stacked-time-series";
import { StackedTimeSeriesPanel } from "@/components/stacked-time-series-panel";
import { displayName, isAttributionDim } from "./taxonomy";

// Stacked cost-over-time chart for the costs explorer: the shared time-series
// panel fed with the slice's per-group daily cost series — the same
// telemetry.query response that drives the breakdown table and its row
// sparklines, so the chart always stacks by the table's current axis. The
// weekly/monthly toggles give the period-over-period comparison read
// (DNO-279); drag-to-zoom comes with the panel.

// The chart keeps this many named series and rolls the rest into "Other" —
// the query returns up to 100 groups (the table wants them all) but a legend
// that long is unreadable. The server's own top-N rollup row, when present,
// is last in the list and folds into the same client rollup.
const MAX_CHART_STACKS = 7;

const compactDollars = new Intl.NumberFormat("en-US", {
  notation: "compact",
  maximumFractionDigits: 1,
});

function formatCost(value: number): string {
  return `$${value.toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`;
}

export function CostBreakdownChart({
  data,
  groupBy,
  loading,
  onSelectRange,
}: {
  // The main breakdown query's response — its timeseries carries one daily,
  // gap-filled cost series per group on a shared bucket grid.
  data: QueryResult | undefined;
  // The axis the slice is grouped by, for series labels and the header hint.
  groupBy: Dimension;
  loading: boolean;
  // Narrows the page's date range to a clicked bar or dragged span.
  onSelectRange: (start: Date, end: Date) => void;
}): JSX.Element {
  // The axis grid: every series shares the same zero-filled buckets, so the
  // first series carries the full grid.
  const bucketsMs = useMemo(() => {
    const points = data?.timeseries?.[0]?.points ?? [];
    return points.map((p) => unixNanoToMs(p.bucketTimeUnixNano));
  }, [data]);

  const stacks = useMemo<TimeSeriesStack[]>(() => {
    // Attribution breakdowns hide the "" group — it's spend where the
    // attribute is not applicable ("not included"), not an "(unset)" slice —
    // matching the breakdown table's visible rows.
    const series = (data?.timeseries ?? []).filter(
      (s) => !(isAttributionDim(groupBy) && s.groupValue === ""),
    );
    // Distinct raw values can share one display name (e.g. two hook_source
    // spellings of the same agent). The panel keys series, legend toggles, and
    // colors by label, so same-label series merge into one stack — users
    // can't tell them apart anyway, and duplicate labels would make one
    // legend toggle hide both. Insertion order keeps the cost ranking.
    const byLabel = new Map<string, number[]>();
    for (const s of series) {
      const label = displayName(groupBy, s.groupValue);
      const values = s.points.map((p) => p.measures.totalCost ?? 0);
      const merged = byLabel.get(label);
      if (merged) {
        values.forEach((v, i) => {
          merged[i] = (merged[i] ?? 0) + v;
        });
      } else {
        byLabel.set(label, values);
      }
    }
    const all = [...byLabel.entries()].map(([label, values]) => ({
      label,
      series: values,
    }));
    // The series arrive ranked by cost (the table's sort), so keeping a
    // prefix keeps the biggest spenders. Skip the rollup when it would hold
    // a single series — an "Other" of one is noise.
    if (all.length <= MAX_CHART_STACKS + 1) return all;
    const kept = all.slice(0, MAX_CHART_STACKS);
    const rest = all.slice(MAX_CHART_STACKS);
    return [
      ...kept,
      {
        label: OTHER_STACK_LABEL,
        series: bucketsMs.map((_, i) =>
          rest.reduce((sum, s) => sum + (s.series[i] ?? 0), 0),
        ),
      },
    ];
  }, [data, groupBy, bucketsMs]);

  return (
    <StackedTimeSeriesPanel
      title="Cost Over Time"
      headerHint="Spend over time, stacked by the selected breakdown. Click or drag on the chart to zoom to a period."
      bucketsMs={bucketsMs}
      stacks={stacks}
      formatValue={formatCost}
      formatAxisValue={(value) => `$${compactDollars.format(value)}`}
      emptyMessage="No cost data in this range."
      loading={loading}
      onSelectRange={onSelectRange}
    />
  );
}
