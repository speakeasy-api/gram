import { telemetryQuery } from "@gram/client/funcs/telemetryQuery";
import {
  Dimension,
  type GroupBy,
  type QueryFilter,
  type QueryRow,
} from "@gram/client/models/components";
import { useGramContext } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { useDateRangeFilter } from "@/components/observe/useDateRangeFilter";
import { useSlugs } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { type CardSpec, CostWidgets } from "./CostWidgets";
import { EntityProfile } from "./EntityProfile";
import {
  BREAKDOWN_PARAM,
  type Crumb,
  defaultGroupBy,
  encodeCrumb,
  isDimension,
  LABELS,
  type Measures,
  nextDimension,
  parseDrillPath,
  PIVOTS,
} from "./taxonomy";

const EMPTY_MEASURES: Measures = { cost: 0, sessions: 0, tools: 0, tokens: 0 };

// Per-breakdown secondary cuts shown as "mix" widgets above the table. Keyed by
// the current group-by axis; complementary to it (never the same dimension).
// The "who's driving it" + "what's the lever" cards per level (see widget plan).
// Email → rendered as "Top spenders"; HookSource also gets a Cost/session stat.
const MIX_DIMS: Partial<Record<Dimension, Dimension[]>> = {
  [Dimension.DivisionName]: [Dimension.DepartmentName, Dimension.Email],
  [Dimension.DepartmentName]: [Dimension.Email, Dimension.Model],
  [Dimension.Group]: [Dimension.Email, Dimension.HookSource],
  [Dimension.Email]: [Dimension.HookSource],
  [Dimension.HookSource]: [Dimension.Model],
  [Dimension.JobTitle]: [Dimension.Email, Dimension.DepartmentName],
  [Dimension.EmployeeType]: [Dimension.Email, Dimension.DepartmentName],
  [Dimension.CostCenterName]: [Dimension.Email, Dimension.DepartmentName],
  [Dimension.Role]: [Dimension.Email, Dimension.DepartmentName],
  [Dimension.Model]: [Dimension.Email, Dimension.HookSource],
};

/**
 * Top-level cost explorer — the org bird's-eye view that walks the taxonomy.
 * It owns the drill state (the filter `path` and the current `groupBy` axis),
 * runs one telemetry.query per level, and renders the generalized
 * {@link EntityProfile} for the current node (the org root when `path` is empty,
 * otherwise the entity last drilled into).
 */
