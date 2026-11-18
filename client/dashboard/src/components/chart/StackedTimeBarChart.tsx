import { ChartNoData } from "@/components/chart/ChartNoData";
import {
  chartColors,
  EXPANDED_LEGEND,
  SHARED_LEGEND,
  SHARED_RESIZE_TRANSITION,
  SHARED_TOOLTIP,
} from "@/components/chart/chartTheme";
import { useChartZoom } from "@/components/chart/useChartZoom";
import type { TimeSeriesDataset } from "@/components/observe/toolUsageTimeSeriesChartData";
import { TOOLTIP } from "@/components/chart/palette";
import type { Chart, ChartOptions, TooltipModel } from "chart.js";
import { useIsDarkTheme } from "@/lib/theme";
import { useEffect, useMemo } from "react";
import { Bar } from "react-chartjs-2";

const STRIP_TOOLTIP_ID = "stacked-time-bar-strip-tooltip";
const STRIP_TOOLTIP_GAP = 8;

/**
 * Chart.js `external` tooltip for the compact strip: a fixed-position element
 * on the body, so it is not clipped by the canvas or by any overflow on the
 * panels around it. Sits above the pointer, or below it when the viewport
 * has no room above.
 */
function renderStripTooltip({
  chart,
  tooltip,
}: {
  chart: Chart;
  tooltip: TooltipModel<"bar">;
}): void {
  let el = document.getElementById(STRIP_TOOLTIP_ID);
  if (!el) {
    el = document.createElement("div");
    el.id = STRIP_TOOLTIP_ID;
    el.setAttribute("role", "tooltip");
    Object.assign(el.style, {
      position: "fixed",
      zIndex: "50",
      pointerEvents: "none",
      padding: "6px 8px",
      fontSize: "11px",
      lineHeight: "1.4",
      whiteSpace: "nowrap",
      background: TOOLTIP.backgroundColor,
      color: TOOLTIP.bodyColor,
      border: `${TOOLTIP.borderWidth}px solid ${TOOLTIP.borderColor}`,
    });
    document.body.appendChild(el);
    // Fixed to the viewport, so any scroll would leave it behind the strip.
    // Capture, because the page scrolls inside containers, not the window.
    window.addEventListener("scroll", hideStripTooltip, {
      capture: true,
      passive: true,
    });
  }

  if (tooltip.opacity === 0 || tooltip.dataPoints.length === 0) {
    el.style.opacity = "0";
    return;
  }

  // textContent only: labels come from data and must not be parsed as HTML.
  el.replaceChildren();
  for (const line of tooltip.title) {
    const title = document.createElement("div");
    title.textContent = line;
    Object.assign(title.style, {
      color: TOOLTIP.titleColor,
      fontWeight: "600",
      marginBottom: "2px",
    });
    el.appendChild(title);
  }
  for (const line of [
    ...tooltip.body.flatMap((item) => item.lines),
    ...tooltip.afterBody,
  ]) {
    if (!line) continue;
    const row = document.createElement("div");
    row.textContent = line;
    el.appendChild(row);
  }

  const canvas = chart.canvas.getBoundingClientRect();
  const { width, height } = el.getBoundingClientRect();
  const x = canvas.left + tooltip.caretX;
  const left = Math.min(
    Math.max(STRIP_TOOLTIP_GAP, x - width / 2),
    window.innerWidth - width - STRIP_TOOLTIP_GAP,
  );
  const above = canvas.top - height - STRIP_TOOLTIP_GAP;
  const top =
    above >= STRIP_TOOLTIP_GAP
      ? above
      : Math.min(
          canvas.bottom + STRIP_TOOLTIP_GAP,
          window.innerHeight - height - STRIP_TOOLTIP_GAP,
        );
  el.style.left = `${left}px`;
  el.style.top = `${top}px`;
  el.style.opacity = "1";
}

function hideStripTooltip(): void {
  const el = document.getElementById(STRIP_TOOLTIP_ID);
  if (el) el.style.opacity = "0";
}

