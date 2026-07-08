import { type QueryMeasures } from "@gram/client/models/components/querymeasures.js";
import { type QuerySeries } from "@gram/client/models/components/queryseries.js";
import { type RiskTokensPoint } from "@gram/client/models/components/risktokenspoint.js";
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
import { type ReactNode, useMemo, useState } from "react";
import { Bar } from "react-chartjs-2";
import { Skeleton } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

ChartJS.register(CategoryScale, LinearScale, BarElement, ChartTooltip, Legend);

// Vercel-style consumption breakdown for tokens under management: a stacked
// bar chart of tokens over the selected billing cycle, stacked by a chosen
// dimension, by token type, or by risk involvement, with client-side
// granularity roll-up (the caller fetches daily buckets) and a cumulative view.

const CHART_COLORS = [
  "#60a5fa", // blue
  "#34d399", // emerald
  "#f97316", // orange
  "#a78bfa", // violet
  "#fb7185", // rose
  "#facc15", // yellow
  "#38bdf8", // sky
  "#c084fc", // purple
  "#4ade80", // green
  "#f472b6", // pink
];
const OTHER_COLOR = "#94a3b8"; // slate — the client-side roll-up bucket

// Beyond this many stacks the legend and colors stop being readable; the
// remainder rolls into a client-side "Other" series.
const MAX_STACKS = 8;

type Granularity = "day" | "week" | "month";
// How the bars stack: by the selected dimension's groups, by token type, by
// risk involvement, or as a single un-broken-down total. Selected via the
// caller's unified breakdown picker.
export type StackMode = "group" | "tokenType" | "risk" | "total";

const RISKY_COLOR = "#fb7185"; // rose — tokens from sessions with risk findings
const CLEAN_COLOR = "#60a5fa"; // blue — everything else