export function CostsExplorer(): JSX.Element {
  const { projectSlug } = useSlugs();
  const routes = useRoutes();
  const location = useLocation();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();

  // Drill state is the URL. The filter `path` is encoded as pathname segments
  // (so the breadcrumb tracks it and the view is shareable/refresh-safe); the
  // leaf breakdown axis rides in `?by=`. Both are derived here, never held in
  // component state — navigation is the only way they change.
  const costsBase = routes.costs.href();
  const path: Crumb[] = useMemo(() => {
    const tail = location.pathname.startsWith(costsBase)
      ? location.pathname.slice(costsBase.length)
      : "";
    return parseDrillPath(tail);
  }, [location.pathname, costsBase]);

  const byParam = searchParams.get(BREAKDOWN_PARAM);
  const groupBy = isDimension(byParam) ? byParam : defaultGroupBy(path);

  // Navigate to a node: encode its filter path into the URL and pin the
  // breakdown axis in `?by=`. `replace` for view-only changes (re-pivoting)
  // that shouldn't add a back-button step.
  const goToNode = (nextPath: Crumb[], by: Dimension, replace = false) => {
    const tail = nextPath.map(encodeCrumb).join("/");
    // Preserve the rest of the query (the date-range filter lives here too) and
    // only override the breakdown axis.
    const params = new URLSearchParams(searchParams);
    params.set(BREAKDOWN_PARAM, by);
    const url = `${tail ? `${costsBase}/${tail}` : costsBase}?${params.toString()}`;
    void navigate(url, { replace });
  };

  const {
    dateRange,
    customRange,
    customRangeLabel,
    from,
    to,
    setDateRangeParam,
    setCustomRangeParam,
    clearCustomRange,
  } = useDateRangeFilter("30d");

  const filters: QueryFilter[] = useMemo(
    () => path.map((c) => ({ dimension: c.dim, values: [c.value] })),
    [path],
  );

  // The generated useTelemetryQuery hook keys its cache on gramSession only — it
  // ignores the request body — so every drill would return the first cached
  // result. Drive useQuery directly with a key that encodes the payload.
  const client = useGramContext();
  const { data, isFetching, isError } = useQuery({
    queryKey: [
      "costs-explorer",
      from.toISOString(),
      to.toISOString(),
      groupBy,
      filters,
    ],
    queryFn: () =>
      unwrapAsync(
        telemetryQuery(client, {
          queryPayload: {
            from,
            to,
            groupBy: groupBy as GroupBy,
            sortBy: "total_cost",
            topN: 100,
            // Daily buckets → ~30 points per group for the row trend sparklines.
            granularitySeconds: 86400,
            filters: filters.length ? filters : undefined,
          },
        }),
      ),
  });

  // The current entity's own attributes: a no-group_by query over the same
  // filters returns a single aggregate row whose dimension_values are this
  // entity's distinct division/department/job_title/roles/etc. Only meaningful
  // once we've drilled into something.
  const { data: detailData } = useQuery({
    queryKey: [
      "costs-explorer-detail",
      from.toISOString(),
      to.toISOString(),
      filters,
    ],
    enabled: path.length > 0,
    queryFn: () =>
      unwrapAsync(
        telemetryQuery(client, {
          queryPayload: {
            from,
            to,
            topN: 1,
            filters: filters.length ? filters : undefined,
          },
        }),
      ),
  });
  const attributes = detailData?.table?.[0]?.dimensionValues;

  // Previous equal-length period (immediately before [from, to]) — for the
  // per-group % change column.
  const { prevFrom, prevTo } = useMemo(() => {
    const durationMs = to.getTime() - from.getTime();
    return { prevFrom: new Date(from.getTime() - durationMs), prevTo: from };
  }, [from, to]);
  const { data: prevData } = useQuery({
    queryKey: [
      "costs-explorer-prev",
      prevFrom.toISOString(),
      prevTo.toISOString(),
      groupBy,
      filters,
    ],
    queryFn: () =>
      unwrapAsync(
        telemetryQuery(client, {
          queryPayload: {
            from: prevFrom,
            to: prevTo,
            groupBy: groupBy as GroupBy,
            sortBy: "total_cost",
            topN: 100,
            filters: filters.length ? filters : undefined,
          },
        }),
      ),
  });

  const rows = data?.table ?? [];

  // Roll the child rows up into the current entity's headline stats.
  const stats: Measures = useMemo(() => {
    const table = data?.table ?? [];
    return table.reduce<Measures>(
      (acc, r) => ({
        cost: acc.cost + (r.measures.totalCost ?? 0),
        sessions: acc.sessions + (r.measures.totalChats ?? 0),
        tools: acc.tools + (r.measures.totalToolCalls ?? 0),
        tokens: acc.tokens + (r.measures.totalTokens ?? 0),
      }),
      { ...EMPTY_MEASURES },
    );
  }, [data]);

  // Per-group daily cost series for the trend sparklines, keyed by group value.
  const seriesByGroup = useMemo(() => {
    const map = new Map<string, number[]>();
    for (const series of data?.timeseries ?? []) {
      map.set(
        series.groupValue,
        series.points.map((p) => p.measures.totalCost ?? 0),
      );
    }
    return map;
  }, [data]);

  // Previous-period totals per measure (for the KPI deltas).
  const prevTotals: Measures = useMemo(() => {
    const table = prevData?.table ?? [];
    return table.reduce<Measures>(
      (acc, r) => ({
        cost: acc.cost + (r.measures.totalCost ?? 0),
        sessions: acc.sessions + (r.measures.totalChats ?? 0),
        tools: acc.tools + (r.measures.totalToolCalls ?? 0),
        tokens: acc.tokens + (r.measures.totalTokens ?? 0),
      }),
      { ...EMPTY_MEASURES },
    );
  }, [prevData]);

  // Each measure summed across groups per time bucket — drives the hero trend
  // chart and the KPI sparklines.
  const widgetSeries = useMemo(() => {
    const ts = data?.timeseries ?? [];
    const n = ts[0]?.points.length ?? 0;
    const cost = Array<number>(n).fill(0);
    const chats = Array<number>(n).fill(0);
    const tools = Array<number>(n).fill(0);
    const tokens = Array<number>(n).fill(0);
    for (const s of ts) {
      s.points.forEach((p, i) => {
        cost[i] = (cost[i] ?? 0) + (p.measures.totalCost ?? 0);
        chats[i] = (chats[i] ?? 0) + (p.measures.totalChats ?? 0);
        tools[i] = (tools[i] ?? 0) + (p.measures.totalToolCalls ?? 0);
        tokens[i] = (tokens[i] ?? 0) + (p.measures.totalTokens ?? 0);
      });
    }
    return { cost, chats, tools, tokens };
  }, [data]);

  // Per-level secondary breakdowns: the configured cuts for the current axis,
  // minus any already filtered or that don't vary within this slice (≤1 value).
  const mixDims = (MIX_DIMS[groupBy] ?? [Dimension.Model]).filter(
    (d) =>
      d !== groupBy &&
      !path.some((c) => c.dim === d) &&
      (!attributes || (attributes[d]?.length ?? 0) > 1),
  );
  const mixDimA = mixDims[0];
  const mixDimB = mixDims[1];

  const { data: mixDataA, isLoading: mixLoadingA } = useQuery({
    queryKey: [
      "costs-explorer-mix-a",
      from.toISOString(),
      to.toISOString(),
      mixDimA,
      filters,
    ],
    enabled: !!mixDimA,
    queryFn: () =>
      unwrapAsync(
        telemetryQuery(client, {
          queryPayload: {
            from,
            to,
            groupBy: mixDimA as GroupBy,
            sortBy: "total_cost",
            topN: 5,
            filters: filters.length ? filters : undefined,
          },
        }),
      ),
  });
  const { data: mixDataB, isLoading: mixLoadingB } = useQuery({
    queryKey: [
      "costs-explorer-mix-b",
      from.toISOString(),
      to.toISOString(),
      mixDimB,
      filters,
    ],
    enabled: !!mixDimB,
    queryFn: () =>
      unwrapAsync(
        telemetryQuery(client, {
          queryPayload: {
            from,
            to,
            groupBy: mixDimB as GroupBy,
            sortBy: "total_cost",
            topN: 5,
            filters: filters.length ? filters : undefined,
          },
        }),
      ),
  });

  const cards: CardSpec[] = useMemo(() => {
    const out: CardSpec[] = [];
    const toRows = (t: QueryRow[]) =>
      t.map((r) => ({ label: r.groupValue, cost: r.measures.totalCost ?? 0 }));
    const cardTitle = (dim: Dimension) =>
      dim === Dimension.Email
        ? "Top spenders"
        : `Spend by ${(LABELS[dim] ?? "").toLowerCase()}`;
    // A mix card's rows are drillable when its dimension has a level below it
    // (e.g. Department → Team); leaf dims like Agent are shown but not clickable.
    const drillableDim = (dim: Dimension) => nextDimension(dim) !== null;
    // Team view groups by user, so its table already ranks people — surface the
    // top spenders as a compact card too (reuses the main rows, no extra query).
    if (groupBy === Dimension.Email) {
      const userRows = (data?.table ?? [])
        .filter((r) => r.groupValue !== "Other")
        .slice(0, 5);
      out.push({
        kind: "mix",
        title: "Top spenders",
        dim: Dimension.Email,
        drillable: drillableDim(Dimension.Email),
        rows: toRows(userRows),
        loading: isFetching && !data,
      });
    }
    if (mixDimA) {
      out.push({
        kind: "mix",
        title: cardTitle(mixDimA),
        dim: mixDimA,
        drillable: drillableDim(mixDimA),
        rows: toRows(mixDataA?.table ?? []),
        loading: mixLoadingA,
      });
    }
    if (mixDimB) {
      out.push({
        kind: "mix",
        title: cardTitle(mixDimB),
        dim: mixDimB,
        drillable: drillableDim(mixDimB),
        rows: toRows(mixDataB?.table ?? []),
        loading: mixLoadingB,
      });
    }
    // Viewing a user (grouped by their agents): show how efficient sessions are.
    if (groupBy === Dimension.HookSource) {
      const cps = stats.sessions > 0 ? stats.cost / stats.sessions : null;
      out.push({
        kind: "stat",
        title: "Cost per session",
        value: cps !== null ? `$${cps.toFixed(2)}` : "—",
        caption: `across ${stats.sessions.toLocaleString()} sessions`,
        loading: isFetching && !data,
      });
    }
    return out;
  }, [
    mixDimA,
    mixDimB,
    mixDataA,
    mixDataB,
    mixLoadingA,
    mixLoadingB,
    groupBy,
    stats,
    data,
    isFetching,
  ]);

  // Filter by a (dimension, value) and advance to that dimension's child axis.
  // Used by both the main table (current axis) and the mix-card rows (their own
  // cross-cut axis, e.g. drilling a department straight from the Division view).
  const drillIntoDim = (dim: Dimension, value: string) => {
    const next = nextDimension(dim);
    if (next === null) return;
    if (value === "" || value === "Other") return;
    goToNode([...path, { dim, value }], next);
  };

  // Drill into a main-table row: use the current breakdown axis.
  const drillInto = (row: QueryRow) => drillIntoDim(groupBy, row.groupValue);

  // Go up one ancestor: drop the deepest filter and regroup by the axis that
  // produced it (the removed crumb's dimension) — i.e. show the parent's profile.
  const goUp = () => {
    if (path.length === 0) return;
    const removed = path[path.length - 1]!;
    goToNode(path.slice(0, -1), removed.dim);
  };

  // Re-pivot the current node's breakdown axis without drilling (view-only).
  const changeGroupBy = (dim: Dimension) => goToNode(path, dim, true);

  // Offer a breakdown axis only if it can actually partition the current slice
  // into >1 row. `attributes` (the entity's distinct dimension values) tells us:
  // a dim with ≤1 value collapses to a single row and is shown as a fact in the
  // Profile grid instead. Always keep the active axis; show everything at the
  // org root, where there's no slice to measure against yet.
  const filteredDims = new Set(path.map((c) => c.dim));
  const pivotOptions = PIVOTS.filter((p) => {
    if (filteredDims.has(p.dim)) return false;
    if (p.dim === groupBy) return true;
    if (!attributes) return true;
    return (attributes[p.dim]?.length ?? 0) > 1;
  });

  const currentEntity = path.length ? path[path.length - 1]! : null;
  const parentValue = path.length >= 2 ? path[path.length - 2]!.value : null;

  const rangePicker = (
    <TimeRangePicker
      preset={customRange ? null : dateRange}
      customRange={customRange}
      customRangeLabel={customRangeLabel}
      onPresetChange={setDateRangeParam}
      onCustomRangeChange={setCustomRangeParam}
      onClearCustomRange={clearCustomRange}
      projectSlug={projectSlug}
      className="py-1"
    />
  );

  const widgets = (
    <CostWidgets
      series={widgetSeries}
      totals={stats}
      prevTotals={prevTotals}
      cards={cards}
      onDrill={drillIntoDim}
      loading={isFetching && !data}
    />
  );

  return (
    <EntityProfile
      entity={currentEntity}
      widgets={widgets}
      onBack={goUp}
      parentValue={parentValue}
      stats={stats}
      groupBy={groupBy}
      pivotOptions={pivotOptions}
      onGroupByChange={changeGroupBy}
      rows={rows}
      onDrill={drillInto}
      seriesByGroup={seriesByGroup}
      rangePicker={rangePicker}
      isLoading={isFetching && !data}
      isError={isError}
    />
  );
}
