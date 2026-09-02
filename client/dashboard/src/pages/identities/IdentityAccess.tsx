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
  useIdentityWindow,
} from "./useIdentityQueries";

const RECENT_CHALLENGES = 5;

export default function IdentityAccess(): JSX.Element {
  const location = useLocation();
  const routes = useRoutes();
  const { identity } = useIdentityOutlet();
  const { from, to } = useIdentityWindow();
  const orgRoutes = useOrgRoutes();
  const { member, query: membersQuery } = useIdentityMember(identity);
  const handoffs = identityHandoffs(
    identity,
    routes,
    orgRoutes,
    // The same fallback the challenge query uses, so the link filters to the
    // principal the panel counted rather than opening the whole log.
    member?.principalUrn ??
      (identity.workosUserId || identity.userIds[0]
        ? `user:${identity.workosUserId ?? identity.userIds[0]}`
        : undefined),
    new URLSearchParams(location.search),
  );
  const rolesQuery = useRoles(undefined, undefined, { throwOnError: false });
  const challengesQuery = useIdentityChallenges(identity, from, to);
  // Roles are the join of the member row and the role catalogue, so both have
  // to land before the panels can say anything true about them.
  const rolesLoading = rolesQuery.isLoading || membersQuery.isLoading;

  const rolesById = new Map(
    (rolesQuery.data?.roles ?? []).map((role) => [role.id, role]),
  );
  const roles = (member?.roleIds ?? []).map(
    (id) => rolesById.get(id) ?? { id, name: id, slug: id, description: "" },
  );
  // A scope slug is "<family>:<action>" (org:read, chat:read). Grouping by
  // family turns a flat list of twenty slugs into the handful of areas this
  // person has any reach over, which is the shape the question is asked in.
  const scopeFamilies = new Map<string, Set<string>>();
  for (const role of roles) {
    for (const grant of ("grants" in role ? role.grants : []) ?? []) {
      const slug = String(grant.scope);
      const [family = slug, action = ""] = slug.split(":");
      if (!scopeFamilies.has(family)) scopeFamilies.set(family, new Set());
      scopeFamilies.get(family)?.add(action || slug);
    }
  }
  const permissions = [...scopeFamilies.entries()]
    .map(([family, actions]) => ({ family, actions: [...actions].sort() }))
    .sort((a, b) => a.family.localeCompare(b.family));
  const scopeCount = permissions.reduce(
    (sum, group) => sum + group.actions.length,
    0,
  );

  const challenges = challengesQuery.data?.challenges ?? [];
  const denied = challenges.filter((c) => c.outcome === "deny");

  return (
    <IdentitySection
      title="Access"
      meta={sectionMeta([
        { count: roles.length, singular: "role" },
        { count: scopeCount, singular: "permission" },
        { count: denied.length, singular: "denied", plural: "denied" },
      ])}
    >
      <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
        <IdentityPanel
          title="Roles"
          handoffLabel="Roles & Permissions"
          handoffHref={handoffs.roles}
          loading={rolesLoading}
          loadingVariant="block"
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
          footer={
            permissions.length > 0
              ? "Granted by the roles above, across every project."
              : undefined
          }
        >
          {permissions.length === 0 ? (
            <IdentityPanelEmpty>
              These roles carry no permissions.
            </IdentityPanelEmpty>
          ) : (
            permissions.map((group) => (
              <div
                key={group.family}
                className="border-border flex items-baseline gap-3 border-b px-4 py-3 last:border-b-0"
              >
                <span className="w-28 shrink-0 truncate font-mono text-xs">
                  {group.family}
                </span>
                <span className="text-muted-foreground min-w-0 flex-1 font-mono text-xs">
                  {group.actions.join("  ")}
                </span>
              </div>
            ))
          )}
        </IdentityPanel>

        <IdentityPanel
          title="Authorization challenges"
          handoffLabel="Roles & Permissions"
          handoffHref={handoffs.challenges}
          loading={challengesQuery.isLoading}
          loadingRows={RECENT_CHALLENGES}
          footer={
            challenges.length > 0
              ? `${denied.length} denied of ${challenges.length} recorded`
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
                      value: challenges.length - denied.length,
                    },
                    { key: "denied", label: "Denied", value: denied.length },
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