const TOKEN_TYPES: { label: string; value: (m: QueryMeasures) => number }[] = [
  { label: "Input", value: (m) => m.totalInputTokens },
  { label: "Output", value: (m) => m.totalOutputTokens },
  { label: "Cache read", value: (m) => m.cacheReadInputTokens },
  { label: "Cache write", value: (m) => m.cacheCreationInputTokens },
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

function bucketLabel(ms: number, granularity: Granularity): string {
  const date = new Date(ms);
  return granularity === "month"
    ? monthLabelFormat.format(date)
    : dayLabelFormat.format(date);
}

type Stack = { label: string; byBucket: Map<number, number> };

function addTo(map: Map<number, number>, bucket: number, value: number): void {
  if (value === 0) return;
  map.set(bucket, (map.get(bucket) ?? 0) + value);
}

// One stack per group value, ranked by total tokens; groups beyond MAX_STACKS
// merge into "Other" (alongside any server-side "Other" roll-up row).
function stacksByGroup(
  series: QuerySeries[],
  granularity: Granularity,
): Stack[] {
  const ranked = series
    .map((s) => ({
      series: s,
      total: s.points.reduce((sum, p) => sum + p.measures.totalTokens, 0),
    }))
    .filter((s) => s.total > 0)
    .sort((a, b) => b.total - a.total);

  const keep = ranked.slice(0, MAX_STACKS);
  const rest = ranked.slice(MAX_STACKS);

  const stacks: Stack[] = keep.map(({ series: s }) => {
    const byBucket = new Map<number, number>();
    for (const p of s.points) {
      addTo(
        byBucket,
        floorBucket(bucketMs(p.bucketTimeUnixNano), granularity),
        p.measures.totalTokens,
      );
    }
    return {
      label: s.groupValue === "" ? "(unset)" : s.groupValue,
      byBucket,
    };
  });

  if (rest.length > 0) {
    const byBucket = new Map<number, number>();
    for (const { series: s } of rest) {
      for (const p of s.points) {
        addTo(
          byBucket,
          floorBucket(bucketMs(p.bucketTimeUnixNano), granularity),
          p.measures.totalTokens,
        );
      }
    }
    // Fold into an existing "Other" stack (the server's top-N remainder row)
    // rather than showing two.
    const existing = stacks.find((s) => s.label === "Other");
    if (existing) {
      for (const [bucket, value] of byBucket) {
        addTo(existing.byBucket, bucket, value);
      }
    } else {
      stacks.push({ label: "Other", byBucket });
    }
  }
  return stacks;
}

// Two stacks — tokens from sessions with risk findings vs the remainder —
// from the dedicated risk endpoint's daily points.
function stacksByRisk(
  points: RiskTokensPoint[],
  granularity: Granularity,
): Stack[] {
  const risky = new Map<number, number>();
  const clean = new Map<number, number>();
  for (const p of points) {
    const bucket = floorBucket(bucketMs(p.bucketTimeUnixNano), granularity);
    addTo(risky, bucket, p.riskyTokens);
    addTo(clean, bucket, Math.max(0, p.totalTokens - p.riskyTokens));
  }
  return [
    { label: "With risk findings", byBucket: risky },
    { label: "No risk findings", byBucket: clean },
  ].filter((s) => s.byBucket.size > 0);
}

// A single stack of all tokens per bucket — the no-breakdown view.
function stacksByTotal(
  series: QuerySeries[],
  granularity: Granularity,
): Stack[] {
  const byBucket = new Map<number, number>();
  for (const s of series) {
    for (const p of s.points) {
      addTo(
        byBucket,
        floorBucket(bucketMs(p.bucketTimeUnixNano), granularity),
        p.measures.totalTokens,
      );
    }
  }
  if (byBucket.size === 0) return [];
  return [{ label: "Total tokens", byBucket }];
}

// One stack per token type, summed across every group.
function stacksByTokenType(
  series: QuerySeries[],
  granularity: Granularity,
): Stack[] {
  return TOKEN_TYPES.map((t) => {
    const byBucket = new Map<number, number>();
    for (const s of series) {
      for (const p of s.points) {
        addTo(
          byBucket,
          floorBucket(bucketMs(p.bucketTimeUnixNano), granularity),
          t.value(p.measures),
        );
      }
    }
    return { label: t.label, byBucket };
  }).filter((s) => [...s.byBucket.values()].some((v) => v > 0));
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
  return "Token usage from your organization's analytics aggregates. Numbers can differ slightly from billed tokens under management, which only counts sessions Gram stored data for.";
}

// The stacks for the modes fed by the main grouped series (risk mode reads the
// dedicated risk endpoint's points instead).
function seriesStacks(
  mode: StackMode,
  series: QuerySeries[],
  granularity: Granularity,
): Stack[] {
  switch (mode) {
    case "total":
      return stacksByTotal(series, granularity);
    case "tokenType":
      return stacksByTokenType(series, granularity);
    case "group":
    case "risk": // unreachable — risk is handled by the caller
      return stacksByGroup(series, granularity);
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

export function TokenUsagePanel({
  series,
  stackBy,
  breakdownPicker,
  riskPoints,
  loading,
}: {
  // Per-group daily timeseries for the selected slice.
  series: QuerySeries[];
  // How the bars stack — controlled by the caller's breakdown picker.
  stackBy: StackMode;
  // The unified breakdown selector (dimensions + token type + risk), rendered
  // at the head of the control row.
  breakdownPicker: ReactNode;
  // Daily tokens split by risk involvement; null while unavailable.
  riskPoints: RiskTokensPoint[] | null;
  loading: boolean;
}): JSX.Element {
  const [granularity, setGranularity] = useState<Granularity>("day");
  const [cumulative, setCumulative] = useState(false);

  // Guard the async gap: risk mode before the risk data lands renders as the
  // dimension stacking rather than an empty chart.
  const effectiveStackBy: StackMode =
    stackBy === "risk" && !riskPoints ? "group" : stackBy;

  const chart = useMemo(() => {
    let stacks: Stack[];
    // The time axis comes from every bucket the server returned (gap-filled
    // with zeros), not just buckets with usage — zero days must keep their
    // slot so the axis stays continuous.
    let axisSource: number[];
    if (effectiveStackBy === "risk") {
      const points = riskPoints ?? [];
      stacks = stacksByRisk(points, granularity);
      axisSource = points.map((p) =>
        floorBucket(bucketMs(p.bucketTimeUnixNano), granularity),
      );
    } else {
      stacks = seriesStacks(effectiveStackBy, series, granularity);
      axisSource = series.flatMap((s) =>
        s.points.map((p) =>
          floorBucket(bucketMs(p.bucketTimeUnixNano), granularity),
        ),
      );
    }
    const buckets = [...new Set(axisSource)].sort((a, b) => a - b);

    const datasets = stacks.map((s, i) => {
      const values = buckets.map((b) => s.byBucket.get(b) ?? 0);
      if (cumulative) {
        for (let j = 1; j < values.length; j++) {
          values[j] = values[j]! + values[j - 1]!;
        }
      }
      return {
        label: s.label,
        data: values,
        backgroundColor: stackColor(s.label, i, effectiveStackBy),
      };
    });

    return {
      labels: buckets.map((b) => bucketLabel(b, granularity)),
      datasets,
    };
  }, [series, riskPoints, granularity, effectiveStackBy, cumulative]);

  const hasData = chart.datasets.length > 0;

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
      plugins: {
        legend: {
          position: "bottom",
          labels: { boxWidth: 12, boxHeight: 12, color: textColor },
        },
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
  }, [isDark]);

  return (
    <div className="border-border rounded-lg border p-4">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <div className="flex items-center gap-1.5 text-sm font-semibold">
          Token usage
          <SimpleTooltip tooltip={headerHint(effectiveStackBy)}>
            <Info className="text-muted-foreground size-3.5" />
          </SimpleTooltip>
        </div>
        <div className="ml-auto flex items-center gap-3">
          {breakdownPicker}
          <div className="bg-border h-4 w-px" />
          <div className="flex items-center gap-1">
            <ToggleButton
              active={granularity === "day"}
              onClick={() => setGranularity("day")}
            >
              Daily
            </ToggleButton>
            <ToggleButton
              active={granularity === "week"}
              onClick={() => setGranularity("week")}
            >
              Weekly
            </ToggleButton>
            <ToggleButton
              active={granularity === "month"}
              onClick={() => setGranularity("month")}
            >
              Monthly
            </ToggleButton>
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
          <div style={{ height: 280 }}>
            <Bar data={chart} options={chartOptions} />
          </div>
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
