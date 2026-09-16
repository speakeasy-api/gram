import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { useAuditLogs } from "@gram/client/react-query/auditLogs.js";
import { useChallenges } from "@gram/client/react-query/challenges.js";
import { HumanizeDateTime } from "@/lib/dates";
import { useOrgRoutes } from "@/routes";
import { useIdentityWindow } from "./useIdentityQueries";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "./IdentityPanel";

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
            title={log.action}
            detail={
              subject
                ? log.actorDisplayName || log.actorId
                : log.subjectDisplayName || log.subjectType
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
