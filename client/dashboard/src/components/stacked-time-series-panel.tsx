import {
  BarElement,
  CategoryScale,
  Chart as ChartJS,
  type ChartOptions,
  Tooltip as ChartTooltip,
  Legend,
  LinearScale,
} from "chart.js";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import { Info } from "lucide-react";
import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { Bar } from "react-chartjs-2";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleButton } from "@/components/ui/toggle-button";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import {
  CHART_COLORS,
  OTHER_COLOR,
  OTHER_STACK_LABEL,
  type TimeSeriesStack,
} from "./stacked-time-series";

ChartJS.register(CategoryScale, LinearScale, BarElement, ChartTooltip, Legend);

// Vercel-style consumption breakdown panel: a stacked bar chart of a measure
// over time, stacked by whatever series the caller supplies, with client-side
// granularity roll-up (the caller feeds daily buckets) and a cumulative view.
// Measure-agnostic — the caller owns the data shape, labels, and formatting;
// the panel owns the bucketing, granularity/cumulative controls, drag-to-select
// drill-down, HTML legend, and theming. Used by the billing page's token-usage
// breakdown and the costs explorer's cost breakdown.

// Pointer movement under this many pixels counts as a click, not a drag.
const DRAG_THRESHOLD_PX = 5;

export type Granularity = "day" | "week" | "month";

const GRANULARITIES: { value: Granularity; label: string }[] = [
  { value: "day", label: "Daily" },
  { value: "week", label: "Weekly" },
  { value: "month", label: "Monthly" },
];

const dayLabelFormat = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
  timeZone: "UTC",
});
const monthLabelFormat = new Intl.DateTimeFormat("en-US", {
  month: "short",
  year: "numeric",
  timeZone: "UTC",
});

const MS_PER_DAY = 24 * 60 * 60 * 1000;

// Floor a bucket to the selected granularity in UTC (weeks start Monday).
function floorBucket(ms: number, granularity: Granularity): number {
  const d = new Date(ms);
  const day = Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate());
  switch (granularity) {
    case "day":
      return day;
    case "week":
      return day - ((new Date(day).getUTCDay() + 6) % 7) * MS_PER_DAY;
    case "month":
      return Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), 1);
  }
}

// The exclusive end of the bucket starting at ms — one day/week/month later.
function bucketEndMs(ms: number, granularity: Granularity): number {
  const d = new Date(ms);
  switch (granularity) {
    case "day":
      return ms + MS_PER_DAY;
    case "week":
      return ms + 7 * MS_PER_DAY;
    case "month":
      return Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 1);
  }
}

function bucketLabel(ms: number, granularity: Granularity): string {
  const date = new Date(ms);
  return granularity === "month"
    ? monthLabelFormat.format(date)
    : dayLabelFormat.format(date);
}

type Stack = { label: string; rollup?: boolean; byBucket: Map<number, number> };

function addTo(map: Map<number, number>, bucket: number, value: number): void {
  if (value === 0) return;
  map.set(bucket, (map.get(bucket) ?? 0) + value);
}

// The caller's daily series summed into granularity buckets; all-zero stacks
// drop out so the legend only lists series that actually chart.
function rolledUpStacks(
  bucketsMs: number[],
  stacks: TimeSeriesStack[],
  granularity: Granularity,
): Stack[] {
  return stacks
    .map((s) => {
      const byBucket = new Map<number, number>();
      bucketsMs.forEach((ms, i) => {
        addTo(byBucket, floorBucket(ms, granularity), s.series[i] ?? 0);
      });
      return { label: s.label, rollup: s.rollup, byBucket };
    })
    .filter((s) => [...s.byBucket.values()].some((v) => v > 0));
}

// The bar color for a stack: a top-N roll-up stays neutral, everything else
// walks the palette. The label match is a fallback for callers that don't set
// the rollup flag (the billing breakdowns, whose server rollup is labeled
// "Other" with no marker).
function stackColor(
  stack: { label: string; rollup?: boolean },
  index: number,
): string {
  if (stack.rollup || stack.label === OTHER_STACK_LABEL) return OTHER_COLOR;
  return CHART_COLORS[index % CHART_COLORS.length]!;
}

// A 6-digit hex color with ~13% alpha, for de-emphasizing non-hovered series.
function dimmed(hex: string): string {
  return `${hex}22`;
}

