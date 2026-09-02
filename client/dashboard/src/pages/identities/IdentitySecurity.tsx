import { HumanizeDateTime } from "@/lib/dates";
import { useLocation } from "react-router";
import { useOrgRoutes, useRoutes } from "@/routes";
import {
  RULE_CATEGORY_META,
  type RuleCategory,
} from "@/pages/security/policy-data";
import { getRuleTitleFallback } from "@/pages/security/risk-utils";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "./IdentityPanel";
import { identityHandoffs } from "./identityHandoffs";
import { useIdentityOutlet } from "./identityRoute";
import { RankedBarList } from "@/components/chart/RankedBarList";
import { ShareBar } from "@/components/chart/ShareBar";
import { IdentitySection } from "./IdentitySection";
import { sectionMeta } from "./sectionMeta";
import {
  retryFailed,
  useCanReadRisk,
  useIdentityChallenges,
  useIdentityMember,
  useIdentityRisk,
  useIdentityShadowServers,
  useIdentityWindow,
} from "./useIdentityQueries";

const TOP_RULES = 5;

export default function IdentitySecurity(): JSX.Element {
  const canReadRisk = useCanReadRisk();
  const { identity } = useIdentityOutlet();
  const { from, to } = useIdentityWindow();
  const location = useLocation();
  const routes = useRoutes();
  const orgRoutes = useOrgRoutes();
  const { member } = useIdentityMember(identity);
  const handoffs = identityHandoffs(
    identity,
    routes,
    orgRoutes,
    // The same fallback the challenge query uses, so the link filters to the
    // principal the panel counted rather than opening the whole log.
    member?.principalUrn ??
      (identity.userIds[0] ? `user:${identity.userIds[0]}` : undefined),
    new URLSearchParams(location.search),
  );

  const riskQuery = useIdentityRisk(identity, from, to);
  const challengesQuery = useIdentityChallenges(identity, from, to);
  const shadowQuery = useIdentityShadowServers(identity, from, to);

  const categories = riskQuery.data?.categories ?? [];
  const rules = riskQuery.data?.rules ?? [];
  const findings = categories.reduce((sum, c) => sum + Number(c.findings), 0);
  const challenges = challengesQuery.data?.challenges ?? [];
  const denied = challenges.filter((c) => c.outcome === "deny");
  const shadowServers = shadowQuery.data?.servers ?? [];

  // The identifier count is the honest caveat on every count here: risk keys on
  // the ids agents report, and a person can be known by more than one.
  const matchedOn = identity.externalUserIds.length;

  return (
    <IdentitySection
      title="Security"
      meta={sectionMeta(
        canReadRisk
          ? [
              { count: findings, singular: "finding" },
              { count: denied.length, singular: "denied", plural: "denied" },
              { count: shadowServers.length, singular: "shadow server" },
            ]
          : [{ count: denied.length, singular: "denied challenge" }],
      )}
    >
      <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
        <IdentityPanel
          title="Risk findings"
          handoffLabel="Risk Events"
          handoffHref={handoffs.riskEvents}
          loading={riskQuery.isLoading}
          loadingVariant="block"
          error={riskQuery.isError && categories.length === 0}
          refreshFailed={riskQuery.isError && categories.length > 0}
          onRetry={retryFailed(riskQuery)}
          footer={
            matchedOn > 0
              ? `Matched on ${matchedOn} identifier${matchedOn === 1 ? "" : "s"}`
              : "This identity reports no agent identifier, so risk cannot key on it."
          }
        >
          {categories.length === 0 ? (
            <IdentityPanelEmpty>No findings in this window.</IdentityPanelEmpty>
          ) : (
            <div className="px-4 py-4">
              <ShareBar
                segments={categories.map((category) => ({
                  key: category.category,
                  label:
                    RULE_CATEGORY_META[category.category as RuleCategory]
                      ?.label ?? category.category,
                  value: Number(category.findings),
                  valueLabel: Number(category.findings).toLocaleString(),
                }))}
                ariaLabel="Findings by category"
              />
            </div>
          )}
        </IdentityPanel>

        <IdentityPanel
          title="Findings by rule"
          handoffLabel="Risk Events"
          handoffHref={handoffs.riskEvents}
          loading={riskQuery.isLoading}
          loadingVariant="block"
          error={riskQuery.isError && rules.length === 0}
          refreshFailed={riskQuery.isError && rules.length > 0}
          onRetry={retryFailed(riskQuery)}
          footer={
            rules.length > TOP_RULES
              ? `Top ${TOP_RULES} of ${rules.length} rules`
              : undefined
          }
        >
          {rules.length === 0 ? (
            <IdentityPanelEmpty>
              No rule matched this identity in this window.
            </IdentityPanelEmpty>
          ) : (
            <div className="px-4 py-4">
              <RankedBarList
                items={rules.slice(0, TOP_RULES).map((rule, index) => ({
                  key: rule.ruleId || `__none_${index}`,
                  label: rule.ruleId
                    ? getRuleTitleFallback(rule.ruleId)
                    : "(no rule_id)",
                  value: Number(rule.findings),
                }))}
              />
            </div>
          )}
        </IdentityPanel>

        <IdentityPanel
          title="Shadow MCP servers"
          handoffLabel="Shadow MCP"
          handoffHref={handoffs.shadowMcp}
          loading={shadowQuery.isLoading}
          error={shadowQuery.isError && shadowServers.length === 0}
          refreshFailed={shadowQuery.isError && shadowServers.length > 0}
          onRetry={retryFailed(shadowQuery)}
          footer="Servers this person reached in this window, not the project-wide inventory."
        >
          {shadowServers.length === 0 ? (
            <IdentityPanelEmpty>
              This identity reached no unsanctioned server.
            </IdentityPanelEmpty>
          ) : (
            shadowServers.map((server) => (
              <IdentityPanelRow
                key={server.serverSlug}
                accent={server.access === "blocked" ? "destructive" : undefined}
                title={server.serverName || server.urlHost}
                detail={
                  server.lastCalled ? (
                    <HumanizeDateTime date={server.lastCalled} />
                  ) : undefined
                }
                trailing={`${server.requestCount} calls`}
              />
            ))
          )}
        </IdentityPanel>

        <IdentityPanel
          title="Denied authorization challenges"
          handoffLabel="Roles & Permissions"
          handoffHref={handoffs.challenges}
          loading={challengesQuery.isLoading}
          error={challengesQuery.isError && denied.length === 0}
          refreshFailed={challengesQuery.isError && denied.length > 0}
          onRetry={retryFailed(challengesQuery)}
          footer={
            denied.length > 0
              ? `${denied.filter((c) => !c.resolvedAt).length} still unresolved`
              : undefined
          }
        >
          {denied.length === 0 ? (
            <IdentityPanelEmpty>
              No denied checks for this identity.
            </IdentityPanelEmpty>
          ) : (
            denied
              .slice(0, 8)
              .map((challenge) => (
                <IdentityPanelRow
                  key={challenge.id}
                  accent={challenge.resolvedAt ? undefined : "destructive"}
                  title={challenge.scope}
                  detail={<HumanizeDateTime date={challenge.timestamp} />}
                  trailing={challenge.resolvedAt ? "Resolved" : "Denied"}
                />
              ))
          )}
        </IdentityPanel>
      </div>
    </IdentitySection>
  );
}
