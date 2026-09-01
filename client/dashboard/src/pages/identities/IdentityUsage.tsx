import {
  StatTile,
  StatTileGroup,
  StatTileSkeleton,
} from "@/components/chart/stat-tile";
import { useLocation } from "react-router";
import { useOrgRoutes, useRoutes } from "@/routes";
import { IdentityPanel, IdentityPanelEmpty } from "./IdentityPanel";
import { identityHandoffs } from "./identityHandoffs";
import { useIdentityOutlet } from "./identityRoute";
import { ShareBar } from "@/components/chart/ShareBar";
import { SplitRankedBarList } from "@/components/chart/SplitRankedBarList";
import { IdentitySection } from "./IdentitySection";
import { sectionMeta } from "./sectionMeta";
import {
  useIdentityMetrics,
  useIdentityPeers,
  useIdentityProject,
  useIdentityWindow,
} from "./useIdentityQueries";

// Enough to show the shape of someone's usage; the handoff owns the long tail.
const TOP_TOOLS = 5;

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

  // hookSources rides on the roster row, not the per-user summary.
  const { self, isPending: peersPending } = useIdentityPeers(
    identity,
    from,
    to,
  );
  const agents = [...(self?.hookSources ?? [])]
    .filter((source) => source.source && source.eventCount > 0)
    .sort((a, b) => b.eventCount - a.eventCount);

  return (
    <IdentitySection
      title="Usage"
      meta={sectionMeta([
        { count: tools.length, singular: "tool" },
        { count: models.length, singular: "model" },
        { count: agents.length, singular: "agent" },
      ])}
    >
      <div className="flex flex-col gap-6">
        <StatTileGroup className="overflow-x-auto [&>*]:min-w-[11.5rem]">
          {metricsQuery.isLoading ? (
            <>
              <StatTileSkeleton />
              <StatTileSkeleton />
              <StatTileSkeleton />
              <StatTileSkeleton />
            </>
          ) : (
            <>
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
                tone={
                  (metrics?.toolCallFailure ?? 0) > 0 ? "warning" : "neutral"
                }
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
            </>
          )}
        </StatTileGroup>

        {/* Which surfaces the work came through. Two people with identical
            tool counts can be running one CLI or juggling four, and nothing
            else on the page says which — the per-user summary does not carry
            it, only the roster row does. */}
        {(peersPending || agents.length > 0) && (
          <IdentityPanel
            title="Agents"
            contentClassName="px-4 py-4"
            loading={peersPending}
            loadingVariant="block"
          >
            <ShareBar
              segments={agents.map((agent) => ({
                key: agent.source,
                label: agent.source,
                value: agent.eventCount,
              }))}
              ariaLabel="Share of activity by agent surface"
            />
          </IdentityPanel>
        )}

        {/* Tools carry their failure share on the same bar: volume alone is
            the boring half of the question, and a heavily-used tool that
            fails often should not be able to hide behind its rank. Model
            usage lives on the Cost tab, the only place it reads against
            money rather than repeating this list. */}
        <IdentityPanel
          title="Tools"
          handoffLabel="Tool Logs"
          handoffHref={handoffs.toolLogs}
          loading={metricsQuery.isLoading}
          loadingVariant="block"
          footer={
            tools.length > TOP_TOOLS
              ? `Top ${TOP_TOOLS} of ${tools.length} tools by call volume`
              : undefined
          }
        >
          {tools.length === 0 ? (
            <IdentityPanelEmpty>
              No tool calls in this window.
            </IdentityPanelEmpty>
          ) : (
            <div className="px-4 py-4">
              <SplitRankedBarList
                items={tools.slice(0, TOP_TOOLS).map((tool) => ({
                  key: tool.urn,
                  label: tool.urn,
                  value: tool.count,
                  failed: tool.failureCount,
                }))}
              />
            </div>
          )}
        </IdentityPanel>
      </div>
    </IdentitySection>
  );
}
