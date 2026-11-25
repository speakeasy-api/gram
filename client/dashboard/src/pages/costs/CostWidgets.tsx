import { formatCost } from "@/lib/money";
import type { Dimension } from "@gram/client/models/components/queryfilter.js";
import { formatWorkUnits, type Measures, unsetLabel } from "./taxonomy";
import { Sparkline } from "./Sparkline";
import {
  movingAverage,
  resample,
  smoothPath,
} from "@/components/chart/sparkline-math";
import { EstimatedCostIndicator } from "@/components/estimated-cost";
import { useIsDarkTheme } from "@/lib/theme";
import {
  useSeriesColors,
  useTrendColors,
} from "@/components/chart/useSeriesColors";

// Ranked-bar shading. These cards list one measure — spend — ordered by it, so
// the bars encode rank, not a verdict: nothing here is good or bad, the top row
// is just the biggest. That makes this an ordinal ramp, one hue stepping from
// the breakdown chart's own leading blue down toward the surface, so the cards
// and the chart below them read as one system. (It used to grade green → red,
// which borrowed the trend signal's meaning for a list that has no direction,
// and clashed with the chart besides.)
//
// One step per row, deepest first; the light end still clears 2:1 against its
// surface so the last row's bar doesn't dissolve into the track.
const RANK_RAMP_LIGHT = [
  "#274f72",
  "#33699f", // the chart's slot-0 blue
  "#4a80b4",
  "#7099c0",
  "#93b2cf",
];
// Dark runs the other way — the deepest step would vanish against the canvas,
// so rank 1 takes the lightest and the ramp steps down from there.
const RANK_RAMP_DARK = ["#b3cde4", "#8fb4d6", "#6f9bc4", "#5183ae", "#3d6b92"];
function useRankRamp(): string[] {
  return useIsDarkTheme() ? RANK_RAMP_DARK : RANK_RAMP_LIGHT;
}
// The bar color for row `rank`. Past the ramp's length the last step repeats —
// these cards show five rows, so that is a guard, not a case.
function rankColor(rank: number, ramp: string[]): string {
  return ramp[Math.min(rank, ramp.length - 1)]!;
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
      className={`bg-muted animate-pulse ${className ?? ""}`}
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
  // Ink — neutral headline accent, lifted to near-white on dark surfaces.
  const brand = useSeriesColors()[0]!;
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
      <path d={area} fill={brand} fillOpacity={0.08} />
      <path
        d={line}
        fill="none"
        stroke={brand}
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
  title: React.ReactNode;
  // Optional muted date-range suffix, e.g. "Total cost (June 15–19)".
  range?: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className="border-border border p-4">
      <div className="text-eyebrow">
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
  billingMode,
}: {
  values: number[];
  total: number;
  prevTotal: number;
  range: string;
  loading: boolean;
  billingMode?: string;
}): JSX.Element {
  const delta = relDelta(total, prevTotal);
  let deltaClass: string | undefined;
  if (delta !== null && Math.abs(delta) >= 1)
    deltaClass =
      delta > 0 ? "text-default-destructive" : "text-default-success";
  const title = (
    <>
      Total cost <EstimatedCostIndicator billingMode={billingMode} />
    </>
  );
  if (loading) {
    return (
      <Card title={title} range={range}>
        <Skeleton className="mt-1 h-8 w-28" />
        <Skeleton className="mt-3 h-20 w-full" />
      </Card>
    );
  }
  return (
    <Card title={title} range={range}>
      <div className="mt-1 flex items-baseline gap-2">
        <span className="font-display text-3xl font-thin tabular-nums">
          {formatCost(total)}
        </span>
        {delta !== null && (
          <span
            className={`text-xs font-medium tabular-nums ${deltaClass ?? ""}`}
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

type MixRow = { label: string; cost: number };

// One ranked row: label + cost + a graded bar. Becomes a button (with a muted
// hover) when `onSelect` is supplied, so the user can drill straight into it.
function MixRowItem({
  label,
  sublabel,
  clampTitle,
  cost,
  barPct,
  barColor,
  onSelect,
}: {
  label: string;
  sublabel?: string;
  clampTitle?: boolean;
  cost: number;
  barPct: number;
  barColor: string;
  onSelect?: () => void;
}): JSX.Element {
  const inner = (
    <>
      <div className="flex items-start justify-between gap-2 text-sm">
        <span className="min-w-0">
          <span
            className={
              clampTitle ? "line-clamp-2 text-[13px]" : "block truncate"
            }
          >
            {label || "(unset)"}
          </span>
          {sublabel ? (
            <span className="text-muted-foreground block truncate text-xs">
              {sublabel}
            </span>
          ) : null}
        </span>
        <span className="text-muted-foreground shrink-0 tabular-nums">
          {formatCost(cost)}
        </span>
      </div>
      {/* Track is muted by default; on row hover the row itself goes muted, so
          flip the track to the page background (white) to keep it legible. */}
      <div className="bg-muted group-hover:bg-background h-2.5 overflow-hidden">
        <div
          className="h-full"
          style={{ width: `${barPct}%`, backgroundColor: barColor }}
        />
      </div>
    </>
  );
  if (!onSelect) {
    // Mirror the selectable button's box (-mx-2/px-2/w-full) so a non-drillable
    // row's bar lines up with the rest instead of overshooting by 16px.
    return (
      <div className="-mx-2 block w-full space-y-1 px-2 py-1.5">{inner}</div>
    );
  }
  return (
    <button
      type="button"
      onClick={onSelect}
      className="group hover:bg-muted -mx-2 block w-full cursor-pointer space-y-1 px-2 py-1.5 text-left transition-colors"
    >
      {inner}
    </button>
  );
}

// Shared loading state for the ranked-list cards (mix breakdowns + sessions):
// descending label/bar widths so it mirrors a populated card.
function RankedSkeleton(): JSX.Element {
  return (
    <>
      {MIX_SKELETON_ROWS.map((r, i) => (
        <div key={i} className="space-y-1.5">
          <div className="flex items-center justify-between gap-2">
            <Skeleton className="h-4" style={{ width: `${r.label}%` }} />
            <Skeleton className="h-4 w-8" />
          </div>
          <Skeleton className="h-2.5" style={{ width: `${r.bar}%` }} />
        </div>
      ))}
    </>
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
  const ramp = useRankRamp();
  return (
    <Card title={title}>
      <div className="mt-3 space-y-0">
        {loading ? (
          <RankedSkeleton />
        ) : top.length === 0 ? (
          <div className="text-muted-foreground/60 text-sm">No data</div>
        ) : (
          top.map((r, i) => {
            // Shade by rank, not magnitude: rows are sorted by cost desc, so a
            // single outlier won't collapse everyone else onto one step.
            const selectable =
              canDrill && r.label !== "" && r.label !== "Other";
            return (
              <MixRowItem
                key={r.label}
                label={r.label === "" ? unsetLabel(dim) : r.label}
                cost={r.cost}
                barPct={(r.cost / max) * 100}
                barColor={rankColor(i, ramp)}
                onSelect={selectable ? () => onDrill!(dim, r.label) : undefined}
              />
            );
          })
        )}
      </div>
    </Card>
  );
}

// The "Most costly sessions" widget: top sessions in this slice ranked by cost,
// each row a button that opens the session detail. Same ranked-bar layout as
// MixCard, but rows key on the chat id (a leaf) rather than a drill dimension.
function SessionsCard({
  title,
  rows,
  loading,
  onOpenSession,
}: {
  title: string;
  rows: SessionRow[];
  loading: boolean;
  onOpenSession?: (id: string) => void;
}): JSX.Element {
  const top = rows.slice(0, 5);
  const max = Math.max(...top.map((r) => r.cost), 0) || 1;
  const ramp = useRankRamp();
  return (
    <Card title={title}>
      <div className="mt-3 space-y-0">
        {loading ? (
          <RankedSkeleton />
        ) : top.length === 0 ? (
          <div className="text-muted-foreground/60 text-sm">No sessions</div>
        ) : (
          top.map((r, i) => {
            // Shade by rank, matching MixCard.
            return (
              <MixRowItem
                key={r.id}
                label={r.label}
                sublabel={r.sublabel}
                clampTitle
                cost={r.cost}
                barPct={(r.cost / max) * 100}
                barColor={rankColor(i, ramp)}
                onSelect={onOpenSession ? () => onOpenSession(r.id) : undefined}
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
  // These sparklines are deliberately colorless — a KPI count has no good or
  // bad direction — so they take the theme's neutral rather than grading.
  const neutral = useTrendColors().flat;
  // Shares the Card shell with the row above rather than a squatter box of its
  // own, so the two rows read as one family: same padding, same title, same
  // value size, with the sparkline sized to fill the tile instead of tucking
  // into a corner.
  return (
    <Card title={label} range={range}>
      {loading ? (
        <Skeleton className="mt-3 h-8 w-20" />
      ) : (
        <div className="mt-3 flex items-end justify-between gap-3">
          <div className="flex items-baseline gap-2">
            <span className="font-display text-3xl font-thin tabular-nums">
              {value}
            </span>
            {delta !== null && (
              <span className="text-muted-foreground text-xs font-medium tabular-nums">
                {formatDelta(delta)}
              </span>
            )}
          </div>
          <Sparkline values={series} width={96} height={36} color={neutral} />
        </div>
      )}
    </Card>
  );
}

export type WidgetSeries = {
  cost: number[];
  chats: number[];
  tools: number[];
  tokens: number[];
  cacheCreation: number[];
  workUnits: number[];
  scoredCost: number[];
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
          <div className="mt-1 font-display text-3xl font-thin tabular-nums">
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

type MixCardSpec = {
  kind: "mix";
  title: string;
  // The dimension these rows rank, and whether it has a level to drill into.
  dim: Dimension;
  drillable: boolean;
  rows: MixRow[];
  loading: boolean;
};
type StatCardSpec = {
  kind: "stat";
  title: string;
  value: string;
  caption?: string;
  loading: boolean;
};
type SessionRow = {
  id: string;
  label: string;
  sublabel?: string;
  cost: number;
};
type SessionsCardSpec = {
  kind: "sessions";
  title: string;
  rows: SessionRow[];
  loading: boolean;
};
export type CardSpec = MixCardSpec | StatCardSpec | SessionsCardSpec;

// Render one secondary card by kind. A dispatcher (not an inline ternary) keeps
// the grid map flat as the kinds grow.
function CardItem({
  card,
  onDrill,
  onOpenSession,
}: {
  card: CardSpec;
  onDrill?: (dim: Dimension, value: string) => void;
  onOpenSession?: (id: string) => void;
}): JSX.Element {
  switch (card.kind) {
    case "mix":
      return (
        <MixCard
          title={card.title}
          dim={card.dim}
          drillable={card.drillable}
          rows={card.rows}
          loading={card.loading}
          onDrill={onDrill}
        />
      );
    case "sessions":
      return (
        <SessionsCard
          title={card.title}
          rows={card.rows}
          loading={card.loading}
          onOpenSession={onOpenSession}
        />
      );
    case "stat":
      return (
        <StatCard
          title={card.title}
          value={card.value}
          caption={card.caption}
          loading={card.loading}
        />
      );
  }
}

// Cost of the slice's scored sessions per work unit delivered; null (rendered
// "—") until the slice has any scored work. Uses scoredCost — not the full
// total — so partial analysis coverage can't overstate the ratio (see Measures).
function costPerUnit(m: Measures): number | null {
  return m.workUnits > 0 ? m.scoredCost / m.workUnits : null;
}

export function CostWidgets({
  series,
  totals,
  prevTotals,
  cards,
  rangeLabel,
  cacheMetric,
  efficiency,
  onDrill,
  onOpenSession,
  loading,
  billingMode,
}: {
  series: WidgetSeries;
  totals: Measures;
  prevTotals: Measures;
  // Per-level secondary cards (mix breakdowns, stats, sessions); varies by axis.
  cards: CardSpec[];
  // Human date-range label shown beside the headline metric titles.
  rangeLabel: string;
  // Attribution lens: swap the "Tool calls" KPI tile for "Tokens added".
  cacheMetric?: boolean;
  // Efficiency lens: swap the "Tool calls"/"Tokens" KPI tiles for the
  // work-units pair (work units delivered, cost per unit).
  efficiency?: boolean;
  // Drill into a mix-card row by its (dimension, value).
  onDrill?: (dim: Dimension, value: string) => void;
  // Open a session's detail from the "Most costly sessions" widget.
  onOpenSession?: (id: string) => void;
  // True while the main slice is still loading (trend + KPI skeletons).
  loading: boolean;
  // The view's resolved billing mode; "metered" hides the cost-estimate caveat.
  billingMode?: string;
}): JSX.Element {
  // The KPI row's second and third tiles, by lens: work-units pair on the
  // efficiency lens, cache tokens on attribution cuts, tool calls otherwise.
  function trailingKpiTiles(): JSX.Element {
    if (efficiency) {
      const unitCost = costPerUnit(totals);
      const prevUnitCost = costPerUnit(prevTotals);
      // Per-bucket ratio for the sparkline; buckets with no scored work chart
      // as 0 rather than inheriting a stale ratio.
      const unitCostSeries = series.workUnits.map((units, i) =>
        units > 0 ? (series.scoredCost[i] ?? 0) / units : 0,
      );
      return (
        <>
          <KpiTile
            label="Work delivered"
            value={formatWorkUnits(totals.workUnits)}
            series={series.workUnits}
            delta={relDelta(totals.workUnits, prevTotals.workUnits)}
            range={rangeLabel}
            loading={loading}
          />
          <KpiTile
            label="Cost efficiency"
            value={unitCost !== null ? formatCost(unitCost) : "—"}
            series={unitCostSeries}
            delta={
              unitCost !== null ? relDelta(unitCost, prevUnitCost ?? 0) : null
            }
            range={rangeLabel}
            loading={loading}
          />
        </>
      );
    }
    if (cacheMetric) {
      return (
        <>
          <KpiTile
            label="Tokens added"
            value={formatCompact(totals.cacheCreation)}
            series={series.cacheCreation}
            delta={relDelta(totals.cacheCreation, prevTotals.cacheCreation)}
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
        </>
      );
    }
    return (
      <>
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
      </>
    );
  }
  return (
    <div className="flex flex-col gap-4">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <TrendCard
          values={series.cost}
          total={totals.cost}
          prevTotal={prevTotals.cost}
          range={rangeLabel}
          loading={loading}
          billingMode={billingMode}
        />
        {cards.map((c) => (
          <CardItem
            key={c.title}
            card={c}
            onDrill={onDrill}
            onOpenSession={onOpenSession}
          />
        ))}
      </div>
      <div className="grid grid-cols-3 gap-4">
        <KpiTile
          label="Agent sessions"
          value={formatCompact(totals.sessions)}
          series={series.chats}
          delta={relDelta(totals.sessions, prevTotals.sessions)}
          range={rangeLabel}
          loading={loading}
        />
        {trailingKpiTiles()}
      </div>
    </div>
  );
}
