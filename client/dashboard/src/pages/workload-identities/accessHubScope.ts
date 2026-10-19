import { useOrgRoutes, useRoutes } from "@/routes";
import { createContext, useContext, useMemo } from "react";

/**
 * Which tier of the trust policy an Access Hub page manages, and where its
 * links go. The organization's hub and each project's render the same pages;
 * this is the only thing that differs between them.
 */
export interface AccessHubScope {
  /**
   * Sent on every workload identity request. The dashboard attaches the
   * selected project's header on organization pages too, so the header alone
   * never moves a request to the project tier.
   */
  projectScoped: boolean;
  /** The hub's own page. */
  hubHref: () => string;
  issuerHref: (issuerId: string) => string;
  catalogPlatformHref: (platformKey: string) => string;
}

export const AccessHubScopeContext = createContext<AccessHubScope | null>(null);

/** The organization's Access Hub: the organization tier alone. */
export function useOrganizationAccessHubScope(): AccessHubScope {
  const orgRoutes = useOrgRoutes();
  return useMemo(
    () => ({
      projectScoped: false,
      hubHref: () => orgRoutes.workloadIssuers.href(),
      issuerHref: (issuerId) =>
        orgRoutes.workloadIssuers.issuerDetail.href(issuerId),
      catalogPlatformHref: (platformKey) =>
        orgRoutes.workloadIssuers.catalogPlatform.href(platformKey),
    }),
    [orgRoutes],
  );
}

/** A project's Access Hub: that project's tier alone. */
export function useProjectAccessHubScope(): AccessHubScope {
  const routes = useRoutes();
  return useMemo(
    () => ({
      projectScoped: true,
      hubHref: () => routes.accessHub.href(),
      issuerHref: (issuerId) => routes.accessHub.issuerDetail.href(issuerId),
      catalogPlatformHref: (platformKey) =>
        routes.accessHub.catalogPlatform.href(platformKey),
    }),
    [routes],
  );
}

/**
 * The scope of the Access Hub being rendered. Outside either root, as when a
 * page is rendered on its own, it is the organization's.
 */
export function useAccessHubScope(): AccessHubScope {
  const scope = useContext(AccessHubScopeContext);
  const organization = useOrganizationAccessHubScope();
  return scope ?? organization;
}
