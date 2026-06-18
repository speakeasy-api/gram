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
import { useMemo, useState } from "react";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { useDateRangeFilter } from "@/components/observe/useDateRangeFilter";
import { useSlugs } from "@/contexts/Sdk";
import { EntityProfile } from "./EntityProfile";
import {
  CHAIN,
  type Crumb,
  type Measures,
  nextDimension,
  PIVOTS,
} from "./taxonomy";

const EMPTY_MEASURES: Measures = { cost: 0, sessions: 0, tools: 0, tokens: 0 };

/**
 * Top-level cost explorer — the org bird's-eye view that walks the taxonomy.
 * It owns the drill state (the filter `path` and the current `groupBy` axis),
 * runs one telemetry.query per level, and renders the generalized
 * {@link EntityProfile} for the current node (the org root when `path` is empty,
 * otherwise the entity last drilled into).
 */
export function CostsExplorer(): JSX.Element {
  const [path, setPath] = useState<Crumb[]>([]);
  const [groupBy, setGroupBy] = useState<Dimension>(Dimension.DivisionName);
  const { projectSlug } = useSlugs();

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
  const prevCostByGroup = useMemo(() => {
    const m = new Map<string, number>();
    for (const r of prevData?.table ?? []) {
      m.set(r.groupValue, r.measures.totalCost ?? 0);
    }
    return m;
  }, [prevData]);

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

  // Drill into a row: filter by it and advance to the next axis.
  const drillInto = (row: QueryRow) => {
    if (nextDimension(groupBy) === null) return;
    if (row.groupValue === "" || row.groupValue === "Other") return;
    const next = nextDimension(groupBy);
    setPath((p) => [...p, { dim: groupBy, value: row.groupValue }]);
    if (next) setGroupBy(next);
  };

  // Go up one ancestor: drop the current entity and regroup by the (new) last
  // entity's child axis — i.e. show the parent's profile.
  const goUp = () => {
    if (path.length === 0) return;
    const newPath = path.slice(0, -1);
    setPath(newPath);
    const last = newPath[newPath.length - 1];
    setGroupBy(last ? (nextDimension(last.dim) ?? last.dim) : CHAIN[0]!.dim);
  };

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

  return (
    <EntityProfile
      entity={currentEntity}
      onBack={goUp}
      parentValue={parentValue}
      stats={stats}
      groupBy={groupBy}
      pivotOptions={pivotOptions}
      onGroupByChange={setGroupBy}
      rows={rows}
      onDrill={drillInto}
      seriesByGroup={seriesByGroup}
      prevCostByGroup={prevCostByGroup}
      rangePicker={rangePicker}
      isLoading={isFetching && !data}
      isError={isError}
    />
  );
}
