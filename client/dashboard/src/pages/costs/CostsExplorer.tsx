import { telemetryListAttributeKeys } from "@gram/client/funcs/telemetryListAttributeKeys";
import { telemetryListSessions } from "@gram/client/funcs/telemetryListSessions";
import { telemetryQuery } from "@gram/client/funcs/telemetryQuery";
import {
  Dimension,
  type GroupBy,
  type QueryFilter,
  type QueryRow,
} from "@gram/client/models/components";
import {
  invalidateAllListChats,
  useChatDeleteMutation,
  useGramContext,
} from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { EnableLoggingOverlay } from "@/components/EnableLoggingOverlay";
import { InsightsConfig } from "@/components/insights-dock";
import { ObservabilitySkeleton } from "@/components/ObservabilitySkeleton";
import { useDateRangeFilter } from "@/components/observe/useDateRangeFilter";
import { useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import {
  type CostEntityLevel,
  costExplorerSuggestions,
} from "@/lib/insights-suggestions";
import { useRoutes } from "@/routes";
import { ChatDetailSheet } from "../chatLogs/ChatDetailPanel";
import { type CardSpec, CostWidgets } from "./CostWidgets";
import { EntityProfile } from "./EntityProfile";
import { SessionTable, type SessionColumnId } from "./SessionTable";
import {
  type Axis,
  availableDimensions,
  BREAKDOWN_PARAM,
  type Crumb,
  defaultGroupBy,
  displayName,
  encodeCrumb,
  isDimension,
  isSessionLeaf,
  isSessionsAxis,
  LABELS,
  type Measures,
  nextAvailableDimension,
  parseDrillPath,
  PIVOTS,
  SESSIONS_AXIS,
  showsTopSessionsWidget,
} from "./taxonomy";

const EMPTY_MEASURES: Measures = { cost: 0, sessions: 0, tools: 0, tokens: 0 };

// Per-breakdown secondary cuts shown as "mix" widgets above the table. Keyed by
// the current group-by axis; complementary to it (never the same dimension).
// The "who's driving it" + "what's the lever" cards per level (see widget plan).
// Email → rendered as "Top spenders"; HookSource also gets a Cost/session stat.
const MIX_DIMS: Partial<Record<Dimension, Dimension[]>> = {
  [Dimension.DivisionName]: [Dimension.DepartmentName, Dimension.Email],
  [Dimension.DepartmentName]: [Dimension.Email, Dimension.Model],
  [Dimension.Email]: [Dimension.HookSource],
  [Dimension.HookSource]: [Dimension.Model],
  [Dimension.JobTitle]: [Dimension.Email, Dimension.DepartmentName],
  [Dimension.EmployeeType]: [Dimension.Email, Dimension.DepartmentName],
  [Dimension.CostCenterName]: [Dimension.Email, Dimension.DepartmentName],
  [Dimension.Role]: [Dimension.Email, Dimension.DepartmentName],
  [Dimension.Model]: [Dimension.Email, Dimension.HookSource],
};

// Which kind of taxonomy node is in view — drives the assistant-dock prompts.
function entityLevel(entity: Crumb | null): CostEntityLevel {
  if (!entity) return "org";
  if (entity.dim === Dimension.Email) return "user";
  if (entity.dim === Dimension.HookSource) return "agent";
  return "group";
}

function formatDollars(value: number): string {
  return `$${value.toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`;
}

// Compact human date range for the widget titles, e.g. "June 15–19" within a
// month or "Jun 28 – Jul 4" across months.
function formatDateRange(from: Date, to: Date): string {
  const sameMonth =
    from.getFullYear() === to.getFullYear() &&
    from.getMonth() === to.getMonth();
  if (sameMonth) {
    const month = from.toLocaleDateString(undefined, { month: "long" });
    return `${month} ${from.getDate()}–${to.getDate()}`;
  }
  const opts: Intl.DateTimeFormatOptions = { month: "short", day: "numeric" };
  return `${from.toLocaleDateString(undefined, opts)} – ${to.toLocaleDateString(undefined, opts)}`;
}

/**
 * Top-level cost explorer — the org bird's-eye view that walks the taxonomy.
 * It owns the drill state (the filter `path` and the current `groupBy` axis),
 * runs one telemetry.query per level, and renders the generalized
 * {@link EntityProfile} for the current node (the org root when `path` is empty,
 * otherwise the entity last drilled into).
 */
export function CostsExplorer(): JSX.Element {
  const { projectSlug } = useSlugs();
  const project = useProject();
  const routes = useRoutes();
  const location = useLocation();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const deleteChat = useChatDeleteMutation();
  // Which session's detail overlay is open (ephemeral UI, not drill state — so
  // it lives in component state rather than the URL).
  const [openChatId, setOpenChatId] = useState<string | null>(null);

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
  // The deepest filter crumb is the entity in view. Agent/Model are leaves —
  // once you're on one, individual sessions are the only meaningful view, so we
  // lock to them: force sessions mode and offer no further dimension breakdown.
  // This stops nonsensical deep drills (e.g. model → role → user → agent).
  const deepestCrumb = path.length ? path[path.length - 1]! : null;
  const atSessionLeaf = deepestCrumb != null && isSessionLeaf(deepestCrumb.dim);
  // The sessions sentinel swaps the table for the per-session list; the rest of
  // the page (filters, widgets, header stats) is unchanged.
  const sessionsMode = isSessionsAxis(byParam) || atSessionLeaf;
  // The "Most costly sessions" widget shows on org/division/department/user
  // (not Agent/Model, which already render the full session table).
  const showSessionsWidget = showsTopSessionsWidget(deepestCrumb);

  // Navigate to a node: encode its filter path into the URL and pin the
  // breakdown axis in `?by=`. `replace` for view-only changes (re-pivoting)
  // that shouldn't add a back-button step.
  const goToNode = (nextPath: Crumb[], by: Axis, replace = false) => {
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

  // Every cost query is scoped to the current project via a project_id filter
  // (the endpoints are org-scoped, but project_id is an allowlisted dimension),
  // then narrowed further by the drill path. This keeps the dashboard to the
  // project in the URL and guarantees session detail (project-scoped) loads.
  const filters: QueryFilter[] = useMemo(() => {
    const drill = path.map((c) => ({ dimension: c.dim, values: [c.value] }));
    if (!project.id) return drill;
    return [{ dimension: Dimension.ProjectId, values: [project.id] }, ...drill];
  }, [path, project.id]);

  // Drop session columns whose dimension the drill path already pins to a single
  // value — that column would just repeat the same value down every row. Mapped
  // from drill dimension to the session-table column id it makes redundant.
  const hiddenSessionColumns = useMemo<SessionColumnId[]>(() => {
    const byDimension: Partial<Record<Dimension, SessionColumnId>> = {
      [Dimension.Email]: "user",
      [Dimension.HookSource]: "agent",
      [Dimension.Model]: "model",
    };
    return path
      .map((c) => byDimension[c.dim])
      .filter((id): id is SessionColumnId => id !== undefined);
  }, [path]);

  // The generated useTelemetryQuery hook keys its cache on gramSession only — it
  // ignores the request body — so every drill would return the first cached
  // result. Drive useQuery directly with a key that encodes the payload.
  const client = useGramContext();

  // Project-scoped queries wait until the active project id resolves, so they
  // never run org-wide (without the project_id filter) during the first paint.
  const projectReady = Boolean(project.id);

  // Which dimensions the org actually has data for in this range — drives both
  // the breakdown dropdown (hide empties) and the default axis (so a customer
  // whose IDP omits the default chain doesn't land on an empty view). Fail open
  // while loading/empty: availableDims is undefined and nothing gets hidden.
  const { data: attrKeysData } = useQuery({
    queryKey: ["costs-attr-keys", from.toISOString(), to.toISOString()],
    throwOnError: false,
    queryFn: () =>
      unwrapAsync(
        telemetryListAttributeKeys(client, {
          getProjectMetricsSummaryPayload: { from, to },
        }),
      ),
  });
  const availableDims = useMemo(
    () => availableDimensions(attrKeysData?.keys),
    [attrKeysData],
  );

  const groupBy = isDimension(byParam)
    ? byParam
    : defaultGroupBy(path, availableDims);

  // The primary query also doubles as the logs-enabled probe: telemetry.query
  // returns 404 when logging is off for the org. Opt out of the app-wide
  // throw-to-error-boundary policy (Sdk.tsx) so that 404 lands in `error`
  // instead of crashing the page, then derive `isLogsDisabled` from it to show
  // the shared EnableLoggingOverlay — same pattern as the Logs/Agents pages.
  const { data, isFetching, isError, refetch, isLogsDisabled } =
    useLogsEnabledErrorCheck(
      useQuery({
        queryKey: [
          "costs-explorer",
          from.toISOString(),
          to.toISOString(),
          groupBy,
          filters,
        ],
        enabled: projectReady,
        throwOnError: false,
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
      }),
    );

  // Treat "project not resolved yet" as loading, so the skeleton shows instead
  // of an empty "no data" flash before the project-scoped queries enable.
  const loadingSlice = (!projectReady || isFetching) && !data;

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
    enabled: projectReady && path.length > 0,
    throwOnError: false,
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
    enabled: projectReady,
    throwOnError: false,
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

  // Per-session list for the current slice — only when the breakdown axis is the
  // sessions sentinel. Org-scoped endpoint; reuses the same drill `filters` and
  // is ranked server-side by cost. The generated useListSessions hook keys its
  // cache on gramSession only (ignores the body), so drive useQuery directly.
  const {
    data: sessionsData,
    isFetching: sessionsFetching,
    isError: sessionsError,
  } = useQuery({
    queryKey: [
      "costs-explorer-sessions",
      from.toISOString(),
      to.toISOString(),
      filters,
      "total_cost",
    ],
    enabled: projectReady && sessionsMode,
    throwOnError: false,
    queryFn: () =>
      unwrapAsync(
        telemetryListSessions(client, {
          listSessionsPayload: {
            from,
            to,
            sortBy: "total_cost",
            limit: 100,
            filters: filters.length ? filters : undefined,
          },
        }),
      ),
  });

  // Top 5 sessions by cost for the "Most costly sessions" widget — shown on the
  // org/structure levels (not Agent/Model). Independent of sessionsMode so the
  // widget renders alongside the dimension breakdown.
  const { data: topSessionsData, isFetching: topSessionsFetching } = useQuery({
    queryKey: [
      "costs-explorer-top-sessions",
      from.toISOString(),
      to.toISOString(),
      filters,
    ],
    enabled: projectReady && showSessionsWidget,
    throwOnError: false,
    queryFn: () =>
      unwrapAsync(
        telemetryListSessions(client, {
          listSessionsPayload: {
            from,
            to,
            sortBy: "total_cost",
            limit: 5,
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
      (!availableDims || availableDims.has(d)) &&
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
    enabled: projectReady && !!mixDimA,
    throwOnError: false,
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
    enabled: projectReady && !!mixDimB,
    throwOnError: false,
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
    // A mix card's rows are drillable when its dimension has a *populated* level
    // below it (e.g. Department → User); leaf dims (Agent) or levels with no data
    // beneath them are shown but not clickable.
    const drillableDim = (dim: Dimension) =>
      isSessionLeaf(dim) || nextAvailableDimension(dim, availableDims) !== null;
    // A user breakdown already ranks people in its table — surface the top
    // spenders as a compact card too (reuses the main rows, no extra query).
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
        loading: loadingSlice,
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
    // Always show three widgets across the top (trend + two). When a level
    // yields fewer than two breakdown cards (e.g. only one mix axis survives
    // pruning), pad with per-session efficiency stats until there are two.
    const sessions = stats.sessions;
    const perSession = (n: number) => (sessions > 0 ? n / sessions : null);
    const caption = `across ${sessions.toLocaleString()} sessions`;
    const loading = loadingSlice;
    const fillers: CardSpec[] = [
      {
        kind: "stat",
        title: "Cost per session",
        value:
          perSession(stats.cost) !== null
            ? `$${perSession(stats.cost)!.toFixed(2)}`
            : "—",
        caption,
        loading,
      },
      {
        kind: "stat",
        title: "Tokens per session",
        value:
          perSession(stats.tokens) !== null
            ? Math.round(perSession(stats.tokens)!).toLocaleString()
            : "—",
        caption,
        loading,
      },
      {
        kind: "stat",
        title: "Tool calls per session",
        value:
          perSession(stats.tools) !== null
            ? perSession(stats.tools)!.toFixed(1)
            : "—",
        caption,
        loading,
      },
    ];
    for (const filler of fillers) {
      if (out.length >= 2) break;
      if (!out.some((c) => c.title === filler.title)) out.push(filler);
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
    availableDims,
    stats,
    data,
    loadingSlice,
  ]);

  // Filter by a (dimension, value) and advance to that dimension's child axis.
  // Used by both the main table (current axis) and the mix-card rows (their own
  // cross-cut axis, e.g. drilling a department straight from the Division view).
  const drillIntoDim = (dim: Dimension, value: string) => {
    // "" (the "(unset)" bucket) is drillable — it filters to the entities
    // missing this attribute. Only "Other" (the synthetic top-N rollup) isn't.
    if (value === "Other") return;
    // Never re-add a dimension already in the path — that produces nonsensical
    // chains (e.g. the same user/agent twice). The pivot list already hides
    // filtered dims; this guards the mix-card + fallback-chain paths too.
    if (path.some((c) => c.dim === dim)) return;
    // Agent/Model are leaves: drilling a row shows that slice's individual
    // sessions instead of pivoting to another dimension.
    if (isSessionLeaf(dim)) {
      goToNode([...path, { dim, value }], SESSIONS_AXIS);
      return;
    }
    // Otherwise land on the next chain axis that actually has data, skipping
    // empty links (e.g. divisions → users when the org has no departments). Null
    // means nothing populated below — don't drill into an empty level. (While
    // availability is still loading this returns the static next dimension, so
    // drilling stays enabled and never blocks prematurely.)
    const next = nextAvailableDimension(dim, availableDims);
    if (next === null) return;
    goToNode([...path, { dim, value }], next);
  };

  // Drill into a main-table row: use the current breakdown axis.
  const drillInto = (row: QueryRow) => drillIntoDim(groupBy, row.groupValue);

  // Rows are drillable only when there's a *populated* level below the current
  // axis — so you can't drill into an empty breakdown. (Availability-unknown
  // during load falls back to the static chain, keeping rows drillable.)
  const canDrill =
    isSessionLeaf(groupBy) ||
    nextAvailableDimension(groupBy, availableDims) !== null;

  // Go up one ancestor: drop the deepest filter and regroup by the axis that
  // produced it (the removed crumb's dimension) — i.e. show the parent's profile.
  const goUp = () => {
    if (path.length === 0) return;
    const removed = path[path.length - 1]!;
    goToNode(path.slice(0, -1), removed.dim);
  };

  // Jump straight back to the org root (clear all filters).
  const goHome = () => goToNode([], defaultGroupBy([], availableDims));

  // Re-pivot the current node's breakdown axis without drilling (view-only).
  const changeGroupBy = (axis: Axis) => goToNode(path, axis, true);

  // Offer a breakdown axis only if it can actually partition the current slice
  // into >1 row. `attributes` (the entity's distinct dimension values) tells us:
  // a dim with ≤1 value collapses to a single row and is shown as a fact in the
  // Profile grid instead. Always keep the active axis; show everything at the
  // org root, where there's no slice to measure against yet.
  const filteredDims = new Set(path.map((c) => c.dim));
  const pivotOptions = PIVOTS.filter((p) => {
    if (filteredDims.has(p.dim)) return false;
    if (p.dim === groupBy) return true;
    // Hide dimensions the org has no data for at all (IDP doesn't populate them).
    if (availableDims && !availableDims.has(p.dim)) return false;
    if (!attributes) return true;
    return (attributes[p.dim]?.length ?? 0) > 1;
  });

  // The breakdown <Select> options: dimension pivots plus the always-available
  // sessions sentinel. At a session leaf (Agent/Model) the only option is
  // Sessions — no further dimension breakdown. `axisValue` is the current
  // selection; `onViewSessions` is the header entry point, omitted while already
  // viewing the list.
  const dimensionAxisOptions = atSessionLeaf
    ? []
    : pivotOptions.map((p) => ({ value: p.dim as string, label: p.label }));
  const axisOptions: { value: string; label: string }[] = [
    ...dimensionAxisOptions,
    { value: SESSIONS_AXIS, label: LABELS[SESSIONS_AXIS]! },
  ];
  const axisValue: string = sessionsMode ? SESSIONS_AXIS : groupBy;
  const onViewSessions = sessionsMode
    ? undefined
    : () => changeGroupBy(SESSIONS_AXIS);

  const currentEntity = deepestCrumb;
  const parentValue = path.length >= 2 ? path[path.length - 2]!.value : null;

  // Project Assistant dock config — recomputed per render, so drilling into a
  // new entity re-registers fresh prompts + context (InsightsConfig diffs on the
  // serialized options). Prompts are framed for the node in view; contextInfo
  // hands the assistant the current slice's numbers so its answers are grounded.
  const level = entityLevel(currentEntity);
  const entityLabel = currentEntity
    ? displayName(currentEntity.dim, currentEntity.value)
    : null;
  const entityType = currentEntity
    ? (LABELS[currentEntity.dim] ?? "entity")
    : "Project";
  const childLabel = LABELS[groupBy] ?? "group";
  const rangeDays = Math.max(
    1,
    Math.round((to.getTime() - from.getTime()) / 86_400_000),
  );
  const rangeLabel = `the last ${rangeDays} days`;
  const assistantTitle = entityLabel
    ? `Ask about ${entityLabel}'s AI spend`
    : "What would you like to know about your AI spend?";
  const assistantSubtitle = entityLabel
    ? `Cost drivers, top spenders, and trends for this ${entityType.toLowerCase()}.`
    : "Cost drivers, top spenders, and trends across this project.";
  const filterSummary =
    path.map((c) => `${LABELS[c.dim] ?? c.dim}=${c.value}`).join(", ") ||
    "none";
  const scope = entityLabel
    ? `the ${entityType.toLowerCase()} "${entityLabel}"`
    : `the "${project.name}" project`;
  const assistantContext = `Cost dashboard — viewing ${scope}, broken down by ${childLabel.toLowerCase()}. Over ${rangeLabel}: ${formatDollars(stats.cost)} total cost, ${stats.sessions.toLocaleString()} chat sessions, ${stats.tools.toLocaleString()} tool calls, ${stats.tokens.toLocaleString()} tokens. Active filters: ${filterSummary}.`;
  const assistantSuggestions = costExplorerSuggestions({
    level,
    entityLabel,
    childLabel,
    rangeLabel,
  });

  const rangePicker = (
    <TimeRangePicker
      preset={customRange ? null : dateRange}
      customRange={customRange}
      customRangeLabel={customRangeLabel}
      onPresetChange={setDateRangeParam}
      onCustomRangeChange={setCustomRangeParam}
      onClearCustomRange={clearCustomRange}
      projectSlug={projectSlug}
      className="bg-background py-1"
    />
  );

  // The "Most costly sessions" widget (top 5 by cost), prepended on the eligible
  // levels. Cap the other cards to one so the top row stays Trend + 2.
  const sessionsCard: CardSpec = {
    kind: "sessions",
    title: "Most costly sessions",
    rows: (topSessionsData?.sessions ?? []).map((s) => ({
      id: s.gramChatId,
      label: s.title?.length ? s.title : s.gramChatId.slice(0, 8),
      sublabel:
        groupBy !== Dimension.Email &&
        currentEntity?.dim !== Dimension.Email &&
        s.userEmail?.length
          ? s.userEmail
          : undefined,
      cost: s.totalCost,
    })),
    loading: topSessionsFetching && !topSessionsData,
  };
  const widgetCards = showSessionsWidget
    ? [sessionsCard, ...cards.slice(0, 1)]
    : cards;

  const widgets = (
    <CostWidgets
      series={widgetSeries}
      totals={stats}
      prevTotals={prevTotals}
      cards={widgetCards}
      rangeLabel={formatDateRange(from, to)}
      onDrill={drillIntoDim}
      onOpenSession={setOpenChatId}
      loading={loadingSlice}
    />
  );

  // Logging off for the org → no cost data exists. Show the shared enable
  // overlay over a skeleton instead of an empty/broken profile, and hide the
  // assistant dock (its prompts assume populated numbers). Enabling refetches.
  if (isLogsDisabled) {
    return (
      <>
        <InsightsConfig hideTrigger />
        <div className="min-h-0 w-full flex-1 space-y-6 overflow-y-auto p-8 pb-24">
          <div className="flex min-w-0 flex-col gap-1">
            <h1 className="text-xl font-semibold">Costs</h1>
            <p className="text-muted-foreground text-sm">
              Break down this project's AI spend by division, department, user,
              agent, and model.
            </p>
          </div>
          <div className="relative flex-1">
            <div
              className="pointer-events-none h-full select-none"
              aria-hidden="true"
            >
              <ObservabilitySkeleton />
            </div>
            <EnableLoggingOverlay onEnabled={() => void refetch()} />
          </div>
        </div>
      </>
    );
  }

  return (
    <>
      <InsightsConfig
        title={assistantTitle}
        subtitle={assistantSubtitle}
        contextInfo={assistantContext}
        suggestions={assistantSuggestions}
      />
      <EntityProfile
        entity={currentEntity}
        widgets={widgets}
        onBack={goUp}
        onHome={goHome}
        projectName={project.name}
        parentValue={parentValue}
        ancestors={path.slice(0, -1)}
        stats={stats}
        groupBy={groupBy}
        canDrill={canDrill}
        axisValue={axisValue}
        axisOptions={axisOptions}
        onAxisChange={(value) => changeGroupBy(value as Axis)}
        rows={rows}
        onDrill={drillInto}
        tableOverride={
          sessionsMode ? (
            <SessionTable
              sessions={sessionsData?.sessions ?? []}
              isLoading={sessionsFetching && !sessionsData}
              isError={sessionsError}
              onOpen={setOpenChatId}
              hiddenColumns={hiddenSessionColumns}
            />
          ) : undefined
        }
        onViewSessions={onViewSessions}
        seriesByGroup={seriesByGroup}
        rangePicker={rangePicker}
        rangeLabel={formatDateRange(from, to)}
        isLoading={loadingSlice}
        isError={isError}
      />
      {/* Interim session drilldown: the existing project-scoped chat trace
          overlay. A dedicated org-aware session page is designed separately. */}
      <ChatDetailSheet
        chatId={openChatId}
        onClose={() => setOpenChatId(null)}
        onDelete={(chatId) => {
          deleteChat.mutate(
            { request: { id: chatId } },
            {
              onSuccess: () => {
                void invalidateAllListChats(queryClient);
                // Deleting a chat removes a session, so refresh every cost query
                // (totals, breakdowns, session list + widget) — not just chats.
                void queryClient.invalidateQueries({
                  predicate: (query) =>
                    typeof query.queryKey[0] === "string" &&
                    query.queryKey[0].startsWith("costs-explorer"),
                });
                setOpenChatId(null);
              },
            },
          );
        }}
      />
    </>
  );
}
