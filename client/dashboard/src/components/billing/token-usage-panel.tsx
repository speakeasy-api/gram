import { type RiskTokensPoint } from "@gram/client/models/components/risktokenspoint.js";
import { type TumDetailsPoint } from "@gram/client/models/components/tumdetailspoint.js";
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
import { SimpleTooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import {
  CHART_COLORS,
  CLEAN_COLOR,
  OTHER_COLOR,
  RISKY_COLOR,
  type StackMode,
} from "./breakdown-options";

ChartJS.register(CategoryScale, LinearScale, BarElement, ChartTooltip, Legend);

// Vercel-style consumption breakdown for tokens under management: a stacked
// bar chart of tokens over the selected billing cycle, stacked by a chosen
// dimension, by token type, or by risk involvement, with client-side
// granularity roll-up (the caller fetches daily buckets) and a cumulative
// view. Everything renders from the billing details response (points +
// per-dimension rows, both scoped server-side to the billed completion
// population) — plus, for the headline total, the billed per-day series the
// usage endpoint already returns.

// Pointer movement under this many pixels counts as a click, not a drag.
const DRAG_THRESHOLD_PX = 5;

type Granularity = "day" | "week" | "month";

const GRANULARITIES: { value: Granularity; label: string }[] = [
  { value: "day", label: "Daily" },
  { value: "week", label: "Weekly" },
  { value: "month", label: "Monthly" },
];

const TOKEN_TYPES: {
  label: string;
  value: (p: TumDetailsPoint) => number;
}[] = [
  { label: "Input", value: (p) => p.inputTokens },
  { label: "Output", value: (p) => p.outputTokens },
  { label: "Cache read", value: (p) => p.cacheReadTokens },
  { label: "Cache write", value: (p) => p.cacheWriteTokens },
];

const compactTokens = new Intl.NumberFormat("en-US", {
  notation: "compact",
  maximumFractionDigits: 1,
});
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

// The server buckets are daily and UTC-aligned; bucket timestamps arrive as
// unix-nano strings, which exceed Number precision — divide as BigInt first.
function bucketMs(nano: string): number {
  try {
    return Number(BigInt(nano) / 1_000_000n);
  } catch {
    return 0;
  }
}

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

type Stack = { label: string; byBucket: Map<number, number> };

// A daily series from the details response's dimension rows, aligned to the
// points grid by index.
export type GroupSeries = { label: string; series: number[] };

function addTo(map: Map<number, number>, bucket: number, value: number): void {
  if (value === 0) return;
  map.set(bucket, (map.get(bucket) ?? 0) + value);
}

// One measure of the daily points, summed into granularity buckets.
function bucketPointValues(
  points: TumDetailsPoint[],
  granularity: Granularity,
  value: (p: TumDetailsPoint, index: number) => number,
): Map<number, number> {
  const byBucket = new Map<number, number>();
  points.forEach((p, i) => {
    addTo(
      byBucket,
      floorBucket(bucketMs(p.bucketTimeUnixNano), granularity),
      value(p, i),
    );
  });
  return byBucket;
}

// One stack per dimension value. The server already ranks rows and rolls the
// remainder into "Other", so the rows map straight to stacks.
function stacksByGroup(
  points: TumDetailsPoint[],
  groups: GroupSeries[],
  granularity: Granularity,
): Stack[] {
  return groups
    .map((g) => ({
      label: g.label,
      byBucket: bucketPointValues(
        points,
        granularity,
        (_, i) => g.series[i] ?? 0,
      ),
    }))
    .filter((s) => [...s.byBucket.values()].some((v) => v > 0));
}

// Two stacks — tokens from sessions with risk findings vs the remainder,
// taken against the same details totals the other stackings use so the
// stacked height matches across modes.
function stacksByRisk(
  riskPoints: RiskTokensPoint[],
  points: TumDetailsPoint[],
  granularity: Granularity,
): Stack[] {
  const risky = new Map<number, number>();
  for (const p of riskPoints) {
    addTo(
      risky,
      floorBucket(bucketMs(p.bucketTimeUnixNano), granularity),
      p.riskyTokens,
    );
  }
  const clean = new Map<number, number>();
  for (const [bucket, total] of bucketPointValues(
    points,
    granularity,
    (p) => p.totalTokens,
  )) {
    addTo(clean, bucket, Math.max(0, total - (risky.get(bucket) ?? 0)));
  }
  return [
    { label: "Sessions with risk findings", byBucket: risky },
    { label: "Sessions without risk findings", byBucket: clean },
  ].filter((s) => s.byBucket.size > 0);
}

// A single stack of all tokens per bucket — the no-breakdown view. Prefers
// the BILLED per-day series (the exact numbers on the usage card) when the
// caller has it; the details totals stand in otherwise.
function stacksByTotal(
  points: TumDetailsPoint[],
  billedSeries: number[] | null,
  granularity: Granularity,
): Stack[] {
  const byBucket = bucketPointValues(points, granularity, (p, i) =>
    billedSeries ? (billedSeries[i] ?? 0) : p.totalTokens,
  );
  if ([...byBucket.values()].every((v) => v === 0)) return [];
  return [{ label: "Total tokens", byBucket }];
}

// One stack per token type. For billed completions the cache series are
// zero (cache traffic is an agent-fleet concept) and drop out naturally.
function stacksByTokenType(
  points: TumDetailsPoint[],
  granularity: Granularity,
): Stack[] {
  return TOKEN_TYPES.map((t) => ({
    label: t.label,
    byBucket: bucketPointValues(points, granularity, t.value),
  })).filter((s) => [...s.byBucket.values()].some((v) => v > 0));
}

function ToggleButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "rounded px-2 py-0.5 text-xs transition-colors",
        active
          ? "bg-muted text-foreground font-medium"
          : "text-muted-foreground hover:text-foreground",
      )}
    >
      {children}
    </button>
  );
}

