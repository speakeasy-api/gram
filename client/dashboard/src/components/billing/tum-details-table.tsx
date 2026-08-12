import { useOrganization } from "@/contexts/Auth";
import { Dimension } from "@gram/client/models/components/queryfilter.js";
import { type TumDetailsResult } from "@gram/client/models/components/tumdetailsresult.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, Info } from "lucide-react";
import { useMemo, useState } from "react";
import { Skeleton } from "@/components/ui/Skeleton";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import {
  useOtherSeriesColor,
  useSeriesColors,
} from "@/components/chart/useSeriesColors";
import {
  type BilledDays,
  type BillingCycle,
  type BillingPeriod,
  type PeriodFigures,
  bucketDateKey,
} from "./billing-cycles";
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
  // The label is an unresolved id (e.g. a deleted project's UUID): render it
  // as a truncated mono chip instead of a raw UUID in running text.
  mono?: boolean;
};

// A raw UUID label means the id could not be mapped to a display name.
const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

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
  // Slot in the theme-resolved series ramp, so the dot matches the chart's
  // series color in both themes.
  colorIndex: number;
  field: MeasureField;
};

// Input + output + cache writes sum to the TUM total; cache reads are
// excluded from the population entirely.
const TOKEN_TYPE_ROWS: MeasureRowSpec[] = [
  { label: "Input", colorIndex: 0, field: "inputTokens" },
  { label: "Output", colorIndex: 1, field: "outputTokens" },
  {
    label: "Cache write",
    colorIndex: 2,
    field: "cacheCreationTokens",
  },
];

// Row color for a dimension value — same palette walk as the chart's stacks
// (the theme-resolved ramp), so a value's dot matches its chart series color.
// The neutral remainder dot uses the SAME rollup identity test as the chart
// (isServerRollupRow), never a label match — a real value that happens to
// read "Other" keeps its palette color in both places.
function valueColor(
  rollup: boolean,
  index: number,
  chartColors: string[],
  otherColor: string,
): string {
  if (rollup) return otherColor;
  return chartColors[index % chartColors.length]!;
}

