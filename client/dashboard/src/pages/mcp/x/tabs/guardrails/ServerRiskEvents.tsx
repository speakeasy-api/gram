import { Text } from "@/components/ui/Text";
import { useSdkClient } from "@/contexts/Sdk";
import { enforcementOutcomeLabel } from "@/pages/security/risk-outcome";
import {
  CategoryLabel,
  RuleLabel,
  SeverityBadge,
} from "@/pages/security/risk-ui";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";

const RECENT_EVENTS = 25;

/** The most recent risk events raised on one MCP server. The full list, with
 *  filters and actions, is Risk Events pre-filtered to the server. */
export function ServerRiskEvents({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element {
  const client = useSdkClient();
  const routes = useRoutes();
  const eventsQuery = useQuery({
    queryKey: ["risk", "results", "mcp-server", mcpServer.id],
    queryFn: () =>
      client.risk.results.list({
        mcpServerId: mcpServer.id,
        limit: RECENT_EVENTS,
      }),
  });
  const policiesQuery = useRiskListPolicies(undefined, undefined, {
    throwOnError: false,
  });
  const scoreByPolicy = new Map(
    (policiesQuery.data?.policies ?? []).map((policy) => [
      policy.id,
      { name: policy.name, score: policy.score },
    ]),
  );
  const events = eventsQuery.data?.results ?? [];

  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between gap-3">
        <Text small muted>
          Findings on tool calls through this server.
        </Text>
        <Link
          to={`${routes.riskEvents.href()}?mcp_server_id=${encodeURIComponent(mcpServer.id)}`}
          className="text-sm underline underline-offset-2"
        >
          Open in Risk Events
        </Link>
      </div>
      {eventsQuery.isLoading ? (
        <Text small muted>
          Loading risk events…
        </Text>
      ) : eventsQuery.isError ? (
        <Text small className="text-destructive">
          {eventsQuery.error.message}
        </Text>
      ) : events.length === 0 ? (
        <div className="border-border border border-dashed p-6 text-center">
          <Text small muted>
            No risk events on this server yet.
          </Text>
        </div>
      ) : (
        <div className="border-border divide-border divide-y border">
          {events.map((event) => {
            const policy = scoreByPolicy.get(event.policyId);
            const outcome = enforcementOutcomeLabel(event.enforcementOutcome);
            return (
              <div
                key={event.id}
                className="flex flex-wrap items-center gap-x-6 gap-y-1 px-4 py-3"
              >
                <SeverityBadge score={policy?.score} />
                <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <CategoryLabel source={event.source} ruleId={event.ruleId} />
                  <RuleLabel source={event.source} ruleId={event.ruleId} />
                </div>
                <span className="text-muted-foreground font-mono text-xs">
                  {event.toolName ?? "—"}
                </span>
                <span className="text-muted-foreground font-mono text-xs">
                  {policy?.name ?? "—"}
                </span>
                {outcome ? (
                  <span className="text-muted-foreground font-mono text-xs">
                    {outcome}
                  </span>
                ) : null}
                <span className="text-muted-foreground font-mono text-xs">
                  {new Date(event.createdAt).toLocaleString()}
                </span>
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}
