import { encodeIdentityUrn, identityUrnFor } from "@/lib/identity-urn";
import type { IdentityRef } from "@/lib/identity-urn";
import { useRBAC } from "@/hooks/useRBAC";
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
  // A reader without org:read only reaches "Access restricted", so they get
  // the name as plain text rather than a link that cannot go anywhere. One
  // check here covers every call site.
  const { hasAnyScope, isLoading } = useRBAC();
  const canOpenIdentities = isLoading || hasAnyScope(["org:read", "org:admin"]);
  return (identifier) =>
    identifier && canOpenIdentities
      ? orgRoutes.identities.detail.overview.href(
          encodeIdentityUrn(identityUrnFor(identifier)),
        )
      : null;
}
