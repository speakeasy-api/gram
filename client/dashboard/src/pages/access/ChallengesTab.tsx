import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { SkeletonTable } from "@/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { useSession } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import type { AuthzChallenge } from "@gram/client/models/components/authzchallenge.js";
import { useChallenges } from "@gram/client/react-query/challenges.js";
import { useMembers } from "@gram/client/react-query/members.js";
import {
  Badge as MoonshineBadge,
  Column,
  Table,
} from "@speakeasy-api/moonshine";
import {
  Building2,
  Check,
  ChevronRight,
  FolderOpen,
  KeyRound,
  Plug,
} from "lucide-react";
import { useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { useGrantFlow } from "./useGrantFlow";

export type { AuthzChallenge } from "@gram/client/models/components/authzchallenge.js";
export { invalidateAllChallenges } from "@gram/client/react-query/challenges.js";

type Outcome = AuthzChallenge["outcome"];
type Reason = AuthzChallenge["reason"];
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

export function getInitials(identifier: string): string {
  const name = identifier.split("@")[0] ?? identifier;
  return name.slice(0, 2).toUpperCase();
}

function ResourceLink({
  challenge,
  orgSlug,
  projectMap,
}: {
  challenge: AuthzChallenge;
  orgSlug: string;
  projectMap: Map<string, { slug: string; name: string }>;
}) {
  const { resourceKind, resourceId, projectId } = challenge;

  if (!resourceKind || !resourceId) {
    return (
      <Type variant="body" className="text-muted-foreground text-sm">
        —
      </Type>
    );
  }

  let to: string | null = null;
  let label = resourceId;
  let IconEl: typeof Building2 | null = null;

  if (resourceKind === "org") {
    label = "Organization";
    IconEl = Building2;
    to = `/${orgSlug}/settings`;
  } else if (resourceKind === "project") {
    const proj = projectMap.get(resourceId);
    label = proj?.name ?? resourceId;
    IconEl = FolderOpen;
    to = proj ? `/${orgSlug}/projects/${proj.slug}` : null;
  } else if (resourceKind === "mcp") {
    label = resourceId;
    IconEl = Plug;
    const proj = projectId ? projectMap.get(projectId) : undefined;
    to = proj ? `/${orgSlug}/projects/${proj.slug}/mcp/${resourceId}` : null;
  }

  if (to) {
    return (
      <Link
        to={to}
        className="inline-flex items-center gap-1.5 truncate text-sm text-blue-600 underline underline-offset-4 hover:text-blue-500 dark:text-blue-400 dark:hover:text-blue-300"
      >
        {IconEl && <IconEl className="h-3.5 w-3.5 shrink-0 opacity-60" />}
        <span className="truncate">{label}</span>
        <ChevronRight className="h-3 w-3 shrink-0" />
      </Link>
    );
  }

  return (
    <span className="text-muted-foreground inline-flex items-center gap-1.5 truncate text-sm">
      {IconEl && <IconEl className="h-3.5 w-3.5 shrink-0" />}
      <span className="truncate">{label}</span>
    </span>
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

function reasonLabel(reason: Reason): string {
  switch (reason) {
    case "grant_matched":
      return "Access granted — a matching role permission was found.";
    case "no_grants":
      return "No permissions configured for this identity.";
    case "scope_unsatisfied":
      return "The identity's roles don't include this permission.";
    case "invalid_check":
      return "The authorization check was malformed or invalid.";
    case "rbac_skipped_apikey":
      return "API keys bypass role checks — access was allowed directly.";
    case "dev_override":
      return "Access allowed via a development override.";
  }
}

export function useChallengeRowColumns(): Column<AuthzChallenge>[] {
  const { orgSlug } = useSlugs();
  const { organization } = useSession();
  const { data: membersData } = useMembers();
  const projectMap = useMemo(() => {
    const m = new Map<string, { slug: string; name: string }>();
    for (const p of organization.projects) {
      m.set(p.id, { slug: p.slug, name: p.name });
    }
    return m;
  }, [organization.projects]);
  const memberMap = useMemo(() => {
    const m = new Map<string, { email: string; photoUrl?: string }>();
    for (const member of membersData?.members ?? []) {
      m.set(member.id, { email: member.email, photoUrl: member.photoUrl });
    }
    return m;
  }, [membersData]);

  return useMemo(
    () => [
      {
        key: "avatar",
        header: "",
        width: "44px",
        render: (row: AuthzChallenge) => {
          const isApiKey = row.principalType === "api_key";
          const display = row.userEmail ?? row.principalUrn;
          return (
            <div
              className={cn(
                (row.outcome === "allow" || row.resolvedAt) && "opacity-40",
              )}
            >
              <Avatar className="h-8 w-8">
                {row.photoUrl && (
                  <AvatarImage src={row.photoUrl} alt={display} />
                )}
                <AvatarFallback className="text-[11px]">
                  {isApiKey ? (
                    <KeyRound className="h-4 w-4" />
                  ) : (
                    getInitials(display)
                  )}
                </AvatarFallback>
              </Avatar>
            </div>
          );
        },
      },
      {
        key: "identity",
        header: "Identity",
        width: "1fr",
        render: (row: AuthzChallenge) => (
          <Tooltip>
            <TooltipTrigger asChild>
              <Type
                variant="body"
                className={cn(
                  "truncate text-sm font-medium",
                  (row.outcome === "allow" || row.resolvedAt) && "opacity-40",
                )}
              >
                {row.userEmail ?? row.principalUrn}
              </Type>
            </TooltipTrigger>
            {row.roleSlugs.length > 0 && (
              <TooltipContent side="bottom">
                Roles: {row.roleSlugs.join(", ")}
              </TooltipContent>
            )}
          </Tooltip>
        ),
      },
      {
        key: "outcome",
        header: "Outcome",
        width: "85px",
        render: (row: AuthzChallenge) => (
          <div
            className={cn(
              (row.outcome === "allow" || row.resolvedAt) && "opacity-40",
            )}
          >
            <OutcomeBadge outcome={row.outcome} resolved={!!row.resolvedAt} />
          </div>
        ),
      },
      {
        key: "scope",
        header: "Required Scope",
        width: "1fr",
        render: (row: AuthzChallenge) => (
          <Tooltip>
            <TooltipTrigger asChild>
              <code
                className={cn(
                  "bg-muted rounded px-1.5 py-0.5 font-mono text-xs",
                  (row.outcome === "allow" || row.resolvedAt) && "opacity-40",
                )}
              >
                {row.scope}
              </code>
            </TooltipTrigger>
            <TooltipContent side="bottom" className="max-w-xs">
              <p className="text-xs">
                {reasonLabel(row.reason)}
                {row.evaluatedGrantCount > 0 &&
                  ` (${row.matchedGrantCount} of ${row.evaluatedGrantCount} grants matched)`}
              </p>
            </TooltipContent>
          </Tooltip>
        ),
      },
      {
        key: "resource",
        header: "Resource",
        width: "1.5fr",
        render: (row: AuthzChallenge) => (
          <div
            className={cn(
              "min-w-0 overflow-hidden",
              (row.outcome === "allow" || row.resolvedAt) && "opacity-40",
            )}
          >
            <ResourceLink
              challenge={row}
              orgSlug={orgSlug ?? ""}
              projectMap={projectMap}
            />
          </div>
        ),
      },
      {
        key: "resolvedBy",
        header: "Resolved By",
        width: "90px",
        render: (row: AuthzChallenge) => {
          if (!row.resolvedBy) {
            return (
              <Type variant="body" className="text-muted-foreground/40 text-sm">
                —
              </Type>
            );
          }
          const userId = row.resolvedBy.replace(/^user:/, "");
          const member = memberMap.get(userId);
          const display = member?.email ?? row.resolvedBy;
          return (
            <Tooltip>
              <TooltipTrigger asChild>
                <Avatar className="h-7 w-7">
                  {member?.photoUrl && (
                    <AvatarImage src={member.photoUrl} alt={display} />
                  )}
                  <AvatarFallback className="text-[10px]">
                    {getInitials(display)}
                  </AvatarFallback>
                </Avatar>
              </TooltipTrigger>
              <TooltipContent>{display}</TooltipContent>
            </Tooltip>
          );
        },
      },
      {
        key: "timestamp",
        header: "Time",
        width: "120px",
        render: (row: AuthzChallenge) => (
          <Tooltip delayDuration={500}>
            <TooltipTrigger asChild>
              <Type
                variant="body"
                className={cn(
                  "text-muted-foreground cursor-default text-sm whitespace-nowrap underline decoration-dotted underline-offset-4",
                  (row.outcome === "allow" || row.resolvedAt) && "opacity-40",
                )}
              >
                <HumanizeDateTime date={row.timestamp} />
              </Type>
            </TooltipTrigger>
            <TooltipContent>{row.timestamp.toLocaleString()}</TooltipContent>
          </Tooltip>
        ),
      },
    ],
    [orgSlug, projectMap, memberMap],
  );
}

export function ChallengesTab() {
  const [searchParams] = useSearchParams();
  const [outcomeFilter, setOutcomeFilter] = useState<OutcomeFilter>("deny");
  const [principalFilter, setPrincipalFilter] = useState(
    searchParams.get("identity") ?? "all",
  );
  const [scopeFilter, setScopeFilter] = useState("all");
  const { actionsColumn, grantFlowPortals } = useGrantFlow();
  const challengeRowColumns = useChallengeRowColumns();

  const { data, isLoading } = useChallenges({ limit: 200 });
  const challenges = data?.challenges ?? [];

  const counts = useMemo(() => {
    const c = { all: challenges.length, deny: 0, allow: 0, resolved: 0 };
    for (const ch of challenges) {
      if (ch.resolvedAt) {
        c.resolved++;
      } else if (ch.outcome === "deny") {
        c.deny++;
      } else {
        c.allow++;
      }
    }
    return c;
  }, [challenges]);

  const uniquePrincipals = useMemo(() => {
    const set = new Set(challenges.map((c) => c.userEmail ?? c.principalUrn));
    return [...set].sort();
  }, [challenges]);

  const uniqueScopes = useMemo(() => {
    const set = new Set(challenges.map((c) => c.scope));
    return [...set].sort();
  }, [challenges]);

  const filtered = useMemo(() => {
    let base = challenges;
    if (outcomeFilter === "resolved") {
      base = base.filter((c) => !!c.resolvedAt);
    } else if (outcomeFilter !== "all") {
      base = base.filter((c) => c.outcome === outcomeFilter && !c.resolvedAt);
    }
    if (principalFilter !== "all") {
      base = base.filter(
        (c) => (c.userEmail ?? c.principalUrn) === principalFilter,
      );
    }
    if (scopeFilter !== "all") {
      base = base.filter((c) => c.scope === scopeFilter);
    }
    return [...base].sort((a, b) => {
      const order = (o: Outcome) => (o === "deny" ? 0 : o === "error" ? 1 : 2);
      const diff = order(a.outcome) - order(b.outcome);
      if (diff !== 0) return diff;
      return b.timestamp.getTime() - a.timestamp.getTime();
    });
  }, [challenges, outcomeFilter, principalFilter, scopeFilter]);

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
          <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-emerald-100 dark:bg-emerald-900/30">
            <Check className="h-6 w-6 text-emerald-600 dark:text-emerald-400" />
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
