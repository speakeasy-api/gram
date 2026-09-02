import { encodeIdentityUrn, identityUrnFor } from "@/lib/identity-urn";
import type { IdentityRef } from "@/lib/identity-urn";
import { useRBAC } from "@/hooks/useRBAC";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";

/**
 * Builds identity page hrefs for person references.
 *
 * The page is project-level, so the href carries a project slug. Org-scoped
 * pages render people too and have no slug in their path, so the builder falls
 * back to the same slug those pages already send on their requests — the link
 * lands in the project whose data the reader was looking at.
 */
export function useIdentityHrefBuilder(): (
  identifier: IdentityRef | null | undefined,
) => string | null {
  const projectSlug = useProjectSlugForRequests();
  const routes = useRoutes({ projectSlug });
  // A reader without project:read only reaches "Access restricted", so they
  // get the name as plain text rather than a link that cannot go anywhere. One
  // check here covers every call site.
  const { hasAnyScope, isLoading } = useRBAC();
  const canOpenIdentities = isLoading || hasAnyScope(["project:read"]);
  return (identifier) =>
    identifier && canOpenIdentities
      ? routes.identities.detail.overview.href(
          encodeIdentityUrn(identityUrnFor(identifier)),
        )
      : null;
}
