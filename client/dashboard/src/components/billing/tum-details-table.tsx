import { useOrganization } from "@/contexts/Auth";
import { Dimension } from "@gram/client/models/components/queryfilter.js";
import { type TumDetailsResult } from "@gram/client/models/components/tumdetailsresult.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, Info } from "lucide-react";
import { useMemo, useState } from "react";
import { Skeleton } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { CHART_COLORS, OTHER_COLOR } from "@/components/stacked-time-series";
import { type BillingPeriod, bucketDateKey } from "./billing-cycles";
import {
  breakdownLabel,
  breakdownValueLabel,
  isServerRollupRow,
} from "./breakdown-options";
import { tumDetailsQuery } from "./tum-queries";

// Vercel-style usage details for the selected billing cycle: one row per
// metric with a colored dot, a mini sparkline of the daily series, the
// cumulative cycle total, and the metric's share of the overage — grouped
// into sections that mirror the chart's breakdown picker (matching names and
// colors), plus activity counts.

const compactNumber = new Intl.NumberFormat("en-US", {
  notation: "compact",
  maximumFractionDigits: 2,
});

type DetailRow = {
  label: string;
  color: string;
  series: number[];
  total: number;
};

type DetailGroup = {
  heading: string;
  // Optional caveat rendered beside the heading (e.g. overlapping rows).
  note?: string;
  rows: DetailRow[];
};

// The dimension sections, split so Model sits directly under Total and the
// rest follow the token-type group, mirroring the chart's breakdown picker:
// the observed session's model and agent surface, the AI account's provider
// and team/personal classification, the project the traffic was recorded
// under, and the emit-time identity snapshot (division, department, user,
// roles).
const LEAD_DIMENSION_SECTIONS: string[] = [Dimension.Model];
const TAIL_DIMENSION_SECTIONS: string[] = [
  Dimension.HookSource,
  Dimension.Provider,
  Dimension.AccountType,
  Dimension.ProjectId,
  Dimension.DivisionName,
  Dimension.DepartmentName,
  Dimension.Email,
  Dimension.Role,
];

// A measure carried by both the daily points and the whole-range totals.
type MeasureField = "inputTokens" | "outputTokens" | "cacheCreationTokens";

type MeasureRowSpec = {
  label: string;
  color: string;
  field: MeasureField;
};

// Input + output + cache writes sum to the TUM total; cache reads are
// excluded from the population entirely.
const TOKEN_TYPE_ROWS: MeasureRowSpec[] = [
  { label: "Input", color: CHART_COLORS[0]!, field: "inputTokens" },
  { label: "Output", color: CHART_COLORS[1]!, field: "outputTokens" },
  {
    label: "Cache write",
    color: CHART_COLORS[2]!,
    field: "cacheCreationTokens",
  },
];

// Row color for a dimension value — same palette walk as the chart's stacks,
// so a value's dot matches its chart series color. The neutral remainder dot
// uses the SAME rollup identity test as the chart (isServerRollupRow), never
// a label match — a real value that happens to read "Other" keeps its
// palette color in both places.
function valueColor(rollup: boolean, index: number): string {
  if (rollup) return OTHER_COLOR;
  return CHART_COLORS[index % CHART_COLORS.length]!;
}

// The dimension sections of the details table, mirroring the chart's group
// stacks: same value order, "(unset)" labeling for unattributed traffic,
// project UUIDs mapped to names.
function dimensionGroups(
  data: TumDetailsResult | undefined,
  keys: string[],
  projectNames: Map<string, string>,
): DetailGroup[] {
  const byKey = new Map(
    (data?.breakdowns ?? []).map((b) => [b.key, b.rows] as const),
  );
  const groups: DetailGroup[] = [];
  for (const key of keys) {
    const rows = byKey.get(key);
    if (!rows) continue;
    // "" rows are real observed traffic that lacks the attribute — shown as
    // "(unset)". Zero-token rows are noise. Rollup identity is resolved on
    // the UNfiltered rows (it is positional: the server appends its remainder
    // last), before the zero-row filter can shift indexes.
    const visible = rows
      .map((row, i) => ({ row, rollup: isServerRollupRow(rows, i) }))
      .filter(({ row }) => row.totalTokens > 0);
    if (visible.length === 0) continue;
    groups.push({
      heading: breakdownLabel(key),
      // Roles are multi-valued: a user can hold several, and a session's
      // tokens count once under each — so these rows overlap and can sum to
      // more than the total.
      note:
        key === Dimension.Role
          ? "Users can hold multiple roles; rows overlap and can sum to more than the total token usage for the selected time period."
          : undefined,
      rows: visible.map(({ row: r, rollup }, i) => ({
        label: breakdownValueLabel(key, r.value, projectNames),
        color: valueColor(rollup, i),
        series: r.series,
        total: r.totalTokens,
      })),
    });
  }
  return groups;
}

