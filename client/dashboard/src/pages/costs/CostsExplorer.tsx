import { formatCost } from "@/lib/money";
import { telemetryListAttributeKeys } from "@gram/client/funcs/telemetryListAttributeKeys";
import { telemetryListSessions } from "@gram/client/funcs/telemetryListSessions";
import { telemetryQuery } from "@gram/client/funcs/telemetryQuery";
import {
  Dimension,
  type QueryFilter,
} from "@gram/client/models/components/queryfilter.js";
import { type GroupBy } from "@gram/client/models/components/querypayload.js";
import { type QueryRow } from "@gram/client/models/components/queryrow.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useChatDeleteMutation } from "@gram/client/react-query/chatDelete.js";
import { invalidateAllListChats } from "@gram/client/react-query/listChats.js";
import { unwrapAsync } from "@gram/client/types/fp";
import {
  keepPreviousData,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { resolveScopeBillingMode } from "@/components/estimated-cost-utils";
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
import { CostBreakdownChart } from "./CostBreakdownChart";
import { type CardSpec, CostWidgets } from "./CostWidgets";
import { EntityProfile } from "./EntityProfile";
import { SessionTable, type SessionColumnId } from "./SessionTable";
import { buildSessionCsv } from "./sessionCsv";
import {
  type Axis,
  availableDimensions,
  BREAKDOWN_PARAM,
  collectionLabel,
  type Crumb,
  type Dataset,
  DATASET_OPTIONS,
  DATASET_PARAM,
  datasetDefaultGroupBy,
  datasetForDim,
  datasetPivotParent,
  datasetPivots,
  defaultGroupBy,
  displayName,
  encodeCrumb,
  firstSplittableDimension,
  isAttributionDim,
  isDataset,
  isDimension,
  isSessionLeaf,
  isSessionsAxis,
  LABELS,
  type Measures,
  nextAvailableDimension,
  parseDrillPath,
  SESSIONS_AXIS,
  showsTopSessionsWidget,
} from "./taxonomy";

// Server-side cap on the per-session list (ranked by cost). SessionTable's
// truncation footer and the search zero-match copy both key off it.
const SESSION_LIMIT = 100;

// Server-side cap on breakdown groups per query. Beyond it the server appends
// one synthetic rollup row — detected by row count below so the chart can fold
// it into its own remainder bucket instead of charting it as a real group.
const BREAKDOWN_TOP_N = 100;

const EMPTY_MEASURES: Measures = {
  cost: 0,
  sessions: 0,
  tools: 0,
  tokens: 0,
  cacheCreation: 0,
};

// Per-breakdown secondary cuts shown as "mix" widgets above the table. Keyed by
// the current group-by axis; complementary to it (never the same dimension).
// The "who's driving it" + "what's the lever" cards per level (see widget plan).
// Email → rendered as "Top spenders"; HookSource also gets a Cost/session stat.
const MIX_DIMS: Partial<Record<Dimension, Dimension[]>> = {
  [Dimension.DivisionName]: [Dimension.DepartmentName, Dimension.Email],
  [Dimension.DepartmentName]: [Dimension.Email, Dimension.Model],
  [Dimension.Email]: [Dimension.HookSource, Dimension.McpServerName],
  [Dimension.HookSource]: [Dimension.Model, Dimension.McpServerName],
  [Dimension.JobTitle]: [Dimension.Email, Dimension.DepartmentName],
  [Dimension.EmployeeType]: [Dimension.Email, Dimension.DepartmentName],
  [Dimension.CostCenterName]: [Dimension.Email, Dimension.DepartmentName],
  [Dimension.Role]: [Dimension.Email, Dimension.DepartmentName],
  [Dimension.Model]: [Dimension.Email, Dimension.McpServerName],
  // Claude attribution axes cross-cut each other and model.
  [Dimension.McpServerName]: [Dimension.McpToolName, Dimension.Model],
  [Dimension.McpToolName]: [Dimension.McpServerName, Dimension.Model],
  [Dimension.SkillName]: [Dimension.Model, Dimension.McpServerName],
  [Dimension.AgentName]: [Dimension.Model, Dimension.McpServerName],
};

// Which kind of taxonomy node is in view — drives the assistant-dock prompts.
function entityLevel(entity: Crumb | null): CostEntityLevel {
  if (!entity) return "org";
  if (entity.dim === Dimension.Email) return "user";
  if (entity.dim === Dimension.HookSource) return "agent";
  return "group";
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
  // Free-text filter over the visible table rows (ephemeral view state, like
  // openChatId). Client-side only: the table already holds the server-ranked
  // top-N slice, so search narrows what's shown without touching the queries.
  const [breakdownSearch, setBreakdownSearch] = useState("");

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
  // The active dataset (spend slice) rides in `?dataset=`; absent/invalid → the
  // full-project `all` view. It scopes both the breakdown options and, via the
  // attribution empty-row handling below, which rows count.
  const datasetParam = searchParams.get(DATASET_PARAM);
  // Backward compat: shared/bookmarked links from before the dataset selector
  // pointed `?by=` straight at an attribution dim (e.g. `?by=mcp_server_name`)
  // with no `?dataset=`. Infer the owning dataset from such a `?by=` so the link
  // still lands on the intended attribution view rather than silently falling
  // back to the org default (BASE_PIVOTS excludes attribution dims).
  const dataset: Dataset = isDataset(datasetParam)
    ? datasetParam
    : isDimension(byParam) && isAttributionDim(byParam)
      ? datasetForDim(byParam)
      : "all";
  // Within a non-`all` dataset the slice is enforced only by dropping the empty
  // attribution group from the *grouped* table. The un-groupable views — the
  // session list, the "Most costly sessions" widget, and cross-cut cards over a
  // non-attribution dim — can't express that "attribute present" predicate with
  // the IN-only filter API, so on their own they'd show whole-project numbers
  // under a dataset label. They only become correct once the drill path pins an
  // attribution value (which inherently restricts rows to the slice); until
  // then, suppress them rather than mislead.
  const sliceScoped =
    dataset === "all" || path.some((c) => isAttributionDim(c.dim));
  // The deepest filter crumb is the entity in view. Agent/Model are leaves —
  // once you're on one, individual sessions are the only meaningful view, so we
  // lock to them: force sessions mode and offer no further dimension breakdown.
  // This stops nonsensical deep drills (e.g. model → role → user → agent).
  const deepestCrumb = path.length ? path[path.length - 1]! : null;
  const atSessionLeaf = deepestCrumb != null && isSessionLeaf(deepestCrumb.dim);
  // The sessions sentinel swaps the table for the per-session list; the rest of
  // the page (filters, widgets, header stats) is unchanged. In an unscoped
  // dataset view the list can't be restricted to the slice, so the sentinel is
  // ignored there (a session leaf always pins an attribution value, so it stays
  // scoped and is still honored).
  const sessionsMode =
    atSessionLeaf || (isSessionsAxis(byParam) && sliceScoped);
  // The "Most costly sessions" widget shows on org/division/department/user
  // (not Agent/Model, which already render the full session table), and only
  // where the slice is scoped — at an unscoped dataset root it would list
  // whole-project sessions.
  const showSessionsWidget =
    showsTopSessionsWidget(deepestCrumb) && sliceScoped;

  // Navigate to a node: encode its filter path into the URL and pin the
  // breakdown axis in `?by=`. `replace` for view-only changes (re-pivoting)
  // that shouldn't add a back-button step. `ds` overrides the dataset param
  // (e.g. drilling from `all` into an attribution cut promotes to its dataset);
  // omitted, the current dataset is preserved.
  const goToNode = (
    nextPath: Crumb[],
    by: Axis,
    replace = false,
    ds?: Dataset,
  ) => {
    const tail = nextPath.map(encodeCrumb).join("/");
    // Preserve the rest of the query (the date-range filter lives here too) and
    // only override the breakdown axis (and dataset, when given).
    const params = new URLSearchParams(searchParams);
    params.set(BREAKDOWN_PARAM, by);
    if (ds !== undefined) {
      if (ds === "all") params.delete(DATASET_PARAM);
      else params.set(DATASET_PARAM, ds);
    }
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

  // The generated useTelemetryQuery hook keys its cache on gramSession only — it
  // ignores the request body — so every drill would return the first cached
  // result. Drive useQuery directly with a key that encodes the payload.
  const client = useGramContext();

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

  // The breakdown axis must belong to the active dataset — a `?by=` left over
  // from another dataset (or an org dim while in an attribution slice) falls
  // back to the dataset's default axis.
  const datasetDimSet = useMemo(
    () => new Set(datasetPivots(dataset).map((p) => p.dim)),
    [dataset],
  );
  // A dataset's nested breakdown dim (MCP Tool under MCP Server, Skill under
  // Subagent) is only valid once its parent is pinned in the drill path. Guard
  // the raw `?by=` against this too — otherwise a deep/stale link like
  // `?dataset=subagents&by=skill_name` would render the child cut at the dataset
  // root while the selector reads "Subagents". Fall back to the dataset default
  // when the required parent is missing.
  const byParamParent = isDimension(byParam)
    ? datasetPivotParent(dataset, byParam)
    : null;
  // A `?by=` naming a dimension the drill path already pins (e.g. a stale link
  // ending `division~X/department~Y?by=department_name`) would render a
  // degenerate one-row "breakdown" of the entity by itself — with a drill
  // chevron that no-ops, since drillIntoDim refuses to re-add a pinned dim.
  // Treat it like any other invalid axis and fall back to the default.
  //
  // This is only the CANDIDATE axis: it validates the URL against the dataset
  // and drill path, but not against the slice's data. The final `groupBy`
  // below re-resolves it once the detail query reports the slice's distinct
  // values, skipping axes that cannot split it (see there).
  const candidateGroupBy =
    isDimension(byParam) &&
    datasetDimSet.has(byParam) &&
    !path.some((c) => c.dim === byParam) &&
    (byParamParent === null || path.some((c) => c.dim === byParamParent))
      ? byParam
      : datasetDefaultGroupBy(dataset, path, availableDims);

  // Every cost query is scoped to the current project via a project_id filter
  // (the endpoints are org-scoped, but project_id is an allowlisted dimension),
  // then narrowed further by the drill path. This keeps the dashboard to the
  // project in the URL and guarantees session detail (project-scoped) loads.
  //
  // These are the ENTITY's filters — everything that identifies the node in
  // view, independent of how it is being broken down. The detail query feeds
  // off them, so the slice's dimension_values (and with them the final
  // breakdown axis) resolve without depending on the axis itself.
  const entityFilters: QueryFilter[] = useMemo(() => {
    const drill = path.map((c) => ({ dimension: c.dim, values: [c.value] }));
    // Skill is its own drill tree (Subagent → Skill). A Skill pinned without an
    // ancestor Subagent identifies the "skills run outside a subagent" branch,
    // so the agent_name='' restriction is part of the entity here (the axis-
    // driven case lives in `filters` below).
    const hasAgentCrumb = path.some((c) => c.dim === Dimension.AgentName);
    const synthetic: QueryFilter[] =
      path.some((c) => c.dim === Dimension.SkillName) && !hasAgentCrumb
        ? [{ dimension: Dimension.AgentName, values: [""] }]
        : [];
    const base: QueryFilter[] = project.id
      ? [{ dimension: Dimension.ProjectId, values: [project.id] }]
      : [];
    return [...base, ...drill, ...synthetic];
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

  // Project-scoped queries wait until the active project id resolves, so they
  // never run org-wide (without the project_id filter) during the first paint.
  const projectReady = Boolean(project.id);

  // A no-group_by query over the entity's filters returns a single aggregate
  // row for the whole slice. Two things come off it:
  //   • its dimension_values are the current entity's distinct division/
  //     department/job_title/roles/etc — "(unset)" buckets included — which
  //     drive the axis resolution and pivot pruning below, and
  //   • its measures are the TRUE slice totals — critically the distinct session
  //     count, which cannot be recovered by summing the per-group breakdown rows
  //     (see `stats`). Runs at every level so the root headline is correct too.
  const { data: detailData, status: detailStatus } = useQuery({
    queryKey: [
      "costs-explorer-detail",
      from.toISOString(),
      to.toISOString(),
      entityFilters,
    ],
    enabled: projectReady,
    throwOnError: false,
    queryFn: () =>
      unwrapAsync(
        telemetryQuery(client, {
          queryPayload: {
            from,
            to,
            topN: 1,
            filters: entityFilters.length ? entityFilters : undefined,
          },
        }),
      ),
  });
  const detailRow = detailData?.table?.[0];
  // Attribute counts + pivot pruning only apply once drilled into an entity; at
  // the root, keep this undefined so those consumers still fail open (show every
  // pivot). The row's measures, by contrast, are used at every level.
  const attributes = path.length > 0 ? detailRow?.dimensionValues : undefined;

  // Distinct groups a breakdown by `dim` would produce within the current
  // slice ("(unset)" included — the server surfaces the '' bucket for every
  // groupable dim); undefined while the slice is unknown (root, or detail
  // still loading) so callers fail open.
  const sliceValueCount = (dim: Dimension): number | undefined =>
    attributes ? (attributes[dim]?.length ?? 0) : undefined;
  // The same count over named values only — for consumers that hide the
  // "(unset)" bucket (the mix cards), where an axis splittable only via
  // "(unset)" would still make a pointless one-row card.
  const sliceNamedValueCount = (dim: Dimension): number | undefined =>
    attributes
      ? (attributes[dim]?.filter((v) => v !== "").length ?? 0)
      : undefined;

  // The final breakdown axis: the candidate, unless the slice can't actually
  // be split by it — a division whose spend all sits in one department must
  // not land on (or offer) a Department cut. Resolved against the entity
  // slice's group counts, falling down the chain to the first splittable axis.
  const groupBy = firstSplittableDimension(
    candidateGroupBy,
    availableDims,
    sliceValueCount,
  );

  // Drill state is the URL: when resolution lands on a different axis than
  // `?by=` claims (stale link, mix-card drill, or a slice the requested axis
  // can't split), rewrite the param in place so copied links and back/forward
  // reflect what is actually rendered. Gated on a settled resolution
  // (attributes known) and skipped in sessions mode, whose sentinel rides
  // `?by=` itself. groupBy always survives its own re-resolution, so the
  // rewrite reaches a fixed point after one pass.
  useEffect(() => {
    if (!attributes || sessionsMode) return;
    if (byParam === groupBy) return;
    goToNode(path, groupBy, true);
  });

  // The breakdown queries' filters: the entity's, plus the skill-only
  // synthetic filter when the AXIS is the root Skill cut — excluding rows that
  // also carry a subagent (agent_name = '') so agent+skill spend surfaces only
  // under Subagent → Skill, never leaking into the root Skill breakdown. (The
  // path-pinned skill case already carries it in entityFilters.)
  const filters: QueryFilter[] = useMemo(() => {
    const skillAxis =
      groupBy === Dimension.SkillName &&
      !path.some(
        (c) => c.dim === Dimension.SkillName || c.dim === Dimension.AgentName,
      );
    return skillAxis
      ? [...entityFilters, { dimension: Dimension.AgentName, values: [""] }]
      : entityFilters;
  }, [entityFilters, groupBy, path]);

  // Hold the grouped queries until the axis is resolved on a drilled node —
  // otherwise a slice whose landing axis collapses fetches every grouped query
  // twice (candidate axis, then corrected axis). The root skips the wait (no
  // slice pruning there), and a failed detail probe fails open rather than
  // blocking the page (the main query doubles as the logs-disabled probe).
  const axisResolving = path.length > 0 && detailStatus === "pending";

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
        enabled: projectReady && !axisResolving,
        throwOnError: false,
        // Re-pivoting or drilling changes the query key; keep the previous
        // slice's rows on screen while the new cut loads, so the page updates
        // in place instead of flashing every widget back to a skeleton.
        placeholderData: keepPreviousData,
        queryFn: () =>
          unwrapAsync(
            telemetryQuery(client, {
              queryPayload: {
                from,
                to,
                groupBy: groupBy as GroupBy,
                sortBy: "total_cost",
                topN: BREAKDOWN_TOP_N,
                // Daily buckets → ~30 points per group for the row trend sparklines.
                granularitySeconds: 86400,
                filters: filters.length ? filters : undefined,
              },
            }),
          ),
      }),
    );

  // Treat "project not resolved yet" (and a still-resolving axis) as loading,
  // so the skeleton shows instead of an empty "no data" flash before the
  // project-scoped queries enable.
  const loadingSlice = (!projectReady || axisResolving || isFetching) && !data;

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
    enabled: projectReady && !axisResolving,
    throwOnError: false,
    placeholderData: keepPreviousData,
    queryFn: () =>
      unwrapAsync(
        telemetryQuery(client, {
          queryPayload: {
            from: prevFrom,
            to: prevTo,
            groupBy: groupBy as GroupBy,
            sortBy: "total_cost",
            topN: BREAKDOWN_TOP_N,
            filters: filters.length ? filters : undefined,
          },
        }),
      ),
  });
  // Previous-period slice totals (un-grouped), for a session delta that uses the
  // same distinct-count basis as the current headline (see `stats`).
  const { data: prevDetailData } = useQuery({
    queryKey: [
      "costs-explorer-prev-detail",
      prevFrom.toISOString(),
      prevTo.toISOString(),
      entityFilters,
    ],
    enabled: projectReady,
    throwOnError: false,
    queryFn: () =>
      unwrapAsync(
        telemetryQuery(client, {
          queryPayload: {
            from: prevFrom,
            to: prevTo,
            topN: 1,
            filters: entityFilters.length ? entityFilters : undefined,
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
            limit: SESSION_LIMIT,
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
    placeholderData: keepPreviousData,
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

  // The server's synthetic remainder row, present only when the slice has
  // more groups than BREAKDOWN_TOP_N — the server always appends it last. The
  // chart folds its series into the client-side remainder bucket so the two
  // rollups read as one "everything else".
  const serverRollupValue =
    rows.length > BREAKDOWN_TOP_N
      ? rows[rows.length - 1]!.groupValue
      : undefined;

  // The axis the FETCHED slice is actually grouped by. With keepPreviousData,
  // `data` lags `groupBy` for the refetch window after a re-pivot — labeling
  // or filtering the old rows under the new axis would mislabel (and could
  // wrongly merge) them. Everything that RENDERS the slice (table rows, chart
  // stacks, section title, drill targets) keys off this; the live `groupBy`
  // keeps driving the queries and the control bar, so the lit segment answers
  // the click instantly while the content swaps atomically when data lands.
  const rawDataGroupBy = data?.groupBy;
  const dataGroupBy: Dimension = isDimension(rawDataGroupBy)
    ? rawDataGroupBy
    : groupBy;

  // The view's billing mode drives whether cost reads as real spend or an
  // estimate: confidently "metered" only when every row in the view is metered.
  const viewBillingMode = useMemo(() => {
    const modes = new Set<string>();
    for (const r of data?.table ?? []) {
      for (const m of r.dimensionValues?.["billing_mode"] ?? []) modes.add(m);
    }
    return resolveScopeBillingMode([...modes]);
  }, [data]);

  // Attribution breakdowns hide the "" group — it's spend where the attribute
  // is not applicable ("not included"), not an "(unset)" slice worth drilling.
  const visibleRows = isAttributionDim(dataGroupBy)
    ? rows.filter((r) => r.groupValue !== "")
    : rows;

  // Search narrows the table only — headline stats, widgets, and CSV-adjacent
  // aggregates stay slice totals. Rows match on both the raw group value and
  // its display name (a user's email matches their prettified name too).
  const normalizedSearch = breakdownSearch.trim().toLowerCase();
  const searchedRows = normalizedSearch
    ? visibleRows.filter(
        (r) =>
          r.groupValue.toLowerCase().includes(normalizedSearch) ||
          displayName(dataGroupBy, r.groupValue)
            .toLowerCase()
            .includes(normalizedSearch),
      )
    : visibleRows;

  // At the root, an attribution breakdown is presented as a "collection" (e.g.
  // "MCP Servers") rather than the project — and its headline stats then sum
  // only the attributed rows, so the hero reconciles with the residual-hidden
  // table below. Everywhere else the hero keeps the full slice total. Keyed to
  // the fetched axis so the hero identity flips together with the rows.
  const collectionDim: Dimension | null =
    path.length === 0 && !sessionsMode && isAttributionDim(dataGroupBy)
      ? dataGroupBy
      : null;

  // Roll the child rows up into the current entity's headline stats. Cost,
  // tokens, tool calls and cache tokens are additive, so summing the breakdown
  // rows gives the correct slice total. Sessions (total_chats) are NOT: it's a
  // distinct count (uniqExact over conversation ids), so summing it across groups
  // double-counts any session that spans more than one group (e.g. a chat that
  // used two models, or hit two agents). That inflated the "Agent sessions" stat
  // — and every "per session" cost — the moment you drilled or re-pivoted, and
  // made a user's session count disagree with the parent breakdown's Sessions
  // column (DNO-390). Take the true distinct count from the un-grouped slice
  // aggregate instead. Skipped for the root "collection" view, whose hero is
  // deliberately the attributed-rows-only subtotal, not the whole slice.
  const stats: Measures = useMemo(() => {
    const table = data?.table ?? [];
    const statsRows =
      collectionDim != null ? table.filter((r) => r.groupValue !== "") : table;
    const summed = statsRows.reduce<Measures>(
      (acc, r) => ({
        cost: acc.cost + (r.measures.totalCost ?? 0),
        sessions: acc.sessions + (r.measures.totalChats ?? 0),
        tools: acc.tools + (r.measures.totalToolCalls ?? 0),
        tokens: acc.tokens + (r.measures.totalTokens ?? 0),
        cacheCreation:
          acc.cacheCreation + (r.measures.cacheCreationInputTokens ?? 0),
      }),
      { ...EMPTY_MEASURES },
    );
    const trueSessions =
      collectionDim == null ? detailRow?.measures.totalChats : undefined;
    return { ...summed, sessions: trueSessions ?? summed.sessions };
  }, [data, collectionDim, detailRow]);

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

  // Previous-period totals per measure (for the KPI deltas). Sessions use the
  // un-grouped distinct count on the same basis as `stats` (see above).
  const prevTotals: Measures = useMemo(() => {
    const table = prevData?.table ?? [];
    const statsRows =
      collectionDim != null ? table.filter((r) => r.groupValue !== "") : table;
    const summed = statsRows.reduce<Measures>(
      (acc, r) => ({
        cost: acc.cost + (r.measures.totalCost ?? 0),
        sessions: acc.sessions + (r.measures.totalChats ?? 0),
        tools: acc.tools + (r.measures.totalToolCalls ?? 0),
        tokens: acc.tokens + (r.measures.totalTokens ?? 0),
        cacheCreation:
          acc.cacheCreation + (r.measures.cacheCreationInputTokens ?? 0),
      }),
      { ...EMPTY_MEASURES },
    );
    const trueSessions =
      collectionDim == null
        ? prevDetailData?.table?.[0]?.measures.totalChats
        : undefined;
    return { ...summed, sessions: trueSessions ?? summed.sessions };
  }, [prevData, prevDetailData, collectionDim]);

  // Each measure summed across groups per time bucket — drives the hero trend
  // chart and the KPI sparklines.
  const widgetSeries = useMemo(() => {
    const ts = (data?.timeseries ?? []).filter(
      (s) => collectionDim == null || s.groupValue !== "",
    );
    const n = ts[0]?.points.length ?? 0;
    const cost = Array<number>(n).fill(0);
    const chats = Array<number>(n).fill(0);
    const tools = Array<number>(n).fill(0);
    const tokens = Array<number>(n).fill(0);
    const cacheCreation = Array<number>(n).fill(0);
    for (const s of ts) {
      s.points.forEach((p, i) => {
        cost[i] = (cost[i] ?? 0) + (p.measures.totalCost ?? 0);
        chats[i] = (chats[i] ?? 0) + (p.measures.totalChats ?? 0);
        tools[i] = (tools[i] ?? 0) + (p.measures.totalToolCalls ?? 0);
        tokens[i] = (tokens[i] ?? 0) + (p.measures.totalTokens ?? 0);
        cacheCreation[i] =
          (cacheCreation[i] ?? 0) + (p.measures.cacheCreationInputTokens ?? 0);
      });
    }
    return { cost, chats, tools, tokens, cacheCreation };
  }, [data, collectionDim]);

  // Per-level secondary breakdowns: the configured cuts for the current axis,
  // minus any already filtered or that don't vary within this slice. Counted
  // over NAMED values: the cards drop the "" bucket from display, so an axis
  // splittable only via "(unset)" would make a pointless one-row card.
  const mixDims = (MIX_DIMS[groupBy] ?? [Dimension.Model]).filter(
    (d) =>
      d !== groupBy &&
      !path.some((c) => c.dim === d) &&
      (!availableDims || availableDims.has(d)) &&
      // In an unscoped dataset view only the dataset's own attribution dims are
      // correctly scoped; a cross-cut like Model would sum whole-project spend.
      (sliceScoped || datasetDimSet.has(d)) &&
      (sliceNamedValueCount(d) ?? 2) > 1,
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
    enabled: projectReady && !axisResolving && !!mixDimA,
    throwOnError: false,
    placeholderData: keepPreviousData,
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
    enabled: projectReady && !axisResolving && !!mixDimB,
    throwOnError: false,
    placeholderData: keepPreviousData,
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
    // Mix cards are compact "spend by X" rankings, so drop the "" bucket
    // entirely — an "(unset)" row is noise here (e.g. the $0 tool-row model
    // bucket). The user dimension is the exception: its "" bucket is the
    // Team-wide API Usage (company-credential sessions carry no user identity),
    // a real ranked spender the card must show. The full breakdown table
    // still surfaces "" for non-attribution dims where it is a real,
    // drillable slice.
    const toRows = (t: QueryRow[], dim: Dimension) =>
      t
        .filter((r) => r.groupValue !== "" || dim === Dimension.Email)
        .map((r) => ({
          label: displayName(dim, r.groupValue),
          cost: r.measures.totalCost ?? 0,
        }));
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
    // Keyed to the FETCHED axis: the rows are only user rows once they land.
    if (dataGroupBy === Dimension.Email) {
      const userRows = (data?.table ?? [])
        .filter(
          (r) => r.groupValue !== "Other" && r.groupValue !== serverRollupValue,
        )
        .slice(0, 5);
      out.push({
        kind: "mix",
        title: "Top spenders",
        dim: Dimension.Email,
        drillable: drillableDim(Dimension.Email),
        rows: toRows(userRows, Dimension.Email),
        loading: loadingSlice,
      });
    }
    if (mixDimA) {
      out.push({
        kind: "mix",
        title: cardTitle(mixDimA),
        dim: mixDimA,
        drillable: drillableDim(mixDimA),
        rows: toRows(mixDataA?.table ?? [], mixDimA),
        loading: mixLoadingA,
      });
    }
    if (mixDimB) {
      out.push({
        kind: "mix",
        title: cardTitle(mixDimB),
        dim: mixDimB,
        drillable: drillableDim(mixDimB),
        rows: toRows(mixDataB?.table ?? [], mixDimB),
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
            ? formatCost(perSession(stats.cost)!)
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
    dataGroupBy,
    serverRollupValue,
    availableDims,
    stats,
    data,
    loadingSlice,
  ]);

  // Filter by a (dimension, value) and advance to that dimension's child axis.
  // Used by both the main table (current axis) and the mix-card rows (their own
  // cross-cut axis, e.g. drilling a department straight from the Division view).
  const drillIntoDim = (
    dim: Dimension,
    value: string,
    rowValues?: Record<string, string[]>,
  ) => {
    // "" (the "(unset)" bucket) is drillable — it filters to the entities
    // missing this attribute. Synthetic top-N rollups aren't: the main
    // table's remainder is matched by identity (serverRollupValue covers the
    // suffixed label a collision produces); the literal covers the mix
    // cards' own smaller-topN rollup rows.
    if (value === "Other" || value === serverRollupValue) return;
    // Never re-add a dimension already in the path — that produces nonsensical
    // chains (e.g. the same user/agent twice). The pivot list already hides
    // filtered dims; this guards the mix-card + fallback-chain paths too.
    if (path.some((c) => c.dim === dim)) return;
    // Drilling from `all` into an attribution cut (e.g. a "Spend by MCP server"
    // mix-card row) promotes the view into that dataset. Within a dataset the
    // drill never switches datasets — undefined preserves the current one.
    const ds =
      dataset === "all" && isAttributionDim(dim)
        ? datasetForDim(dim)
        : undefined;
    // Agent/Model are leaves: drilling a row shows that slice's individual
    // sessions instead of pivoting to another dimension.
    if (isSessionLeaf(dim)) {
      goToNode([...path, { dim, value }], SESSIONS_AXIS, false, ds);
      return;
    }
    // Otherwise land on the next chain axis that actually has data, skipping
    // empty links (e.g. divisions → users when the org has no departments). Null
    // means nothing populated below — don't drill into an empty level. (While
    // availability is still loading this returns the static next dimension, so
    // drilling stays enabled and never blocks prematurely.)
    const staticNext = nextAvailableDimension(dim, availableDims);
    if (staticNext === null) return;
    // When the drilled row's own distinct values are known (a main-table row —
    // each query row carries the dimension_values of its slice), resolve the
    // landing axis against them so a one-department division drills straight
    // to its users, with the URL correct from the first render. Same resolver
    // as the on-load `groupBy` — one policy, two entry points. Mix-card drills
    // pass no rowValues and land statically; the destination's own resolution
    // (plus the URL rewrite) corrects the axis on arrival.
    const next = rowValues
      ? firstSplittableDimension(
          staticNext,
          availableDims,
          (d) => rowValues[d]?.length ?? 0,
        )
      : staticNext;
    goToNode([...path, { dim, value }], next, false, ds);
  };

  // Drill into a main-table row: use the FETCHED axis — the displayed rows'
  // groupValues belong to it, and during a re-pivot's keepPreviousData window
  // drilling must target what the user actually sees, not the pending axis.
  const drillInto = (row: QueryRow) =>
    drillIntoDim(dataGroupBy, row.groupValue, row.dimensionValues);

  // Rows are drillable only when there's a *populated* level below the
  // displayed axis — so you can't drill into an empty breakdown.
  // (Availability-unknown during load falls back to the static chain, keeping
  // rows drillable.)
  const canDrill =
    isSessionLeaf(dataGroupBy) ||
    nextAvailableDimension(dataGroupBy, availableDims) !== null;

  // Go up one ancestor: drop the deepest filter and regroup by the axis that
  // produced it (the removed crumb's dimension) — i.e. show the parent's profile.
  const goUp = () => {
    if (path.length === 0) return;
    const removed = path[path.length - 1]!;
    goToNode(path.slice(0, -1), removed.dim);
  };

  // Jump straight back to the org root (clear all filters) and reset to the full
  // `all` dataset — Home always lands on the project-wide overview.
  const goHome = () =>
    goToNode([], defaultGroupBy([], availableDims), false, "all");

  // The control bar's Reset: back to the default view in one navigation —
  // root drill path, default axis, `all` dataset, default date range (drops
  // the from/to/range/label params goToNode would preserve), search cleared.
  const resetView = () => {
    setBreakdownSearch("");
    const params = new URLSearchParams();
    params.set(BREAKDOWN_PARAM, defaultGroupBy([], availableDims));
    void navigate(`${costsBase}?${params.toString()}`);
  };

  // Re-pivot the current node's breakdown axis without drilling (view-only).
  const changeGroupBy = (axis: Axis) => goToNode(path, axis, true);

  // Switch datasets: start a fresh drill at that slice's root grouped by its
  // default axis. A drill path from another dataset (e.g. a division filter)
  // doesn't carry over, so clear it.
  const changeDataset = (ds: Dataset) =>
    goToNode([], datasetDefaultGroupBy(ds, [], availableDims), false, ds);

  // Offer a breakdown axis only if it can actually partition the current slice
  // into >1 group ("(unset)" buckets count — the table renders them, see
  // sliceValueCount). The active axis is always kept: the slice-aware `groupBy`
  // resolution guarantees it can split the slice, except (a) the terminal
  // nothing-splits case, where a one-row table with its axis shown beats an
  // empty track, and (b) attribution axes, which the resolver accepts as-is
  // (their pruning is the dataset structure itself). Show everything at the
  // org root, where there's no slice to measure against yet.
  const filteredDims = new Set(path.map((c) => c.dim));
  const pivotOptions = datasetPivots(dataset).filter((p) => {
    if (filteredDims.has(p.dim)) return false;
    if (p.dim === groupBy) return true;
    // A nested attribution cut (MCP Tool under MCP Server, Skill under Subagent)
    // is only meaningful once its parent is pinned in the drill path — offer it
    // as a breakdown axis only then.
    const parent = datasetPivotParent(dataset, p.dim);
    if (parent && !filteredDims.has(parent)) return false;
    // Hide dimensions the org has no data for at all (IDP doesn't populate them).
    if (availableDims && !availableDims.has(p.dim)) return false;
    return (sliceValueCount(p.dim) ?? 2) > 1;
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
    // The per-session list is only offered where the slice is scoped — at an
    // unscoped dataset root it can't be filtered to the slice (see sliceScoped).
    ...(sliceScoped
      ? [{ value: SESSIONS_AXIS, label: LABELS[SESSIONS_AXIS]! }]
      : []),
  ];
  const axisValue: string = sessionsMode ? SESSIONS_AXIS : groupBy;

  // A search typed against one cut is meaningless against another — clear it
  // when the drill path or the breakdown axis changes.
  useEffect(() => {
    setBreakdownSearch("");
  }, [location.pathname, axisValue]);

  // The session list matches on everything its rows display: title, chat id,
  // user, agent, and model.
  const allSessions = sessionsData?.sessions ?? [];
  const searchedSessions = normalizedSearch
    ? allSessions.filter((s) =>
        [s.title, s.gramChatId, s.userEmail, s.hookSource, s.model].some(
          (field) => field?.toLowerCase().includes(normalizedSearch),
        ),
      )
    : allSessions;

  // Zero-match copy for the session search. Over a capped slice, scope the
  // claim to what was actually searched — lower-ranked sessions may still match.
  let sessionsEmptyMessage: string | undefined;
  if (normalizedSearch) {
    sessionsEmptyMessage = "No matches for your search.";
    if (allSessions.length >= SESSION_LIMIT) {
      sessionsEmptyMessage = `No matches in the ${SESSION_LIMIT} most expensive sessions.`;
    }
  }
  const onViewSessions =
    sessionsMode || !sliceScoped
      ? undefined
      : () => changeGroupBy(SESSIONS_AXIS);

  // The root Skill breakdown is scoped to agent-less spend (skill-only branch of
  // the Subagent → Skill tree). Rather than relabel the axis "Skill (only)",
  // surface the caveat as an info tooltip beside the breakdown select. Keyed
  // to the FETCHED axis like the heading it decorates, so the caveat never
  // describes a cut the rows aren't showing yet.
  const skillOnlyBranch =
    dataGroupBy === Dimension.SkillName &&
    !path.some((c) => c.dim === Dimension.AgentName);
  const axisHint =
    skillOnlyBranch && !sessionsMode
      ? "Skills run outside a subagent. Skills invoked inside a subagent are grouped under that subagent (Subagent → Skill)."
      : undefined;

  const currentEntity = deepestCrumb;
  const parentValue = path.length >= 2 ? path[path.length - 2]!.value : null;

  // The root attribution "collection" identity (title + icon) for the hero, in
  // place of the project. Null everywhere else.
  const collection =
    collectionDim != null
      ? {
          dim: collectionDim,
          label: collectionLabel(collectionDim) ?? LABELS[collectionDim] ?? "",
        }
      : null;
  // Whether this view is an attribution lens (root collection, or drilled into
  // an attribution entity). Drives the hero/KPI swap of "Tool calls" → "Cache
  // added" — the meaningful measure for a server/tool/skill/subagent.
  const attributionView =
    collectionDim != null ||
    (currentEntity != null && isAttributionDim(currentEntity.dim));

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
  const assistantContext = `Cost dashboard — viewing ${scope}, broken down by ${childLabel.toLowerCase()}. Over ${rangeLabel}: ${formatCost(stats.cost)} total cost, ${stats.sessions.toLocaleString()} chat sessions, ${stats.tools.toLocaleString()} tool calls, ${stats.tokens.toLocaleString()} tokens. Active filters: ${filterSummary}.`;
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
      // Whose session it was — dropped only once the drill path pins a user, as
      // every row then repeats that same address. Deliberately independent of
      // `groupBy`: these are the slice's top sessions whatever the table below
      // is grouped by, so keying the sublabel off the breakdown axis both hid a
      // useful attribution and resized this card (and with it the whole widget
      // row) on every re-pivot.
      sublabel:
        currentEntity?.dim !== Dimension.Email && s.userEmail?.length
          ? s.userEmail
          : undefined,
      cost: s.totalCost,
    })),
    loading: topSessionsFetching && !topSessionsData,
  };
  const widgetCards = showSessionsWidget
    ? [sessionsCard, ...cards.slice(0, 1)]
    : cards;

  // Chart drill-down: a clicked/dragged bucket becomes the page's custom date
  // range, clamped to the current period (week/month buckets can overhang its
  // edges, and a week bar can extend past "now"). Stable identity — it feeds
  // the chart panel's chartOptions memo.
  const handleChartRangeSelect = useCallback(
    (start: Date, end: Date): void => {
      const s = new Date(Math.max(start.getTime(), from.getTime()));
      const e = new Date(Math.min(end.getTime(), to.getTime()));
      if (e <= s) return;
      setCustomRangeParam(s, e);
    },
    [from, to, setCustomRangeParam],
  );

  const widgets = (
    <CostWidgets
      series={widgetSeries}
      totals={stats}
      prevTotals={prevTotals}
      cards={widgetCards}
      rangeLabel={formatDateRange(from, to)}
      cacheMetric={attributionView}
      onDrill={drillIntoDim}
      onOpenSession={setOpenChatId}
      loading={loadingSlice}
      billingMode={viewBillingMode}
    />
  );

  // The stacked cost-over-time chart lives inside the breakdown section (under
  // the control bar, above the table) — it stacks by the same axis the bar
  // controls. Sessions mode swaps the table for the per-session list, where a
  // dimension-stacked chart would mismatch the view, so it comes off with it.
  const breakdownChart = sessionsMode ? undefined : (
    <CostBreakdownChart
      data={data}
      groupBy={dataGroupBy}
      serverRollupValue={serverRollupValue}
      loading={loadingSlice}
      isError={isError}
      onSelectRange={handleChartRangeSelect}
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
        path={path}
        collection={collection}
        cacheMetric={attributionView}
        widgets={widgets}
        onBack={goUp}
        onHome={goHome}
        projectName={project.name}
        parentValue={parentValue}
        stats={stats}
        // The FETCHED axis: the rows/table/section title render what the data
        // is actually grouped by, while axisValue (the lit segment) tracks the
        // live selection — see dataGroupBy.
        groupBy={dataGroupBy}
        canDrill={canDrill}
        axisValue={axisValue}
        axisOptions={axisOptions}
        axisHint={axisHint}
        onAxisChange={(value) => changeGroupBy(value as Axis)}
        searchValue={breakdownSearch}
        onSearchChange={setBreakdownSearch}
        rows={searchedRows}
        billingMode={viewBillingMode}
        onDrill={drillInto}
        tableOverride={
          sessionsMode ? (
            <SessionTable
              sessions={searchedSessions}
              isLoading={sessionsFetching && !sessionsData}
              isError={sessionsError}
              onOpen={setOpenChatId}
              hiddenColumns={hiddenSessionColumns}
              billingMode={viewBillingMode}
              emptyMessage={sessionsEmptyMessage}
              sourceCount={allSessions.length}
            />
          ) : undefined
        }
        overrideCsv={
          sessionsMode
            ? {
                rowCount: searchedSessions.length,
                build: () =>
                  buildSessionCsv(
                    searchedSessions,
                    hiddenSessionColumns,
                    viewBillingMode,
                  ),
              }
            : undefined
        }
        onViewSessions={onViewSessions}
        onReset={resetView}
        chart={breakdownChart}
        seriesByGroup={seriesByGroup}
        datasetValue={dataset}
        datasetOptions={DATASET_OPTIONS}
        onDatasetChange={(value) => changeDataset(value as Dataset)}
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
