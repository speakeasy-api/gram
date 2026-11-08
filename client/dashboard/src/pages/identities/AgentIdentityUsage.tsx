import { StackedTimeBarChart } from "@/components/chart/StackedTimeBarChart";
import { buildToolOutcomeTimeSeries } from "@/components/observe/toolUsageTimeSeriesChartData";
import { useOtherSeriesColor } from "@/components/chart/useSeriesColors";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { telemetryGetToolUsageTargetTimeSeries } from "@gram/client/funcs/telemetryGetToolUsageTargetTimeSeries";
import { telemetryGetToolUsageTargets } from "@gram/client/funcs/telemetryGetToolUsageTargets";
import { telemetryGetToolUsageTotals } from "@gram/client/funcs/telemetryGetToolUsageTotals";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { unwrapAsync } from "@gram/client/types/fp";
import type { GetToolUsageTargetsResult } from "@gram/client/models/components/gettoolusagetargetsresult.js";
import { useCanViewToolLogs } from "@/components/observe/observeDeepLink";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { useMemo } from "react";
import { useScopedToolUsagePayload } from "@/components/observe/toolUsagePayload";
import type { ToolUsageUserFilter } from "@gram/client/models/components/toolusageuserfilter.js";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "./IdentityPanel";
import { useIdentityProject, useIdentityWindow } from "./useIdentityQueries";
import { summarizeAgentServerUse } from "./agentServerUse";
import { ServerOnBehalfOfLine } from "./GrantOnBehalfOf";
import { useAgentPolicyGrants } from "./useAgentPolicyGrants";
import { useServerInventory } from "@/pages/agents/credentials/useServerInventory";

/**
 * What an agent has actually been doing, in the window the page is set to.
 *
 * Telemetry attributes a call to exactly one principal, and for an agent that
 * is the agent itself. Whom it acted for is answered per server, from the
 * credential each call was signed in with.
 */
