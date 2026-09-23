import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { encodeIdentityUrn, withIdentityWindow } from "@/lib/identity-urn";
import { useRoutes } from "@/routes";
import { useMembers } from "@gram/client/react-query/members.js";
import { useMemo } from "react";
import { useLocation, useNavigate } from "react-router";
import type { LauncherCandidate } from "./types";

/**
 * Org members, jumping straight to their identity page. Org-scoped, so the
 * caller gates `enabled` on an org scope rather than on being in a project.
 *
 * Directory members only: the identities index also lists unattributed
 * addresses and agent ids, but reaching those needs an all-time telemetry
 * crawl, far too heavy for a surface that answers on every keystroke.
 *
 * People are fuzzy-only: the ranker never sends `person` candidates to Jev.
 */
export function usePersonCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  // The identity page lives under a project, and the palette opens from the
  // org shell too, where the path carries no slug. Fall back to the slug those
  // pages already send on their requests, the same way IdentityLink does.
  const projectSlug = useProjectSlugForRequests();
  const routes = useRoutes({ projectSlug });
  const navigate = useNavigate();
  // The palette opens over whatever page the reader had narrowed, so the
  // person's page opens on that same window rather than the default one.
  const { search } = useLocation();
  const { data } = useMembers(undefined, undefined, { enabled });

  return useMemo(
    () =>
      (data?.members ?? []).map((member): LauncherCandidate => {
        const roles = member.roleIds.join(", ");
        return {
          id: `person:${member.id}`,
          kind: "person",
          title: member.name || member.email,
          detail: roles ? `Member · ${roles}` : "Member",
          keywords: ["person", member.email, member.id],
          verbs: ["open"],
          icon: "user",
          group: "People",
          run: () => {
            void navigate(
              withIdentityWindow(
                routes.identities.detail.overview.href(
                  encodeIdentityUrn(`user:${member.id}`),
                ),
                search,
              ),
            );
          },
        };
      }),
    [data, routes, navigate, search],
  );
}
