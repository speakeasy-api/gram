import { useOrganization } from "@/contexts/Auth";
import { Dimension } from "@gram/client/models/components/queryfilter.js";
import { type TumDetailsResult } from "@gram/client/models/components/tumdetailsresult.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useQuery } from "@tanstack/react-query";
import { Info } from "lucide-react";
import { useMemo } from "react";
import { Skeleton } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { isAttributionDim } from "@/pages/costs/taxonomy";
import { type BillingCycle } from "./billing-cycles";
import {
  breakdownLabel,
  CHART_COLORS,
  CLEAN_COLOR,
  OTHER_COLOR,
  RISKY_COLOR,
} from "./breakdown-options";
import { riskPointsQuery, tumDetailsQuery } from "./tum-queries";

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
  // Only token-denominated rows get an overage share; counts show "—".
  kind: "tokens" | "count";
};

type DetailGroup = {
  heading: string;
  // Optional caveat rendered beside the heading (e.g. overlapping rows).
  note?: string;
  rows: DetailRow[];
};

// The dimension sections, split so the headline cuts (model, provider, MCP
// server) sit directly under Total and the rest follow the token/risk groups.
const LEAD_DIMENSION_SECTIONS: string[] = [
  Dimension.Model,
  Dimension.Provider,
  Dimension.McpServerName,
];
const TAIL_DIMENSION_SECTIONS: string[] = [
  Dimension.AccountType,
  Dimension.DivisionName,
  Dimension.Email,
  Dimension.Role,
  Dimension.HookSource,
  Dimension.SkillName,
  Dimension.McpToolName,
];

// A measure carried by both the daily points and the whole-range totals.
type MeasureField =
  | "inputTokens"
  | "outputTokens"
  | "cacheReadTokens"
  | "cacheWriteTokens"
  | "agentSessions"
  | "toolCalls"
  | "activeUsers"
  | "toolMessageTokens";

type MeasureRowSpec = {
  label: string;
  color: string;
  field: MeasureField;
  kind: DetailRow["kind"];
};

const TOKEN_TYPE_ROWS: MeasureRowSpec[] = [
  {
    label: "Input",
    color: CHART_COLORS[0]!,
    field: "inputTokens",
    kind: "tokens",
  },
  {
    label: "Output",
    color: CHART_COLORS[1]!,
    field: "outputTokens",
    kind: "tokens",
  },
  {
    label: "Cache read",
    color: CHART_COLORS[2]!,
    field: "cacheReadTokens",
    kind: "tokens",
  },
  {
    label: "Cache write",
    color: CHART_COLORS[3]!,
    field: "cacheWriteTokens",
    kind: "tokens",
  },
];

const ACTIVITY_ROWS: MeasureRowSpec[] = [
  {
    label: "Agent sessions",
    color: "#38bdf8",
    field: "agentSessions",
    kind: "count",
  },
  { label: "Tool calls", color: "#4ade80", field: "toolCalls", kind: "count" },
  {
    label: "Active users",
    color: "#facc15",
    field: "activeUsers",
    kind: "count",
  },
  {
    label: "Tokens from tool call messages",
    color: "#94a3b8",
    field: "toolMessageTokens",
    kind: "tokens",
  },
];

// Row color for a dimension value — same palette walk as the chart's stacks,
// so a value's dot matches its chart series color.
function valueColor(value: string, index: number): string {
  if (value === "Other") return OTHER_COLOR;
  return CHART_COLORS[index % CHART_COLORS.length]!;
}

