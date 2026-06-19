import type { Dimension } from "@gram/client/models/components";
import type { Measures } from "./taxonomy";
import { Sparkline } from "./Sparkline";
import { movingAverage, resample, smoothPath } from "./sparkline-math";

const BRAND = "#6366f1"; // indigo-500 — neutral headline accent
const NEUTRAL = "#64748b"; // slate-500 — KPI sparklines
const UP = "#e11d48"; // rose-600
const DOWN = "#059669"; // emerald-600

// Bar grading: emerald (lowest cost) → slate → rose (highest). Avoids lime by
// passing through neutral grey rather than yellow.
type RGB = [number, number, number];
const GRADE_LOW: RGB = [5, 150, 105];
const GRADE_MID: RGB = [148, 163, 184];
const GRADE_HIGH: RGB = [225, 29, 72];
function mixRgb(a: RGB, b: RGB, k: number): string {
  const c = (i: 0 | 1 | 2) => Math.round(a[i] + (b[i] - a[i]) * k);
  return `rgb(${c(0)}, ${c(1)}, ${c(2)})`;
}
function gradeColor(t: number): string {
  const u = Math.max(0, Math.min(1, t));
  return u < 0.5
    ? mixRgb(GRADE_LOW, GRADE_MID, u / 0.5)
    : mixRgb(GRADE_MID, GRADE_HIGH, (u - 0.5) / 0.5);
}

function Skeleton({
  className,
  style,
}: {
  className?: string;
  style?: React.CSSProperties;
}): JSX.Element {
  return (
    <div
      className={`bg-muted animate-pulse rounded ${className ?? ""}`}
      style={style}
    />
  );
}

// Descending label/bar widths so the loading state mirrors a populated card.
const MIX_SKELETON_ROWS = [
  { label: 46, bar: 90 },
  { label: 36, bar: 68 },
  { label: 42, bar: 54 },
  { label: 30, bar: 40 },
  { label: 38, bar: 28 },
];

function formatCost(value: number): string {
  return `$${value.toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`;
}

function formatCompact(value: number): string {
  return value.toLocaleString(undefined, {
    notation: "compact",
    maximumFractionDigits: 1,
  });
}

function relDelta(current: number, prev: number): number | null {
  if (!prev || prev <= 0) return null;
  return ((current - prev) / prev) * 100;
}

function formatDelta(pct: number): string {
  return `${pct > 0 ? "+" : ""}${pct.toFixed(1)}%`;
}

// A filled, smoothed area chart of a series — the hero "cost trend".
function AreaChart({ values }: { values: number[] }): JSX.Element {
  const W = 600;
  const H = 80;
  const pad = 4;
  const series = resample(movingAverage(values, 11), 24);
  if (series.length < 2) return <div className="h-20" />;

  const min = Math.min(...series);
  const max = Math.max(...series);
  const span = max - min || 1;
  const innerW = W - pad * 2;
  const innerH = H - pad * 2;
  const pts = series.map((v, i) => ({
    x: pad + (i / (series.length - 1)) * innerW,
    y: pad + innerH - ((v - min) / span) * innerH,
  }));
  const line = smoothPath(pts);
  const area = `${line} L ${pts[pts.length - 1]!.x.toFixed(1)},${H} L ${pts[0]!.x.toFixed(1)},${H} Z`;

  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      preserveAspectRatio="none"
      aria-hidden="true"
      className="h-20 w-full"
    >
      <defs>
        <linearGradient id="cost-area-grad" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={BRAND} stopOpacity="0.18" />
          <stop offset="100%" stopColor={BRAND} stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={area} fill="url(#cost-area-grad)" />
      <path
        d={line}
        fill="none"
        stroke={BRAND}
        strokeWidth={1.5}
        strokeLinejoin="round"
        strokeLinecap="round"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}

function Card({
  title,
  range,
  children,
}: {
  title: string;
  // Optional muted date-range suffix, e.g. "Total cost (June 15–19)".
  range?: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className="border-border rounded-lg border p-4">
      <div className="text-sm font-semibold">
        {title}
        {range && (
          <span className="text-muted-foreground ml-1 font-normal">
            {range}
          </span>
        )}
      </div>
      {children}
    </div>
  );
}