export function StackedTimeBarChart({
  labels,
  timestamps,
  bucketMs,
  tooltipLabels,
  datasets,
  tooltipAfterBody,
  onRangeSelect,
  height = 200,
  expanded = false,
  compact = false,
}: {
  labels: string[];
  timestamps: number[];
  bucketMs: number;
  tooltipLabels: string[];
  datasets: TimeSeriesDataset[];
  tooltipAfterBody?: (dataIndex: number) => string[];
  onRangeSelect?: (from: Date, to: Date) => void;
  height?: number;
  expanded?: boolean;
  /** Strip variant: no legend, no axis furniture — just the silhouette. */
  compact?: boolean;
}): JSX.Element {
  const colors = chartColors(useIsDarkTheme());
  const { chartRef, zoomPluginOptions, resetZoom } = useChartZoom<"bar">({
    onRangeSelect,
    resolveRange: (min, max) => {
      if (timestamps.length === 0) return null;
      const fromIndex = Math.max(0, Math.floor(min));
      const toIndex = Math.min(timestamps.length - 1, Math.ceil(max));
      const from = timestamps[fromIndex];
      const to = timestamps[toIndex];
      if (from == null || to == null) return null;
      // `to` is a bucket start; extend by the bucket width so the selection
      // covers the last bucket's events.
      return { from: new Date(from), to: new Date(to + bucketMs) };
    },
  });

  // Callers restyle datasets on hover, which rebuilds the array without
  // changing a single number. Keying the reset on the array identity would
  // throw away the reader's zoom every time the pointer moved, so key it on
  // the data itself.
  const dataSignature = useMemo(
    () => JSON.stringify(datasets.map((dataset) => dataset.data)),
    [datasets],
  );

  useEffect(() => {
    resetZoom();
  }, [dataSignature, resetZoom]);

  // The strip tooltip lives on the body, so it outlives the chart unless
  // hidden when the chart goes away mid-hover — including when it swaps to
  // the no-data state, which unmounts the Bar.
  const hasData = labels.length > 0 && datasets.length > 0;
  useEffect(() => (compact ? hideStripTooltip : undefined), [compact, hasData]);

  // Chart.js resolves bar sizing from the dataset, not from the scale, so the
  // strip's near-continuous bars have to be set here.
  const sizedDatasets = useMemo(
    () =>
      compact
        ? datasets.map((dataset) => ({
            ...dataset,
            barPercentage: 0.92,
            categoryPercentage: 0.98,
          }))
        : datasets,
    [datasets, compact],
  );

  // Datasets too, not just labels: a window whose every bucket is zero still
  // produces a full axis, and the series are dropped for being empty — which
  // would draw a labelled but entirely blank chart.
  if (labels.length === 0 || datasets.length === 0) {
    // Inside the caller's height: the compact strip is 76-88px and ChartNoData
    // is a fixed 96, so letting it size itself makes the panel jump taller the
    // moment a filter empties it.
    return (
      <div className="flex items-center justify-center" style={{ height }}>
        <ChartNoData />
      </div>
    );
  }

  const options: ChartOptions<"bar"> = {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: "index", intersect: false },
    plugins: {
      legend: expanded && !compact ? EXPANDED_LEGEND : SHARED_LEGEND,
      tooltip: {
        ...SHARED_TOOLTIP,
        // A tooltip is drawn inside the canvas, so on a 76px strip a
        // three-line box has nowhere to sit and ends up over the bars it is
        // describing. Compact keeps it above the cursor and small enough to
        // clear them.
        // Anchored to the bar the pointer is nearest and centred beside it,
        // rather than floating above the middle of the stack: a tall tooltip
        // over a short bar covers the chart it describes. xAlign is left to
        // Chart.js so the box flips to whichever side has room near the edges.
        position: "nearest" as const,
        yAlign: "center" as const,
        caretPadding: 8,
        // The strip is shorter than its own tooltip, and a canvas tooltip is
        // clipped at the canvas edge. Compact draws it as HTML on the page
        // instead, so it can hang outside the strip.
        ...(compact ? { enabled: false, external: renderStripTooltip } : {}),
        // Index-mode hover activates every series at that x. Drop zeros so a
        // sparse multi-series chart doesn't build a tooltip listing every
        // inactive source (which balloons until it covers the chart). Returning
        // `undefined` from `label` is not enough — Chart.js treats that as
        // "use the default callback" and still renders the line.
        filter: (item) => (item.parsed.y ?? 0) !== 0,
        callbacks: {
          title: (items) => tooltipLabels[items[0]?.dataIndex ?? 0] ?? "",
          label: (item) =>
            item.formattedValue
              ? `${item.dataset.label}: ${item.formattedValue}`
              : "",
          ...(tooltipAfterBody
            ? {
                afterBody: (items) =>
                  tooltipAfterBody(items[0]?.dataIndex ?? 0),
              }
            : {}),
        },
      },
      zoom: zoomPluginOptions,
    },
    scales: {
      x: {
        stacked: true,
        grid: { display: false },
        ticks: compact
          ? {
              // The strip labels day boundaries, which the caller marks by
              // giving only those buckets a label. autoSkip would drop them by
              // its own spacing rules, so it is off and the empty labels do
              // the skipping.
              display: true,
              autoSkip: false,
              maxRotation: 0,
              padding: 2,
              font: { size: 10 },
              color: colors.labelFaded,
            }
          : { maxTicksLimit: 8, color: colors.labelFaded },
        // The strip keeps its baseline: without one the bars float and the
        // short buckets read as gaps rather than as small values.
        border: { display: true, color: colors.gridLine },
      },
      y: {
        stacked: true,
        beginAtZero: true,
        display: !compact,
        grid: { color: colors.gridLine },
        ticks: { precision: 0, color: colors.labelFaded },
      },
    },
    transitions: SHARED_RESIZE_TRANSITION,
    // Zooming replaces the whole bucket grid, so bar N of the old window and
    // bar N of the new one describe different times. Animating x would slide
    // every bar sideways to a position that means nothing; growing them in
    // place from the baseline reads as the window redrawing.
    animation: { duration: 220, easing: "easeOutQuad" },
    animations: {
      x: { duration: 0 },
      y: { duration: 220, easing: "easeOutQuad" },
    },
  };

  return (
    <div
      className="relative transition-all duration-200 ease-in-out"
      style={{ height }}
    >
      <Bar
        ref={chartRef}
        data={{ labels, datasets: sizedDatasets }}
        options={options}
      />
    </div>
  );
}
