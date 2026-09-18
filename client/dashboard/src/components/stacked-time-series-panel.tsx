import {
  BarElement,
  CategoryScale,
  Chart as ChartJS,
  type ChartDataset,
  type ChartOptions,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip as ChartTooltip,
} from "chart.js";
import { useConfig as useMoonshineConfig } from "@/components/ui/hooks/useConfig";
import { Info } from "lucide-react";
import {
  type ReactNode,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Chart } from "react-chartjs-2";
import { Skeleton } from "@/components/ui/Skeleton";
import { ToggleButton } from "@/components/ui/SegmentedControl";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { AXIS, TOOLTIP, withAlpha } from "@/components/chart/palette";
import {
  useOtherSeriesColor,
  useSeriesColors,
} from "@/components/chart/useSeriesColors";
import {
  type SeriesStyle,
  useSeriesFocus,
} from "@/components/chart/seriesFocus";
import { cn } from "@/lib/utils";
import { type TimeSeriesStack } from "./stacked-time-series";

ChartJS.register(
  CategoryScale,
  LinearScale,
  BarElement,
  LineElement,
  PointElement,
  ChartTooltip,
  Legend,
);

// Shared stacked time-series panel with client-side granularity roll-up and a
// cumulative view. Measure-agnostic: callers own the data, identity, labels,
// exact-value formatting, and units; the panel owns bucketing, controls,
// click/drag drill-down, legend interaction, and theming. Meter callers retain
// decimal integers for rollups/tooltips while plotting Number coordinates.

// Pointer movement under this many pixels counts as a click, not a drag.
const DRAG_THRESHOLD_PX = 5;

type Granularity = "day" | "week" | "month";

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
const rangeLabelFormat = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
  year: "numeric",
  timeZone: "UTC",
});

const rangeTimeLabelFormat = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
  year: "numeric",
  hour: "numeric",
  minute: "2-digit",
  timeZone: "UTC",
  timeZoneName: "short",
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
function bucketRangeLabel(from: number, to: number): string {
  const inclusiveEnd = Math.max(from, to - 1);
  const fromDate = new Date(from);
  const toDate = new Date(to);
  const dayAligned =
    fromDate.getUTCHours() === 0 &&
    fromDate.getUTCMinutes() === 0 &&
    fromDate.getUTCSeconds() === 0 &&
    toDate.getUTCHours() === 0 &&
    toDate.getUTCMinutes() === 0 &&
    toDate.getUTCSeconds() === 0;
  const formatter = dayAligned ? rangeLabelFormat : rangeTimeLabelFormat;
  const startLabel = formatter.format(fromDate);
  const endLabel = formatter.format(new Date(inclusiveEnd));
  return startLabel === endLabel ? startLabel : `${startLabel} – ${endLabel}`;
}

type Stack = {
  key: string;
  label: string;
  rollup?: boolean;
  unset?: boolean;
  byBucket: Map<number, number>;
  exactByBucket?: Map<number, bigint>;
};

function addTo(map: Map<number, number>, bucket: number, value: number): void {
  if (value === 0) return;
  map.set(bucket, (map.get(bucket) ?? 0) + value);
}

function addExact(
  map: Map<number, bigint>,
  bucket: number,
  value: string,
): void {
  const exact = BigInt(value);
  if (exact === 0n) return;
  map.set(bucket, (map.get(bucket) ?? 0n) + exact);
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
      const exactByBucket = s.exactSeries
        ? new Map<number, bigint>()
        : undefined;
      bucketsMs.forEach((ms, i) => {
        const bucket = floorBucket(ms, granularity);
        const exact = s.exactSeries?.[i];
        if (exactByBucket && exact !== undefined) {
          addExact(exactByBucket, bucket, exact);
        } else if (!exactByBucket) {
          addTo(byBucket, bucket, s.series[i] ?? 0);
        }
      });
      return {
        key: s.key ?? s.label,
        label: s.label,
        rollup: s.rollup,
        unset: s.unset,
        byBucket,
        exactByBucket,
      };
    })
    .filter((s) => {
      if (s.exactByBucket) {
        return s.exactByBucket.size > 0;
      }
      return s.byBucket.size > 0;
    });
}

function stackValues(
  stack: Stack,
  buckets: number[],
  cumulative: boolean,
): { data: number[]; exactData?: string[] } {
  if (stack.exactByBucket) {
    const values = buckets.map(
      (bucket) => stack.exactByBucket!.get(bucket) ?? 0n,
    );
    if (cumulative) {
      for (let index = 1; index < values.length; index++) {
        values[index] = values[index]! + values[index - 1]!;
      }
    }
    return {
      data: values.map(Number),
      exactData: values.map((value) => value.toString()),
    };
  }
  const data = buckets.map((bucket) => stack.byBucket.get(bucket) ?? 0);
  if (cumulative) {
    for (let index = 1; index < data.length; index++) {
      data[index] = data[index]! + data[index - 1]!;
    }
  }
  return { data };
}

