import { Sparkline } from "@/components/chart/Sparkline";
import { seriesPalette } from "@/components/chart/chart-theme";
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
import { isAttributionDim } from "@/pages/costs/taxonomy";
import { type BillingPeriod } from "./billing-cycles";
import {
  breakdownLabel,
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
  | "toolMessageTokens";

type MeasureRowSpec = {
  label: string;
  color: string;
  field: MeasureField;
};

// Built from the shared chart series palette (resolved at call time so
// theme/token changes apply without a rebuild) rather than a duplicated
// local color array.
function tokenTypeRows(palette: string[]): MeasureRowSpec[] {
  return [
    { label: "Input", color: palette[0]!, field: "inputTokens" },
    { label: "Output", color: palette[1]!, field: "outputTokens" },
    { label: "Cache read", color: palette[2]!, field: "cacheReadTokens" },
    { label: "Cache write", color: palette[3]!, field: "cacheWriteTokens" },
  ];
}

const TOOL_MESSAGE_ROW: MeasureRowSpec = {
  label: "Tokens from tool call messages",
  color: OTHER_COLOR,
  field: "toolMessageTokens",
};

// The UTC calendar day of a daily bucket, as "YYYY-MM-DD" — the key the
// billed per-day series (BillingCycle.days) aligns on. Bucket timestamps are
// unix-nano strings that exceed Number precision; divide as BigInt first.
function bucketDate(nano: string): string {
  try {
    return new Date(Number(BigInt(nano) / 1_000_000n))
      .toISOString()
      .slice(0, 10);
  } catch {
    return "";
  }
}

// Row color for a dimension value — same palette walk as the chart's stacks,
// so a value's dot matches its chart series color.
function valueColor(value: string, index: number, palette: string[]): string {
  if (value === "Other") return OTHER_COLOR;
  return palette[index % palette.length]!;
}

// The dimension sections of the details table, mirroring the chart's group
// stacks: same value order, "(unset)" labeling, attribution "" rows dropped.
function dimensionGroups(
  data: TumDetailsResult | undefined,
  keys: string[],
  palette: string[],
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
          ? "Users can hold multiple roles; rows overlap and can sum to more than the total token usage for the selected time period."
          : undefined,
      rows: visible.map((r, i) => ({
        label: r.value === "" ? "(unset)" : r.value,
        color: valueColor(r.value, i, palette),
        series: r.series,
        total: r.totalTokens,
      })),
    });
  }
  return groups;
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
        <Sparkline
          data={row.series}
          color={row.color}
          width={120}
          height={24}
        />
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
 * request (plus the risk series shared with the chart); closed periods cache
 * forever (their data is immutable).
 */
