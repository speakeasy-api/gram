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
import { useChallenges } from "@gram/client/react-query/challenges.js";
import {
  Badge as MoonshineBadge,
  type Column,
  Table,
} from "@speakeasy-api/moonshine";
import { Check } from "lucide-react";
import { useCallback, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import { countChallenges, scopeChallenges } from "./challengeHelpers";
import { useChallengeRowColumns } from "./useChallengeRowColumns";
import { useGrantFlow } from "./useGrantFlow";

export type { AuthzChallenge } from "@gram/client/models/components/authzchallenge.js";

type Outcome = AuthzChallenge["outcome"];
type OutcomeFilter = "all" | "deny" | "allow" | "resolved";

export function OutcomeBadge({
  outcome,
  resolved,
}: {
  outcome: Outcome;
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
    Outcome,
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
  count: number;
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
      <span
        className={cn(
          "tabular-nums",
          active ? "text-primary/70" : "text-muted-foreground/70",
        )}
      >
        {count}
      </span>
    </button>
  );
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
  const groupSiblingIdsRef = useRef<Map<string, string[]>>(new Map());
  const getGroupChallengeIds = useCallback(
    (id: string) => groupSiblingIdsRef.current.get(id) ?? [id],
    [],
  );
  const {
    actionsColumn,
    grantFlowPortals,
    recentlyResolvedIds,
    animatingOutIds,
  } = useGrantFlow(getGroupChallengeIds);

  const { data, isLoading } = useChallenges({ limit: 200 });
  const challenges = useMemo(
    () => (data?.challenges ?? []).filter((c) => !!c.scope),
    [data?.challenges],
  );

  const scopedChallenges = useMemo(
    () => scopeChallenges(challenges, principalFilter, scopeFilter),
    [challenges, principalFilter, scopeFilter],
  );

  const counts = useMemo(
    () => countChallenges(scopedChallenges),
    [scopedChallenges],
  );

  const uniquePrincipals = useMemo(() => {
    const set = new Set(challenges.map((c) => c.userEmail ?? c.principalUrn));
    return [...set].filter(Boolean).sort();
  }, [challenges]);

  const uniqueScopes = useMemo(() => {
    const set = new Set(challenges.map((c) => c.scope));
    return [...set].filter(Boolean).sort();
  }, [challenges]);

  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(
    () => new Set(),
  );
  const toggleGroup = useCallback((groupKey: string) => {
    setExpandedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(groupKey)) next.delete(groupKey);
      else next.add(groupKey);
      return next;
    });
  }, []);

  const { filtered, groupCounts, groupKeys } = useMemo(() => {
    let base = scopedChallenges;
    if (outcomeFilter === "resolved") {
      base = base.filter(
        (c) => !!c.resolvedAt && !recentlyResolvedIds.has(c.id),
      );
    } else if (outcomeFilter !== "all") {
      base = base.filter(
        (c) =>
          (c.outcome === outcomeFilter && !c.resolvedAt) ||
          recentlyResolvedIds.has(c.id),
      );
    }
    const sorted = [...base].sort((a, b) => {
      const order = (o: Outcome) => (o === "deny" ? 0 : o === "error" ? 1 : 2);
      const diff = order(a.outcome) - order(b.outcome);
      if (diff !== 0) return diff;
      return b.timestamp.getTime() - a.timestamp.getTime();
    });

    // Group by (principal, scope, outcome, resource)
    const groups = new Map<string, AuthzChallenge[]>();
    for (const c of sorted) {
      const displayIdentity = c.userEmail ?? c.principalUrn;
      const key = `${displayIdentity}|${c.scope}|${c.outcome}|${c.resourceKind ?? ""}|${c.resourceId ?? ""}`;
      const arr = groups.get(key);
      if (arr) arr.push(c);
      else groups.set(key, [c]);
    }

    const result: AuthzChallenge[] = [];
    const counts = new Map<string, number>();
    const keys = new Map<string, string>();
    const siblingIds = new Map<string, string[]>();
    for (const [key, members] of groups) {
      const memberIds = members.map((m) => m.id);
      counts.set(members[0].id, members.length);
      for (const m of members) {
        keys.set(m.id, key);
        siblingIds.set(m.id, memberIds);
      }
      if (expandedGroups.has(key)) {
        result.push(...members);
      } else {
        result.push(members[0]);
      }
    }
    groupSiblingIdsRef.current = siblingIds;
    return { filtered: result, groupCounts: counts, groupKeys: keys };
  }, [scopedChallenges, outcomeFilter, recentlyResolvedIds, expandedGroups]);

  const challengeRowColumns = useChallengeRowColumns(
    animatingOutIds,
    groupCounts,
    groupKeys,
    toggleGroup,
    outcomeFilter,
  );

  const columns: Column<AuthzChallenge>[] = [
    ...challengeRowColumns,
    actionsColumn,
  ];

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <FilterPill
          active={outcomeFilter === "deny"}
          onClick={() => setOutcomeFilter("deny")}
          count={counts.deny}
        >
          Denied
        </FilterPill>
        <FilterPill
          active={outcomeFilter === "resolved"}
          onClick={() => setOutcomeFilter("resolved")}
          count={counts.resolved}
        >
          Resolved
        </FilterPill>
        <FilterPill
          active={outcomeFilter === "all"}
          onClick={() => setOutcomeFilter("all")}
          count={counts.all}
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

      {isLoading ? (
        <SkeletonTable />
      ) : filtered.length === 0 ? (
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
      ) : (
        <Table columns={columns} data={filtered} rowKey={(row) => row.id} />
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
