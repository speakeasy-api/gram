import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import type { AuthzChallenge } from "@gram/client/models/components/authzchallenge.js";
import type { ChallengeBucket } from "@gram/client/models/components/challengebucket.js";
import { Outcome } from "@gram/client/models/operations/listchallengebuckets.js";
import { useChallengeBuckets } from "@gram/client/react-query/challengeBuckets.js";
import { useChallenges } from "@gram/client/react-query/challenges.js";
import {
  Badge as MoonshineBadge,
  type Column,
  Table,
} from "@speakeasy-api/moonshine";
import { Button } from "@/components/ui/button";
import { Check, Loader2 } from "lucide-react";
import { keepPreviousData } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import { useChallengeRowColumns } from "./useChallengeRowColumns";
import { useGrantFlow } from "./useGrantFlow";

export type { ChallengeBucket } from "@gram/client/models/components/challengebucket.js";

type BucketOutcome = ChallengeBucket["outcome"];
type OutcomeFilter = "all" | "deny" | "allow" | "resolved";

/** Map an individual AuthzChallenge to a ChallengeBucket shape so the same table columns render it. */
function challengeToBucket(c: AuthzChallenge): ChallengeBucket {
  return {
    id: c.id,
    lastSeen: c.timestamp,
    firstSeen: c.timestamp,
    organizationId: c.organizationId,
    projectId: c.projectId,
    principalUrn: c.principalUrn,
    principalType: c.principalType,
    userEmail: c.userEmail,
    photoUrl: c.photoUrl,
    operation: c.operation,
    outcome: c.outcome,
    reason: c.reason,
    scope: c.scope,
    resourceKind: c.resourceKind,
    resourceId: c.resourceId,
    roleSlugs: c.roleSlugs,
    evaluatedGrantCount: c.evaluatedGrantCount,
    matchedGrantCount: c.matchedGrantCount,
    challengeCount: 1,
    challengeIds: [c.id],
    resolvedAt: c.resolvedAt,
    resolutionType: c.resolutionType,
    resolvedBy: c.resolvedBy,
    resolutionRoleSlug: c.resolutionRoleSlug,
  };
}

export function OutcomeBadge({
  outcome,
  resolved,
}: {
  outcome: BucketOutcome;
  resolved?: boolean;
}) {
  if (resolved) {
    return (
      <MoonshineBadge variant="neutral">
        <MoonshineBadge.Text>Resolved</MoonshineBadge.Text>
      </MoonshineBadge>
    );
  }

  const config: Record<
    BucketOutcome,
    { variant: "destructive" | "success" | "neutral"; label: string }
  > = {
    deny: { variant: "destructive", label: "Denied" },
    allow: { variant: "success", label: "Allowed" },
    error: { variant: "neutral", label: "Error" },
  };
  const c = config[outcome];

  return (
    <MoonshineBadge variant={c.variant}>
      <MoonshineBadge.Text>{c.label}</MoonshineBadge.Text>
    </MoonshineBadge>
  );
}

function FilterPill({
  active,
  onClick,
  children,
  count,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
  count?: number;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "inline-flex cursor-pointer items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs font-medium transition-colors",
        active
          ? "border-primary bg-primary/5 text-primary"
          : "border-border text-muted-foreground hover:bg-accent hover:text-foreground",
      )}
    >
      {children}
      {count !== undefined && (
        <span
          className={cn(
            "tabular-nums",
            active ? "text-primary/70" : "text-muted-foreground/70",
          )}
        >
          {count}
        </span>
      )}
    </button>
  );
}

export function ChallengesEmptyState({
  outcomeFilter,
}: {
  outcomeFilter: OutcomeFilter;
}) {
  return (
    <div className="border-border/50 bg-muted/20 rounded-lg border px-6 py-16 text-center">
      <div className="bg-primary/10 mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <Check className="text-primary h-6 w-6" />
      </div>
      <Type variant="body" className="font-medium">
        {outcomeFilter === "deny"
          ? "No denied access attempts"
          : outcomeFilter === "resolved"
            ? "No resolved challenges yet"
            : "No challenges found"}
      </Type>
      <Type variant="body" className="text-muted-foreground mt-1 text-sm">
        {outcomeFilter === "deny"
          ? "All authorization checks are passing. Your team's permissions look good."
          : outcomeFilter === "resolved"
            ? "Denied challenges that are resolved by granting access will appear here."
            : "Authorization challenges will appear here as your team uses the platform."}
      </Type>
    </div>
  );
}

