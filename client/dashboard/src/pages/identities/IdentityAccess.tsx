import { Badge } from "@/components/ui/Badge";
import { HumanizeDateTime } from "@/lib/dates";
import { useLocation } from "react-router";
import { useOrgRoutes, useRoutes } from "@/routes";
import { useRoles } from "@gram/client/react-query/roles.js";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "./IdentityPanel";
import { identityHandoffs } from "./identityHandoffs";
import { useIdentityOutlet } from "./identityRoute";
import { ShareBar } from "@/components/chart/ShareBar";
import { IdentitySection } from "./IdentitySection";
import { sectionMeta } from "./sectionMeta";
import {
  useIdentityChallenges,
  useIdentityMember,
  useIdentityPrincipalUrn,
  useIdentityWindow,
} from "./useIdentityQueries";

const RECENT_CHALLENGES = 5;

/**
 * Whether a scope slug is an exception rather than a permission.
 *
 * A `*:blocked_*` grant subtracts an action from whatever a role otherwise
 * confers, and the risk-policy bypass and block grants target a policy's
 * enforcement rather than something this person may do. Listed among the
 * permissions they read as extra reach, which is the opposite of what they are.
 */
function isExclusionScope(slug: string): boolean {
  return (
    slug.includes(":blocked_") ||
    slug === "risk_policy:bypass" ||
    slug === "risk_policy:block"
  );
}

