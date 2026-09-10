import {
  encodeIdentityUrn,
  identityUrnFor,
  withIdentityWindow,
} from "@/lib/identity-urn";
import type { IdentityRef } from "@/lib/identity-urn";
import { useRBAC } from "@/hooks/useRBAC";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { useLocation } from "react-router";

/** The sub-page a link lands on. Overview unless the sender means a subsystem. */
export type IdentitySection = "overview" | "access";

/**
 * Builds identity page hrefs for person references.
 *
 * The page is project-level, so the href carries a project slug. Org-scoped
 * pages render people too and have no slug in their path, so the builder falls
 * back to the same slug those pages already send on their requests — the link
 * lands in the project whose data the reader was looking at. A page that picks
 * its project through a filter rather than through the address passes that
 * choice in `projectSlug`, since neither its path nor its requests carry it.
 *
 * The href also carries the window the reader has open, so the person's page
 * opens on the period they were reading rather than resetting to the default.
 */
export function useIdentityHrefBuilder(
  section: IdentitySection = "overview",
  projectSlug?: string,
): (identifier: IdentityRef | null | undefined) => string | null {
  const requestProjectSlug = useProjectSlugForRequests();
  const routes = useRoutes({ projectSlug: projectSlug ?? requestProjectSlug });
  const { search } = useLocation();
  // org:read, the same gate IdentityDetailRoot and identity.resolve carry: a
  // reader without it only reaches "Access restricted", so they get the name
  // as plain text rather than a link that cannot go anywhere. One check here
  // covers every call site, and it has to be the destination's gate — checking
  // project:read would hide the link from org readers who can open the page
  // and show it to project readers who cannot.
  const { hasAnyScope, isLoading } = useRBAC();
  const canOpenIdentities = isLoading || hasAnyScope(["org:read"]);
  return (identifier) =>
    identifier && canOpenIdentities
      ? withIdentityWindow(
          routes.identities.detail[section].href(
            encodeIdentityUrn(identityUrnFor(identifier)),
          ),
          search,
        )
      : null;
}
