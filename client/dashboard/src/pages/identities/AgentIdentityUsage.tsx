import { StackedTimeBarChart } from "@/components/chart/StackedTimeBarChart";
import { buildToolUsageTimeSeries } from "@/components/observe/toolUsageTimeSeriesChartData";
import { HumanizeDateTime } from "@/lib/dates";
import { IdentityLink } from "@/components/identity-link";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { telemetryGetToolUsageTargetTimeSeries } from "@gram/client/funcs/telemetryGetToolUsageTargetTimeSeries";
import { telemetryGetToolUsageTargets } from "@gram/client/funcs/telemetryGetToolUsageTargets";
import { telemetryGetToolUsageTotals } from "@gram/client/funcs/telemetryGetToolUsageTotals";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { useSdkClient } from "@/contexts/Sdk";
import { useOrganization } from "@/contexts/Auth";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "./IdentityPanel";
import { useIdentityProject, useIdentityWindow } from "./useIdentityQueries";

/**
 * What an agent has actually been doing, in the window the page is set to.
 *
 * Telemetry attributes a call to exactly one principal, and for an agent that
 * is the agent itself — never also the human who authorized it. So the calls
 * here are the agent's own, and "who it acted for" has to be answered from
 * its sessions instead.
 */
export function AgentIdentityUsage({
  agent,
}: {
  agent: ManagedAgent;
}): JSX.Element {
  const client = useGramContext();
  const { from, to } = useIdentityWindow();
  const { slug: gramProject } = useIdentityProject();

  // The agent's bare id, not its `agent:` urn: the telemetry filter takes the
  // principal's id and builds the urn itself.
  const payload = useMemo(
    () => ({
      from,
      to,
      userFilters: [{ kind: "agent_id" as const, key: agent.id }],
    }),
    [from, to, agent.id],
  );
  // Keyed by hand. These endpoints take their subject and window in the body,
  // and the generated key covers only the project and auth headers — so every
  // agent and every range would share one cache entry and the first answer
  // would be kept for all of them.
  const key = [
    "agent-usage",
    gramProject,
    agent.id,
    from.toISOString(),
    to.toISOString(),
  ];

  const totals = useQuery({
    queryKey: ["totals", ...key],
    queryFn: () =>
      unwrapAsync(
        telemetryGetToolUsageTotals(client, {
          gramProject,
          getToolUsageSummaryPayload: payload,
        }),
      ),
    throwOnError: false,
  });
  const targets = useQuery({
    queryKey: ["targets", ...key],
    queryFn: () =>
      unwrapAsync(
        telemetryGetToolUsageTargets(client, {
          gramProject,
          getToolUsageSummaryPayload: payload,
        }),
      ),
    throwOnError: false,
  });
  const series = useQuery({
    queryKey: ["series", ...key],
    queryFn: () =>
      unwrapAsync(
        telemetryGetToolUsageTargetTimeSeries(client, {
          gramProject,
          getToolUsageSummaryPayload: payload,
        }),
      ),
    throwOnError: false,
  });

  // Memoised rather than defaulted inline: a bare `?? []` is a new array on
  // every render, so the chart was rebuilt each time regardless.
  const timeSeries = series.data?.targetTimeSeries;
  const points = useMemo(() => timeSeries ?? [], [timeSeries]);
  const chart = useMemo(
    () =>
      buildToolUsageTimeSeries(
        points,
        (point) => point.targetLabel || point.targetId,
        from,
        to,
      ),
    [points, from, to],
  );
  const rows = targets.data?.targets ?? [];
  const calls = totals.data?.totals?.eventCount ?? 0;

  return (
    <>
      <IdentityPanel
        title="Tool calls"
        footer={
          calls > 0
            ? `${calls.toLocaleString()} calls in this period, by server`
            : undefined
        }
        loading={series.isPending}
        error={series.isError && points.length === 0}
        refreshFailed={series.isError && points.length > 0}
        onRetry={() => void series.refetch()}
        loadingVariant="block"
      >
        {points.length === 0 ? (
          <IdentityPanelEmpty>
            This agent made no tool calls in this period.
          </IdentityPanelEmpty>
        ) : (
          <div className="p-4">
            <StackedTimeBarChart
              labels={chart.labels}
              timestamps={chart.timestamps}
              bucketMs={chart.bucketMs}
              tooltipLabels={chart.tooltipLabels}
              datasets={chart.datasets}
            />
          </div>
        )}
      </IdentityPanel>

      <IdentityPanel
        title="Top servers"
        loading={targets.isPending}
        error={targets.isError && rows.length === 0}
        refreshFailed={targets.isError && rows.length > 0}
        onRetry={() => void targets.refetch()}
        footer={
          rows.length > 5
            ? `Top 5 of ${rows.length} servers this agent called`
            : undefined
        }
      >
        {rows.length === 0 ? (
          <IdentityPanelEmpty>
            This agent called no servers in this period.
          </IdentityPanelEmpty>
        ) : (
          rows
            .slice(0, 5)
            .map((target) => (
              <IdentityPanelRow
                key={target.targetId}
                title={target.targetLabel || target.targetId}
                detail={
                  target.failureCount > 0
                    ? `${target.failureCount.toLocaleString()} failed`
                    : undefined
                }
                trailing={target.eventCount.toLocaleString()}
              />
            ))
        )}
      </IdentityPanel>

      <AgentAuthorizers agent={agent} />
    </>
  );
}

