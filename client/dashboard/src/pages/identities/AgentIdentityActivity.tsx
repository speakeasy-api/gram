import {
  auditActorPrincipal,
  useAuditPrincipals,
} from "@/components/auditlogs/audit-principals";
import { AuditPrincipalLink } from "@/components/auditlogs/principals";
import type { AuditLog } from "@gram/client/models/components/auditlog.js";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { useAuditLogs } from "@gram/client/react-query/auditLogs.js";
import { useChallenges } from "@gram/client/react-query/challenges.js";
import type { JSX, ReactNode } from "react";
import { SimpleTooltip, TooltipProvider } from "@/components/ui/Tooltip";
import { isAuditAction } from "@/lib/audit-actions";
import { formatAuditActionLabel } from "@/lib/audit-log-format";
import { HumanizeDateTime } from "@/lib/dates";
import { useOrgRoutes } from "@/routes";
import { useResourceLinkMaps } from "@/pages/access/useResourceLinkMaps";
import { useIdentityWindow } from "./useIdentityQueries";
import { agentChangeSummary } from "./agent-change-summary";
import {
  type CheckGroup,
  checkCountLabel,
  checkResourceLabel,
  groupChecks,
} from "./agent-checks";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "./IdentityPanel";

/**
 * A change in words, with its audit code a hover or focus away. A code with no
 * words of its own is shown as the code rather than a guess at its meaning.
 */
function ChangeLabel({ action }: { action: string }): JSX.Element {
  if (!isAuditAction(action)) {
    return <code className="text-xs">{action}</code>;
  }
  // Its own provider, so the row also renders where none is mounted.
  return (
    <TooltipProvider>
      <SimpleTooltip tooltip={<code className="text-xs">{action}</code>}>
        <span
          tabIndex={0}
          className="cursor-default underline decoration-dotted underline-offset-4"
        >
          {formatAuditActionLabel(action)}
        </span>
      </SimpleTooltip>
    </TooltipProvider>
  );
}

/** Shared so the "no principals to resolve" case is a stable reference. */
const NO_LOGS: AuditLog[] = [];

export function AgentIdentityAudit({
  agent,
  subject = false,
}: {
  agent: ManagedAgent;
  subject?: boolean;
}): JSX.Element {
  const { from, to } = useIdentityWindow();
  const query = useAuditLogs(
    {
      ...(subject
        ? { subjectType: "agent", subjectId: agent.id }
        : { actorId: agent.id }),
      from,
      to,
    },
    undefined,
    { throwOnError: false },
  );
  const rows = query.data?.result.logs ?? [];
  // The server nulls the display name for an agent actor, so a change one
  // agent made to another showed as a bare uuid. The audit log's own renderer
  // resolves the name and draws the agent's face.
  //
  // Only the subject feed names an actor; the actions feed names the subject
  // from the row itself. Resolving in both modes paid for the agent and
  // member inventories to fill a column that mode does not render.
  const identities = useAuditPrincipals(subject ? rows : NO_LOGS);
  // Server and project names say what a grant or connection block touched;
  // the lists load only when one is listed.
  const maps = useResourceLinkMaps(
    subject &&
      rows.some(
        (log) =>
          log.action.startsWith("agent:policy_grant") ||
          log.action.startsWith("agent:connection_block"),
      ),
  );
  const names = {
    server: (id: string) =>
      maps.toolsetMap.get(id)?.name ?? maps.mcpServerMap.get(id)?.name,
    project: (id: string) => maps.projectMap.get(id)?.name,
  };
  return (
    <IdentityPanel
      title={
        subject
          ? "Recent changes to this agent"
          : "Recent actions by this agent"
      }
      loading={query.isLoading}
      error={query.isError && rows.length === 0}
      refreshFailed={query.isError && rows.length > 0}
      onRetry={() => void query.refetch()}
      footer={
        subject
          ? "Identity and permission changes in the selected period, including changes made by human administrators."
          : "Audit events attributed to this agent in the selected period."
      }
    >
      {rows.length === 0 ? (
        <IdentityPanelEmpty>
          No recorded events in this period.
        </IdentityPanelEmpty>
      ) : (
        rows.map((log) => {
          const summary = agentChangeSummary(log, names);
          return (
            <IdentityPanelRow
              key={log.id}
              title={<ChangeLabel action={log.action} />}
              detail={
                subject ? (
                  <>
                    <AuditPrincipalLink
                      principal={auditActorPrincipal(log)}
                      identities={identities}
                      fallback={log.actorDisplayName || log.actorId}
                    />
                    {summary && <> · {summary}</>}
                  </>
                ) : (
                  log.subjectDisplayName || log.subjectType
                )
              }
              trailing={<HumanizeDateTime date={log.createdAt} />}
            />
          );
        })
      )}
    </IdentityPanel>
  );
}

/**
 * Scopes an agent's checks list leaves out. An agent records one
 * org:hooks_ingest check for every hook event it sends, thousands in a single
 * run, and they buried every check that says what the agent may reach. They
 * are left out on the server, so the total does not count them either.
 */
const HIDDEN_AGENT_CHECK_SCOPES = ["org:hooks_ingest"];