function TrendCard({
  values,
  total,
  prevTotal,
  range,
  loading,
}: {
  values: number[];
  total: number;
  prevTotal: number;
  range: string;
  loading: boolean;
}): JSX.Element {
  const delta = relDelta(total, prevTotal);
  let deltaColor: string | undefined;
  if (delta !== null && Math.abs(delta) >= 1)
    deltaColor = delta > 0 ? UP : DOWN;
  if (loading) {
    return (
      <Card title="Total cost" range={range}>
        <Skeleton className="mt-1 h-8 w-28" />
        <Skeleton className="mt-3 h-20 w-full" />
      </Card>
    );
  }
  return (
    <Card title="Total cost" range={range}>
      <div className="mt-1 flex items-baseline gap-2">
        <span className="text-2xl font-semibold tabular-nums">
          {formatCost(total)}
        </span>
        {delta !== null && (
          <span
            className="text-xs font-medium tabular-nums"
            style={deltaColor ? { color: deltaColor } : undefined}
          >
            {formatDelta(delta)}
          </span>
        )}
      </div>
      <div className="mt-3">
        <AreaChart values={values} />
      </div>
    </Card>
  );
}

export type MixRow = { label: string; cost: number };

// One ranked row: label + cost + a graded bar. Becomes a button (with a muted
// hover) when `onSelect` is supplied, so the user can drill straight into it.
function MixRowItem({
  label,
  cost,
  barPct,
  barColor,
  onSelect,
}: {
  label: string;
  cost: number;
  barPct: number;
  barColor: string;
  onSelect?: () => void;
}): JSX.Element {
  const inner = (
    <>
      <div className="flex items-center justify-between gap-2 text-sm">
        <span className="truncate">{label || "(unset)"}</span>
        <span className="text-muted-foreground tabular-nums">
          {formatCost(cost)}
        </span>
      </div>
      {/* Track is muted by default; on row hover the row itself goes muted, so
          flip the track to the page background (white) to keep it legible. */}
      <div className="bg-muted group-hover:bg-background h-2.5 overflow-hidden rounded-full">
        <div
          className="h-full rounded-full"
          style={{ width: `${barPct}%`, backgroundColor: barColor }}
        />
      </div>
    </>
  );
  if (!onSelect) {
    return <div className="space-y-1 py-1.5">{inner}</div>;
  }
  return (
    <button
      type="button"
      onClick={onSelect}
      className="group hover:bg-muted -mx-2 block w-full cursor-pointer space-y-1 rounded-md px-2 py-1.5 text-left transition-colors"
    >
      {inner}
    </button>
  );
}

function MixCard({
  title,
  dim,
  drillable,
  rows,
  loading,
  onDrill,
}: {
  title: string;
  dim: Dimension;
  drillable: boolean;
  rows: MixRow[];
  loading: boolean;
  onDrill?: (dim: Dimension, value: string) => void;
}): JSX.Element {
  const top = rows.slice(0, 5);
  const costs = top.map((r) => r.cost);
  const max = Math.max(...costs, 0) || 1;
  const canDrill = drillable && !!onDrill;
  return (
    <Card title={title}>
      <div className="mt-3 space-y-0">
        {loading ? (
          MIX_SKELETON_ROWS.map((r, i) => (
            <div key={i} className="space-y-1.5">
              <div className="flex items-center justify-between gap-2">
                <Skeleton className="h-4" style={{ width: `${r.label}%` }} />
                <Skeleton className="h-4 w-8" />
              </div>
              <Skeleton
                className="h-2.5 rounded-full"
                style={{ width: `${r.bar}%` }}
              />
            </div>
          ))
        ) : top.length === 0 ? (
          <div className="text-muted-foreground/60 text-sm">No data</div>
        ) : (
          top.map((r, i) => {
            // Colour by rank, not magnitude: rows are sorted by cost desc, so a
            // single outlier won't collapse everyone else to one colour. Top
            // rank → rose, last → emerald, middle ranks → slate/grey.
            const t = top.length > 1 ? 1 - i / (top.length - 1) : 1;
            const selectable =
              canDrill && r.label !== "" && r.label !== "Other";
            return (
              <MixRowItem
                key={r.label}
                label={r.label}
                cost={r.cost}
                barPct={(r.cost / max) * 100}
                barColor={gradeColor(t)}
                onSelect={selectable ? () => onDrill!(dim, r.label) : undefined}
              />
            );
          })
        )}
      </div>
    </Card>
  );
}

