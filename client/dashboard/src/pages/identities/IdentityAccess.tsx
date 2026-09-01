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
import { Badge } from "@/components/ui/Badge";
import { IdentitySection } from "./IdentitySection";
import { sectionMeta } from "./sectionMeta";
import {
  useIdentityChallenges,
  useIdentityMember,
  useIdentityProject,
} from "./useIdentityQueries";

const RECENT_CHALLENGES = 5;

export default function IdentityAccess(): JSX.Element {
  const location = useLocation();
  const project = useIdentityProject();
  // Project routes resolve against the project this page is filtered to: the
  // page is org-level, so the router has no :projectSlug of its own to fill in
  // and every handoff would otherwise resolve to a path with the slug missing.
  const routes = useRoutes({ projectSlug: project.slug });
  const { identity } = useIdentityOutlet();
  const orgRoutes = useOrgRoutes();
  const { member } = useIdentityMember(identity);
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
  const challengesQuery = useIdentityChallenges(identity);

  const rolesById = new Map(
    (rolesQuery.data?.roles ?? []).map((role) => [role.id, role]),
  );
  const roles = (member?.roleIds ?? []).map(
    (id) => rolesById.get(id) ?? { id, name: id, slug: id },
  );
  const challenges = challengesQuery.data?.challenges ?? [];
  const denied = challenges.filter((c) => c.outcome === "deny");

  return (
    <IdentitySection
      title="Access"
      meta={sectionMeta([
        { count: roles.length, singular: "role" },
        { count: denied.length, singular: "denied", plural: "denied" },
      ])}
    >
      <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
        <IdentityPanel
          title="Roles"
          handoffLabel="Roles & Permissions"
          handoffHref={handoffs.roles}
          footer={
            member ? undefined : "No org member row resolves to this identity."
          }
        >
          {roles.length === 0 ? (
            <IdentityPanelEmpty>
              No roles assigned in this organization.
            </IdentityPanelEmpty>
          ) : (
            <ul className="flex flex-wrap gap-2 px-4 py-4">
              {roles.map((role) => (
                <li key={role.id}>
                  <Badge variant="neutral" title={role.slug}>
                    {role.name}
                  </Badge>
                </li>
              ))}
            </ul>
          )}
        </IdentityPanel>

        <IdentityPanel
          title="Authorization challenges"
          handoffLabel="Roles & Permissions"
          handoffHref={handoffs.challenges}
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
