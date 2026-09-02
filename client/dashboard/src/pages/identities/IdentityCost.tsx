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
import { peerStanding, standingLabel } from "./identityPeers";
import { IdentitySection } from "./IdentitySection";
import { sectionMeta } from "./sectionMeta";
import {
  hasMetricsSubject,
  retryFailed,
  useIdentityMetrics,
  useIdentityPeers,
  useIdentityWindow,
} from "./useIdentityQueries";

export default function IdentityCost(): JSX.Element {
  const { identity } = useIdentityOutlet();
  const { from, to } = useIdentityWindow();
  const location = useLocation();
  const routes = useRoutes();
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
  const models = [...(metrics?.models ?? [])].sort((a, b) => b.count - a.count);
  // Zero is a finding, and neither of these earned it: an unsupported subject
  // was never asked about, and a failed read did not come back. Both would
  // otherwise render "$0" and "no token usage in this window" — a bill this
  // person did not run up.
  const unsupported = !hasMetricsSubject(identity);
  const metricsUnavailable = unsupported || metricsQuery.isError;
  const tileValue = metricsUnavailable ? "—" : undefined;
  const tileTooltip = unsupported
    ? "This identity carries no identifier the usage endpoint can key on."
    : metricsQuery.isError
      ? "Usage could not be loaded."
      : undefined;

  const { peers } = useIdentityPeers(identity, from, to);
  const spend = metrics?.totalCost ?? 0;
  const spendStanding = peerStanding(
    peers.map((peer) => peer.totalCost ?? 0),
    spend,
  );
  const costStanding = spendStanding
    ? standingLabel(spendStanding, spend)
    : undefined;

  // Cache reads are the lever worth surfacing: they are billed at a fraction
  // of fresh input, so the same token count costs wildly different amounts
  // depending on this split.
  const tokenSegments = [
    { key: "input", label: "Input", value: metrics?.totalInputTokens ?? 0 },
    { key: "output", label: "Output", value: metrics?.totalOutputTokens ?? 0 },
    {
      key: "cacheRead",
      label: "Cache reads",
      value: metrics?.cacheReadInputTokens ?? 0,
    },
    {
      key: "cacheWrite",
      label: "Cache writes",
      value: metrics?.cacheCreationInputTokens ?? 0,
    },
  ].filter((segment) => segment.value > 0);
  const tokenTotal = tokenSegments.reduce((sum, s) => sum + s.value, 0);
  const cacheRead = metrics?.cacheReadInputTokens ?? 0;
  const cacheShareLabel =
    tokenTotal > 0 && cacheRead > 0
      ? `${Math.round((cacheRead / tokenTotal) * 100)}% served from cache`
      : undefined;

  return (
    <IdentitySection
      title="Cost"
      meta={sectionMeta([{ count: models.length, singular: "model" }])}
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
                title="Spend"
                subtext={metricsUnavailable ? undefined : costStanding}
                value={metrics?.totalCost ?? 0}
                displayValue={tileValue}
                tooltip={tileTooltip}
                format="currency"
                tone="neutral"
                icon="credit-card"
              />
              <StatTile
                title="Input tokens"
                value={metrics?.totalInputTokens ?? 0}
                displayValue={tileValue}
                tooltip={tileTooltip}
                format="compact"
                tone="neutral"
                icon="arrow-down-to-line"
              />
              <StatTile
                title="Output tokens"
                value={metrics?.totalOutputTokens ?? 0}
                displayValue={tileValue}
                tooltip={tileTooltip}
                format="compact"
                tone="neutral"
                icon="arrow-up-from-line"
              />
              <StatTile
                title="Cache reads"
                value={metrics?.cacheReadInputTokens ?? 0}
                displayValue={tileValue}
                tooltip={tileTooltip}
                format="compact"
                tone="neutral"
                icon="database"
              />
            </>
          )}
        </StatTileGroup>

        {/* Two compositions rather than two lists. Where the tokens went is
            the only question this tab can answer that Usage cannot: a person
            whose spend is nearly all cache reads is cheap for the same
            apparent volume as one whose spend is fresh input. */}
        <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
          <IdentityPanel
            title="Where the tokens went"
            handoffLabel="Costs"
            handoffHref={handoffs.costs}
            loading={metricsQuery.isLoading}
            loadingVariant="block"
            error={metricsQuery.isError && tokenSegments.length === 0}
            refreshFailed={metricsQuery.isError && tokenSegments.length > 0}
            onRetry={retryFailed(metricsQuery)}
            footer={cacheShareLabel}
          >
            {tokenSegments.length === 0 ? (
              <IdentityPanelEmpty>
                {unsupported
                  ? "Token usage is not recorded for this kind of identity."
                  : "No token usage in this window."}
              </IdentityPanelEmpty>
            ) : (
              <div className="px-4 py-4">
                <ShareBar
                  segments={tokenSegments}
                  ariaLabel="Token usage by kind"
                />
              </div>
            )}
          </IdentityPanel>

          <IdentityPanel
            title="Model mix"
            handoffLabel="Costs"
            handoffHref={handoffs.costs}
            loading={metricsQuery.isLoading}
            loadingVariant="block"
            error={metricsQuery.isError && models.length === 0}
            refreshFailed={metricsQuery.isError && models.length > 0}
            onRetry={retryFailed(metricsQuery)}
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
                {unsupported
                  ? "Model usage is not recorded for this kind of identity."
                  : "No model usage in this window."}
              </IdentityPanelEmpty>
            ) : (
              <div className="px-4 py-4">
                <ShareBar
                  segments={models.map((model) => ({
                    key: model.name,
                    label: model.name,
                    value: model.count,
                  }))}
                  ariaLabel="Share of calls by model"
                />
              </div>
            )}
          </IdentityPanel>
        </div>
      </div>
    </IdentitySection>
  );
}
