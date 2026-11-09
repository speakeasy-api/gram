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
import { useSlugs } from "@/contexts/Sdk";
import { ResourceLink } from "@/pages/access/ResourceLink";
import {
  useResourceLinkMaps,
  type ResourceLinkMaps,
} from "@/pages/access/useResourceLinkMaps";
import { useIdentityWindow } from "./useIdentityQueries";
import { agentChangeSummary } from "./agent-change-summary";
import {
  type CheckGroup,
  checkCountLabel,
  checksFooter,
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
 * Scopes left out of the agent's other allowed checks. An agent records one
 * org:hooks_ingest check for every hook event it sends, thousands in a single
 * run, and they buried every check that says what the agent may reach. Only
 * allowed ones are routine: a denied hook event is a real failure, so the
 * failed slice keeps them.
 */
const HIDDEN_ALLOWED_CHECK_SCOPES = ["org:hooks_ingest"];

/** Denied checks fetched to group; the period's own total is counted apart. */
const FAILED_CHECKS_LIMIT = 200;
/** Allowed checks of each slice fetched to group below the failed ones. */
const ALLOWED_CHECKS_LIMIT = 100;
const MCP_CONNECT = "mcp:connect";

/** A label over one slice of rows inside the checks panel. */
function ChecksHeading({ children }: { children: ReactNode }): JSX.Element {
  return (
    <div
      role="heading"
      aria-level={4}
      className="border-border bg-muted/40 text-muted-foreground border-b px-4 py-1.5 text-xs font-medium"
    >
      {children}
    </div>
  );
}

/** A short note under a slice's rows, about how to read them. */
function ChecksNote({ children }: { children: ReactNode }): JSX.Element {
  return (
    <p className="border-border text-muted-foreground border-b px-4 py-2 text-xs">
      {children}
    </p>
  );
}

/** One scope on one resource: what was checked, how often, and when last. */
function CheckGroupRow({
  group,
  outcome,
  atLeast,
  maps,
}: {
  group: CheckGroup;
  outcome: "allow" | "deny";
  /** Whether the count is only the fetched checks of a longer slice. */
  atLeast: boolean;
  maps: ResourceLinkMaps;
}): JSX.Element {
  const { orgSlug } = useSlugs();
  return (
    <IdentityPanelRow
      title={
        group.toolListFilter ? (
          <span className="text-muted-foreground">Tool list filtering</span>
        ) : (
          <span className="inline-flex min-w-0 items-center gap-2">
            <code className="text-xs">{group.scope}</code>
            {/* Named and linked as the Roles & Permissions page does. */}
            <ResourceLink challenge={group} orgSlug={orgSlug ?? ""} {...maps} />
          </span>
        )
      }
      detail={
        <>
          {checkCountLabel(outcome, group.count, atLeast)} · last{" "}
          <HumanizeDateTime date={group.latest} />
        </>
      }
      trailing={outcome === "deny" ? "Denied" : "Allowed"}
      accent={outcome === "deny" ? "destructive" : undefined}
    />
  );
}

/** One outcome's slice, as read: its rows, how many there were, and its state. */
type CheckSlice = {
  groups: CheckGroup[];
  /** Fetched rows; fewer than `total` when the slice was cut off. */
  fetched: number;
  total: number | undefined;
  /** The read failed and nothing from an earlier read is left to show. */
  loadFailed: boolean;
  retry: () => void;
};

/**
 * One outcome's grouped checks. A slice whose own read failed says so, with a
 * way to try again, rather than claiming the agent had no such checks.
 */
function CheckSection({
  slice,
  outcome,
  emptyText,
  failedText,
  note,
  maps,
}: {
  slice: CheckSlice;
  outcome: "allow" | "deny";
  emptyText: string;
  failedText: string;
  /** How to read the rows, shown under them when there are any. */
  note?: ReactNode;
  maps: ResourceLinkMaps;
}): JSX.Element {
  if (slice.loadFailed)
    return (
      <IdentityPanelEmpty>
        {failedText}{" "}
        <button
          type="button"
          onClick={slice.retry}
          className="hover:text-foreground focus-visible:ring-ring underline underline-offset-2 focus-visible:ring-2 focus-visible:outline-none"
        >
          Try again
        </button>
      </IdentityPanelEmpty>
    );
  if (slice.groups.length === 0)
    return <IdentityPanelEmpty>{emptyText}</IdentityPanelEmpty>;
  // Grouping only the newest page can undercount a resource, and can leave
  // out one whose checks are all older.
  const truncated = slice.total !== undefined && slice.total > slice.fetched;
  return (
    <>
      {slice.groups.map((group) => (
        <CheckGroupRow
          key={group.key}
          group={group}
          outcome={outcome}
          atLeast={truncated}
          maps={maps}
        />
      ))}
      {note && <ChecksNote>{note}</ChecksNote>}
      {truncated && (
        <ChecksNote>
          Grouped from the latest {slice.fetched.toLocaleString()} of{" "}
          {slice.total?.toLocaleString()} checks, so older resources may be
          missing. Roles & Permissions lists every check.
        </ChecksNote>
      )}
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
  const slice = (
    filter: { outcome: "allow" | "deny"; scope?: string },
    excludeScopes: string[],
    limit: number,
  ) => ({ principalUrn, ...filter, excludeScopes, from, to, limit });
  // Each slice is read on its own so none is pushed off the page by another:
  // an agent's gateway checks every server on every call, so allowed checks
  // arrive many times faster than denials.
  const failedQuery = useChallenges(
    slice({ outcome: "deny" }, [], FAILED_CHECKS_LIMIT),
    undefined,
    { throwOnError: false },
  );
  // The servers the agent was allowed to connect to, below the failures: what
  // it may connect to matters less than what it was refused, but more than
  // any other check.
  const connectQuery = useChallenges(
    slice({ outcome: "allow", scope: MCP_CONNECT }, [], ALLOWED_CHECKS_LIMIT),
    undefined,
    { throwOnError: false },
  );
  const otherQuery = useChallenges(
    slice(
      { outcome: "allow" },
      [...HIDDEN_ALLOWED_CHECK_SCOPES, MCP_CONNECT],
      ALLOWED_CHECKS_LIMIT,
    ),
    undefined,
    { throwOnError: false },
  );
  const queries = [failedQuery, connectQuery, otherQuery];
  const read = (query: typeof failedQuery): CheckSlice => {
    const rows = query.data?.challenges ?? [];
    return {
      groups: groupChecks(rows),
      fetched: rows.length,
      total: query.data?.total,
      loadFailed: query.isError && !query.data,
      retry: () => void query.refetch(),
    };
  };
  const failed = read(failedQuery);
  const connected = read(connectQuery);
  const other = read(otherQuery);
  const loaded = queries.some((query) => !!query.data);
  // Resource names come from the org's lists, read only when a check names a
  // resource.
  const maps = useResourceLinkMaps(
    [...failed.groups, ...connected.groups, ...other.groups].some(
      (group) => !!group.resourceId,
    ),
  );
  const handoffParams = new URLSearchParams({
    identity: principalUrn,
    from: from.toISOString(),
    to: to.toISOString(),
  });
  const allowedTotal =
    connected.total === undefined || other.total === undefined
      ? undefined
      : connected.total + other.total;
  return (
    <IdentityPanel
      title="Authorization checks"
      handoffLabel="Roles & Permissions"
      handoffHref={`${routes.access.href()}/challenges?${handoffParams}`}
      loading={queries.some((query) => query.isLoading)}
      error={queries.some((query) => query.isError) && !loaded}
      // Only a slice that failed to refresh still shows an earlier result; one
      // that never loaded says so in its own section, with its own retry.
      refreshFailed={queries.some((query) => query.isError && !!query.data)}
      onRetry={() => {
        for (const query of queries) void query.refetch();
      }}
      footer={checksFooter(failed.total, allowedTotal)}
    >
      <ChecksHeading>Failed</ChecksHeading>
      <CheckSection
        slice={failed}
        outcome="deny"
        emptyText="No failed checks for this agent in this period."
        failedText="Failed checks could not be loaded."
        note={
          failed.groups.some((group) => group.scope === MCP_CONNECT) &&
          "The agent gateway checks every server in the organization on each request and records a denial for each one outside this agent's grants, so a server here may be one the agent never asked for."
        }
        maps={maps}
      />
      <ChecksHeading>MCP servers</ChecksHeading>
      <CheckSection
        slice={connected}
        outcome="allow"
        emptyText="No MCP server connections for this agent in this period."
        failedText="MCP server checks could not be loaded."
        maps={maps}
      />
      {(other.groups.length > 0 || other.loadFailed) && (
        <>
          <ChecksHeading>Other allowed checks</ChecksHeading>
          <CheckSection
            slice={other}
            outcome="allow"
            emptyText=""
            failedText="Other allowed checks could not be loaded."
            maps={maps}
          />
        </>
      )}
    </IdentityPanel>
  );
}