// How a stack is painted: an explicitly-flagged "unset" group draws hollow in
// the neutral — it is the absence of a category, so it neither fills nor
// spends a real hue; an explicitly-flagged top-N roll-up fills with the same
// neutral; everything else walks the palette. A real group that merely
// DISPLAYS as "Other" keeps its own color, so callers must mark their rollup
// and unset series (see TimeSeriesStack.rollup / .unset).
function stackStyle(
  stack: { label: string; rollup?: boolean; unset?: boolean },
  index: number,
  colors: string[],
  otherColor: string,
): SeriesStyle {
  if (stack.unset) return { color: otherColor, outline: true };
  if (stack.rollup) return { color: otherColor };
  return { color: colors[index % colors.length]! };
}

// A palette color at ~13% alpha, for de-emphasizing non-hovered series.
function dimmed(color: string): string {
  return withAlpha(color, 0.13);
}

export function StackedTimeSeriesPanel({
  title,
  headerHint,
  bucketsMs,
  totalSeries,
  bucketEndsMs,
  stacks,
  headerControls,
  formatValue,
  formatExactValue,
  formatAxisValue,
  emptyMessage,
  loading,
  onSelectRange,
  onSelectSeries,
}: {
  title: string;
  headerHint: ReactNode;
  /** Gap-filled daily UTC bucket bounds. */
  bucketsMs: number[];
  bucketEndsMs?: number[];
  /** Optional total line, aligned to the same daily buckets. */
  totalSeries?: TimeSeriesStack;
  stacks: TimeSeriesStack[];
  headerControls?: ReactNode;
  /** Existing callers format the plotted Number coordinate. */
  formatValue: (value: number) => string;
  /** Meter callers format the exact rolled-up decimal integer. */
  formatExactValue?: (value: string) => string;
  formatAxisValue: (value: number) => string;
  emptyMessage: string;
  loading: boolean;
  onSelectRange?: (start: Date, end: Date) => void;
  /**
   * Clicking a bar SEGMENT selects that series — the same act as clicking its
   * row in the table beside the chart. When set, it takes the click and
   * `onSelectRange` is left to dragging (and to clicking the empty space above
   * a bar, which names a bucket but no series).
   */
  onSelectSeries?: (key: string) => void;
}): JSX.Element {
  const [granularity, setGranularity] = useState<Granularity>("day");
  const [cumulative, setCumulative] = useState(false);
  // Legend state is keyed by stable series identity, never display labels.
  const [hiddenKeys, setHiddenKeys] = useState<Set<string>>(new Set());
  // Focus (the spotlighted series) is shared with a sibling table when a
  // SeriesFocus provider is above the panel, so hovering a bar segment also
  // dims the table's other rows. Standalone panels keep it local.
  const sharedFocus = useSeriesFocus();
  const [localFocusKey, setLocalFocusKey] = useState<string | null>(null);
  const focusKey = sharedFocus ? sharedFocus.focusKey : localFocusKey;
  const setFocusKey = sharedFocus ? sharedFocus.setFocusKey : setLocalFocusKey;
  const chartRef = useRef<ChartJS<"bar" | "line", number[], string> | null>(
    null,
  );
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

  // Theme-resolved series ramp and rollup neutral; stable per-theme values,
  // so the memo below only rebuilds when the theme actually flips.
  const seriesColors = useSeriesColors();
  const otherColor = useOtherSeriesColor();

  // The expensive pass — granularity roll-up, axis derivation, cumulative
  // sums, base colors — keyed on the data inputs only. Hover/toggle state
  // stays out so sweeping the legend doesn't rebuild the bucketing.
  const rolled = useMemo(() => {
    const rolledStacks = rolledUpStacks(bucketsMs, stacks, granularity);
    // The time axis comes from every bucket the caller supplied (gap-filled
    // with zeros), not just buckets with usage — zero days must keep their
    const axisSource = bucketsMs.map((ms) => floorBucket(ms, granularity));
    const buckets = [...new Set(axisSource)].sort((a, b) => a - b);
    const starts = buckets.map((bucket) => {
      let start = bucketEndMs(bucket, granularity);
      bucketsMs.forEach((candidate) => {
        if (floorBucket(candidate, granularity) === bucket) {
          start = Math.min(start, candidate);
        }
      });
      return start;
    });
    const ends = buckets.map((bucket) => {
      let end = bucket;
      bucketsMs.forEach((start, index) => {
        if (floorBucket(start, granularity) !== bucket) return;
        end = Math.max(
          end,
          Math.max(start, bucketEndsMs?.[index] ?? start + MS_PER_DAY),
        );
      });
      return Math.min(end, bucketEndMs(bucket, granularity));
    });
    const datasets = rolledStacks.map((s, i) => {
      return {
        key: s.key,
        label: s.label,
        ...stackValues(s, buckets, cumulative),
        type: "bar" as const,
        stack: "__series_stack__",
        order: 2,
        base: stackStyle(s, totalSeries ? i + 1 : i, seriesColors, otherColor),
      };
    });

    const rolledTotal = totalSeries
      ? rolledUpStacks(bucketsMs, [totalSeries], granularity)[0]
      : undefined;
    let totalDataset:
      | (ChartDataset<"line", number[]> & {
          key: string;
          exactData?: string[];
          base: SeriesStyle;
        })
      | undefined;
    if (rolledTotal) {
      totalDataset = {
        key: rolledTotal.key,
        label: rolledTotal.label,
        ...stackValues(rolledTotal, buckets, cumulative),
        base: { color: seriesColors[0]! },
        type: "line",
        borderColor: seriesColors[0]!,
        backgroundColor: "transparent",
        pointRadius: 0,
        pointHoverRadius: 4,
        borderWidth: 2,
        tension: 0,
        fill: false,
        order: 1,
        stack: "__total_line__",
      };
    }

    return {
      labels: buckets.map((bucket) => bucketLabel(bucket, granularity)),
      datasets: totalDataset ? [...datasets, totalDataset] : datasets,
      buckets,
      starts,
      ends,
    };
  }, [
    bucketsMs,
    bucketEndsMs,
    stacks,
    granularity,
    cumulative,
    seriesColors,
    otherColor,
    totalSeries,
  ]);

  // Publish the color each stack was painted in, so a sibling table can dot
  // its rows to match. Keyed on the rolled datasets, so it only republishes
  // when the stacks or the resolved theme actually change.
  const publishStyles = sharedFocus?.setStyleByKey;
  useEffect(() => {
    publishStyles?.(new Map(rolled.datasets.map((d) => [d.key, d.base])));
  }, [publishStyles, rolled.datasets]);

  // A drill or a range change swaps the whole cast of series. A focus key left
  // over from the previous set matches nothing in the new one, which would
  // leave a sibling table fading every one of its rows against a series that
  // is no longer on screen — so the focus is dropped with the old datasets.
  useEffect(() => {
    setFocusKey(null);
  }, [rolled.datasets, setFocusKey]);

  const focus =
    focusKey !== null && !hiddenKeys.has(focusKey) ? focusKey : null;
  const chart = useMemo(
    () => ({
      data: {
        labels: rolled.labels,
        datasets: rolled.datasets.map(({ base, ...dataset }) => {
          const color =
            focus === null || dataset.key === focus
              ? base.color
              : dimmed(base.color);
          if (dataset.type === "line") {
            return {
              ...dataset,
              hidden: hiddenKeys.has(dataset.key),
              borderColor: color,
              backgroundColor: "transparent",
            };
          }
          // An "unset" stack draws hollow: a box outlined in the neutral
          // rather than filled with it.
          if (base.outline) {
            return {
              ...dataset,
              hidden: hiddenKeys.has(dataset.key),
              backgroundColor: "transparent",
              borderColor: color,
              borderWidth: 1,
              // Chart.js skips the shared edge between stacked bars unless
              // every side is asked for explicitly.
              borderSkipped: false as const,
            };
          }
          return {
            ...dataset,
            hidden: hiddenKeys.has(dataset.key),
            backgroundColor: color,
          };
        }),
      },
      buckets: rolled.buckets,
      starts: rolled.starts,
      ends: rolled.ends,
    }),
    [rolled, focus, hiddenKeys],
  );

  const hasData = rolled.datasets.length > 0;

  const buckets = chart.buckets;
  const drillToBuckets = useCallback(
    (fromIndex: number, toIndex: number): void => {
      if (!onSelectRange) return;
      const start = chart.starts[fromIndex] ?? buckets[fromIndex];
      if (start === undefined) return;
      const end =
        chart.ends[toIndex] ??
        bucketEndMs(buckets[toIndex] ?? start, granularity);
      setGranularity("day");
      onSelectRange(new Date(start), new Date(end));
    },
    [buckets, chart.ends, chart.starts, granularity, onSelectRange],
  );

  // Pixel positions map to axis indexes through the Chart.js category scale.
  const selectPixelRange = (x1: number, x2: number): void => {
    const scale = chartRef.current?.scales["x"];
    if (!scale || chart.buckets.length === 0) return;
    const clampIndex = (v: number | undefined): number =>
      Math.min(chart.buckets.length - 1, Math.max(0, Math.round(v ?? 0)));
    drillToBuckets(
      clampIndex(scale.getValueForPixel(Math.min(x1, x2))),
      clampIndex(scale.getValueForPixel(Math.max(x1, x2))),
    );
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

  const toggleSeries = (key: string) => {
    setHiddenKeys((previous) => {
      const next = new Set(previous);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  };

  // Chart.js paints the canvas with static defaults that ignore the CSS
  // theme, so axis/legend text and gridlines need explicit dark-mode colors.
  const { theme } = useMoonshineConfig();
  const isDark = theme === "dark";

  const chartOptions = useMemo<ChartOptions<"bar">>(() => {
    const textColor = isDark ? AXIS.faded : AXIS.label;
    const gridColor = isDark ? AXIS.gridDark : AXIS.grid;
    return {
      responsive: true,
      maintainAspectRatio: false,
      // Clicking a bar SEGMENT selects its series when the caller takes
      // series clicks — the chart's equivalent of clicking that series' row
      // in the table below. Otherwise (and anywhere no segment was hit) the
      // click drills the page's period down to that bucket; the zoomed view
      // re-buckets daily so a week/month bar expands into its days instead of
      // one lone bar.
      onClick: (_event, elements) => {
        if (didDragRef.current) return;
        const hit = elements[0];
        if (hit === undefined) return;
        if (onSelectSeries) {
          const key = rolled.datasets[hit.datasetIndex]?.key;
          if (key !== undefined) {
            onSelectSeries(key);
            return;
          }
        }
        drillToBuckets(hit.index, hit.index);
      },
      // Hovering a bar segment spotlights its series the same way hovering
      // its legend entry does — and, under a SeriesFocus provider, dims the
      // sibling table's other rows.
      onHover: (event, elements) => {
        const target = event.native?.target;
        if (target instanceof HTMLElement) {
          target.style.cursor =
            (onSelectRange || onSelectSeries) && elements.length > 0
              ? "pointer"
              : "default";
        }
        const datasetIndex = elements[0]?.datasetIndex;
        setFocusKey(
          datasetIndex === undefined
            ? null
            : (rolled.datasets[datasetIndex]?.key ?? null),
        );
      },
      plugins: {
        // The canvas legend can't style hover or read as clickable — an HTML
        // legend below the chart replaces it (see the buttons in the JSX).
        legend: { display: false },
        tooltip: {
          ...TOOLTIP,
          callbacks: {
            title: (items) => {
              const index = items[0]?.dataIndex;
              if (index === undefined) return "";
              const from = rolled.starts[index];
              const to = rolled.ends[index];
              return from === undefined || to === undefined
                ? ""
                : bucketRangeLabel(from, to);
            },
            label: (item) => {
              const dataset = rolled.datasets[item.datasetIndex];
              const exact = dataset?.exactData?.[item.dataIndex];
              const value =
                exact !== undefined && formatExactValue
                  ? formatExactValue(exact)
                  : formatValue(Number(item.raw));
              return `${item.dataset.label}: ${value}`;
            },
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
    drillToBuckets,
    onSelectRange,
    onSelectSeries,
    setFocusKey,
    formatValue,
    formatExactValue,
    formatAxisValue,
    rolled.datasets,
    rolled.starts,
    rolled.ends,
  ]);

  return (
    <div className="border-border border p-4">
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
              onMouseLeave={() => setFocusKey(null)}
            >
              <Chart<"bar" | "line", number[], string>
                ref={chartRef}
                type="bar"
                datasetIdKey="key"
                data={chart.data}
                options={chartOptions}
              />
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
              {rolled.datasets.map((dataset) => {
                const hidden = hiddenKeys.has(dataset.key);
                return (
                  <button
                    key={dataset.key}
                    type="button"
                    aria-pressed={!hidden}
                    onClick={() => toggleSeries(dataset.key)}
                    onMouseEnter={() => setFocusKey(dataset.key)}
                    onMouseLeave={() => setFocusKey(null)}
                    className={cn(
                      "hover:bg-muted hover:text-foreground flex cursor-pointer items-center gap-1.5 px-2 py-0.5 text-xs transition-colors",
                      hidden
                        ? "text-muted-foreground/60 line-through"
                        : "text-muted-foreground",
                    )}
                  >
                    <span
                      className={cn("size-2.5", hidden && "opacity-40")}
                      style={
                        dataset.base.outline
                          ? { border: `1px solid ${dataset.base.color}` }
                          : { backgroundColor: dataset.base.color }
                      }
                    />
                    {dataset.label}
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