export function AgentIdentityUsage({
  agent,
  includeLocalTools = true,
}: {
  agent: ManagedAgent;
  /** The page's "Show local tools" choice, shared with its Tool logs. */
  includeLocalTools?: boolean;
}): JSX.Element {
  const client = useGramContext();
  const { from, to } = useIdentityWindow();
  const { slug: gramProject } = useIdentityProject();

  // The agent's bare id, not its `agent:` urn: the telemetry filter takes the
  // principal's id and builds the urn itself.
  const userFilters = useMemo<ToolUsageUserFilter[]>(
    () => [{ kind: "agent_id", key: agent.id }],
    [agent.id],
  );
  // Built and keyed exactly as the Tool logs further down this page build
  // theirs, so the totals and the series are one request each for both. Keyed
  // by hand before, the page sent each of them twice. The key carries the
  // body (subject, window, local tools choice), which the generated key
  // leaves out.
  const { summaryPayload: payload, sharedQueryKey } = useScopedToolUsagePayload(
    { userFilters, includeLocalTools, from, to },
  );

  const totals = useQuery({
    queryKey: ["tool-usage-totals", ...sharedQueryKey],
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
    queryKey: ["tool-usage-targets", ...sharedQueryKey],
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
    queryKey: ["tool-usage-target-time-series", ...sharedQueryKey],
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
  // Split by outcome in the colors the Tool logs timeline below uses for the
  // same calls. Stacked by source, unlabelled, its blue and pink read as
  // success and failure; Top sources beside it already splits the calls by
  // source.
  const pendingColor = useOtherSeriesColor();
  const chart = useMemo(
    () => buildToolOutcomeTimeSeries(points, from, to, pendingColor),
    [points, from, to, pendingColor],
  );
  const rows = targets.data?.targets ?? [];
  const calls = totals.data?.totals?.eventCount ?? 0;
  const failed = totals.data?.totals?.failureCount ?? 0;

  return (
    <>
      <IdentityPanel
        title="Tool calls"
        footer={
          calls > 0
            ? `${calls.toLocaleString()} calls in this period${
                failed > 0 ? `, ${failed.toLocaleString()} failed` : ""
              }`
            : undefined
        }
        // The footer's count comes from `totals`, so its failure has to reach
        // the panel too: reporting only `series` left the count silently
        // missing with nothing to retry.
        loading={series.isPending || totals.isPending}
        error={(series.isError || totals.isError) && points.length === 0}
        refreshFailed={(series.isError || totals.isError) && points.length > 0}
        onRetry={() => {
          void series.refetch();
          void totals.refetch();
        }}
        loadingVariant="block"
      >
        {points.length === 0 ? (
          <IdentityPanelEmpty>
            {includeLocalTools
              ? "This agent made no tool calls in this period."
              : "This agent made no tool calls other than local tools in this period."}
          </IdentityPanelEmpty>
        ) : (
          <div className="p-4">
            <StackedTimeBarChart
              labels={chart.labels}
              timestamps={chart.timestamps}
              bucketMs={chart.bucketMs}
              tooltipLabels={chart.tooltipLabels}
              datasets={chart.datasets}
              // A legend names each outcome, so color is never the only
              // signal.
              expanded
            />
          </div>
        )}
      </IdentityPanel>

      {/* "Sources", not "servers": the endpoint ranks every target a call
          can name, and a local tool set and a skill are both targets and
          neither is a server. Called one of those, an agent read its own
          activity as a mislabelled server row — or, using nothing else, as
          having called no server at all. */}
      <IdentityPanel
        title="Top sources"
        loading={targets.isPending}
        error={targets.isError && rows.length === 0}
        refreshFailed={targets.isError && rows.length > 0}
        onRetry={() => void targets.refetch()}
        footer={
          rows.length > 5
            ? `Top 5 of ${rows.length} sources this agent called`
            : undefined
        }
      >
        {rows.length === 0 ? (
          <IdentityPanelEmpty>
            {includeLocalTools
              ? "This agent called no tools in this period."
              : "This agent called no tools other than local tools in this period."}
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

      <AgentServersPanel agent={agent} targets={targets} />
    </>
  );
}

/** What the MCP servers panel says when it has no row to show. */
function agentServersEmptyText({
  canReadPolicy,
  canViewToolLogs,
  anyServer,
}: {
  canReadPolicy: boolean;
  canViewToolLogs: boolean;
  anyServer: boolean;
}): string {
  if (!canReadPolicy && !canViewToolLogs)
    return "You need permission to manage this agent, or the log:read permission on this project, to see its MCP servers.";
  if (canReadPolicy && !anyServer)
    return "This agent cannot connect to any MCP server.";
  // It may connect to any server, so only its calls can name them.
  if (!canViewToolLogs)
    return "This agent may connect to any MCP server. You need the log:read permission on this project to see the ones it called.";
  return "This agent called no MCP servers in this period.";
}

/**
 * Whom the agent acts for, per MCP server it can connect to, and how often it
 * called each in the period.
 *
 * The servers and their credentials are the Permissions tab's MCP servers
 * section, read through the same queries: a person's own connection assigned
 * to the agent there means it acts on that person's behalf, anything else is
 * a shared credential that acts for no one. They used to be read off the
 * credential each call recorded, which calls through the gateway did not
 * carry, so every server read "No credential recorded", and a period with no
 * such calls listed no servers at all.
 *
 * The counts come from the targets read the Top sources panel above already
 * makes, so this panel adds no telemetry request.
 */
function AgentServersPanel({
  agent,
  targets,
}: {
  agent: ManagedAgent;
  targets: UseQueryResult<GetToolUsageTargetsResult>;
}): JSX.Element {
  const canViewToolLogs = useCanViewToolLogs();
  const { id: projectId } = useIdentityProject();
  const canReadPolicy = agent.permissions.write;
  const grants = useAgentPolicyGrants(agent);
  const inventory = useServerInventory({ lookupOnly: true });

  const targetRows = canViewToolLogs ? targets.data?.targets : undefined;
  const { rows, anyServer } = useMemo(
    () =>
      summarizeAgentServerUse({
        grants: canReadPolicy ? grants.data : undefined,
        servers: inventory.servers,
        targets: targetRows,
        projectId,
      }),
    [canReadPolicy, grants.data, inventory.servers, targetRows, projectId],
  );

  const grantsFailed = canReadPolicy && grants.isError;
  const targetsFailed = canViewToolLogs && targets.isError;
  const loading =
    (canReadPolicy && grants.isPending) ||
    (canViewToolLogs && targets.isPending);

  let footer: string | undefined;
  if (rows.length > 0) {
    if (anyServer) {
      footer =
        "This agent may connect to any MCP server. Listed: the ones it called in this period.";
    } else if (!canReadPolicy) {
      footer =
        "The servers this agent called in this period. Managing the agent shows every server it can reach.";
    } else {
      footer =
        "Every MCP server this agent can connect to, whose credential it uses there, and its calls in this period.";
    }
  }

  const empty = agentServersEmptyText({
    canReadPolicy,
    canViewToolLogs,
    anyServer,
  });

  return (
    <IdentityPanel
      title="Acting on behalf of"
      loading={loading}
      error={(grantsFailed || targetsFailed) && rows.length === 0}
      refreshFailed={(grantsFailed || targetsFailed) && rows.length > 0}
      onRetry={() => {
        if (grantsFailed) void grants.refetch();
        if (targetsFailed) void targets.refetch();
      }}
      footer={footer}
    >
      {rows.length === 0 ? (
        <IdentityPanelEmpty>{empty}</IdentityPanelEmpty>
      ) : (
        rows.map((row) => (
          <IdentityPanelRow
            key={row.key}
            title={row.name}
            detail={
              <ServerOnBehalfOfLine
                agentId={agent.id}
                resourceId={row.resourceId}
              />
            }
            trailing={
              row.calls === undefined
                ? undefined
                : `${row.calls.toLocaleString()} ${row.calls === 1 ? "call" : "calls"}`
            }
          />
        ))
      )}
    </IdentityPanel>
  );
}