export function StackedTimeSeriesPanel({
  title,
  headerHint,
  bucketsMs,
  stacks,
  headerControls,
  formatValue,
  formatAxisValue,
  emptyMessage,
  loading,
  onSelectRange,
}: {
  // The panel title, with the info-tooltip copy beside it.
  title: string;
  headerHint: string;
  // Gap-filled daily UTC bucket start times — the axis grid. Every stack's
  // series aligns to it by index.
  bucketsMs: number[];
  // The stacked series (already mode-resolved by the caller). All-zero stacks
  // are dropped; a stack labeled OTHER_STACK_LABEL renders in the neutral
  // rollup color.
  stacks: TimeSeriesStack[];
  // Caller controls (e.g. a breakdown picker) rendered at the head of the
  // control row, before the granularity toggles.
  headerControls?: ReactNode;
  // Formats a value for the tooltip (with units, e.g. "1,234 tokens").
  formatValue: (value: number) => string;
  // Formats a value for the y-axis ticks (compact, e.g. "1.2M" or "$40k").
  formatAxisValue: (value: number) => string;
  emptyMessage: string;
  loading: boolean;
  // Called when a bar is clicked with the bucket's time range — the caller
  // narrows the page's period to it (drill-down). Bars aren't clickable
  // without it.
  onSelectRange?: (start: Date, end: Date) => void;
}): JSX.Element {
  const [granularity, setGranularity] = useState<Granularity>("day");
  const [cumulative, setCumulative] = useState(false);
  // Series hidden via the legend, keyed by label so toggles survive
  // granularity switches. Labels from other stack sets are simply inert.
  const [hiddenLabels, setHiddenLabels] = useState<Set<string>>(new Set());
  // The legend item under the pointer; every other series renders dimmed.
  const [focusLabel, setFocusLabel] = useState<string | null>(null);
  const chartRef = useRef<ChartJS<"bar"> | null>(null);
  // Drag-to-select: pixel positions of an in-progress drag over the chart,
  // relative to the chart container. Null when not dragging.
  const [dragX, setDragX] = useState<{ start: number; current: number } | null>(
    null,
  );
  // Set when a drag just completed so the ensuing Chart.js click event (fired
  // on the same mouseup) doesn't ALSO drill into the release bar.
  const didDragRef = useRef(false);
  // Teardown for an in-flight drag's window listeners. Normally run on
  // mouseup; also run on unmount so a drag interrupted by navigation doesn't
  // leave listeners firing into a dead component.
  const dragTeardownRef = useRef<(() => void) | null>(null);
  useEffect(() => () => dragTeardownRef.current?.(), []);

  // The expensive pass — granularity roll-up, axis derivation, cumulative
  // sums, base colors — keyed on the data inputs only. Hover/toggle state
  // stays out so sweeping the legend doesn't rebuild the bucketing.
  const rolled = useMemo(() => {
    const rolledStacks = rolledUpStacks(bucketsMs, stacks, granularity);
    // The time axis comes from every bucket the caller supplied (gap-filled
    // with zeros), not just buckets with usage — zero days must keep their
    // slot so the axis stays continuous.
    const axisSource = bucketsMs.map((ms) => floorBucket(ms, granularity));
    const buckets = [...new Set(axisSource)].sort((a, b) => a - b);

    const datasets = rolledStacks.map((s, i) => {
      const values = buckets.map((b) => s.byBucket.get(b) ?? 0);
      if (cumulative) {
        for (let j = 1; j < values.length; j++) {
          values[j] = values[j]! + values[j - 1]!;
        }
      }
      return { label: s.label, data: values, base: stackColor(s, i) };
    });

    return {
      labels: buckets.map((b) => bucketLabel(b, granularity)),
      datasets,
      // Bucket start times parallel to the axis, for bar-click drill-down.
      buckets,
    };
  }, [bucketsMs, stacks, granularity, cumulative]);

  // The cheap pass: map base colors through the hover spotlight.
  const chart = useMemo(() => {
    // Focusing a hidden series resolves to no focus — otherwise hiding an
    // item while hovering it would leave every visible series dimmed.
    const focus =
      focusLabel !== null && !hiddenLabels.has(focusLabel) ? focusLabel : null;
    return {
      data: {
        labels: rolled.labels,
        datasets: rolled.datasets.map(({ base, ...d }) => ({
          ...d,
          backgroundColor:
            focus === null || d.label === focus ? base : dimmed(base),
        })),
      },
      buckets: rolled.buckets,
    };
  }, [rolled, focusLabel, hiddenLabels]);

  const hasData = rolled.datasets.length > 0;

  // Sync legend toggles into the chart instance (visibility is imperative
  // Chart.js state, not part of the data props). Keyed on the dataset LABELS,
  // not the chart object: visibility is index-based on the instance, so it
  // only needs re-asserting when the set of series changes or a toggle flips
  // — value-only data changes and hover recolors are handled by the Bar
  // component's own update.
  const datasetLabels = useMemo(
    () => rolled.datasets.map((d) => d.label),
    [rolled],
  );
  useEffect(() => {
    const instance = chartRef.current;
    if (!instance) return;
    datasetLabels.forEach((label, i) => {
      instance.setDatasetVisibility(i, !hiddenLabels.has(label));
    });
    instance.update();
  }, [datasetLabels, hiddenLabels]);

  // Selects the buckets covered by [x1, x2] (container pixels) as a date
  // range, re-bucketing daily. Pixel positions map to axis indexes through
  // the Chart.js category scale.
  const selectPixelRange = (x1: number, x2: number): void => {
    const scale = chartRef.current?.scales["x"];
    if (!onSelectRange || !scale || chart.buckets.length === 0) return;
    const clampIndex = (v: number | undefined): number =>
      Math.min(chart.buckets.length - 1, Math.max(0, Math.round(v ?? 0)));
    const from = clampIndex(scale.getValueForPixel(Math.min(x1, x2)));
    const to = clampIndex(scale.getValueForPixel(Math.max(x1, x2)));
    const startMs = chart.buckets[from]!;
    const endMs = bucketEndMs(chart.buckets[to]!, granularity);
    setGranularity("day");
    onSelectRange(new Date(startMs), new Date(endMs));
  };

  // Dragging horizontally across the chart selects the covered buckets (a
  // movement under the threshold stays a plain click). Tracking happens on
  // window listeners installed at mousedown, so the drag survives leaving the
  // container and completes wherever the button is released; the listeners
  // remove themselves on mouseup.
  const handleChartMouseDown = (e: React.MouseEvent<HTMLDivElement>): void => {
    if (!onSelectRange || e.button !== 0) return;
    didDragRef.current = false;
    const rect = e.currentTarget.getBoundingClientRect();
    const clampX = (clientX: number): number =>
      Math.min(rect.width, Math.max(0, clientX - rect.left));
    const startX = clampX(e.clientX);
    setDragX({ start: startX, current: startX });

    const onMove = (ev: MouseEvent): void => {
      setDragX({ start: startX, current: clampX(ev.clientX) });
    };
    const onUp = (ev: MouseEvent): void => {
      teardown();
      setDragX(null);
      const endX = clampX(ev.clientX);
      if (Math.abs(endX - startX) < DRAG_THRESHOLD_PX) return; // plain click
      didDragRef.current = true;
      selectPixelRange(startX, endX);
    };
    const teardown = (): void => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
      dragTeardownRef.current = null;
    };
    dragTeardownRef.current = teardown;
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
  };

  const toggleLabel = (label: string) => {
    setHiddenLabels((prev) => {
      const next = new Set(prev);
      if (next.has(label)) {
        next.delete(label);
      } else {
        next.add(label);
      }
      return next;
    });
  };

  // Chart.js paints the canvas with static defaults that ignore the CSS
  // theme, so axis/legend text and gridlines need explicit dark-mode colors.
  const { theme } = useMoonshineConfig();
  const isDark = theme === "dark";

  const chartOptions = useMemo<ChartOptions<"bar">>(() => {
    const textColor = isDark ? "rgba(255, 255, 255, 0.85)" : "#666";
    const gridColor = isDark ? "#666" : "rgba(0, 0, 0, 0.08)";
    return {
      responsive: true,
      maintainAspectRatio: false,
      // Clicking a bar drills the page's period down to that bucket. The
      // zoomed view re-buckets daily so a week/month bar expands into its
      // days instead of one lone bar.
      onClick: (_event, elements) => {
        if (!onSelectRange || didDragRef.current) return;
        const index = elements[0]?.index;
        const start = index === undefined ? undefined : chart.buckets[index];
        if (start === undefined) return;
        const end = bucketEndMs(start, granularity);
        setGranularity("day");
        onSelectRange(new Date(start), new Date(end));
      },
      onHover: (event, elements) => {
        const target = event.native?.target;
        if (target instanceof HTMLElement) {
          target.style.cursor =
            onSelectRange && elements.length > 0 ? "pointer" : "default";
        }
      },
      plugins: {
        // The canvas legend can't style hover or read as clickable — an HTML
        // legend below the chart replaces it (see the buttons in the JSX).
        legend: { display: false },
        tooltip: {
          callbacks: {
            label: (item) =>
              `${item.dataset.label}: ${formatValue(Number(item.raw))}`,
          },
        },
      },
      scales: {
        x: {
          stacked: true,
          grid: { display: false },
          ticks: { maxTicksLimit: 16, color: textColor },
        },
        y: {
          stacked: true,
          beginAtZero: true,
          grid: { color: gridColor },
          ticks: {
            color: textColor,
            callback: (value) => formatAxisValue(Number(value)),
          },
        },
      },
    };
  }, [
    isDark,
    chart.buckets,
    granularity,
    onSelectRange,
    formatValue,
    formatAxisValue,
  ]);

  return (
    <div className="border-border rounded-lg border p-4">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <div className="flex items-center gap-1.5 text-sm font-semibold">
          {title}
          <SimpleTooltip tooltip={headerHint}>
            <Info className="text-muted-foreground size-3.5" />
          </SimpleTooltip>
        </div>
        <div className="ml-auto flex items-center gap-3">
          {headerControls}
          {headerControls && <div className="bg-border h-4 w-px" />}
          <div className="flex items-center gap-1">
            {GRANULARITIES.map((g) => (
              <ToggleButton
                key={g.value}
                active={granularity === g.value}
                onClick={() => setGranularity(g.value)}
              >
                {g.label}
              </ToggleButton>
            ))}
          </div>
          <div className="bg-border h-4 w-px" />
          <ToggleButton
            active={cumulative}
            onClick={() => setCumulative(!cumulative)}
          >
            Cumulative
          </ToggleButton>
        </div>
      </div>

      <div className="mt-4">
        {loading && <Skeleton className="h-[280px] w-full" />}
        {!loading && hasData && (
          <>
            <div
              className="relative"
              style={{ height: 280 }}
              onMouseDown={handleChartMouseDown}
            >
              <Bar ref={chartRef} data={chart.data} options={chartOptions} />
              {dragX &&
                Math.abs(dragX.current - dragX.start) >= DRAG_THRESHOLD_PX && (
                  <div
                    className="bg-primary/10 border-primary/40 pointer-events-none absolute inset-y-0 border-x"
                    style={{
                      left: Math.min(dragX.start, dragX.current),
                      width: Math.abs(dragX.current - dragX.start),
                    }}
                  />
                )}
            </div>
            {/* HTML legend: hoverable, clearly clickable buttons that toggle
                their series; hovering spotlights the series in the chart. */}
            <div className="mt-3 flex flex-wrap items-center justify-center gap-1.5">
              {rolled.datasets.map((d) => {
                const hidden = hiddenLabels.has(d.label);
                return (
                  <button
                    key={d.label}
                    type="button"
                    // Pressed = series visible; unpressed = hidden.
                    aria-pressed={!hidden}
                    onClick={() => toggleLabel(d.label)}
                    onMouseEnter={() => setFocusLabel(d.label)}
                    onMouseLeave={() => setFocusLabel(null)}
                    className={cn(
                      "hover:bg-muted hover:text-foreground flex cursor-pointer items-center gap-1.5 rounded-md px-2 py-0.5 text-xs transition-colors",
                      hidden
                        ? "text-muted-foreground/60 line-through"
                        : "text-muted-foreground",
                    )}
                  >
                    <span
                      className={cn(
                        "size-2.5 rounded-[3px]",
                        hidden && "opacity-40",
                      )}
                      style={{
                        backgroundColor: d.base,
                      }}
                    />
                    {d.label}
                  </button>
                );
              })}
            </div>
          </>
        )}
        {!loading && !hasData && (
          <div className="text-muted-foreground flex h-[280px] items-center justify-center text-sm">
            {emptyMessage}
          </div>
        )}
      </div>
    </div>
  );
}