// The dimension sections of the details table, mirroring the chart's group
// stacks: same value order, "(unset)" labeling, attribution "" rows dropped.
function dimensionGroups(
  data: TumDetailsResult | undefined,
  keys: string[],
): DetailGroup[] {
  const byKey = new Map(
    (data?.breakdowns ?? []).map((b) => [b.key, b.rows] as const),
  );
  const groups: DetailGroup[] = [];
  for (const key of keys) {
    const rows = byKey.get(key);
    if (!rows) continue;
    // Attribution "" rows are not-applicable spend (same rule as the chart);
    // zero-token rows (e.g. groups with only tool calls) are noise.
    const visible = rows.filter(
      (r) =>
        r.totalTokens > 0 &&
        (!isAttributionDim(key as Dimension) || r.value !== ""),
    );
    if (visible.length === 0) continue;
    groups.push({
      heading: breakdownLabel(key),
      // Roles are multi-valued: a user can hold several, and a session's
      // tokens count once under each — so these rows overlap and can sum to
      // more than the total.
      note:
        key === Dimension.Role
          ? "Users can hold multiple roles; rows overlap and can sum to more than the total."
          : undefined,
      rows: visible.map((r, i) => ({
        label: r.value === "" ? "(unset)" : r.value,
        color: valueColor(r.value, i),
        series: r.series,
        total: r.totalTokens,
        kind: "tokens",
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
    row.kind === "tokens" && overageWeights !== null
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

/**
 * Per-metric usage details for one billing cycle, rendered under the token
 * usage chart. Everything comes from a single telemetry.queryTumDetails
 * request (plus the risk series shared with the chart); closed cycles cache
 * forever (their data is immutable).
 */
export function TumDetailsTable({
  cycle,
  projectId,
  limit,
}: {
  cycle: BillingCycle;
  // Optional project scope, matching the page-level project filter.
  projectId: string | null;
  // Contracted monthly allowance; drives the per-metric overage share.
  limit: number | null;
}): JSX.Element {
  const client = useGramContext();
  const organization = useOrganization();
  const scope = { client, orgId: organization.id, cycle, projectId };
  const { data, isFetching, isError } = useQuery(tumDetailsQuery(scope));
  // Same query (and key) as the chart's risk series — React Query dedupes.
  const { data: riskData } = useQuery(riskPointsQuery(scope));

  // Billed normalization and overage attribution are organization-level
  // concepts (the TUM contract has no per-project split), so both switch off
  // when a project filter narrows the data.
  const orgScoped = projectId == null;

  // The table presents BILLED tokens: the analytics aggregate supplies the
  // distribution across metrics (it has the dimensions; billing's per-session
  // qualification can't be expressed there), and one uniform scale converts
  // it into billed units so the Total row equals the cycle's billed tokens —
  // the usage card's number — exactly. The two aggregates track within a
  // fraction of a percent, so the correction is invisible per metric.
  const billedScale = useMemo(() => {
    if (!orgScoped) return 1;
    const analyticsTotal = data?.totals?.totalTokens ?? 0;
    if (analyticsTotal === 0 || cycle.tokens === 0) return 1;
    return cycle.tokens / analyticsTotal;
  }, [data, cycle.tokens, orgScoped]);

  const groups = useMemo<DetailGroup[]>(() => {
    const points = data?.points ?? [];
    const totals = data?.totals;
    const riskPoints = riskData?.points ?? [];
    const riskyTotal = riskPoints.reduce((sum, p) => sum + p.riskyTokens, 0);
    // Remainder against the same totals as the "Total tokens" row, so the two
    // risk rows sum to it exactly (the risk endpoint's own session aggregate
    // includes forwarded tokens that the analytics totals exclude).
    const cleanSeries = points.map((p, i) =>
      Math.max(0, p.totalTokens - (riskPoints[i]?.riskyTokens ?? 0)),
    );

    const measureRow = (spec: MeasureRowSpec): DetailRow => ({
      label: spec.label,
      color: spec.color,
      series: points.map((p) => p[spec.field]),
      total: totals?.[spec.field] ?? 0,
      kind: spec.kind,
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
            kind: "tokens",
          },
        ],
      },
      ...dimensionGroups(data, LEAD_DIMENSION_SECTIONS),
      { heading: "Token type", rows: TOKEN_TYPE_ROWS.map(measureRow) },
      {
        heading: "Risk findings",
        rows: [
          {
            label: "Sessions with risk findings",
            color: RISKY_COLOR,
            series: riskPoints.map((p) => p.riskyTokens),
            total: riskyTotal,
            kind: "tokens",
          },
          {
            label: "Sessions without risk findings",
            color: CLEAN_COLOR,
            series: cleanSeries,
            total: cleanSeries.reduce((sum, v) => sum + v, 0),
            kind: "tokens",
          },
          {
            label: "Messages with risk findings",
            color: "#e879f9",
            series: points.map((p) => p.riskyMessageTokens),
            total: totals?.riskyMessageTokens ?? 0,
            kind: "tokens",
          },
        ],
      },
      ...dimensionGroups(data, TAIL_DIMENSION_SECTIONS),
      { heading: "Activity", rows: ACTIVITY_ROWS.map(measureRow) },
    ];

    // Convert every token-denominated row into billed units (see billedScale).
    return raw.map((group) => ({
      ...group,
      rows: group.rows.map((row) =>
        row.kind === "tokens"
          ? {
              ...row,
              total: Math.round(row.total * billedScale),
              series: row.series.map((v) => v * billedScale),
            }
          : row,
      ),
    }));
  }, [data, riskData, billedScale]);

  // Time-based overage attribution: tokens count as overage from the moment
  // the cycle's cumulative usage crossed the included allowance. Days before
  // the crossing weigh 0, days after weigh 1, and the crossing day is prorated
  // by how far into its tokens the allowance ran out (the data is daily, so
  // metrics are assumed to share the within-day distribution). The weights are
  // then scaled so the "Total tokens" row reproduces the BILLED overage
  // (cycle TUM tokens − allowance) to the token — the analytics totals the
  // weights are computed from run slightly apart from the billed aggregate,
  // and the usage card's Overage stat is the number to agree with.
  //
  // Null when overage does not apply: no contracted allowance, or a project
  // filter is active (the allowance is an org-level number).
  const overageWeights = useMemo<number[] | null>(() => {
    if (limit == null || !orgScoped) return null;
    // The crossing point is found on the billed-unit series (see billedScale),
    // matching the allowance's own units.
    const totals = data?.points.map((p) => p.totalTokens * billedScale) ?? [];
    const weights = totals.map(() => 0);
    let cumulative = 0;
    for (let i = 0; i < totals.length; i++) {
      const before = cumulative;
      cumulative += totals[i]!;
      if (cumulative <= limit) continue;
      weights[i] =
        before >= limit ? 1 : (cumulative - limit) / (totals[i]! || 1);
    }
    const billedOverage = Math.max(0, cycle.tokens - limit);
    const weightedTotal = totals.reduce(
      (sum, t, i) => sum + t * weights[i]!,
      0,
    );
    if (weightedTotal === 0) return weights.map(() => 0);
    const scale = billedOverage / weightedTotal;
    return weights.map((w) => w * scale);
  }, [data, limit, cycle.tokens, billedScale, orgScoped]);

  const loading = isFetching && !data;
  const failed = !loading && !data && isError;

  const totalTooltip = orgScoped
    ? "Billed tokens under management, attributed across metrics by the analytics distribution."
    : "Tokens for the selected project, from the analytics aggregates. Billed normalization applies to the whole organization only.";
  const overageTooltip = orgScoped
    ? "The billed overage (tokens beyond the included allowance), attributed to each metric by its tokens recorded after the allowance ran out. The crossing day is prorated."
    : "The token allowance is an organization-level number; select All projects to see the overage.";

  return (
    <div className="border-border overflow-hidden rounded-lg border">
      <div className="flex items-baseline gap-2 px-4 pt-3 pb-1">
        <span className="text-sm font-semibold">Usage details</span>
        <span className="text-muted-foreground text-xs">
          Cumulative totals over the selected billing cycle
        </span>
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
          <div key={group.heading}>
            <div className="bg-muted/50 text-muted-foreground border-border flex items-center gap-1.5 border-t px-4 py-1.5 text-xs">
              <span className="font-medium">{group.heading}</span>
              {group.note && (
                <SimpleTooltip tooltip={group.note}>
                  <Info className="size-3 cursor-help" />
                </SimpleTooltip>
              )}
            </div>
            <div className="divide-border divide-y">
              {group.rows.map((row) => (
                <DetailRowItem
                  key={row.label}
                  row={row}
                  overageWeights={overageWeights}
                />
              ))}
            </div>
          </div>
        ))}
    </div>
  );
}
