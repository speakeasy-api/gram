import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
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
  useIdentityMember,
  useIdentityMetrics,
  useIdentityProject,
  useIdentityWindow,
} from "./useIdentityQueries";

export default function IdentityCost(): JSX.Element {
  const { identity } = useIdentityOutlet();
  const { from, to } = useIdentityWindow();
  const project = useIdentityProject();
  // Project routes resolve against the project this page is filtered to: the
  // page is org-level, so the router has no :projectSlug of its own to fill in
  // and every handoff would otherwise resolve to a path with the slug missing.
  const routes = useRoutes({ projectSlug: project.slug });
  const orgRoutes = useOrgRoutes();
  const { member } = useIdentityMember(identity);
  const handoffs = identityHandoffs(
    identity,
    routes,
    orgRoutes,
    member?.principalUrn,
  );

  const metricsQuery = useIdentityMetrics(identity, from, to);
  const metrics = metricsQuery.data?.metrics;
  const models = [...(metrics?.models ?? [])].sort((a, b) => b.count - a.count);
  const totalCalls = models.reduce((sum, model) => sum + model.count, 0);

  return (
    <IdentitySection
      title="Cost"
      meta={`${models.length} model${models.length === 1 ? "" : "s"}`}
    >
      <div className="flex flex-col gap-6">
        <StatTileGroup>
          <StatTile
            title="Spend"
            value={metrics?.totalCost ?? 0}
            format="currency"
            tone="neutral"
            icon="credit-card"
          />
          <StatTile
            title="Input tokens"
            value={metrics?.totalInputTokens ?? 0}
            format="compact"
            tone="neutral"
            icon="arrow-down"
          />
          <StatTile
            title="Output tokens"
            value={metrics?.totalOutputTokens ?? 0}
            format="compact"
            tone="neutral"
            icon="arrow-up"
          />
          <StatTile
            title="Cache reads"
            value={metrics?.cacheReadInputTokens ?? 0}
            format="compact"
            tone="neutral"
            icon="database"
          />
        </StatTileGroup>

        <IdentityPanel
          title="Model mix"
          handoffLabel="Costs"
          handoffHref={handoffs.costs}
          footer={
            // Cost aggregates over every address the subject is known by,
            // which is why this can exceed what one address alone would show.
            identity.emails.length > 1
              ? `Aggregated over ${identity.emails.length} addresses`
              : undefined
          }
        >
          {models.length === 0 ? (
            <IdentityPanelEmpty>
              No model usage in this window.
            </IdentityPanelEmpty>
          ) : (
            models.map((model) => (
              <IdentityPanelRow
                key={model.name}
                title={model.name}
                detail={
                  totalCalls > 0
                    ? `${Math.round((model.count / totalCalls) * 100)}% of calls`
                    : undefined
                }
                trailing={model.count.toLocaleString()}
              />
            ))
          )}
        </IdentityPanel>
      </div>
    </IdentitySection>
  );
}