export function TumDetailsTable({
  period,
  projectId,
  limit,
}: {
  period: BillingPeriod;
  // Optional project scope, matching the page-level project filter.
  projectId: string | null;
  // Contracted monthly allowance; drives the per-metric overage share.
  limit: number | null;
}): JSX.Element {
  const client = useGramContext();
  const organization = useOrganization();
  const scope = { client, orgId: organization.id, period, projectId };
  const { data, isFetching, isError } = useQuery(tumDetailsQuery(scope));
  // Same query (and key) as the chart's risk series — React Query dedupes.
  const { data: riskData } = useQuery(riskPointsQuery(scope));

  // Sections collapsed via their header band, keyed by heading so the state
  // survives period/project switches.
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
  // concepts (the TUM contract has no per-project or sub-cycle split), so
  // both switch off when a project filter or a custom range narrows the data.
  const billedCycle = projectId == null ? period.cycle : null;

  // The table presents BILLED tokens: the analytics aggregate supplies the
  // distribution across metrics (it has the dimensions; billing's per-session
  // qualification can't be expressed there), and one uniform scale converts
  // it into billed units so the Total row equals the cycle's billed tokens —
  // the usage card's number — exactly. The two aggregates track within a
  // fraction of a percent, so the correction is invisible per metric.
  const billedScale = useMemo(() => {
    if (!billedCycle) return 1;
    const analyticsTotal = data?.totals?.totalTokens ?? 0;
    if (analyticsTotal === 0 || billedCycle.tokens === 0) return 1;
    return billedCycle.tokens / analyticsTotal;
  }, [data, billedCycle]);

  const groups = useMemo<DetailGroup[]>(() => {
    const palette = seriesPalette();
    const points = data?.points ?? [];
    const totals = data?.totals;
    // The risk series comes from a separate request — align it to the main
    // points grid by bucket timestamp rather than by array position.
    const riskyByBucket = new Map(
      (riskData?.points ?? []).map((p) => [
        p.bucketTimeUnixNano,
        p.riskyTokens,
      ]),
    );
    const riskySeries = points.map(
      (p) => riskyByBucket.get(p.bucketTimeUnixNano) ?? 0,
    );
    const riskyTotal = riskySeries.reduce((sum, v) => sum + v, 0);
    // Remainder against the same totals as the "Total tokens" row, so the two
    // risk rows sum to it exactly (the risk endpoint's own session aggregate
    // includes forwarded tokens that the analytics totals exclude).
    const cleanSeries = points.map((p, i) =>
      Math.max(0, p.totalTokens - riskySeries[i]!),
    );

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
            color: palette[0]!,
            series: points.map((p) => p.totalTokens),
            total: totals?.totalTokens ?? 0,
          },
        ],
      },
      ...dimensionGroups(data, LEAD_DIMENSION_SECTIONS, palette),
      {
        heading: "Token type",
        rows: tokenTypeRows(palette).map(measureRow),
      },
      {
        heading: "Sessions & messages",
        rows: [
          {
            label: "Sessions with risk findings",
            color: RISKY_COLOR,
            series: riskySeries,
            total: riskyTotal,
          },
          {
            label: "Sessions without risk findings",
            color: CLEAN_COLOR,
            series: cleanSeries,
            total: cleanSeries.reduce((sum, v) => sum + v, 0),
          },
          {
            label: "Messages with risk findings",
            // A stable palette slot distinct from the 4 token-type rows above.
            color: palette[4]!,
            series: points.map((p) => p.riskyMessageTokens),
            total: totals?.riskyMessageTokens ?? 0,
          },
          measureRow(TOOL_MESSAGE_ROW),
        ],
      },
      ...dimensionGroups(data, TAIL_DIMENSION_SECTIONS, palette),
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
  }, [data, riskData, billedScale]);

  // Time-based overage attribution: tokens count as overage from the moment
  // the ORGANIZATION's cumulative usage crossed the included allowance. Days
  // before the crossing weigh 0, days after weigh 1, and the crossing day is
  // prorated by how far into its tokens the allowance ran out (the data is
  // daily, so metrics are assumed to share the within-day distribution). The
  // crossing point comes from the cycle's org-wide BILLED daily series — a
  // project filter must not move it — so a project-scoped view shows that
  // project's share of the overage: its tokens recorded after the org
  // crossed.
  //
  // Null when overage does not apply: no contracted allowance, or a custom
  // range (the allowance is an org-cycle number).
  const overageWeights = useMemo<number[] | null>(() => {
    const cycle = period.cycle;
    if (limit == null || cycle == null) return null;
    const points = data?.points ?? [];

    // The daily series the crossing is walked on. Normally the cycle's
    // org-wide billed days; when the TUM response didn't carry them (the
    // synthesized active-cycle fallback has none), an all-zero walk would
    // silently zero the whole column — instead org scope falls back to the
    // billed-scaled analytics series, and project scope dashes out (its
    // filtered series can't locate the org-wide crossing).
    let billed: number[];
    if (cycle.days.length > 0) {
      const billedByDate = new Map(cycle.days.map((d) => [d.date, d.tokens]));
      billed = points.map(
        (p) => billedByDate.get(bucketDate(p.bucketTimeUnixNano)) ?? 0,
      );
    } else if (billedCycle) {
      billed = points.map((p) => p.totalTokens * billedScale);
    } else {
      return null;
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
    if (!billedCycle) {
      // Project scope: the raw weights are exact against the billed series,
      // and weighting the project's rows by them yields its true share.
      return weights;
    }
    // Org scope: the rows are billed-scaled analytics, which track the billed
    // series within a fraction of a percent but not to the token — pin the
    // "Total tokens" row's overage to the usage card's number exactly.
    const billedOverage = Math.max(0, cycle.tokens - limit);
    const totals = points.map((p) => p.totalTokens * billedScale);
    const weightedTotal = totals.reduce(
      (sum, t, i) => sum + t * weights[i]!,
      0,
    );
    if (weightedTotal === 0) return weights.map(() => 0);
    const scale = billedOverage / weightedTotal;
    return weights.map((w) => w * scale);
  }, [data, limit, period.cycle, billedCycle, billedScale]);

  const loading = isFetching && !data;
  const failed = !loading && !data && isError;

  const totalTooltip = billedCycle
    ? "Billed tokens under management, attributed across metrics by the analytics distribution."
    : "Tokens for the selected slice, from the analytics aggregates. Billed normalization applies to full organization billing cycles only.";
  let overageTooltip: string;
  if (billedCycle) {
    overageTooltip =
      "The billed overage (tokens beyond the included allowance), attributed to each metric by its tokens recorded after the allowance ran out. The crossing day is prorated.";
  } else if (period.cycle) {
    overageTooltip =
      "This project's tokens recorded after the organization's usage crossed the included allowance (its share of the overage, measured in the project's analytics tokens). The crossing day is prorated.";
  } else {
    overageTooltip =
      "The token allowance is an organization-per-cycle number; select a full billing cycle to see the overage.";
  }

  return (
    <div className="border-border overflow-hidden border">
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
