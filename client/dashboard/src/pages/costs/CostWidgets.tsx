import type { Dimension } from "@gram/client/models/components/queryfilter.js";
import type { Measures } from "./taxonomy";
import { EstimatedCostIndicator } from "@/components/estimated-cost";
import { RankedBar, type RankedBarItem } from "@/components/chart/RankedBar";
import { Sparkline } from "@/components/chart/Sparkline";
import { trendLineColor } from "@/components/chart/chart-theme";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { StatCard } from "@/components/ui/stat-tile";
import { cn } from "@/lib/utils";

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

// Rising cost reads as bad (destructive/red), falling as good (success/green);
// under a 1% move there's no clear trend, so no tone is applied at all —
// matches the plain (uncolored) delta text used for small moves.
function deltaToneClass(delta: number | null): string | undefined {
  if (delta === null || Math.abs(delta) < 1) return undefined;
  return delta > 0 ? "text-destructive" : "text-default-success";
}

// The bordered widget-card chrome shared by every secondary card in this
// grid: a title (with an optional muted date-range suffix, e.g. "Total cost
// (June 15–19)") over freeform content.
function WidgetCard({
  title,
  range,
  children,
}: {
  title: React.ReactNode;
  range?: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <Card>
      <div className="text-sm font-semibold">
        {title}
        {range && (
          <span className="text-muted-foreground ml-1 font-normal">
            {range}
          </span>
        )}
      </div>
      {children}
    </Card>
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
  const title = (
    <>
      Total cost <EstimatedCostIndicator billingMode={billingMode} />
    </>
  );
  if (loading) {
    return (
      <WidgetCard title={title} range={range}>
        <Skeleton className="h-8 w-28" />
        <Skeleton className="h-20 w-full" />
      </WidgetCard>
    );
  }
  return (
    <WidgetCard title={title} range={range}>
      <div className="flex items-baseline gap-2">
        <span className="font-display text-2xl font-thin tracking-[-0.015em] tabular-nums">
          {formatCost(total)}
        </span>
        {delta !== null && (
          <span
            className={cn(
              "text-xs font-medium tabular-nums",
              deltaToneClass(delta),
            )}
          >
            {formatDelta(delta)}
          </span>
        )}
      </div>
      <Sparkline
        data={values}
        mode="area"
        color={trendLineColor()}
        width={600}
        height={80}
        className="h-20 w-full"
      />
    </WidgetCard>
  );
}

type MixRow = { label: string; cost: number };

// Descending label/bar widths so the loading state mirrors a populated card.
const MIX_SKELETON_ROWS = [
  { label: 46, bar: 90 },
  { label: 36, bar: 68 },
  { label: 42, bar: 54 },
  { label: 30, bar: 40 },
  { label: 38, bar: 28 },
];

// Shared loading state for the ranked-list cards (mix breakdowns + sessions):
// descending label/bar widths so it mirrors a populated card.
function RankedRowsSkeleton(): JSX.Element {
  return (
    <div className="space-y-3">
      {MIX_SKELETON_ROWS.map((r, i) => (
        <div key={i} className="flex items-center gap-3">
          <Skeleton className="h-3 w-4 shrink-0" />
          <div className="min-w-0 flex-1 space-y-1.5">
            <div className="flex items-center justify-between gap-2">
              <Skeleton className="h-4" style={{ width: `${r.label}%` }} />
              <Skeleton className="h-4 w-8" />
            </div>
            <Skeleton className="h-1" style={{ width: `${r.bar}%` }} />
          </div>
        </div>
      ))}
    </div>
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
  const canDrill = drillable && !!onDrill;
  const items: RankedBarItem[] = top.map((r) => ({
    label: r.label || "(unset)",
    value: r.cost,
    onSelect:
      canDrill && r.label !== "" && r.label !== "Other"
        ? () => onDrill!(dim, r.label)
        : undefined,
  }));
  return (
    <WidgetCard title={title}>
      {loading ? (
        <RankedRowsSkeleton />
      ) : top.length === 0 ? (
        <div className="text-muted-foreground/60 text-sm">No data</div>
      ) : (
        <RankedBar
          items={items}
          colorMode="rank-gradient"
          formatValue={formatCost}
        />
      )}
    </WidgetCard>
  );
}

// The "Most costly sessions" widget: top sessions in this slice ranked by cost,
// each row opening the session detail. Same ranked-bar layout as MixCard, but
// rows key on the chat id (a leaf) rather than a drill dimension.
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
  const items: RankedBarItem[] = top.map((r) => ({
    label: r.label || "(unset)",
    value: r.cost,
    sublabel: r.sublabel,
    onSelect: onOpenSession ? () => onOpenSession(r.id) : undefined,
  }));
  return (
    <WidgetCard title={title}>
      {loading ? (
        <RankedRowsSkeleton />
      ) : top.length === 0 ? (
        <div className="text-muted-foreground/60 text-sm">No sessions</div>
      ) : (
        <RankedBar
          items={items}
          colorMode="rank-gradient"
          formatValue={formatCost}
        />
      )}
    </WidgetCard>
  );
}

// One KPI stat tile (e.g. Agent sessions): a headline number with a trend
// sparkline and delta, in the design system's bordered StatCard chrome.
function KpiStatCard({
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
    <StatCard
      label={label}
      value={value}
      delta={
        delta !== null
          ? { value: formatDelta(delta), tone: "neutral" }
          : undefined
      }
      caption={range}
      isLoading={loading}
      sparkline={<Sparkline data={series} trendColor width={64} height={24} />}
    />
  );
}

export type WidgetSeries = {
  cost: number[];
  chats: number[];
  tools: number[];
  tokens: number[];
  cacheCreation: number[];
};

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
          label={card.title}
          value={card.value}
          caption={card.caption}
          isLoading={card.loading}
        />
      );
  }
}

export function CostWidgets({
  series,
  totals,
  prevTotals,
  cards,
  rangeLabel,
  cacheMetric,
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
  // Drill into a mix-card row by its (dimension, value).
  onDrill?: (dim: Dimension, value: string) => void;
  // Open a session's detail from the "Most costly sessions" widget.
  onOpenSession?: (id: string) => void;
  // True while the main slice is still loading (trend + KPI skeletons).
  loading: boolean;
  // The view's resolved billing mode; "metered" hides the cost-estimate caveat.
  billingMode?: string;
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
        <KpiStatCard
          label="Agent sessions"
          value={formatCompact(totals.sessions)}
          series={series.chats}
          delta={relDelta(totals.sessions, prevTotals.sessions)}
          range={rangeLabel}
          loading={loading}
        />
        {cacheMetric ? (
          <KpiStatCard
            label="Tokens added"
            value={formatCompact(totals.cacheCreation)}
            series={series.cacheCreation}
            delta={relDelta(totals.cacheCreation, prevTotals.cacheCreation)}
            range={rangeLabel}
            loading={loading}
          />
        ) : (
          <KpiStatCard
            label="Tool calls"
            value={formatCompact(totals.tools)}
            series={series.tools}
            delta={relDelta(totals.tools, prevTotals.tools)}
            range={rangeLabel}
            loading={loading}
          />
        )}
        <KpiStatCard
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
