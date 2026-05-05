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
import { useSlugs } from "@/contexts/Sdk";
import {
  Badge as MoonshineBadge,
  Column,
  Table,
} from "@speakeasy-api/moonshine";
import { ChevronRight, KeyRound } from "lucide-react";
import { useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { useGrantFlow } from "./useGrantFlow";

export type Outcome = "deny" | "allow";
type Operation = "require" | "require_any" | "filter";
type Reason =
  | "grant_matched"
  | "no_grants"
  | "scope_unsatisfied"
  | "rbac_skipped_apikey"
  | "dev_override";

export interface AuthzChallenge {
  id: string;
  timestamp: string;
  organizationId: string;
  projectId: string;
  principalUrn: string;
  principalType: string;
  userEmail: string | null;
  photoUrl?: string;
  operation: Operation;
  outcome: Outcome;
  reason: Reason;
  scope: string;
  resourceKind: string;
  resourceId: string;
  roleSlugs: string[];
  evaluatedGrantCount: number;
  matchedGrantCount: number;
  /** Set when admin resolves the challenge (e.g. grants access). Null = unresolved. */
  resolvedAt: string | null;
}

type OutcomeFilter = "all" | Outcome | "resolved";

export const MOCK_CHALLENGES: AuthzChallenge[] = [
  {
    id: "01961f3a-0001-7000-8000-000000000001",
    timestamp: new Date(Date.now() - 2 * 60_000).toISOString(),
    organizationId: "org-1",
    projectId: "proj-1",
    principalUrn: "user:usr_abc123",
    principalType: "user",
    userEmail: "alice@acme.com",
    operation: "require",
    outcome: "deny",
    reason: "scope_unsatisfied",
    scope: "project:write",
    resourceKind: "project",
    resourceId: "proj-1",
    roleSlugs: ["member"],
    evaluatedGrantCount: 3,
    matchedGrantCount: 0,
    resolvedAt: null,
  },
  {
    id: "01961f3a-0002-7000-8000-000000000002",
    timestamp: new Date(Date.now() - 5 * 60_000).toISOString(),
    organizationId: "org-1",
    projectId: "",
    principalUrn: "api_key:key_xyz789",
    principalType: "api_key",
    userEmail: null,
    operation: "require",
    outcome: "deny",
    reason: "no_grants",
    scope: "org:admin",
    resourceKind: "",
    resourceId: "org-1",
    roleSlugs: [],
    evaluatedGrantCount: 0,
    matchedGrantCount: 0,
    resolvedAt: null,
  },
  {
    id: "01961f3a-0003-7000-8000-000000000003",
    timestamp: new Date(Date.now() - 8 * 60_000).toISOString(),
    organizationId: "org-1",
    projectId: "proj-2",
    principalUrn: "user:usr_def456",
    principalType: "user",
    userEmail: "bob@acme.com",
    operation: "require_any",
    outcome: "allow",
    reason: "grant_matched",
    scope: "mcp:connect",
    resourceKind: "project",
    resourceId: "proj-2",
    roleSlugs: ["admin"],
    evaluatedGrantCount: 12,
    matchedGrantCount: 2,
    resolvedAt: null,
  },
  {
    id: "01961f3a-0004-7000-8000-000000000004",
    timestamp: new Date(Date.now() - 12 * 60_000).toISOString(),
    organizationId: "org-1",
    projectId: "proj-1",
    principalUrn: "user:usr_ghi789",
    principalType: "user",
    userEmail: "carol@acme.com",
    operation: "filter",
    outcome: "allow",
    reason: "grant_matched",
    scope: "project:read",
    resourceKind: "project",
    resourceId: "proj-1",
    roleSlugs: ["member"],
    evaluatedGrantCount: 5,
    matchedGrantCount: 1,
    resolvedAt: null,
  },
  {
    id: "01961f3a-0005-7000-8000-000000000005",
    timestamp: new Date(Date.now() - 15 * 60_000).toISOString(),
    organizationId: "org-1",
    projectId: "",
    principalUrn: "user:usr_jkl012",
    principalType: "user",
    userEmail: "dave@acme.com",
    operation: "require",
    outcome: "deny",
    reason: "scope_unsatisfied",
    scope: "org:admin",
    resourceKind: "",
    resourceId: "org-1",
    roleSlugs: ["member"],
    evaluatedGrantCount: 3,
    matchedGrantCount: 0,
    resolvedAt: new Date(Date.now() - 10 * 60_000).toISOString(),
  },
  {
    id: "01961f3a-0007-7000-8000-000000000007",
    timestamp: new Date(Date.now() - 30 * 60_000).toISOString(),
    organizationId: "org-1",
    projectId: "proj-1",
    principalUrn: "user:usr_abc123",
    principalType: "user",
    userEmail: "alice@acme.com",
    operation: "require",
    outcome: "allow",
    reason: "grant_matched",
    scope: "project:read",
    resourceKind: "project",
    resourceId: "proj-1",
    roleSlugs: ["member"],
    evaluatedGrantCount: 3,
    matchedGrantCount: 1,
    resolvedAt: null,
  },
  {
    id: "01961f3a-0008-7000-8000-000000000008",
    timestamp: new Date(Date.now() - 45 * 60_000).toISOString(),
    organizationId: "org-1",
    projectId: "",
    principalUrn: "api_key:key_xyz789",
    principalType: "api_key",
    userEmail: null,
    operation: "require",
    outcome: "allow",
    reason: "rbac_skipped_apikey",
    scope: "project:read",
    resourceKind: "project",
    resourceId: "proj-1",
    roleSlugs: [],
    evaluatedGrantCount: 0,
    matchedGrantCount: 0,
    resolvedAt: null,
  },
  {
    id: "01961f3a-0009-7000-8000-000000000009",
    timestamp: new Date(Date.now() - 55 * 60_000).toISOString(),
    organizationId: "org-1",
    projectId: "proj-3",
    principalUrn: "user:usr_mno345",
    principalType: "user",
    userEmail: "eve@acme.com",
    operation: "require",
    outcome: "deny",
    reason: "scope_unsatisfied",
    scope: "mcp:write",
    resourceKind: "mcp",
    resourceId: "weather-tools",
    roleSlugs: ["member"],
    evaluatedGrantCount: 4,
    matchedGrantCount: 0,
    resolvedAt: new Date(Date.now() - 50 * 60_000).toISOString(),
  },
  {
    id: "01961f3a-000a-7000-8000-00000000000a",
    timestamp: new Date(Date.now() - 1.2 * 3600_000).toISOString(),
    organizationId: "org-1",
    projectId: "proj-1",
    principalUrn: "user:usr_pqr678",
    principalType: "user",
    userEmail: "frank@acme.com",
    operation: "require_any",
    outcome: "allow",
    reason: "grant_matched",
    scope: "mcp:read",
    resourceKind: "mcp",
    resourceId: "db-connector",
    roleSlugs: ["member"],
    evaluatedGrantCount: 6,
    matchedGrantCount: 1,
    resolvedAt: null,
  },
  {
    id: "01961f3a-000b-7000-8000-00000000000b",
    timestamp: new Date(Date.now() - 1.5 * 3600_000).toISOString(),
    organizationId: "org-1",
    projectId: "proj-2",
    principalUrn: "user:usr_stu901",
    principalType: "user",
    userEmail: "grace@acme.com",
    operation: "require",
    outcome: "deny",
    reason: "no_grants",
    scope: "mcp:connect",
    resourceKind: "mcp",
    resourceId: "slack-bot",
    roleSlugs: [],
    evaluatedGrantCount: 0,
    matchedGrantCount: 0,
    resolvedAt: null,
  },
  {
    id: "01961f3a-000c-7000-8000-00000000000c",
    timestamp: new Date(Date.now() - 2 * 3600_000).toISOString(),
    organizationId: "org-1",
    projectId: "proj-1",
    principalUrn: "api_key:key_deploy_01",
    principalType: "api_key",
    userEmail: null,
    operation: "require",
    outcome: "allow",
    reason: "rbac_skipped_apikey",
    scope: "project:write",
    resourceKind: "project",
    resourceId: "proj-1",
    roleSlugs: [],
    evaluatedGrantCount: 0,
    matchedGrantCount: 0,
    resolvedAt: null,
  },
  {
    id: "01961f3a-000d-7000-8000-00000000000d",
    timestamp: new Date(Date.now() - 2.5 * 3600_000).toISOString(),
    organizationId: "org-1",
    projectId: "",
    principalUrn: "user:usr_vwx234",
    principalType: "user",
    userEmail: "heidi@acme.com",
    operation: "require",
    outcome: "allow",
    reason: "grant_matched",
    scope: "org:read",
    resourceKind: "",
    resourceId: "org-1",
    roleSlugs: ["member"],
    evaluatedGrantCount: 3,
    matchedGrantCount: 1,
    resolvedAt: null,
  },
  {
    id: "01961f3a-000e-7000-8000-00000000000e",
    timestamp: new Date(Date.now() - 3 * 3600_000).toISOString(),
    organizationId: "org-1",
    projectId: "proj-3",
    principalUrn: "user:usr_mno345",
    principalType: "user",
    userEmail: "eve@acme.com",
    operation: "require",
    outcome: "deny",
    reason: "scope_unsatisfied",
    scope: "project:write",
    resourceKind: "project",
    resourceId: "proj-3",
    roleSlugs: ["member"],
    evaluatedGrantCount: 3,
    matchedGrantCount: 0,
    resolvedAt: null,
  },
  {
    id: "01961f3a-000f-7000-8000-00000000000f",
    timestamp: new Date(Date.now() - 3.5 * 3600_000).toISOString(),
    organizationId: "org-1",
    projectId: "proj-2",
    principalUrn: "user:usr_def456",
    principalType: "user",
    userEmail: "bob@acme.com",
    operation: "filter",
    outcome: "allow",
    reason: "grant_matched",
    scope: "mcp:read",
    resourceKind: "project",
    resourceId: "proj-2",
    roleSlugs: ["admin"],
    evaluatedGrantCount: 8,
    matchedGrantCount: 3,
    resolvedAt: null,
  },
  {
    id: "01961f3a-0010-7000-8000-000000000010",
    timestamp: new Date(Date.now() - 4 * 3600_000).toISOString(),
    organizationId: "org-1",
    projectId: "proj-1",
    principalUrn: "api_key:key_ci_02",
    principalType: "api_key",
    userEmail: null,
    operation: "require",
    outcome: "deny",
    reason: "no_grants",
    scope: "project:write",
    resourceKind: "project",
    resourceId: "proj-1",
    roleSlugs: [],
    evaluatedGrantCount: 0,
    matchedGrantCount: 0,
    resolvedAt: null,
  },
  {
    id: "01961f3a-0011-7000-8000-000000000011",
    timestamp: new Date(Date.now() - 5 * 3600_000).toISOString(),
    organizationId: "org-1",
    projectId: "proj-2",
    principalUrn: "user:usr_ghi789",
    principalType: "user",
    userEmail: "carol@acme.com",
    operation: "require",
    outcome: "allow",
    reason: "grant_matched",
    scope: "mcp:connect",
    resourceKind: "mcp",
    resourceId: "slack-bot",
    roleSlugs: ["admin"],
    evaluatedGrantCount: 10,
    matchedGrantCount: 2,
    resolvedAt: null,
  },
];

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

  const config = {
    deny: { variant: "destructive" as const, label: "Denied" },
    allow: { variant: "success" as const, label: "Allowed" },
  }[outcome];

  return (
    <MoonshineBadge variant={config.variant}>
      <MoonshineBadge.Text>{config.label}</MoonshineBadge.Text>
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
}: {
  challenge: AuthzChallenge;
  orgSlug: string;
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
  if (resourceKind === "project" && resourceId) {
    to = `/${orgSlug}/projects/${resourceId}`;
  } else if (resourceKind === "mcp" && projectId && resourceId) {
    to = `/${orgSlug}/projects/${projectId}/mcp/${resourceId}`;
  }

  if (to) {
    return (
      <Link
        to={to}
        className="inline-flex items-center gap-1 truncate text-sm text-blue-600 underline underline-offset-4 hover:text-blue-500 dark:text-blue-400 dark:hover:text-blue-300"
      >
        {resourceId}
        <ChevronRight className="h-3 w-3 shrink-0" />
      </Link>
    );
  }

  return (
    <Type variant="body" className="text-muted-foreground truncate text-sm">
      {resourceId}
    </Type>
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
    case "rbac_skipped_apikey":
      return "API keys bypass role checks — access was allowed directly.";
    case "dev_override":
      return "Access allowed via a development override.";
  }
}

export function useChallengeRowColumns(): Column<AuthzChallenge>[] {
  const { orgSlug } = useSlugs();

  return useMemo(
    () => [
      {
        key: "avatar",
        header: "",
        width: "52px",
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
        width: "180px",
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
        width: "90px",
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
        width: "150px",
        render: (row: AuthzChallenge) => (
          <div
            className={cn(
              (row.outcome === "allow" || row.resolvedAt) && "opacity-40",
            )}
          >
            <ResourceLink challenge={row} orgSlug={orgSlug ?? ""} />
          </div>
        ),
      },
      {
        key: "timestamp",
        header: "Time",
        width: "160px",
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
                <HumanizeDateTime date={new Date(row.timestamp)} />
              </Type>
            </TooltipTrigger>
            <TooltipContent>
              {new Date(row.timestamp).toLocaleString()}
            </TooltipContent>
          </Tooltip>
        ),
      },
    ],
    [orgSlug],
  );
}

