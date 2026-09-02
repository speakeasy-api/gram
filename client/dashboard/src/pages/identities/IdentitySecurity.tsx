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
import { shadowMCPAccessSummaryOf } from "@/components/shadow-mcp/shadowMCPInventoryStatus";
import {
  useCanReadRisk,
  useIdentityChallenges,
  useIdentityPrincipalUrn,
  useIdentityRisk,
  useIdentityShadowServers,
  useIdentityWindow,
} from "./useIdentityQueries";

const TOP_RULES = 5;
const DENIED_ROWS = 8;

/**
 * Risk and the shadow inventory are org:admin surfaces and their queries are
 * held back without it. Said outright, because the alternative rendering — the
 * panel's own empty state — reads as "we looked and there is nothing".
 */
const RISK_UNAVAILABLE =
  "Risk and shadow MCP findings need the org:admin permission.";

export default function IdentitySecurity(): JSX.Element {
  const canReadRisk = useCanReadRisk();
  const { identity } = useIdentityOutlet();
  const { from, to } = useIdentityWindow();
  const location = useLocation();
  const routes = useRoutes();
  const orgRoutes = useOrgRoutes();
  // The same principal the challenge query uses, so the link filters to the
  // principal the panel counted rather than opening the whole log.
  const principalUrn = useIdentityPrincipalUrn(identity);
  const handoffs = identityHandoffs(
    identity,
    routes,
    orgRoutes,
    principalUrn,
    new URLSearchParams(location.search),
  );

  const riskQuery = useIdentityRisk(identity, from, to);
  // Asked for as a denied-only slice so the count below is the API's total for
  // exactly what the panel claims, rather than however many denials happened
  // to fall inside one capped page of mixed outcomes.
  const deniedQuery = useIdentityChallenges(identity, from, to, {
    outcome: "deny",
  });
  const unresolvedQuery = useIdentityChallenges(identity, from, to, {
    outcome: "deny",
    resolved: false,
    limit: 1,
  });
  const shadowQuery = useIdentityShadowServers(identity, from, to);

  const categories = riskQuery.data?.categories ?? [];
  const rules = riskQuery.data?.rules ?? [];
  const findings = categories.reduce((sum, c) => sum + Number(c.findings), 0);
  const denied = deniedQuery.data?.challenges ?? [];
  const deniedTotal = deniedQuery.data?.total ?? 0;
  const unresolvedTotal = unresolvedQuery.data?.total;
  const shadowServers = shadowQuery.data?.servers ?? [];

  // Risk keys on the ids agents report and the endpoint takes one of them, so
  // this names the identifier the panel actually asked about.
  const riskIdentifier = identity.externalUserIds[0];
  const unqueriedIdentifiers = Math.max(identity.externalUserIds.length - 1, 0);

  return (
    <IdentitySection
      title="Security"
      meta={sectionMeta(
        canReadRisk
          ? [
              { count: findings, singular: "finding" },
              { count: deniedTotal, singular: "denied", plural: "denied" },
              { count: shadowServers.length, singular: "shadow server" },
            ]
          : [{ count: deniedTotal, singular: "denied challenge" }],
      )}
    >
      <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
        <IdentityPanel
          title="Risk findings"
          handoffLabel="Risk Events"
          handoffHref={handoffs.riskEvents}
          loading={riskQuery.isLoading}
          loadingVariant="block"
          error={riskQuery.isError}
          onRetry={() => void riskQuery.refetch()}
          footer={
            !canReadRisk
              ? undefined
              : !riskIdentifier
                ? "This identity reports no agent identifier, so risk cannot key on it."
                : unqueriedIdentifiers > 0
                  ? `Matched on ${riskIdentifier}. This identity reports ${unqueriedIdentifiers} further identifier${
                      unqueriedIdentifiers === 1 ? "" : "s"
                    }, which are not counted here.`
                  : `Matched on ${riskIdentifier}`
          }
        >
          {!canReadRisk ? (
            <IdentityPanelEmpty>{RISK_UNAVAILABLE}</IdentityPanelEmpty>
          ) : categories.length === 0 ? (
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
          error={riskQuery.isError}
          onRetry={() => void riskQuery.refetch()}
          footer={
            canReadRisk && rules.length > TOP_RULES
              ? `Top ${TOP_RULES} of ${rules.length} rules`
              : undefined
          }
        >
          {!canReadRisk ? (
            <IdentityPanelEmpty>{RISK_UNAVAILABLE}</IdentityPanelEmpty>
          ) : rules.length === 0 ? (
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
          error={shadowQuery.isError}
          onRetry={() => void shadowQuery.refetch()}
          footer={
            canReadRisk
              ? "Servers this person reached in this window, not the project-wide inventory."
              : undefined
          }
        >
          {!canReadRisk ? (
            <IdentityPanelEmpty>{RISK_UNAVAILABLE}</IdentityPanelEmpty>
          ) : shadowServers.length === 0 ? (
            <IdentityPanelEmpty>
              This identity reached no unsanctioned server.
            </IdentityPanelEmpty>
          ) : (
            shadowServers.map((server) => (
              <IdentityPanelRow
                key={server.serverSlug}
                // The canonical verdict, not the deprecated `access` string it
                // can disagree with.
                accent={
                  shadowMCPAccessSummaryOf(server).state === "blocked"
                    ? "destructive"
                    : undefined
                }
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
          loading={deniedQuery.isLoading}
          error={deniedQuery.isError}
          onRetry={() => void deniedQuery.refetch()}
          footer={
            deniedTotal > 0 && unresolvedTotal !== undefined
              ? `${unresolvedTotal.toLocaleString()} of ${deniedTotal.toLocaleString()} still unresolved`
              : undefined
          }
        >
          {denied.length === 0 ? (
            <IdentityPanelEmpty>
              No denied checks for this identity.
            </IdentityPanelEmpty>
          ) : (
            denied
              .slice(0, DENIED_ROWS)
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
