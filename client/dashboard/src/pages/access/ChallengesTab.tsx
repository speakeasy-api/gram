import { Badge } from "@/components/ui/badge";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { SkeletonTable } from "@/components/ui/skeleton";
import { Column, Table } from "@speakeasy-api/moonshine";
import { useMemo, useState } from "react";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

type Outcome = "deny" | "allow" | "error";
type Operation = "require" | "require_any" | "filter";
type Reason =
  | "grant_matched"
  | "no_grants"
  | "scope_unsatisfied"
  | "invalid_check"
  | "rbac_skipped_apikey"
  | "dev_override";

interface AuthzChallenge {
  id: string;
  timestamp: string;
  organizationId: string;
  projectId: string;
  principalUrn: string;
  principalType: string;
  userEmail: string | null;
  operation: Operation;
  outcome: Outcome;
  reason: Reason;
  scope: string;
  resourceKind: string;
  resourceId: string;
  roleSlugs: string[];
  evaluatedGrantCount: number;
  matchedGrantCount: number;
}

type OutcomeFilter = "all" | Outcome;

const MOCK_CHALLENGES: AuthzChallenge[] = [
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
    scope: "build:write",
    resourceKind: "project",
    resourceId: "proj-1",
    roleSlugs: ["member"],
    evaluatedGrantCount: 3,
    matchedGrantCount: 0,
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
    scope: "build:read",
    resourceKind: "project",
    resourceId: "proj-1",
    roleSlugs: ["member"],
    evaluatedGrantCount: 5,
    matchedGrantCount: 1,
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
  },
  {
    id: "01961f3a-0006-7000-8000-000000000006",
    timestamp: new Date(Date.now() - 20 * 60_000).toISOString(),
    organizationId: "org-1",
    projectId: "proj-3",
    principalUrn: "user:usr_mno345",
    principalType: "user",
    userEmail: "eve@acme.com",
    operation: "require",
    outcome: "error",
    reason: "invalid_check",
    scope: "mcp:write",
    resourceKind: "mcp",
    resourceId: "mcp-server-1",
    roleSlugs: ["member"],
    evaluatedGrantCount: 3,
    matchedGrantCount: 0,
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
    scope: "build:read",
    resourceKind: "project",
    resourceId: "proj-1",
    roleSlugs: ["member"],
    evaluatedGrantCount: 3,
    matchedGrantCount: 1,
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
    scope: "build:read",
    resourceKind: "project",
    resourceId: "proj-1",
    roleSlugs: [],
    evaluatedGrantCount: 0,
    matchedGrantCount: 0,
  },
];

function OutcomeBadge({ outcome }: { outcome: Outcome }) {
  const config = {
    deny: { variant: "destructive" as const, label: "Denied" },
    allow: { variant: "outline" as const, label: "Allowed" },
    error: { variant: "warning" as const, label: "Error" },
  }[outcome];

  return (
    <Badge variant={config.variant} size="sm">
      {config.label}
    </Badge>
  );
}

function PrincipalCell({ challenge }: { challenge: AuthzChallenge }) {
  const isApiKey = challenge.principalType === "api_key";
  const display = challenge.userEmail ?? challenge.principalUrn;

  return (
    <div className="flex flex-col gap-0.5">
      <Type variant="body" className="max-w-[200px] truncate font-medium">
        {display}
      </Type>
      <div className="flex items-center gap-1.5">
        <Badge variant="outline" size="sm" className="px-1 text-[10px]">
          {isApiKey ? "API Key" : "User"}
        </Badge>
        {challenge.roleSlugs.length > 0 && (
          <Type variant="body" className="text-muted-foreground text-[11px]">
            {challenge.roleSlugs.join(", ")}
          </Type>
        )}
      </div>
    </div>
  );
}