export default function IdentityAccess(): JSX.Element {
  const location = useLocation();
  const routes = useRoutes();
  const { identity } = useIdentityOutlet();
  const { from, to } = useIdentityWindow();
  const orgRoutes = useOrgRoutes();
  const { member, query: membersQuery } = useIdentityMember(identity);
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
  const rolesQuery = useRoles(undefined, undefined, { throwOnError: false });
  const challengesQuery = useIdentityChallenges(identity, from, to);
  // The rows above are one capped page, so the outcome split is asked for as
  // its own denied-only slice and read off the API's total.
  const deniedQuery = useIdentityChallenges(identity, from, to, {
    outcome: "deny",
    limit: 1,
  });
  // Roles are the join of the member row and the role catalogue, so both have
  // to land before the panels can say anything true about them — and if either
  // read fails, "no roles assigned in this organization" is a claim about
  // someone's access made from data we never received.
  const rolesLoading = rolesQuery.isLoading || membersQuery.isLoading;
  const rolesFailed = rolesQuery.isError || membersQuery.isError;
  const retryRoles = () => {
    void rolesQuery.refetch();
    void membersQuery.refetch();
  };

  const rolesById = new Map(
    (rolesQuery.data?.roles ?? []).map((role) => [role.id, role]),
  );
  const roles = (member?.roleIds ?? []).map(
    (id) => rolesById.get(id) ?? { id, name: id, slug: id, description: "" },
  );
  // A scope slug is "<family>:<action>" (org:read, chat:read). Grouping by
  // family turns a flat list of twenty slugs into the handful of areas this
  // person has any reach over, which is the shape the question is asked in.
  //
  // A scope's actions are held with whether every grant of it was
  // unrestricted: a grant carrying selectors reaches only the resources those
  // name, and printing it beside an unrestricted one claims a reach this
  // person does not have.
  const scopeFamilies = new Map<string, Map<string, boolean>>();
  const exclusions = new Set<string>();
  for (const role of roles) {
    for (const grant of ("grants" in role ? role.grants : []) ?? []) {
      const slug = String(grant.scope);
      if (isExclusionScope(slug)) {
        exclusions.add(slug);
        continue;
      }
      const [family = slug, action = ""] = slug.split(":");
      const actions = scopeFamilies.get(family) ?? new Map<string, boolean>();
      scopeFamilies.set(family, actions);
      const key = action || slug;
      const restricted = (grant.selectors?.length ?? 0) > 0;
      actions.set(key, (actions.get(key) ?? true) && restricted);
    }
  }
  const permissions = [...scopeFamilies.entries()]
    .map(([family, actions]) => ({
      family,
      actions: [...actions.entries()]
        .map(([action, restricted]) => ({ action, restricted }))
        .sort((a, b) => a.action.localeCompare(b.action)),
    }))
    .sort((a, b) => a.family.localeCompare(b.family));
  const scopeCount = permissions.reduce(
    (sum, group) => sum + group.actions.length,
    0,
  );
  const anyRestricted = permissions.some((group) =>
    group.actions.some((action) => action.restricted),
  );

  const challenges = challengesQuery.data?.challenges ?? [];
  const challengeTotal = challengesQuery.data?.total ?? 0;
  const deniedTotal = deniedQuery.data?.total ?? 0;

  return (
    <IdentitySection
      title="Access"
      meta={sectionMeta([
        { count: roles.length, singular: "role" },
        { count: scopeCount, singular: "permission" },
        { count: deniedTotal, singular: "denied", plural: "denied" },
      ])}
    >
      <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
        <IdentityPanel
          title="Roles"
          handoffLabel="Roles & Permissions"
          handoffHref={handoffs.roles}
          loading={rolesLoading}
          loadingVariant="block"
          error={rolesFailed}
          onRetry={retryRoles}
          footer={
            member ? undefined : "No org member row resolves to this identity."
          }
        >
          {roles.length === 0 ? (
            <IdentityPanelEmpty>
              No roles assigned in this organization.
            </IdentityPanelEmpty>
          ) : (
            // A row each, laid out like the permissions beside it: the badge
            // holds the left column and what the role is for runs alongside
            // it. As badges alone they were three words in a corner of an
            // otherwise empty card, and said nothing about what holding one
            // means.
            roles.map((role) => (
              <div
                key={role.id}
                className="border-border flex items-baseline gap-3 border-b px-4 py-3 last:border-b-0"
              >
                <span className="w-28 shrink-0">
                  <Badge variant="neutral" title={role.slug}>
                    {role.name}
                  </Badge>
                </span>
                <span className="text-muted-foreground min-w-0 flex-1 text-xs">
                  {role.description || role.slug}
                </span>
              </div>
            ))
          )}
        </IdentityPanel>

        <IdentityPanel
          title="What this identity can do"
          handoffLabel="Roles & Permissions"
          handoffHref={handoffs.roles}
          loading={rolesLoading}
          error={rolesFailed}
          onRetry={retryRoles}
          footer={
            permissions.length > 0 || exclusions.size > 0
              ? [
                  anyRestricted
                    ? "Granted by the roles above; an action marked * reaches only the resources its grant names."
                    : "Granted by the roles above, across every project.",
                  exclusions.size > 0
                    ? `${exclusions.size} exception grant${
                        exclusions.size === 1 ? "" : "s"
                      } listed below remove access rather than confer it.`
                    : "",
                ]
                  .filter(Boolean)
                  .join(" ")
              : undefined
          }
        >
          {permissions.length === 0 && exclusions.size === 0 ? (
            <IdentityPanelEmpty>
              These roles carry no permissions.
            </IdentityPanelEmpty>
          ) : (
            <>
              {permissions.map((group) => (
                <div
                  key={group.family}
                  className="border-border flex items-baseline gap-3 border-b px-4 py-3 last:border-b-0"
                >
                  <span className="w-28 shrink-0 truncate font-mono text-xs">
                    {group.family}
                  </span>
                  <span className="text-muted-foreground min-w-0 flex-1 font-mono text-xs">
                    {group.actions
                      .map(
                        ({ action, restricted }) =>
                          `${action}${restricted ? "*" : ""}`,
                      )
                      .join("  ")}
                  </span>
                </div>
              ))}
              {exclusions.size > 0 && (
                <div className="border-border flex items-baseline gap-3 border-b px-4 py-3 last:border-b-0">
                  <span className="w-28 shrink-0 truncate font-mono text-xs">
                    excluded
                  </span>
                  <span className="text-muted-foreground min-w-0 flex-1 font-mono text-xs">
                    {[...exclusions].sort().join("  ")}
                  </span>
                </div>
              )}
            </>
          )}
        </IdentityPanel>

        <IdentityPanel
          title="Authorization challenges"
          handoffLabel="Roles & Permissions"
          handoffHref={handoffs.challenges}
          loading={challengesQuery.isLoading}
          loadingRows={RECENT_CHALLENGES}
          error={challengesQuery.isError}
          onRetry={() => void challengesQuery.refetch()}
          footer={
            challengeTotal > 0
              ? `${deniedTotal.toLocaleString()} denied of ${challengeTotal.toLocaleString()} recorded`
              : undefined
          }
        >
          {challenges.length === 0 ? (
            <IdentityPanelEmpty>
              No authorization checks recorded for this identity.
            </IdentityPanelEmpty>
          ) : (
            <>
              <div className="border-border border-b px-4 py-4">
                <ShareBar
                  segments={[
                    {
                      key: "allowed",
                      label: "Allowed",
                      value: challengeTotal - deniedTotal,
                    },
                    { key: "denied", label: "Denied", value: deniedTotal },
                  ].filter((segment) => segment.value > 0)}
                  ariaLabel="Authorization outcomes"
                />
              </div>
              {challenges.slice(0, RECENT_CHALLENGES).map((challenge) => (
                <IdentityPanelRow
                  key={challenge.id}
                  accent={
                    challenge.outcome === "deny" && !challenge.resolvedAt
                      ? "destructive"
                      : undefined
                  }
                  title={challenge.scope}
                  detail={<HumanizeDateTime date={challenge.timestamp} />}
                  trailing={
                    challenge.resolvedAt
                      ? "Resolved"
                      : challenge.outcome === "deny"
                        ? "Denied"
                        : "Allowed"
                  }
                />
              ))}
            </>
          )}
        </IdentityPanel>
      </div>
    </IdentitySection>
  );
}