/**
 * The people whose authorization this agent has been acting under.
 *
 * Read from its sessions, not from its calls: a tool call records the agent
 * as the caller and nothing else, because the approving human is credential
 * provenance rather than the actor. The session is the only place the two are
 * recorded together.
 */
function AgentAuthorizers({ agent }: { agent: ManagedAgent }): JSX.Element {
  const sdk = useSdkClient();
  const organization = useOrganization();
  // A session carries the authorizer's id and nothing else, so the name comes
  // from the member list. A failed read leaves the id, which is still true.
  const members = useMembers(undefined, undefined, { throwOnError: false });
  const nameFor = (id: string) =>
    members.data?.members.find((member) => member.id === id)?.name || id;
  // Listing sessions is the same decision as revoking one: agent:read alone
  // does not reach an agent's credentials.
  const canManage = agent.permissions.authorize;
  const sessions = useQuery({
    queryKey: ["agent-authorizers", organization.id, agent.id],
    queryFn: async ({ signal }) => {
      const page = await sdk.agents.listSessions(
        { agentId: agent.id, limit: 50 },
        undefined,
        { signal },
      );
      return page.result;
    },
    enabled: canManage,
    throwOnError: false,
    retry: false,
  });
  // Memoised rather than defaulted inline: a bare `?? []` is a new array on
  // every render, which made the ranking below recompute each time.
  const items = sessions.data?.items;
  const all = useMemo(() => items ?? [], [items]);
  // Counted per authorizer, so a name that appears once does not outrank one
  // that appears daily, and the most recent session is kept beside it.
  const ranked = useMemo(() => {
    const byUser = new Map<string, { count: number; latest: Date }>();
    for (const session of all) {
      const id = session.authorizerUserId;
      if (!id) continue;
      const seen = byUser.get(id);
      if (seen) {
        seen.count += 1;
        if (session.createdAt > seen.latest) seen.latest = session.createdAt;
      } else {
        byUser.set(id, { count: 1, latest: session.createdAt });
      }
    }
    return [...byUser.entries()].sort((a, b) => b[1].count - a[1].count);
  }, [all]);

  return (
    <IdentityPanel
      title="Acting for"
      loading={canManage && sessions.isPending}
      error={sessions.isError && all.length === 0}
      refreshFailed={sessions.isError && all.length > 0}
      onRetry={() => void sessions.refetch()}
      footer={
        ranked.length > 0
          ? "Counted from this agent's sessions. A tool call records the agent, not the person who authorized it."
          : undefined
      }
    >
      {!canManage ? (
        <IdentityPanelEmpty>
          You need permission to manage this agent to see who it acts for.
        </IdentityPanelEmpty>
      ) : ranked.length === 0 ? (
        <IdentityPanelEmpty>
          No one has authorized a session as this agent. A key or a federated
          platform signs in as the agent alone.
        </IdentityPanelEmpty>
      ) : (
        ranked
          .slice(0, 5)
          .map(([id, entry]) => (
            <IdentityPanelRow
              key={id}
              title={
                <IdentityLink identifier={{ userId: id }}>
                  {nameFor(id)}
                </IdentityLink>
              }
              detail={<HumanizeDateTime date={entry.latest} />}
              trailing={`${entry.count.toLocaleString()} ${entry.count === 1 ? "session" : "sessions"}`}
            />
          ))
      )}
    </IdentityPanel>
  );
}
