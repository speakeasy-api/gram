import { HumanizeDateTime } from "@/lib/dates";
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
import { useIdentityOutlet } from "./identityRoute";
import { IdentitySection } from "./IdentitySection";
import {
  useIdentityProject,
  useIdentityChallenges,
  useIdentityRisk,
  useIdentityShadowServers,
  useIdentityWindow,
} from "./useIdentityQueries";

export default function IdentitySecurity(): JSX.Element {
  const { identity } = useIdentityOutlet();
  const { from, to } = useIdentityWindow();
  const project = useIdentityProject();
  // Project routes resolve against the project this page is filtered to: the
  // page is org-level, so the router has no :projectSlug of its own to fill in
  // and every handoff would otherwise resolve to a path with the slug missing.
  const routes = useRoutes({ projectSlug: project.slug });
  const orgRoutes = useOrgRoutes();

  const riskQuery = useIdentityRisk(identity, from, to);
  const challengesQuery = useIdentityChallenges(identity);
  const shadowQuery = useIdentityShadowServers(identity);

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
      meta={`${findings} finding${findings === 1 ? "" : "s"} · ${
        denied.length
      } denied · ${shadowServers.length} shadow server${
        shadowServers.length === 1 ? "" : "s"
      }`}
    >
      <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
        <IdentityPanel
          title="Risk findings"
          handoffLabel="Risk Events"
          handoffHref={routes.riskEvents.href()}
          footer={
            matchedOn > 0
              ? `Matched on ${matchedOn} identifier${matchedOn === 1 ? "" : "s"}`
              : "This identity reports no agent identifier, so risk cannot key on it."
          }
        >
          {categories.length === 0 ? (
            <IdentityPanelEmpty>No findings in this window.</IdentityPanelEmpty>
          ) : (
            categories.map((category) => (
              <IdentityPanelRow
                key={category.category}
                accent="destructive"
                title={
                  RULE_CATEGORY_META[category.category as RuleCategory]
                    ?.label ?? category.category
                }
                trailing={Number(category.findings).toLocaleString()}
              />
            ))
          )}
        </IdentityPanel>

        <IdentityPanel
          title="Findings by rule"
          handoffLabel="Risk Events"
          handoffHref={routes.riskEvents.href()}
        >
          {rules.length === 0 ? (
            <IdentityPanelEmpty>
              No rule matched this identity in this window.
            </IdentityPanelEmpty>
          ) : (
            rules
              .slice(0, 8)
              .map((rule, index) => (
                <IdentityPanelRow
                  key={rule.ruleId || `__none_${index}`}
                  title={
                    rule.ruleId
                      ? getRuleTitleFallback(rule.ruleId)
                      : "(no rule_id)"
                  }
                  detail={rule.source}
                  trailing={Number(rule.findings).toLocaleString()}
                />
              ))
          )}
        </IdentityPanel>

        <IdentityPanel
          title="Shadow MCP servers"
          handoffLabel="Shadow MCP"
          handoffHref={routes.shadowMCP.href()}
          footer="Servers this person reached, not the project-wide inventory."
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
          handoffHref={orgRoutes.access.href()}
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
