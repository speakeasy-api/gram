import { useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { encodeIdentityUrn, identityUrnFor } from "@/lib/identity-urn";
import type { IdentityRef } from "@/lib/identity-urn";
import { useRoutes } from "@/routes";

/**
 * Builds identity page hrefs for person references.
 *
 * Person references are rendered on org-scoped pages too (Team, Audit Logs,
 * MCP Sessions), where the route carries no :projectSlug to fill in and the
 * link would otherwise resolve to a path matching no route, bouncing the
 * viewer to login. The project context holds the preferred project on those
 * pages, which is the one the identity's data should be read in.
 */
export function useIdentityHrefBuilder(): (
  identifier: IdentityRef | null | undefined,
) => string | null {
  const { projectSlug: routeProjectSlug } = useSlugs();
  const contextProject = useProject();
  const routes = useRoutes({
    projectSlug: routeProjectSlug || contextProject.slug,
  });
  return (identifier) =>
    identifier
      ? routes.identities.overview.href(
          encodeIdentityUrn(identityUrnFor(identifier)),
        )
      : null;
}