// The header info copy per stacking mode.
function headerHint(stackBy: StackMode): string {
  if (stackBy === "risk") {
    return "Tokens split by whether the session had at least one active risk finding (see Secure → Risk) during the period. Computed from per-session token totals, so numbers can differ slightly from the dimension views.";
  }
  return "Tokens from LLM completions that run through the platform — the usage billed as tokens under management. Agent telemetry observed via OTEL (Claude Code, Cursor, Codex) is not billed and not included here; see the Insights pages for it.";
}

// The stacks for the modes fed by the details points (risk mode reads the
// dedicated risk endpoint's points instead).
function pointStacks(
  mode: StackMode,
  points: TumDetailsPoint[],
  groups: GroupSeries[],
  billedSeries: number[] | null,
  granularity: Granularity,
): Stack[] {
  switch (mode) {
    case "total":
      return stacksByTotal(points, billedSeries, granularity);
    case "tokenType":
      return stacksByTokenType(points, granularity);
    case "group":
    case "risk": // unreachable — risk is handled by the caller
      return stacksByGroup(points, groups, granularity);
  }
}

// The bar color for a stack: risk mode uses a fixed risky/clean pair, the
// client-side "Other" roll-up stays neutral, everything else walks the palette.
function stackColor(label: string, index: number, stackBy: StackMode): string {
  if (stackBy === "risk") {
    return index === 0 ? RISKY_COLOR : CLEAN_COLOR;
  }
  if (label === "Other") return OTHER_COLOR;
  return CHART_COLORS[index % CHART_COLORS.length]!;
}

// A 6-digit hex color with ~13% alpha, for de-emphasizing non-hovered series.
function dimmed(hex: string): string {
  return `${hex}22`;
}

