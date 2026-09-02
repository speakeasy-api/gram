import {
  StatTile,
  StatTileGroup,
  StatTileSkeleton,
} from "@/components/chart/stat-tile";
import { useLocation, useSearchParams } from "react-router";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { useOrgRoutes, useRoutes } from "@/routes";
import { IdentityPanel, IdentityPanelEmpty } from "./IdentityPanel";
import { identityHandoffs } from "./identityHandoffs";
import { useIdentityOutlet } from "./identityRoute";
import { ShareBar } from "@/components/chart/ShareBar";
import { SplitRankedBarList } from "@/components/chart/SplitRankedBarList";
import { IdentitySection } from "./IdentitySection";
import { sectionMeta } from "./sectionMeta";
import {
  hasMetricsSubject,
  retryFailed,
  useIdentityMetrics,
  useIdentityPeers,
  useIdentityWindow,
} from "./useIdentityQueries";

// Enough to show the shape of someone's usage; the handoff owns the long tail.
const TOP_TOOLS = 5;

/**
 * Which class of AI account this tab is reading through.
 *
 * Someone signed into both a company account and their own subscription is two
 * usage stories under one name, and the interesting question — how much of
 * this work went through a subscription we do not govern — is unanswerable
 * while the two are summed. Every read on this tab takes the scope, so the
 * tiles, the agent split and the tool ranking all move together.
 *
 * Kept in the URL so a link to a scoped view arrives scoped. "" is every
 * account, and is the only value that shares its reads with the other tabs.
 */
const ACCOUNT_SCOPE_PARAM = "account";
type AccountScope = "" | "team" | "personal";

function toAccountScope(value: string | null): AccountScope {
  return value === "team" || value === "personal" ? value : "";
}

export default function IdentityUsage(): JSX.Element {
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

  const [searchParams, setSearchParams] = useSearchParams();
  const scope = toAccountScope(searchParams.get(ACCOUNT_SCOPE_PARAM));
  const setScope = (next: AccountScope) => {
    setSearchParams(
      (params) => {
        if (next) params.set(ACCOUNT_SCOPE_PARAM, next);
        else params.delete(ACCOUNT_SCOPE_PARAM);
        return params;
      },
      { replace: true },
    );
  };

  const metricsQuery = useIdentityMetrics(identity, from, to, scope);
  const metrics = metricsQuery.data?.metrics;
  const tools = [...(metrics?.tools ?? [])].sort((a, b) => b.count - a.count);
  const models = [...(metrics?.models ?? [])].sort((a, b) => b.count - a.count);
  // Zero tool calls is a finding about how someone works, and neither of these
  // earned it: an unsupported subject was never asked about, and a failed read
  // did not come back.
  const unsupported = !hasMetricsSubject(identity);
  const metricsUnavailable = unsupported || metricsQuery.isError;
  const tileValue = metricsUnavailable ? "—" : undefined;
  const tileTooltip = unsupported
    ? "This identity carries no identifier the usage endpoint can key on."
    : metricsQuery.isError
      ? "Usage could not be loaded."
      : undefined;

  // hookSources rides on the roster row, not the per-user summary.
  const {
    self,
    isPending: peersPending,
    isError: peersFailed,
    refetch: refetchPeers,
  } = useIdentityPeers(identity, from, to, scope);
  const retryPeers = retryFailed({
    isError: peersFailed,
    refetch: refetchPeers,
  });
  // Which scopes exist has to be read unscoped, or picking "personal" would
  // narrow the roster row the filter itself is built from and the control
  // would take away its own options. Unscoped, this is the same read every
  // other tab makes, so it costs nothing while the filter is off.
  const { self: unscopedSelf } = useIdentityPeers(identity, from, to);
  // Only offered to someone who actually works through more than one class of
  // account: with a single class the filter can only ever return everything
  // or nothing.
  const showScope = (unscopedSelf?.accountTypes ?? []).length > 1;
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
      action={
        showScope ? (
          <SegmentedControl<AccountScope>
            value={scope}
            onChange={setScope}
            options={[
              { value: "", label: "All" },
              {
                value: "team",
                label: "Team",
                tooltip: "Usage through company AI accounts.",
              },
              {
                value: "personal",
                label: "Personal",
                tooltip:
                  "Usage through personal AI subscriptions this organization does not govern.",
              },
            ]}
          />
        ) : undefined
      }
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
                displayValue={tileValue}
                tooltip={tileTooltip}
                format="compact"
                tone="information"
                icon="wrench"
              />
              <StatTile
                title="Failed calls"
                value={metrics?.toolCallFailure ?? 0}
                displayValue={tileValue}
                tooltip={tileTooltip}
                format="compact"
                tone={
                  !metricsUnavailable && (metrics?.toolCallFailure ?? 0) > 0
                    ? "warning"
                    : "neutral"
                }
                icon="triangle-alert"
              />
              <StatTile
                title="Chat requests"
                value={metrics?.totalChatRequests ?? 0}
                displayValue={tileValue}
                tooltip={tileTooltip}
                format="compact"
                tone="information"
                icon="message-square"
              />
              <StatTile
                title="Tokens"
                value={metrics?.totalTokens ?? 0}
                displayValue={tileValue}
                tooltip={tileTooltip}
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
        {(peersPending || peersFailed || agents.length > 0) && (
          <IdentityPanel
            title="Agents"
            contentClassName="px-4 py-4"
            loading={peersPending}
            loadingVariant="block"
            error={peersFailed && agents.length === 0}
            refreshFailed={peersFailed && agents.length > 0}
            onRetry={retryPeers}
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
          error={metricsQuery.isError && tools.length === 0}
          refreshFailed={metricsQuery.isError && tools.length > 0}
          onRetry={retryFailed(metricsQuery)}
          footer={
            tools.length > TOP_TOOLS
              ? `Top ${TOP_TOOLS} of ${tools.length} tools by call volume`
              : undefined
          }
        >
          {tools.length === 0 ? (
            <IdentityPanelEmpty>
              {unsupported
                ? "Tool calls are not recorded for this kind of identity."
                : scope
                  ? `No tool calls through ${scope} accounts in this window.`
                  : "No tool calls in this window."}
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