// Minimal inline sparkline — a normalized polyline of the daily series.
function Sparkline({
  series,
  color,
}: {
  series: number[];
  color: string;
}): JSX.Element {
  const width = 120;
  const height = 24;
  const pad = 2;
  const max = Math.max(...series, 1);
  const step = series.length > 1 ? (width - pad * 2) / (series.length - 1) : 0;
  const points = series
    .map((v, i) => {
      const x = pad + i * step;
      const y = height - pad - (v / max) * (height - pad * 2);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");
  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      aria-hidden="true"
    >
      <polyline
        points={points}
        fill="none"
        stroke={color}
        strokeWidth="1.5"
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}

function DetailRowItem({
  row,
  overageWeights,
}: {
  row: DetailRow;
  // Per-day overage fraction: 0 before the allowance was crossed, prorated on
  // the crossing day, 1 after. A metric's overage is its tokens weighted by
  // it. Null when overage does not apply to the current view.
  overageWeights: number[] | null;
}): JSX.Element {
  const overageTokens =
    overageWeights !== null
      ? Math.round(
          row.series.reduce(
            (sum, v, i) => sum + v * (overageWeights[i] ?? 0),
            0,
          ),
        )
      : null;
  const overage =
    overageTokens === null ? "—" : compactNumber.format(overageTokens);
  return (
    <div className="flex items-center gap-3 px-4 py-2.5">
      <span
        className="size-2 shrink-0 rounded-full"
        style={{ backgroundColor: row.color }}
      />
      <span className="min-w-0 flex-1 truncate text-sm">{row.label}</span>
      <span className="text-muted-foreground shrink-0">
        <Sparkline series={row.series} color={row.color} />
      </span>
      <span
        className="w-24 shrink-0 text-right text-sm tabular-nums"
        title={row.total.toLocaleString()}
      >
        {compactNumber.format(row.total)}
      </span>
      <span
        className={cn(
          "w-24 shrink-0 text-right text-sm tabular-nums",
          // Matches the usage card's Overage stat tone.
          overageTokens !== null && overageTokens > 0
            ? "text-warning"
            : "text-muted-foreground",
        )}
        title={overageTokens?.toLocaleString()}
      >
        {overage}
      </span>
    </div>
  );
}

// One collapsible section: a clickable header band with its metric rows.
function DetailGroupSection({
  group,
  collapsed,
  onToggle,
  overageWeights,
}: {
  group: DetailGroup;
  collapsed: boolean;
  onToggle: () => void;
  overageWeights: number[] | null;
}): JSX.Element {
  return (
    <div>
      {/* Clicking the section band collapses/expands its rows. */}
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={!collapsed}
        className="bg-muted text-muted-foreground hover:text-foreground border-border dark:border-white/20 flex w-full cursor-pointer items-center gap-1.5 border-t px-4 py-1.5 text-xs transition-colors"
      >
        <ChevronDown
          className={cn(
            "size-3 transition-transform",
            collapsed && "-rotate-90",
          )}
        />
        <span className="font-medium">{group.heading}</span>
        {group.note && (
          <SimpleTooltip tooltip={group.note}>
            <Info className="size-3 cursor-help" />
          </SimpleTooltip>
        )}
      </button>
      {/* The default border token nearly vanishes on the dark canvas; lift
          the internal dividers so rows stay separable. */}
      {!collapsed && (
        <div className="divide-border dark:divide-white/20 divide-y">
          {group.rows.map((row) => (
            <DetailRowItem
              key={row.label}
              row={row}
              overageWeights={overageWeights}
            />
          ))}
        </div>
      )}
    </div>
  );
}

/**
 * Per-metric usage details for the selected period, rendered under the token
 * usage chart. Everything comes from a single telemetry.queryTumDetails
 * request; closed periods cache forever (their data is immutable).
 */
export function TumDetailsTable({
  period,
  projectNames,
  limit,
}: {
  period: BillingPeriod;
  // Project id → name, for labeling the Project section's UUID values.
  projectNames: Map<string, string>;
  // Contracted monthly allowance; drives the per-metric overage share.
  limit: number | null;
}): JSX.Element {
  const client = useGramContext();
  const organization = useOrganization();
  const scope = { client, orgId: organization.id, period };
  const { data, isFetching, isError } = useQuery(tumDetailsQuery(scope));

  // Sections collapsed via their header band, keyed by heading so the state
  // survives period switches.
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const toggleGroup = (heading: string): void => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(heading)) {
        next.delete(heading);
      } else {
        next.add(heading);
      }
      return next;
    });
  };

  // Billed normalization and overage attribution are organization-cycle
  // concepts (the TUM contract has no sub-cycle split), so both switch off
  // when a custom range narrows the data.
  const billedCycle = period.cycle;

  // The table presents BILLED tokens: the analytics aggregate supplies the
  // distribution across metrics (it has the dimensions; billing's per-session
  // qualification can't be expressed there), and one uniform scale converts
  // it into billed units so the Total row equals the cycle's billed tokens —
  // the usage card's number — exactly. The two aggregates track within a
  // fraction of a percent, so the correction is invisible per metric.
  const billedScale = useMemo(() => {
    if (!billedCycle) return 1;
    const analyticsTotal = data?.totals?.totalTokens ?? 0;
    if (analyticsTotal === 0) return 1;
    // A CLOSED zero-token cycle is a known zero: scale everything to 0 so
    // the Total row matches the card even when live analytics recomputed
    // nonzero tokens after the seal. The active cycle is exempt — its card
    // total is a live number that can trail the details query by a refetch,
    // and a transient zero must not blank real traffic.
    if (billedCycle.tokens === 0) {
      return billedCycle.current ? 1 : 0;
    }
    return billedCycle.tokens / analyticsTotal;
  }, [data, billedCycle]);

  const groups = useMemo<DetailGroup[]>(() => {
    const points = data?.points ?? [];
    const totals = data?.totals;

    const measureRow = (spec: MeasureRowSpec): DetailRow => ({
      label: spec.label,
      color: spec.color,
      series: points.map((p) => p[spec.field]),
      total: totals?.[spec.field] ?? 0,
    });

    const raw: DetailGroup[] = [
      {
        heading: "Total",
        rows: [
          {
            label: "Total tokens",
            color: CHART_COLORS[0]!,
            series: points.map((p) => p.totalTokens),
            total: totals?.totalTokens ?? 0,
          },
        ],
      },
      ...dimensionGroups(data, LEAD_DIMENSION_SECTIONS, projectNames),
      { heading: "Token type", rows: TOKEN_TYPE_ROWS.map(measureRow) },
      ...dimensionGroups(data, TAIL_DIMENSION_SECTIONS, projectNames),
    ];

    // Convert every row into billed units (see billedScale).
    return raw.map((group) => ({
      ...group,
      rows: group.rows.map((row) => ({
        ...row,
        total: Math.round(row.total * billedScale),
        series: row.series.map((v) => v * billedScale),
      })),
    }));
  }, [data, billedScale, projectNames]);

  // Time-based overage attribution: tokens count as overage from the moment
  // the organization's cumulative usage crossed the included allowance. Days
  // before the crossing weigh 0, days after weigh 1, and the crossing day is
  // prorated by how far into its tokens the allowance ran out (the data is
  // daily, so metrics are assumed to share the within-day distribution). The
  // crossing point is walked on the cycle's BILLED daily series.
  //
  // Null when overage does not apply: no contracted allowance, or a custom
  // range (the allowance is an org-cycle number).
  const overageWeights = useMemo<number[] | null>(() => {
    const cycle = billedCycle;
    if (limit == null || cycle == null) return null;
    const points = data?.points ?? [];

    // The daily series the crossing is walked on. Normally the cycle's
    // billed days; when the TUM response didn't carry them (the synthesized
    // active-cycle fallback has none), an all-zero walk would silently zero
    // the whole column — fall back to the billed-scaled analytics series.
    let billed: number[];
    if (cycle.days.length > 0) {
      // The daily series is advisory: it recomputes live under the CURRENT
      // billing scope, while a sealed cycle's total is the invoiced record
      // and can describe a larger (or drifted) population. Walking the raw
      // days against the allowance would then never reach the crossing the
      // card reports — scale the series to the cycle's billed total first,
      // the same normalization the chart's billed series applies.
      const daysSum = cycle.days.reduce((sum, d) => sum + d.tokens, 0);
      const daysScale = daysSum > 0 ? cycle.tokens / daysSum : 0;
      const billedByDate = new Map(
        cycle.days.map((d) => [d.date, d.tokens * daysScale]),
      );
      billed = points.map(
        (p) => billedByDate.get(bucketDateKey(p.bucketTimeUnixNano)) ?? 0,
      );
    } else {
      billed = points.map((p) => p.totalTokens * billedScale);
    }

    const weights = billed.map(() => 0);
    let cumulative = 0;
    for (let i = 0; i < billed.length; i++) {
      const before = cumulative;
      cumulative += billed[i]!;
      if (cumulative <= limit) continue;
      weights[i] =
        before >= limit ? 1 : (cumulative - limit) / (billed[i]! || 1);
    }
    // The rows are billed-scaled analytics, which track the billed series
    // within a fraction of a percent but not to the token — pin the "Total
    // tokens" row's overage to the usage card's number exactly.
    const billedOverage = Math.max(0, cycle.tokens - limit);
    const totals = points.map((p) => p.totalTokens * billedScale);
    const weightedTotal = totals.reduce(
      (sum, t, i) => sum + t * weights[i]!,
      0,
    );
    if (weightedTotal === 0) return weights.map(() => 0);
    const scale = billedOverage / weightedTotal;
    return weights.map((w) => w * scale);
  }, [data, limit, billedCycle, billedScale]);

  const loading = isFetching && !data;
  const failed = !loading && !data && isError;

  const totalTooltip = billedCycle
    ? "Billed tokens under management, attributed across metrics by the analytics distribution."
    : "Tokens for the selected range, from the analytics aggregates. Billed normalization applies to full billing cycles only.";
  const overageTooltip = billedCycle
    ? "The billed overage (tokens beyond the included allowance), attributed to each metric by its tokens recorded after the allowance ran out. The crossing day is prorated."
    : "The token allowance is an organization-per-cycle number; select a full billing cycle to see the overage.";

  return (
    <div className="border-border overflow-hidden rounded-lg border">
      <div className="flex items-baseline gap-2 px-4 pt-3 pb-1">
        <span className="text-sm font-semibold">
          Token Usage Cumulative Breakdown
        </span>
        <div className="ml-auto flex items-center gap-3">
          <button
            type="button"
            onClick={() => setCollapsed(new Set(groups.map((g) => g.heading)))}
            className="text-muted-foreground hover:text-foreground text-xs transition-colors"
          >
            Collapse all
          </button>
          <button
            type="button"
            onClick={() => setCollapsed(new Set())}
            className="text-muted-foreground hover:text-foreground text-xs transition-colors"
          >
            Expand all
          </button>
        </div>
      </div>
      <div className="text-muted-foreground flex items-center px-4 py-2 text-xs font-medium">
        <span className="flex-1">Metric</span>
        <SimpleTooltip tooltip={totalTooltip}>
          <span className="w-24 cursor-help text-right">Total</span>
        </SimpleTooltip>
        <SimpleTooltip tooltip={overageTooltip}>
          <span className="w-24 cursor-help text-right">Overage</span>
        </SimpleTooltip>
      </div>
      {loading && (
        <div className="space-y-3 p-4">
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-2/3" />
        </div>
      )}
      {failed && (
        <div className="text-muted-foreground border-border border-t px-4 py-8 text-center text-sm">
          Couldn't load usage details for this cycle. Try again shortly.
        </div>
      )}
      {!loading &&
        !failed &&
        groups.map((group) => (
          <DetailGroupSection
            key={group.heading}
            group={group}
            collapsed={collapsed.has(group.heading)}
            onToggle={() => toggleGroup(group.heading)}
            overageWeights={overageWeights}
          />
        ))}
    </div>
  );
}