function useOutcomeApiParam(outcomeFilter: OutcomeFilter): {
  outcome?: Outcome;
  resolved?: boolean;
} {
  if (outcomeFilter === "deny") return { outcome: Outcome.Deny };
  if (outcomeFilter === "allow") return { outcome: Outcome.Allow };
  if (outcomeFilter === "resolved") return { resolved: true };
  return {};
}

/** Fetch individual challenges for all currently-expanded buckets and build a lookup. */
function useExpandedChallenges(
  expandedIds: Set<string>,
  buckets: ChallengeBucket[],
): Map<string, ChallengeBucket[]> {
  // Collect all challenge IDs from expanded buckets into a single request.
  const allIds = useMemo(() => {
    const ids: string[] = [];
    for (const b of buckets) {
      if (expandedIds.has(b.id) && b.challengeCount > 1) {
        ids.push(...b.challengeIds);
      }
    }
    return ids;
  }, [expandedIds, buckets]);

  const { data } = useChallenges(
    allIds.length > 0 ? { ids: allIds } : undefined,
  );

  return useMemo(() => {
    const map = new Map<string, ChallengeBucket[]>();
    if (!data?.challenges) return map;

    // Index challenges by ID for fast lookup.
    const byId = new Map<string, AuthzChallenge>();
    for (const c of data.challenges) {
      byId.set(c.id, c);
    }

    // For each expanded bucket, map its challengeIds to bucket-shaped rows.
    for (const b of buckets) {
      if (!expandedIds.has(b.id) || b.challengeCount <= 1) continue;
      const rows: ChallengeBucket[] = [];
      for (const id of b.challengeIds) {
        const c = byId.get(id);
        if (c) rows.push(challengeToBucket(c));
      }
      if (rows.length > 0) map.set(b.id, rows);
    }
    return map;
  }, [data, buckets, expandedIds]);
}

/** Flatten buckets with expanded rows interleaved. */
function flattenWithExpanded(
  buckets: ChallengeBucket[],
  expandedMap: Map<string, ChallengeBucket[]>,
): ChallengeBucket[] {
  if (expandedMap.size === 0) return buckets;
  const result: ChallengeBucket[] = [];
  for (const b of buckets) {
    result.push(b);
    const expanded = expandedMap.get(b.id);
    if (expanded) result.push(...expanded);
  }
  return result;
}