function KpiTile({
  label,
  value,
  series,
  delta,
  range,
  loading,
}: {
  label: string;
  value: string;
  series: number[];
  delta: number | null;
  range: string;
  loading: boolean;
}): JSX.Element {
  return (
    <div className="border-border rounded-lg border p-3">
      <div className="text-muted-foreground text-xs">
        {label}
        <span className="text-muted-foreground/60 ml-1">{range}</span>
      </div>
      {loading ? (
        <Skeleton className="mt-2 h-6 w-16" />
      ) : (
        <>
          <div className="mt-1 flex items-end justify-between gap-2">
            <span className="text-lg font-semibold tabular-nums">{value}</span>
            <Sparkline values={series} width={64} height={20} color={NEUTRAL} />
          </div>
          {delta !== null && (
            <div className="text-muted-foreground mt-1 text-xs tabular-nums">
              {formatDelta(delta)}
            </div>
          )}
        </>
      )}
    </div>
  );
}

export type WidgetSeries = {
  cost: number[];
  chats: number[];
  tools: number[];
  tokens: number[];
};

// A single big-number stat (e.g. cost per session).
function StatCard({
  title,
  value,
  caption,
  loading,
}: {
  title: string;
  value: string;
  caption?: string;
  loading: boolean;
}): JSX.Element {
  return (
    <Card title={title}>
      {loading ? (
        <Skeleton className="mt-2 h-8 w-24" />
      ) : (
        <>
          <div className="mt-1 text-2xl font-semibold tabular-nums">
            {value}
          </div>
          {caption && (
            <div className="text-muted-foreground mt-1 text-xs">{caption}</div>
          )}
        </>
      )}
    </Card>
  );
}

export type MixCardSpec = {
  kind: "mix";
  title: string;
  // The dimension these rows rank, and whether it has a level to drill into.
  dim: Dimension;
  drillable: boolean;
  rows: MixRow[];
  loading: boolean;
};
export type StatCardSpec = {
  kind: "stat";
  title: string;
  value: string;
  caption?: string;
  loading: boolean;
};
export type CardSpec = MixCardSpec | StatCardSpec;

export function CostWidgets({
  series,
  totals,
  prevTotals,
  cards,
  rangeLabel,
  onDrill,
  loading,
}: {
  series: WidgetSeries;
  totals: Measures;
  prevTotals: Measures;
  // Per-level secondary cards (mix breakdowns + stats); varies by the axis.
  cards: CardSpec[];
  // Human date-range label shown beside the headline metric titles.
  rangeLabel: string;
  // Drill into a mix-card row by its (dimension, value).
  onDrill?: (dim: Dimension, value: string) => void;
  // True while the main slice is still loading (trend + KPI skeletons).
  loading: boolean;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-4">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <TrendCard
          values={series.cost}
          total={totals.cost}
          prevTotal={prevTotals.cost}
          range={rangeLabel}
          loading={loading}
        />
        {cards.map((c) =>
          c.kind === "mix" ? (
            <MixCard
              key={c.title}
              title={c.title}
              dim={c.dim}
              drillable={c.drillable}
              rows={c.rows}
              loading={c.loading}
              onDrill={onDrill}
            />
          ) : (
            <StatCard
              key={c.title}
              title={c.title}
              value={c.value}
              caption={c.caption}
              loading={c.loading}
            />
          ),
        )}
      </div>
      <div className="grid grid-cols-3 gap-4">
        <KpiTile
          label="Chat sessions"
          value={formatCompact(totals.sessions)}
          series={series.chats}
          delta={relDelta(totals.sessions, prevTotals.sessions)}
          range={rangeLabel}
          loading={loading}
        />
        <KpiTile
          label="Tool calls"
          value={formatCompact(totals.tools)}
          series={series.tools}
          delta={relDelta(totals.tools, prevTotals.tools)}
          range={rangeLabel}
          loading={loading}
        />
        <KpiTile
          label="Tokens"
          value={formatCompact(totals.tokens)}
          series={series.tokens}
          delta={relDelta(totals.tokens, prevTotals.tokens)}
          range={rangeLabel}
          loading={loading}
        />
      </div>
    </div>
  );
}
