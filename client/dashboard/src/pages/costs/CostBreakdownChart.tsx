import { type Dimension } from "@gram/client/models/components/queryfilter.js";
import { type QueryResult } from "@gram/client/models/components/queryresult.js";
import { useMemo } from "react";
import { unixNanoToMs } from "@/components/chart/chartUtils";
import {
  OTHER_STACK_LABEL,
  type TimeSeriesStack,
} from "@/components/stacked-time-series";
import { StackedTimeSeriesPanel } from "@/components/stacked-time-series-panel";
import { formatCompactDollars, formatCost } from "@/lib/money";
import { displayName, formatWorkUnits, isAttributionDim } from "./taxonomy";

// Compact work-units axis tick (e.g. "1.2K"), pairing with formatWorkUnits the
// way formatCompactDollars pairs with formatCost.
function formatCompactUnits(value: number): string {
  return value.toLocaleString(undefined, {
    notation: "compact",
    maximumFractionDigits: 1,
  });
}

// Stacked cost-over-time chart for the costs explorer: the shared time-series
// panel fed with the slice's per-group daily cost series — the same
// telemetry.query response that drives the breakdown table and its row
// sparklines, so the chart always stacks by the table's current axis. The
// weekly/monthly toggles give the period-over-period comparison read
// (DNO-279); drag-to-zoom comes with the panel.

// The chart keeps this many named series and rolls the rest into a rollup —
// the query returns up to 100 groups (the table wants them all) but a legend
// that long is unreadable. The server's own top-N rollup row, when present,
// ranks by its combined cost like any other series.
const MAX_CHART_STACKS = 7;

export function CostBreakdownChart({
  data,
  groupBy,
  serverRollupValue,
  efficiency,
  loading,
  isError,
  onSelectRange,
}: {
  // The main breakdown query's response — its timeseries carries one daily,
  // gap-filled cost series per group on a shared bucket grid.
  data: QueryResult | undefined;
  // The axis the slice is grouped by, for series labels and the header hint.
  groupBy: Dimension;
  // The groupValue of the server's synthetic top-N remainder row, when the
  // slice exceeded the query's topN. Its series folds into the chart's own
  // remainder bucket — two nested rollups would otherwise chart side by side
  // as separate gray "Other" stacks.
  serverRollupValue?: string;
  // The efficiency lens charts each group's work units over time instead of
  // its cost — matching the "Work units by X" table it sits above.
  efficiency?: boolean;
  loading: boolean;
  // The slice query failed — say so instead of the benign empty state, so the
  // chart agrees with the table's error message below it.
  isError?: boolean;
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
    // legend toggle hide both. The server's own remainder row never becomes a
    // named stack: it seeds the chart's rollup below.
    const byLabel = new Map<string, number[]>();
    let rollupSeed: number[] | null = null;
    for (const s of series) {
      const values = s.points.map((p) =>
        efficiency
          ? (p.measures.totalWorkUnits ?? 0)
          : (p.measures.totalCost ?? 0),
      );
      if (s.groupValue === serverRollupValue) {
        rollupSeed = values;
        continue;
      }
      const label = displayName(groupBy, s.groupValue);
      const merged = byLabel.get(label);
      if (merged) {
        values.forEach((v, i) => {
          merged[i] = (merged[i] ?? 0) + v;
        });
      } else {
        byLabel.set(label, values);
      }
    }
    // Rank by each stack's summed cost — a merge can lift a label far above
    // either raw value's own rank, so the arrival order (the table's per-raw-
    // value sort) is not a valid ranking once labels collide.
    const all = [...byLabel.entries()]
      .map(([label, values]) => ({
        label,
        series: values,
        total: values.reduce((sum, v) => sum + v, 0),
      }))
      .sort((a, b) => b.total - a.total)
      .map(({ label, series: values }) => ({ label, series: values }));
    // Skip the rollup when it would hold a single client series and there is
    // no server remainder to carry — an "Other" of one is noise.
    if (rollupSeed === null && all.length <= MAX_CHART_STACKS + 1) return all;
    const kept = all.slice(0, MAX_CHART_STACKS);
    const rest = all.slice(MAX_CHART_STACKS);
    // The rollup label steps aside from any real group that displays as
    // "Other" (same policy as the server's rollup); the `rollup` flag — not
    // the label — is what pins it to the neutral color.
    let rollupLabel = OTHER_STACK_LABEL;
    while (byLabel.has(rollupLabel)) rollupLabel += " (other)";
    return [
      ...kept,
      {
        label: rollupLabel,
        rollup: true,
        series: bucketsMs.map(
          (_, i) =>
            (rollupSeed?.[i] ?? 0) +
            rest.reduce((sum, s) => sum + (s.series[i] ?? 0), 0),
        ),
      },
    ];
  }, [data, groupBy, serverRollupValue, bucketsMs, efficiency]);

  if (efficiency) {
    return (
      <StackedTimeSeriesPanel
        title="Work Delivered Over Time"
        headerHint="Work delivered over time, stacked by the selected breakdown. Click or drag on the chart to zoom to a period."
        bucketsMs={bucketsMs}
        stacks={isError ? [] : stacks}
        formatValue={formatWorkUnits}
        formatAxisValue={formatCompactUnits}
        emptyMessage={
          isError
            ? "Failed to load cost data."
            : "No work analysis scores in this range."
        }
        loading={loading}
        onSelectRange={onSelectRange}
      />
    );
  }
  return (
    <StackedTimeSeriesPanel
      title="Cost Over Time"
      headerHint="Spend over time, stacked by the selected breakdown. Click or drag on the chart to zoom to a period."
      bucketsMs={bucketsMs}
      stacks={isError ? [] : stacks}
      formatValue={formatCost}
      formatAxisValue={formatCompactDollars}
      emptyMessage={
        isError ? "Failed to load cost data." : "No cost data in this range."
      }
      loading={loading}
      onSelectRange={onSelectRange}
    />
  );
}
