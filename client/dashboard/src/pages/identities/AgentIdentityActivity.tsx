import {
  auditActorPrincipal,
  useAuditPrincipals,
} from "@/components/auditlogs/audit-principals";
import { AuditPrincipalLink } from "@/components/auditlogs/principals";
import type { AuditLog } from "@gram/client/models/components/auditlog.js";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { useAuditLogs } from "@gram/client/react-query/auditLogs.js";
import { useChallenges } from "@gram/client/react-query/challenges.js";
import type { JSX } from "react";
import { SimpleTooltip, TooltipProvider } from "@/components/ui/Tooltip";
import { isAuditAction } from "@/lib/audit-actions";
import { formatAuditActionLabel } from "@/lib/audit-log-format";
import { HumanizeDateTime } from "@/lib/dates";
import { useOrgRoutes } from "@/routes";
import { useIdentityWindow } from "./useIdentityQueries";
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
        rows.map((log) => (
          <IdentityPanelRow
            key={log.id}
            title={<ChangeLabel action={log.action} />}
            detail={
              subject ? (
                <AuditPrincipalLink
                  principal={auditActorPrincipal(log)}
                  identities={identities}
                  fallback={log.actorDisplayName || log.actorId}
                />
              ) : (
                log.subjectDisplayName || log.subjectType
              )
            }
            trailing={<HumanizeDateTime date={log.createdAt} />}
          />
        ))
      )}
    </IdentityPanel>
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
  const query = useChallenges(
    { principalUrn, from, to, limit: 25 },
    undefined,
    { throwOnError: false },
  );
  const rows = query.data?.challenges ?? [];
  const handoffParams = new URLSearchParams({
    identity: principalUrn,
    from: from.toISOString(),
    to: to.toISOString(),
  });
  return (
    <IdentityPanel
      title="Authorization checks"
      handoffLabel="Roles & Permissions"
      handoffHref={`${routes.access.href()}/challenges?${handoffParams}`}
      loading={query.isLoading}
      error={query.isError && rows.length === 0}
      refreshFailed={query.isError && rows.length > 0}
      onRetry={() => void query.refetch()}
      footer={
        query.data
          ? `${rows.length} of ${query.data.total} recorded checks in this period`
          : undefined
      }
    >
      {rows.length === 0 ? (
        <IdentityPanelEmpty>
          No authorization checks recorded for this agent in this period.
        </IdentityPanelEmpty>
      ) : (
        rows.map((check) => (
          <IdentityPanelRow
            key={check.id}
            title={check.scope}
            detail={<HumanizeDateTime date={check.timestamp} />}
            trailing={check.outcome === "deny" ? "Denied" : "Allowed"}
            accent={check.outcome === "deny" ? "destructive" : undefined}
          />
        ))
      )}
    </IdentityPanel>
  );
}
