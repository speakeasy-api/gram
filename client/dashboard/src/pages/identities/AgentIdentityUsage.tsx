import { StackedTimeBarChart } from "@/components/chart/StackedTimeBarChart";
import { buildToolUsageTimeSeries } from "@/components/observe/toolUsageTimeSeriesChartData";
import { IdentityLink } from "@/components/identity-link";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import { telemetryGetToolUsageTargetTimeSeries } from "@gram/client/funcs/telemetryGetToolUsageTargetTimeSeries";
import { telemetryGetToolUsageTargets } from "@gram/client/funcs/telemetryGetToolUsageTargets";
import { telemetryGetToolUsageTotals } from "@gram/client/funcs/telemetryGetToolUsageTotals";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { telemetryListToolUsageTraces } from "@gram/client/funcs/telemetryListToolUsageTraces";
import type { ListToolUsageTracesPayloadTargetTypes } from "@gram/client/models/components/listtoolusagetracespayload";
import { useCanViewToolLogs } from "@/components/observe/observeDeepLink";
import { telemetryReadRetry } from "@/components/observe/telemetryReadRetry";
import { useUser } from "@/contexts/Auth";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { agentToolUsagePayload } from "./identityLocalTools";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "./IdentityPanel";
import { useIdentityProject, useIdentityWindow } from "./useIdentityQueries";
import { summarizeAgentCredentialUse } from "./agentCredentialUse";

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

  const payload = useMemo(
    () =>
      agentToolUsagePayload({
        agentId: agent.id,
        from,
        to,
        includeLocalTools,
      }),
    [from, to, agent.id, includeLocalTools],
  );
  // Keyed by hand. These endpoints take their subject and window in the body,
  // and the generated key covers only the project and auth headers — so every
  // agent and every range would share one cache entry and the first answer
  // would be kept for all of them. The local tools choice changes the body
  // too, so it is in the key.
  const key = [
    "agent-usage",
    gramProject,
    agent.id,
    from.toISOString(),
    to.toISOString(),
    includeLocalTools ? "with-local-tools" : "without-local-tools",
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
    retry: telemetryReadRetry,
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
    retry: telemetryReadRetry,
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
    retry: telemetryReadRetry,
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
            ? `${calls.toLocaleString()} calls in this period, by source`
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

      <AgentCredentialUsePanel agent={agent} />
    </>
  );
}

/** Calls read for "Acting on behalf of": the most one request returns. */
const CREDENTIAL_TRACE_LIMIT = 1000;

/** The trace target types a Speakeasy server signs in upstream. */
const SERVED_TARGET_TYPES: ListToolUsageTracesPayloadTargetTypes[] = [
  "hosted_mcp_server",
  "tunneled_mcp_server",
  "meta_mcp_server",
];

/**
 * Whom the agent acted for, per MCP server: the person whose connection
 * signed its calls in, or no one when a shared credential did.
 *
 * Read from the credential each call recorded, not from the agent's keys or
 * sessions. The person who issued a key or approved a session let the agent
 * connect; the credential is what the server saw. Not from the agent's
 * configured sign-in permissions either: those are granted to every auth
 * client at once today, so they cannot say which credential a server uses.
 */
function AgentCredentialUsePanel({
  agent,
}: {
  agent: ManagedAgent;
}): JSX.Element {
  const client = useGramContext();
  const viewer = useUser();
  const canViewToolLogs = useCanViewToolLogs();
  const { from, to } = useIdentityWindow();
  const { slug: gramProject } = useIdentityProject();

  const traces = useQuery({
    // Keyed by hand for the same reason as the usage reads above: the body
    // carries the agent and the window, which the generated key leaves out.
    queryKey: [
      "agent-credential-use",
      gramProject,
      agent.id,
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: () =>
      unwrapAsync(
        telemetryListToolUsageTraces(client, {
          gramProject,
          listToolUsageTracesPayload: {
            from,
            to,
            userFilters: [{ kind: "agent_id", key: agent.id }],
            targetTypes: SERVED_TARGET_TYPES,
            limit: CREDENTIAL_TRACE_LIMIT,
            sort: "desc",
          },
        }),
      ),
    enabled: canViewToolLogs,
    retry: telemetryReadRetry,
    throwOnError: false,
  });

  const loaded = traces.data?.traces;
  const rows = useMemo(
    () => summarizeAgentCredentialUse(loaded ?? [], viewer.id),
    [loaded, viewer.id],
  );
  const truncated = Boolean(traces.data?.nextCursor);

  let footer: string | undefined;
  if (rows.length > 0) {
    footer = truncated
      ? `From this agent's latest ${CREDENTIAL_TRACE_LIMIT.toLocaleString()} server calls in this period.`
      : "From the credential each of this agent's server calls signed in with.";
  }

  let body: JSX.Element | JSX.Element[];
  if (!canViewToolLogs) {
    body = (
      <IdentityPanelEmpty>
        You need the log:read permission on this project to see whose
        credentials this agent uses.
      </IdentityPanelEmpty>
    );
  } else if (rows.length === 0) {
    body = (
      <IdentityPanelEmpty>
        This agent called no MCP servers in this period.
      </IdentityPanelEmpty>
    );
  } else {
    body = rows.map((row) => (
      <IdentityPanelRow
        key={row.key}
        title={row.serverLabel}
        detail={
          <CredentialUseDetail
            label={row.credentialLabel}
            personUserId={row.personUserId}
            shared={row.shared}
          />
        }
        trailing={`${row.calls.toLocaleString()} ${row.calls === 1 ? "call" : "calls"}`}
      />
    ));
  }

  return (
    <IdentityPanel
      title="Acting on behalf of"
      loading={canViewToolLogs && traces.isPending}
      error={traces.isError && rows.length === 0}
      refreshFailed={traces.isError && rows.length > 0}
      onRetry={() => void traces.refetch()}
      footer={footer}
    >
      {body}
    </IdentityPanel>
  );
}

function CredentialUseDetail({
  label,
  personUserId,
  shared,
}: {
  label: string | undefined;
  personUserId: string | undefined;
  shared: boolean;
}): JSX.Element {
  if (!label) {
    // Calls recorded before credentials were tracked, or to a server that
    // signs in no one.
    return <>No credential recorded</>;
  }
  if (shared) {
    return <>{label}, acts for no one</>;
  }
  if (!personUserId) {
    return <>{label}</>;
  }
  return (
    <IdentityLink identifier={{ userId: personUserId }}>{label}</IdentityLink>
  );
}
