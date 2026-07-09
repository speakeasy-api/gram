// The promoted horizontal stacked bar chart for the chart system — the same
// shape as the internal `StackedBarChart` in
// src/components/observe/InsightsTools.tsx (around its `stackTotalPlugin`),
// generalized to any label/series input and restyled from ./chart-theme.
import { LoadMoreButton } from "@/components/ui/load-more-footer";
import { Skeleton } from "@/components/ui/skeleton";
import type {
  Chart as ChartJSInstance,
  ChartOptions,
  Plugin,
  TooltipItem,
} from "chart.js";
import { useMemo } from "react";
import { Bar } from "react-chartjs-2";
import { ChartLegend, ChartNoData, type ChartLegendEntry } from "./Timeseries";
import {
  chartGrid,
  chartTicks,
  chartTooltip,
  monoFontStack,
  registerChartJs,
  seriesPalette,
  tickColor,
  withAlpha,
} from "./chart-theme";

registerChartJs();

export type StackedBarSeries = {
  label: string;
  /** Overrides the palette color assigned by series index. */
  color?: string;
  /** Values aligned 1:1 with the chart's `labels` prop. */
  values: Array<number | null>;
};

export type StackedBarChartProps = {
  labels: string[];
  series: StackedBarSeries[];
  height?: number;
  valueFormatter?: (value: number) => string;
  /** Draws the per-row stack total at the end of each bar. */
  showTotals?: boolean;
  /** Caps the visible rows, revealing the rest via a "Show N more" control. */
  maxRows?: number;
  expanded?: boolean;
  onShowAll?: () => void;
  onBarClick?: (seriesLabel: string, rowLabel: string) => void;
  loading?: boolean;
  emptyMessage?: string;
};

const ROW_HEIGHT = 22;
const ROW_SPACING = 10;
const MIN_HEIGHT = 120;

function hideZeroSegments(values: Array<number | null>): Array<number | null> {
  return values.map((value) => (value === 0 ? null : value));
}

function buildStackTotalPlugin(textColor: string, font: string): Plugin<"bar"> {
  return {
    id: "stackTotal",
    afterDatasetsDraw(chart: ChartJSInstance) {
      const { ctx, data } = chart;
      ctx.save();
      ctx.font = font;
      ctx.fillStyle = textColor;
      ctx.textAlign = "left";
      ctx.textBaseline = "middle";
      for (let i = 0; i < (data.labels?.length ?? 0); i++) {
        let total = 0;
        let labelX: number | null = null;
        let labelY: number | null = null;

        data.datasets.forEach((dataset, datasetIndex) => {
          const value = dataset.data[i];
          if (typeof value !== "number" || value === 0) return;
          total += value;
          const bar = chart.getDatasetMeta(datasetIndex).data[i];
          if (!bar || typeof bar.x !== "number" || typeof bar.y !== "number")
            return;
          if (labelX === null || bar.x > labelX) {
            labelX = bar.x;
            labelY = bar.y;
          }
        });

        if (total > 0 && labelX !== null && labelY !== null) {
          ctx.fillText(String(total), labelX + 4, labelY);
        }
      }
      ctx.restore();
    },
  };
}

export function StackedBarChart({
  labels,
  series,
  height,
  valueFormatter,
  showTotals = false,
  maxRows,
  expanded = false,
  onShowAll,
  onBarClick,
  loading = false,
  emptyMessage,
}: StackedBarChartProps): JSX.Element {
  const hasData = labels.length > 0 && series.length > 0;

  const hiddenCount =
    !expanded && maxRows && labels.length > maxRows
      ? labels.length - maxRows
      : 0;
  const visibleLabels = hiddenCount > 0 ? labels.slice(0, maxRows) : labels;

  const { datasets, legendEntries } = useMemo(() => {
    const palette = seriesPalette();
    const visibleSeries =
      hiddenCount > 0
        ? series.map((s) => ({ ...s, values: s.values.slice(0, maxRows) }))
        : series;

    const datasets = visibleSeries.map((s, i) => {
      const color = s.color ?? palette[i % palette.length]!;
      return {
        label: s.label,
        data: hideZeroSegments(s.values),
        backgroundColor: withAlpha(color, 0.78),
        hoverBackgroundColor: color,
        borderRadius: 0,
        borderSkipped: false as const,
        stack: "stack",
      };
    });

    const legendEntries: ChartLegendEntry[] = visibleSeries.map((s, i) => ({
      label: s.label,
      color: s.color ?? palette[i % palette.length]!,
    }));

    return { datasets, legendEntries };
  }, [series, hiddenCount, maxRows]);

  const containerHeight =
    height ??
    Math.max(
      MIN_HEIGHT,
      visibleLabels.length * (ROW_HEIGHT + ROW_SPACING) + 40,
    );

  const stackTotalPlugin = useMemo(
    () => buildStackTotalPlugin(tickColor(), `12px ${monoFontStack()}`),
    [],
  );

  const options = useMemo<ChartOptions<"bar">>(
    () => ({
      indexAxis: "y",
      responsive: true,
      maintainAspectRatio: false,
      transitions: { resize: { animation: { duration: 0 } } },
      onClick(_event, elements) {
        if (!elements.length || !onBarClick) return;
        const { datasetIndex, index } = elements[0]!;
        const seriesLabel = datasets[datasetIndex]?.label;
        const rowLabel = visibleLabels[index];
        if (seriesLabel && rowLabel) onBarClick(seriesLabel, rowLabel);
      },
      onHover(event, elements) {
        const el = event.native?.target as HTMLElement | null;
        if (el) el.style.cursor = elements.length ? "pointer" : "default";
      },
      plugins: {
        legend: { display: false },
        tooltip: {
          ...chartTooltip(),
          callbacks: {
            label: (item: TooltipItem<"bar">) => {
              const value = item.parsed.x;
              if (value == null) return "";
              const formatted = valueFormatter
                ? valueFormatter(value)
                : String(value);
              return ` ${item.dataset.label}: ${formatted}`;
            },
          },
        },
      },
      scales: {
        x: {
          stacked: true,
          grid: chartGrid(),
          ticks: { ...chartTicks(), precision: 0 },
        },
        y: {
          stacked: true,
          grid: { display: false },
          ticks: {
            ...chartTicks(),
            crossAlign: "far" as const,
            callback(value) {
              const label = this.getLabelForValue(value as number);
              return label.length > 18 ? `${label.slice(0, 18)}…` : label;
            },
          },
        },
      },
    }),
    [datasets, visibleLabels, onBarClick, valueFormatter],
  );

  if (loading) {
    return <Skeleton className="w-full" style={{ height: containerHeight }} />;
  }

  if (!hasData) {
    return <ChartNoData message={emptyMessage} height={containerHeight} />;
  }

  return (
    <div className="w-full">
      <div style={{ height: containerHeight }}>
        <Bar
          plugins={showTotals ? [stackTotalPlugin] : []}
          data={{ labels: visibleLabels, datasets }}
          options={options}
        />
      </div>
      {hiddenCount > 0 && onShowAll && (
        <LoadMoreButton
          hasMore
          onLoadMore={onShowAll}
          label={`Show ${hiddenCount} more`}
          className="justify-start py-0 pt-2"
        />
      )}
      {legendEntries.length > 1 && <ChartLegend entries={legendEntries} />}
    </div>
  );
}