export function ChallengesTab() {
  const [searchParams] = useSearchParams();
  const [outcomeFilter, setOutcomeFilter] = useState<OutcomeFilter>("deny");
  const [principalFilter, setPrincipalFilter] = useState(
    searchParams.get("identity") ?? "all",
  );

  // Sync principalFilter during render when URL param changes (no stale frame).
  const prevIdentityRef = useRef(searchParams.get("identity"));
  const identity = searchParams.get("identity");
  if (identity !== prevIdentityRef.current) {
    prevIdentityRef.current = identity;
    setPrincipalFilter(identity ?? "all");
  }
  const [scopeFilter, setScopeFilter] = useState("all");

  const {
    actionsColumn,
    grantFlowPortals,
    animatingOutIds,
    recentlyResolvedIds,
  } = useGrantFlow();

  const PAGE_SIZE = 50;
  const [accumulated, setAccumulated] = useState<ChallengeBucket[]>([]);
  const [pageCount, setPageCount] = useState(1);

  const outcomeParam = useOutcomeApiParam(outcomeFilter);
  const offset = (pageCount - 1) * PAGE_SIZE;

  const { data, isLoading, isFetching } = useChallengeBuckets(
    {
      ...outcomeParam,
      principalUrn: principalFilter !== "all" ? principalFilter : undefined,
      scope: scopeFilter !== "all" ? scopeFilter : undefined,
      limit: PAGE_SIZE,
      offset,
    },
    undefined,
    { placeholderData: keepPreviousData },
  );

  // Reset accumulated data when filters change.
  const filterKey = `${outcomeFilter}|${principalFilter}|${scopeFilter}`;
  const prevFilterKeyRef = useRef(filterKey);
  if (filterKey !== prevFilterKeyRef.current) {
    prevFilterKeyRef.current = filterKey;
    setAccumulated([]);
    setPageCount(1);
  }

  // Accumulate results as pages load.
  useEffect(() => {
    if (!data?.buckets) return;
    if (pageCount === 1) {
      setAccumulated(data.buckets);
    } else {
      setAccumulated((prev) => {
        const existingIds = new Set(prev.map((b) => b.id));
        const newItems = data.buckets.filter((b) => !existingIds.has(b.id));
        return [...prev, ...newItems];
      });
    }
  }, [data, pageCount]);

  const totalBuckets = data?.total ?? 0;
  const hasMore = accumulated.length < totalBuckets;
  const isLoadingMore = isFetching && pageCount > 1;

  // Pill counts: fetch totals for each tab (lightweight, limit=1).
  const baseFilters = {
    principalUrn: principalFilter !== "all" ? principalFilter : undefined,
    scope: scopeFilter !== "all" ? scopeFilter : undefined,
    limit: 1,
    offset: 0,
  };
  const { data: denyData } = useChallengeBuckets({
    ...baseFilters,
    outcome: Outcome.Deny,
  });
  const { data: resolvedData } = useChallengeBuckets({
    ...baseFilters,
    resolved: true,
  });
  const { data: allData } = useChallengeBuckets(baseFilters);

  // Unique values for filter dropdowns (from loaded data).
  const uniquePrincipals = useMemo(() => {
    const set = new Set(accumulated.map((b) => b.userEmail ?? b.principalUrn));
    return [...set].filter(Boolean).sort();
  }, [accumulated]);

  const uniqueScopes = useMemo(() => {
    const set = new Set(accumulated.map((b) => b.scope));
    return [...set].filter(Boolean).sort();
  }, [accumulated]);

  // Expansion state.
  const [expandedBucketIds, setExpandedBucketIds] = useState<Set<string>>(
    () => new Set(),
  );
  const toggleBucket = useCallback((bucketId: string) => {
    setExpandedBucketIds((prev) => {
      const next = new Set(prev);
      if (next.has(bucketId)) next.delete(bucketId);
      else next.add(bucketId);
      return next;
    });
  }, []);

  const expandedMap = useExpandedChallenges(expandedBucketIds, accumulated);
  const flatData = useMemo(
    () => flattenWithExpanded(accumulated, expandedMap),
    [accumulated, expandedMap],
  );

  // IDs of individual challenge rows (expanded children, excluding the bucket trigger row) for grey styling.
  const expandedChildIds = useMemo(() => {
    const ids = new Set<string>();
    const bucketIds = new Set(accumulated.map((b) => b.id));
    for (const rows of expandedMap.values()) {
      for (const r of rows) {
        if (!bucketIds.has(r.id)) ids.add(r.id);
      }
    }
    return ids;
  }, [expandedMap, accumulated]);

  const challengeRowColumns = useChallengeRowColumns(
    animatingOutIds,
    outcomeFilter,
    toggleBucket,
    expandedChildIds,
    recentlyResolvedIds,
  );

  const wrappedActionsColumn: Column<ChallengeBucket> = useMemo(
    () => ({
      ...actionsColumn,
      render: (row: ChallengeBucket) =>
        expandedChildIds.has(row.id) ? null : actionsColumn.render?.(row),
    }),
    [actionsColumn, expandedChildIds],
  );

  const columns: Column<ChallengeBucket>[] = [
    ...challengeRowColumns,
    wrappedActionsColumn,
  ];

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <FilterPill
          active={outcomeFilter === "deny"}
          onClick={() => setOutcomeFilter("deny")}
          count={denyData?.total}
        >
          Denied
        </FilterPill>
        <FilterPill
          active={outcomeFilter === "resolved"}
          onClick={() => setOutcomeFilter("resolved")}
          count={resolvedData?.total}
        >
          Resolved
        </FilterPill>
        <FilterPill
          active={outcomeFilter === "all"}
          onClick={() => setOutcomeFilter("all")}
          count={allData?.total}
        >
          All
        </FilterPill>

        <div className="border-border mx-1 h-6 border-l" />

        <Select value={principalFilter} onValueChange={setPrincipalFilter}>
          <SelectTrigger size="sm" className="h-8 w-[180px]">
            <SelectValue placeholder="All users" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All users</SelectItem>
            {uniquePrincipals.map((p) => (
              <SelectItem key={p} value={p}>
                {p}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={scopeFilter} onValueChange={setScopeFilter}>
          <SelectTrigger size="sm" className="h-8 w-[180px]">
            <SelectValue placeholder="All scopes" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All scopes</SelectItem>
            {uniqueScopes.map((s) => (
              <SelectItem key={s} value={s}>
                {s}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {isLoading && accumulated.length === 0 ? (
        <SkeletonTable />
      ) : accumulated.length === 0 ? (
        <ChallengesEmptyState outcomeFilter={outcomeFilter} />
      ) : (
        <>
          <Table columns={columns}>
            <Table.Header columns={columns} />
            <Table.Body>
              {flatData.map((row) => (
                <Table.Row
                  key={row.id}
                  row={row}
                  columns={columns}
                  className={
                    expandedChildIds.has(row.id) ? "bg-muted/50" : undefined
                  }
                />
              ))}
            </Table.Body>
          </Table>
          {(accumulated.length > 0 || isLoadingMore) && (
            <div className="bg-muted/20 flex items-center justify-between border-t px-4 py-3">
              <Type muted small>
                Showing {accumulated.length.toLocaleString()} of{" "}
                {totalBuckets.toLocaleString()}
              </Type>
              {hasMore ? (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setPageCount((p) => p + 1)}
                  disabled={isLoadingMore}
                >
                  {isLoadingMore ? (
                    <>
                      <Loader2 className="size-4 animate-spin" />
                      Loading...
                    </>
                  ) : (
                    "Load more"
                  )}
                </Button>
              ) : (
                <Type muted small>
                  All results loaded
                </Type>
              )}
            </div>
          )}
        </>
      )}

      <div className="border-border/50 bg-muted/30 mt-8 rounded-md border px-4 py-3">
        <Type variant="subheading" className="mb-3">
          About Challenges
        </Type>
        <div className="space-y-2 text-sm">
          <div className="flex items-start gap-3">
            <MoonshineBadge variant="destructive" className="mt-0.5 shrink-0">
              <MoonshineBadge.Text>Denied</MoonshineBadge.Text>
            </MoonshineBadge>
            <Type variant="body" className="text-muted-foreground text-sm">
              The principal lacked the required scope or grants to perform the
              action. Check role assignments and grant selectors.
            </Type>
          </div>
          <div className="flex items-start gap-3">
            <MoonshineBadge variant="success" className="mt-0.5 shrink-0">
              <MoonshineBadge.Text>Allowed</MoonshineBadge.Text>
            </MoonshineBadge>
            <Type variant="body" className="text-muted-foreground text-sm">
              The principal had matching grants satisfying the requested scope.
            </Type>
          </div>
          <div className="flex items-start gap-3">
            <MoonshineBadge variant="neutral" className="mt-0.5 shrink-0">
              <MoonshineBadge.Text>Resolved</MoonshineBadge.Text>
            </MoonshineBadge>
            <Type variant="body" className="text-muted-foreground text-sm">
              A denied challenge that has since been addressed by granting the
              required access.
            </Type>
          </div>
        </div>
      </div>

      {grantFlowPortals}
    </div>
  );
}