export function ChallengesTab() {
  const [searchParams] = useSearchParams();
  const [outcomeFilter, setOutcomeFilter] = useState<OutcomeFilter>("all");
  const [principalFilter, setPrincipalFilter] = useState(
    searchParams.get("identity") ?? "all",
  );
  const [scopeFilter, setScopeFilter] = useState("all");
  const { actionsColumn, grantFlowPortals } = useGrantFlow();
  const challengeRowColumns = useChallengeRowColumns();

  // TODO: Replace with real API call once backend endpoint is ready
  const challenges = MOCK_CHALLENGES;
  const isLoading = false;

  const counts = useMemo(() => {
    const c = { all: challenges.length, deny: 0, allow: 0, resolved: 0 };
    for (const ch of challenges) {
      if (ch.resolvedAt) {
        c.resolved++;
      } else {
        c[ch.outcome]++;
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
      const outcomeOrder: Record<Outcome, number> = { deny: 0, allow: 1 };
      const diff = outcomeOrder[a.outcome] - outcomeOrder[b.outcome];
      if (diff !== 0) return diff;
      return new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime();
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
          active={outcomeFilter === "all"}
          onClick={() => setOutcomeFilter("all")}
          count={counts.all}
        >
          All
        </FilterPill>
        <FilterPill
          active={outcomeFilter === "deny"}
          onClick={() => setOutcomeFilter("deny")}
          count={counts.deny}
        >
          Denied
        </FilterPill>
        <FilterPill
          active={outcomeFilter === "allow"}
          onClick={() => setOutcomeFilter("allow")}
          count={counts.allow}
        >
          Allowed
        </FilterPill>
        <FilterPill
          active={outcomeFilter === "resolved"}
          onClick={() => setOutcomeFilter("resolved")}
          count={counts.resolved}
        >
          Resolved
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
        <div className="border-border/50 bg-muted/30 rounded-md border px-6 py-12 text-center">
          <Type variant="body" className="text-muted-foreground">
            No challenges found
            {outcomeFilter !== "all" && ` with outcome "${outcomeFilter}"`}.
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