export function TokenUsagePanel({
  points,
  groups,
  billedSeries,
  stackBy,
  breakdownPicker,
  riskPoints,
  loading,
  onSelectRange,
}: {
  // Gap-filled daily buckets from the billing details response — the axis
  // grid plus the total/token-type measures.
  points: TumDetailsPoint[];
  // The selected dimension's rows, with daily series aligned to points.
  groups: GroupSeries[];
  // The billed per-day token series aligned to points, when the caller has
  // it (org scope with known cycle days); the total view plots it so the
  // chart matches the usage card exactly.
  billedSeries: number[] | null;
  // How the bars stack — controlled by the caller's breakdown picker.
  stackBy: StackMode;
  // The unified breakdown selector (dimensions + token type + risk), rendered
  // at the head of the control row.
  breakdownPicker: ReactNode;
  // Daily tokens split by risk involvement; null while unavailable.
  riskPoints: RiskTokensPoint[] | null;
  loading: boolean;
  // Called when a bar is clicked with the bucket's time range — the caller
  // narrows the page's period to it (drill-down). Bars aren't clickable
  // without it.
  onSelectRange?: (start: Date, end: Date) => void;
}): JSX.Element {
  const [granularity, setGranularity] = useState<Granularity>("day");
  const [cumulative, setCumulative] = useState(false);
  // Series hidden via the legend, keyed by label so toggles survive
  // granularity switches. Labels from other stack modes are simply inert.
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

  // Guard the async gap: risk mode before the risk data lands renders as the
  // dimension stacking rather than an empty chart.
  const effectiveStackBy: StackMode =
    stackBy === "risk" && !riskPoints ? "group" : stackBy;

  const chart = useMemo(() => {
    const stacks =
      effectiveStackBy === "risk"
        ? stacksByRisk(riskPoints ?? [], points, granularity)
        : pointStacks(
            effectiveStackBy,
            points,
            groups,
            billedSeries,
            granularity,
          );
    // The time axis comes from every bucket the server returned (gap-filled
    // with zeros), not just buckets with usage — zero days must keep their
    // slot so the axis stays continuous.
    const axisSource = points.map((p) =>
      floorBucket(bucketMs(p.bucketTimeUnixNano), granularity),
    );
    const buckets = [...new Set(axisSource)].sort((a, b) => a - b);

    // Focusing a hidden series resolves to no focus — otherwise hiding an
    // item while hovering it would leave every visible series dimmed.
    const focus =
      focusLabel !== null && !hiddenLabels.has(focusLabel) ? focusLabel : null;

    const datasets = stacks.map((s, i) => {
      const values = buckets.map((b) => s.byBucket.get(b) ?? 0);
      if (cumulative) {
        for (let j = 1; j < values.length; j++) {
          values[j] = values[j]! + values[j - 1]!;
        }
      }
      const base = stackColor(s.label, i, effectiveStackBy);
      return {
        label: s.label,
        data: values,
        backgroundColor:
          focus === null || s.label === focus ? base : dimmed(base),
      };
    });

    return {
      data: {
        labels: buckets.map((b) => bucketLabel(b, granularity)),
        datasets,
      },
      // Bucket start times parallel to the axis, for bar-click drill-down.
      buckets,
    };
  }, [
    points,
    groups,
    billedSeries,
    riskPoints,
    granularity,
    effectiveStackBy,
    cumulative,
    focusLabel,
    hiddenLabels,
  ]);

  const hasData = chart.data.datasets.length > 0;

  // Sync legend toggles into the chart instance (visibility is imperative
  // Chart.js state, not part of the data props).
  useEffect(() => {
    const instance = chartRef.current;
    if (!instance) return;
    chart.data.datasets.forEach((d, i) => {
      instance.setDatasetVisibility(i, !hiddenLabels.has(d.label));
    });
    instance.update();
  }, [chart, hiddenLabels]);

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
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
      setDragX(null);
      const endX = clampX(ev.clientX);
      if (Math.abs(endX - startX) < DRAG_THRESHOLD_PX) return; // plain click
      didDragRef.current = true;
      selectPixelRange(startX, endX);
    };
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
              `${item.dataset.label}: ${Number(item.raw).toLocaleString()} tokens`,
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
            callback: (value) => compactTokens.format(Number(value)),
          },
        },
      },
    };
  }, [isDark, chart.buckets, granularity, onSelectRange]);

  return (
    <div className="border-border rounded-lg border p-4">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <div className="flex items-center gap-1.5 text-sm font-semibold">
          Token Usage Time Series
          <SimpleTooltip tooltip={headerHint(effectiveStackBy)}>
            <Info className="text-muted-foreground size-3.5" />
          </SimpleTooltip>
        </div>
        <div className="ml-auto flex items-center gap-3">
          {breakdownPicker}
          <div className="bg-border h-4 w-px" />
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
              {chart.data.datasets.map((d, i) => {
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
                        backgroundColor: stackColor(
                          d.label,
                          i,
                          effectiveStackBy,
                        ),
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
            No token usage in this range.
          </div>
        )}
      </div>
    </div>
  );
}
