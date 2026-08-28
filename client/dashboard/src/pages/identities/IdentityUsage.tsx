import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
import { useLocation } from "react-router";
import { useOrgRoutes, useRoutes } from "@/routes";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "./IdentityPanel";
import { identityHandoffs } from "./identityHandoffs";
import { useIdentityOutlet } from "./identityRoute";
import { IdentitySection } from "./IdentitySection";
import {
  useIdentityMetrics,
  useIdentityProject,
  useIdentityWindow,
} from "./useIdentityQueries";

export default function IdentityUsage(): JSX.Element {
  const { identity } = useIdentityOutlet();
  const { from, to } = useIdentityWindow();
  const location = useLocation();
  const project = useIdentityProject();
  // Project routes resolve against the project this page is filtered to: the
  // page is org-level, so the router has no :projectSlug of its own to fill in
  // and every handoff would otherwise resolve to a path with the slug missing.
  const routes = useRoutes({ projectSlug: project.slug });
  const orgRoutes = useOrgRoutes();
  // No handoff on this page filters by principal, so the member list this
  // would otherwise fetch is not worth the request.
  const handoffs = identityHandoffs(
    identity,
    routes,
    orgRoutes,
    undefined,
    new URLSearchParams(location.search),
  );

  const metricsQuery = useIdentityMetrics(identity, from, to);
  const metrics = metricsQuery.data?.metrics;
  const tools = [...(metrics?.tools ?? [])].sort((a, b) => b.count - a.count);
  const models = [...(metrics?.models ?? [])].sort((a, b) => b.count - a.count);

  return (
    <IdentitySection
      title="Usage"
      meta={`${tools.length} tool${tools.length === 1 ? "" : "s"} · ${models.length} model${models.length === 1 ? "" : "s"}`}
    >
      <div className="flex flex-col gap-6">
        <StatTileGroup>
          <StatTile
            title="Tool calls"
            value={metrics?.totalToolCalls ?? 0}
            format="compact"
            tone="information"
            icon="wrench"
          />
          <StatTile
            title="Failed calls"
            value={metrics?.toolCallFailure ?? 0}
            format="compact"
            tone={(metrics?.toolCallFailure ?? 0) > 0 ? "warning" : "neutral"}
            icon="triangle-alert"
          />
          <StatTile
            title="Chat requests"
            value={metrics?.totalChatRequests ?? 0}
            format="compact"
            tone="information"
            icon="message-square"
          />
          <StatTile
            title="Tokens"
            value={metrics?.totalTokens ?? 0}
            format="compact"
            tone="neutral"
            icon="hash"
          />
        </StatTileGroup>

        <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
          <IdentityPanel
            title="Top tools"
            handoffLabel="Tool Logs"
            handoffHref={handoffs.toolLogs}
            footer={
              tools.length > 8 ? `Top 8 of ${tools.length} tools` : undefined
            }
          >
            {tools.length === 0 ? (
              <IdentityPanelEmpty>
                No tool calls in this window.
              </IdentityPanelEmpty>
            ) : (
              tools
                .slice(0, 8)
                .map((tool) => (
                  <IdentityPanelRow
                    key={tool.urn}
                    title={tool.urn}
                    detail={
                      tool.failureCount > 0
                        ? `${tool.failureCount} failed`
                        : undefined
                    }
                    trailing={tool.count.toLocaleString()}
                  />
                ))
            )}
          </IdentityPanel>

          <IdentityPanel
            title="Models"
            handoffLabel="Costs"
            handoffHref={handoffs.costs}
          >
            {models.length === 0 ? (
              <IdentityPanelEmpty>
                No model usage in this window.
              </IdentityPanelEmpty>
            ) : (
              models
                .slice(0, 8)
                .map((model) => (
                  <IdentityPanelRow
                    key={model.name}
                    title={model.name}
                    trailing={model.count.toLocaleString()}
                  />
                ))
            )}
          </IdentityPanel>
        </div>
      </div>
    </IdentitySection>
  );
}
