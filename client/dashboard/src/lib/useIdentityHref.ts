import { encodeIdentityUrn, identityUrnFor } from "@/lib/identity-urn";
import type { IdentityRef } from "@/lib/identity-urn";
import { useOrgRoutes } from "@/routes";

/**
 * Builds identity page hrefs for person references.
 *
 * The page is org-level, so a reference links the same way from a project page
 * and from an org page — no project slug has to be in scope at the call site.
 */
export function useIdentityHrefBuilder(): (
  identifier: IdentityRef | null | undefined,
) => string | null {
  const orgRoutes = useOrgRoutes();
  return (identifier) =>
    identifier
      ? orgRoutes.identities.detail.overview.href(
          encodeIdentityUrn(identityUrnFor(identifier)),
        )
      : null;
}