/** Denied checks fetched to group; the period's own total is counted apart. */
const FAILED_CHECKS_LIMIT = 200;
/** Allowed checks fetched to group below the failed ones. */
const ALLOWED_CHECKS_LIMIT = 100;

/** A label over one outcome's rows inside the checks panel. */
function ChecksHeading({ children }: { children: ReactNode }): JSX.Element {
  return (
    <div
      role="heading"
      aria-level={3}
      className="border-border bg-muted/40 text-muted-foreground border-b px-4 py-1.5 text-xs font-medium"
    >
      {children}
    </div>
  );
}

/** One scope on one resource: what was checked, how often, and when last. */
function CheckGroupRow({
  group,
  outcome,
}: {
  group: CheckGroup;
  outcome: "allow" | "deny";
}): JSX.Element {
  return (
    <IdentityPanelRow
      title={
        <>
          <code className="text-xs">{group.scope}</code>
          <span className="text-muted-foreground">
            {" · "}
            {checkResourceLabel(group)}
          </span>
        </>
      }
      detail={
        <>
          {checkCountLabel(outcome, group.count)} · last{" "}
          <HumanizeDateTime date={group.latest} />
        </>
      }
      trailing={outcome === "deny" ? "Denied" : "Allowed"}
      accent={outcome === "deny" ? "destructive" : undefined}
    />
  );
}

/**
 * One outcome's grouped checks. A slice whose own read failed says so rather
 * than claiming the agent had no such checks; its last loaded rows stay shown
 * under the panel's refresh banner.
 */
function CheckSection({
  groups,
  outcome,
  loadFailed,
  emptyText,
  failedText,
}: {
  groups: CheckGroup[];
  outcome: "allow" | "deny";
  loadFailed: boolean;
  emptyText: string;
  failedText: string;
}): JSX.Element {
  if (loadFailed) return <IdentityPanelEmpty>{failedText}</IdentityPanelEmpty>;
  if (groups.length === 0)
    return <IdentityPanelEmpty>{emptyText}</IdentityPanelEmpty>;
  return (
    <>
      {groups.map((group) => (
        <CheckGroupRow key={group.key} group={group} outcome={outcome} />
      ))}
    </>
  );
}

export function AgentIdentityChallenges({
  agent,
}: {
  agent: ManagedAgent;
}): JSX.Element {
  const { from, to } = useIdentityWindow();
  const routes = useOrgRoutes();
  const principalUrn = `agent:${agent.id}`;
  // Failed checks are read as their own slice so they are never pushed off
  // the page by allowed ones: an agent's gateway checks every server on every
  // call, so allowed checks arrive many times faster than denials.
  const failedQuery = useChallenges(
    {
      principalUrn,
      outcome: "deny",
      excludeScopes: HIDDEN_AGENT_CHECK_SCOPES,
      from,
      to,
      limit: FAILED_CHECKS_LIMIT,
    },
    undefined,
    { throwOnError: false },
  );
  const allowedQuery = useChallenges(
    {
      principalUrn,
      outcome: "allow",
      excludeScopes: HIDDEN_AGENT_CHECK_SCOPES,
      from,
      to,
      limit: ALLOWED_CHECKS_LIMIT,
    },
    undefined,
    { throwOnError: false },
  );
  const failedRows = failedQuery.data?.challenges ?? [];
  const allowedRows = allowedQuery.data?.challenges ?? [];
  const failed = groupChecks(failedRows);
  const allowed = groupChecks(allowedRows);
  const failedTotal = failedQuery.data?.total ?? 0;
  const allowedTotal = allowedQuery.data?.total ?? 0;
  const loaded = !!failedQuery.data || !!allowedQuery.data;
  const anyFailed = failedQuery.isError || allowedQuery.isError;
  const handoffParams = new URLSearchParams({
    identity: principalUrn,
    from: from.toISOString(),
    to: to.toISOString(),
  });
  const truncated =
    failedTotal > failedRows.length || allowedTotal > allowedRows.length;
  return (
    <IdentityPanel
      title="Authorization checks"
      handoffLabel="Roles & Permissions"
      handoffHref={`${routes.access.href()}/challenges?${handoffParams}`}
      loading={failedQuery.isLoading || allowedQuery.isLoading}
      error={anyFailed && !loaded}
      refreshFailed={anyFailed && loaded}
      onRetry={() => {
        void failedQuery.refetch();
        void allowedQuery.refetch();
      }}
      footer={
        failedTotal + allowedTotal > 0
          ? `${failedTotal.toLocaleString()} failed and ${allowedTotal.toLocaleString()} allowed checks in this period, grouped by scope and resource${truncated ? " from the latest ones" : ""}.`
          : undefined
      }
    >
      <ChecksHeading>Failed</ChecksHeading>
      <CheckSection
        groups={failed}
        outcome="deny"
        loadFailed={failedQuery.isError && !failedQuery.data}
        emptyText="No failed checks for this agent in this period."
        failedText="Failed checks could not be loaded."
      />
      <ChecksHeading>Allowed</ChecksHeading>
      <CheckSection
        groups={allowed}
        outcome="allow"
        loadFailed={allowedQuery.isError && !allowedQuery.data}
        emptyText="No allowed checks for this agent in this period."
        failedText="Allowed checks could not be loaded."
      />
    </IdentityPanel>
  );
}