// The dimension sections of the details table, mirroring the chart's group
// stacks: same value order, "(unset)" labeling for unattributed traffic,
// project UUIDs mapped to names.
function dimensionGroups(
  data: TumDetailsResult | undefined,
  keys: string[],
  projectNames: Map<string, string>,
  chartColors: string[],
  otherColor: string,
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
      rows: visible.map(({ row: r, rollup }, i) => {
        const label = breakdownValueLabel(key, r.value, projectNames);
        return {
          label,
          color: valueColor(rollup, i, chartColors, otherColor),
          series: r.series,
          total: r.totalTokens,
          // A Project row still carrying its UUID is a project the name map
          // doesn't know (e.g. deleted) — show a truncated mono id instead.
          mono: key === Dimension.ProjectId && UUID_RE.test(label),
        };
      }),
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
      {row.mono ? (
        <span
          className="min-w-0 flex-1 truncate font-mono text-xs"
          title={row.label}
        >
          {`${row.label.slice(0, 8)}…`}
        </span>
      ) : (
        <span className="min-w-0 flex-1 truncate text-sm">{row.label}</span>
      )}
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
        className="text-eyebrow hover:text-foreground border-border dark:border-white/20 flex w-full cursor-pointer items-center gap-1.5 border-t px-4 py-1.5 transition-colors"
      >
        <ChevronDown
          className={cn(
            "size-3 transition-transform",
            collapsed && "-rotate-90",
          )}
        />
        <span>{group.heading}</span>
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

// Rescale raw overage weights so the "Total tokens" row's weighted sum lands
// exactly on the billed overage target. When the analytics carry no tokens
// where the weights are nonzero there is nothing to attribute over: a zero
// target yields a clean all-zero column, a positive one is unattributable
// (null — the "—" column) rather than silently rendering as zero overage.
function pinWeights(
  weights: number[],
  points: { totalTokens: number }[],
  billedScale: number,
  target: number,
): number[] | null {
  const billedTarget = Math.max(0, Math.round(target));
  const weightedTotal = points.reduce(
    (sum, p, i) => sum + p.totalTokens * billedScale * weights[i]!,
    0,
  );
  if (weightedTotal === 0) {
    return billedTarget > 0 ? null : weights.map(() => 0);
  }
  const scale = billedTarget / weightedTotal;
  return weights.map((w) => w * scale);
}

// What the Total column carries: billed-normalized tokens whenever the
// billed data covers the view (full cycles and ranges within them), raw
// analytics otherwise.
function totalTooltipFor(billedNormalized: boolean): string {
  if (billedNormalized) {
    return "Billed tokens under management, attributed across metrics by the analytics distribution.";
  }
  return "Tokens for the selected range, from the analytics aggregates. Billed normalization does not apply because this range cannot be fully represented by the billed daily data.";
}

// What the Overage column means in the current view: full-cycle attribution,
// range attribution, or not attributable at all (the "—" column).
function overageTooltipFor(
  billedCycle: BillingCycle | null,
  attributed: boolean,
): string {
  if (billedCycle) {
    return "The billed overage (tokens beyond the included allowance), attributed to each metric by its tokens recorded after the allowance ran out. The crossing day is prorated.";
  }
  if (attributed) {
    return "The range's billed overage (tokens recorded after the cycle's cumulative usage crossed the included allowance), attributed to each metric by its tokens in that window. The crossing day is prorated.";
  }
  return "Overage can't be attributed here — no contracted allowance is set, the range cannot be fully represented by the billed daily data, or there are no analytics tokens in the overage window to attribute it over.";
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
  billedDays,
  overageDays,
  figures,
}: {
  period: BillingPeriod;
  // Project id → name, for labeling the Project section's UUID values.
  projectNames: Map<string, string>;
  // Contracted monthly allowance; drives the per-metric overage share.
  limit: number | null;
  // The billed per-day series the per-day overage fractions divide by.
  billedDays: BilledDays;
  // Per-day billed overage across covered cycles; null when the org has no
  // contracted allowance.
  overageDays: Map<string, number> | null;
  // The shared resolved figures — the same tokens/overage the usage card
  // displays, which the Total row pins to exactly.
  figures: PeriodFigures;
}): JSX.Element {
  const client = useGramContext();
  const organization = useOrganization();
  const scope = { client, orgId: organization.id, period };
  const { data, isFetching, isError } = useQuery(tumDetailsQuery(scope));
  // Theme-resolved series ramp and rollup neutral, matching the chart the
  // table sits under.
  const chartColors = useSeriesColors();
  const otherColor = useOtherSeriesColor();

  // The passed-in map comes from the projects list fetch; the session's own
  // project entries fill any gaps (e.g. before that fetch resolves) so
  // Project rows show names instead of raw UUIDs whenever possible.
  const projectLabels = useMemo(() => {
    const merged = new Map(projectNames);
    for (const p of organization.projects) {
      if (!merged.has(p.id)) merged.set(p.id, p.name);
    }
    return merged;
  }, [projectNames, organization.projects]);

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

  // Billed normalization and overage attribution apply to full cycles and
  // to custom ranges the billed daily series fully covers; both switch off
  // when the range escapes the billed data.
  const billedCycle = period.cycle;
  const rangeCovered = billedCycle == null && figures.covered;

  // The table presents BILLED tokens: the analytics aggregate supplies the
  // distribution across metrics (it has the dimensions; billing's per-session
  // qualification can't be expressed there), and one uniform scale converts
  // it into billed units so the Total row equals the billed tokens for the
  // period — the usage card's number — exactly. The two aggregates track
  // within a fraction of a percent, so the correction is invisible per
  // metric.
  const billedScale = useMemo(() => {
    const analyticsTotal = data?.totals?.totalTokens ?? 0;
    if (analyticsTotal === 0) return 1;
    if (billedCycle) {
      // A CLOSED zero-token cycle is a known zero: scale everything to 0 so
      // the Total row matches the card even when live analytics recomputed
      // nonzero tokens after the seal. The active cycle is exempt — its card
      // total is a live number that can trail the details query by a refetch,
      // and a transient zero must not blank real traffic.
      if (billedCycle.tokens === 0) {
        return billedCycle.current ? 1 : 0;
      }
      return billedCycle.tokens / analyticsTotal;
    }
    if (!rangeCovered || figures.tokens == null) return 1;
    // Covered custom range: normalize to the billed range total — the
    // card's number. Covered windows with zero billed tokens are sealed
    // zeros (an active cycle's empty window is never covered), so a zero
    // total scales to zero exactly like a sealed zero-token cycle.
    if (figures.tokens === 0) return 0;
    return figures.tokens / analyticsTotal;
  }, [data, billedCycle, rangeCovered, figures.tokens]);

  const groups = useMemo<DetailGroup[]>(() => {
    const points = data?.points ?? [];
    const totals = data?.totals;

    const measureRow = (spec: MeasureRowSpec): DetailRow => ({
      label: spec.label,
      color: chartColors[spec.colorIndex]!,
      series: points.map((p) => p[spec.field]),
      total: totals?.[spec.field] ?? 0,
    });

    const raw: DetailGroup[] = [
      {
        heading: "Total",
        rows: [
          {
            label: "Total tokens",
            color: chartColors[0]!,
            series: points.map((p) => p.totalTokens),
            total: totals?.totalTokens ?? 0,
          },
        ],
      },
      ...dimensionGroups(
        data,
        LEAD_DIMENSION_SECTIONS,
        projectLabels,
        chartColors,
        otherColor,
      ),
      { heading: "Token type", rows: TOKEN_TYPE_ROWS.map(measureRow) },
      ...dimensionGroups(
        data,
        TAIL_DIMENSION_SECTIONS,
        projectLabels,
        chartColors,
        otherColor,
      ),
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
  }, [data, billedScale, projectLabels, chartColors, otherColor]);

  // Time-based overage attribution: tokens count as overage from the moment
  // the organization's cumulative usage crossed the included allowance. Days
  // before the crossing weigh 0, days after weigh 1, and the crossing day is
  // prorated (the data is daily, so metrics are assumed to share the
  // within-day distribution). Full cycles and covered ranges both read the
  // per-day fractions from the shared overageDays/billedDays walks — the
  // same maps behind the usage card's Overage figure — and pin the "Total
  // tokens" row to the card's number exactly (the rows are billed-scaled
  // analytics, which track the billed series closely but not to the token).
  //
  // Null when overage does not apply or can't be attributed: no contracted
  // allowance, a range that escapes the billed daily data, or no analytics
  // tokens in the overage window to spread it over (the "—" column).
  const overageWeights = useMemo<number[] | null>(() => {
    const cycle = billedCycle;
    if (limit == null) return null;
    const points = data?.points ?? [];

    // Synthesized active cycle without a daily series: there is no billed
    // per-day shape, so walk the crossing on the billed-scaled analytics
    // series directly.
    if (cycle != null && cycle.days.length === 0) {
      const billed = points.map((p) => p.totalTokens * billedScale);
      const weights = billed.map(() => 0);
      let cumulative = 0;
      for (let i = 0; i < billed.length; i++) {
        const before = cumulative;
        cumulative += billed[i]!;
        if (cumulative <= limit) continue;
        weights[i] =
          before >= limit ? 1 : (cumulative - limit) / (billed[i]! || 1);
      }
      return pinWeights(weights, points, billedScale, cycle.tokens - limit);
    }

    if (overageDays == null) return null;
    if (cycle == null && !rangeCovered) return null;
    // Per-day overage fraction of the billed series, applied to each
    // metric's tokens that day.
    const weights = points.map((p) => {
      const key = bucketDateKey(p.bucketTimeUnixNano);
      const dayBilled = billedDays.byDate.get(key) ?? 0;
      if (dayBilled <= 0) return 0;
      return (overageDays.get(key) ?? 0) / dayBilled;
    });
    const target =
      cycle != null ? cycle.tokens - limit : (figures.overage ?? 0);
    return pinWeights(weights, points, billedScale, target);
  }, [
    data,
    limit,
    billedCycle,
    billedScale,
    billedDays,
    overageDays,
    rangeCovered,
    figures.overage,
  ]);

  const loading = isFetching && !data;
  const failed = !loading && !data && isError;

  const totalTooltip = totalTooltipFor(billedCycle != null || rangeCovered);
  const overageTooltip = overageTooltipFor(
    billedCycle,
    overageWeights !== null,
  );

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
      <div className="text-eyebrow flex items-center px-4 py-2">
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