function ScopeCell({ challenge }: { challenge: AuthzChallenge }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className="flex flex-col gap-0.5">
          <code className="bg-muted w-fit rounded px-1.5 py-0.5 font-mono text-xs">
            {challenge.scope}
          </code>
          {challenge.resourceKind && (
            <Type
              variant="body"
              className="text-muted-foreground max-w-[180px] truncate text-[11px]"
            >
              {challenge.resourceKind}:{challenge.resourceId}
            </Type>
          )}
        </div>
      </TooltipTrigger>
      <TooltipContent side="bottom" className="max-w-xs">
        <div className="space-y-1 text-xs">
          <div>
            <span className="text-muted-foreground">Operation:</span>{" "}
            {challenge.operation}
          </div>
          <div>
            <span className="text-muted-foreground">Reason:</span>{" "}
            {challenge.reason.replace(/_/g, " ")}
          </div>
          <div>
            <span className="text-muted-foreground">Grants evaluated:</span>{" "}
            {challenge.evaluatedGrantCount}
          </div>
          <div>
            <span className="text-muted-foreground">Grants matched:</span>{" "}
            {challenge.matchedGrantCount}
          </div>
        </div>
      </TooltipContent>
    </Tooltip>
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
  const [outcomeFilter, setOutcomeFilter] = useState<OutcomeFilter>("all");

  // TODO: Replace with real API call once backend endpoint is ready
  const challenges = MOCK_CHALLENGES;
  const isLoading = false;

  const counts = useMemo(() => {
    const c = { all: challenges.length, deny: 0, allow: 0, error: 0 };
    for (const ch of challenges) {
      c[ch.outcome]++;
    }
    return c;
  }, [challenges]);

  const filtered = useMemo(() => {
    const base =
      outcomeFilter === "all"
        ? challenges
        : challenges.filter((c) => c.outcome === outcomeFilter);
    return [...base].sort((a, b) => {
      const outcomeOrder = { deny: 0, error: 1, allow: 2 };
      const diff = outcomeOrder[a.outcome] - outcomeOrder[b.outcome];
      if (diff !== 0) return diff;
      return new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime();
    });
  }, [challenges, outcomeFilter]);

  const columns: Column<AuthzChallenge>[] = [
    {
      key: "outcome",
      header: "Outcome",
      width: "100px",
      render: (row) => <OutcomeBadge outcome={row.outcome} />,
    },
    {
      key: "principal",
      header: "Principal",
      width: "1fr",
      render: (row) => <PrincipalCell challenge={row} />,
    },
    {
      key: "scope",
      header: "Scope & Resource",
      width: "1fr",
      render: (row) => <ScopeCell challenge={row} />,
    },
    {
      key: "timestamp",
      header: "Time",
      width: "160px",
      render: (row) => (
        <Type variant="body" className="text-muted-foreground text-sm">
          <HumanizeDateTime date={new Date(row.timestamp)} />
        </Type>
      ),
    },
  ];

  return (
    <div>
      <div className="mb-4">
        <Heading variant="h4">Authorization Challenges</Heading>
        <Type muted small className="mt-1">
          Every authz decision (allow &amp; deny) across the organization.
          Denied decisions are prioritized.
        </Type>
      </div>

      <div className="mb-4 flex items-center gap-2">
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
          active={outcomeFilter === "error"}
          onClick={() => setOutcomeFilter("error")}
          count={counts.error}
        >
          Errors
        </FilterPill>
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
            <Badge
              variant="destructive"
              size="sm"
              className="mt-0.5 w-16 shrink-0 justify-center"
            >
              Denied
            </Badge>
            <Type variant="body" className="text-muted-foreground text-sm">
              The principal lacked the required scope or grants to perform the
              action. Check role assignments and grant selectors.
            </Type>
          </div>
          <div className="flex items-start gap-3">
            <Badge
              variant="outline"
              size="sm"
              className="mt-0.5 w-16 shrink-0 justify-center"
            >
              Allowed
            </Badge>
            <Type variant="body" className="text-muted-foreground text-sm">
              The principal had matching grants satisfying the requested scope.
            </Type>
          </div>
          <div className="flex items-start gap-3">
            <Badge
              variant="warning"
              size="sm"
              className="mt-0.5 w-16 shrink-0 justify-center"
            >
              Error
            </Badge>
            <Type variant="body" className="text-muted-foreground text-sm">
              The authorization check itself failed (e.g. invalid check
              definition). Investigate the scope and resource configuration.
            </Type>
          </div>
        </div>
      </div>
    </div>
  );
}
