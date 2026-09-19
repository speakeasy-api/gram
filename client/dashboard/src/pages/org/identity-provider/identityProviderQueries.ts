import type { QueryClient } from "@tanstack/react-query";
import { invalidateAllIdentityProviderConnection } from "@gram/client/react-query/identityProviderConnection.js";
import { invalidateAllIdentityProviderConnectionApplications } from "@gram/client/react-query/identityProviderConnectionApplications.js";
import { invalidateAllXaaReadiness } from "@gram/client/react-query/xaaReadiness.js";

export const SESSION_SECURITY = { sessionHeaderGramSession: "" } as const;

export const CONNECTION_SECTION_ID = "connection";

/** Top-level identity tabs; provider workspaces live inside Enterprise Managed Auth. */
export const IDENTITY_TABS = ["sso", "enterprise-managed-auth"] as const;
export type IdentityPageTab = (typeof IDENTITY_TABS)[number];

/** Legacy concern tabs remain the child-flow API. */
const PROVIDER_TABS = ["provider", "applications", "cross-app-access"] as const;
export type ProviderTab = (typeof PROVIDER_TABS)[number];
export type IdentityTab = "sso" | ProviderTab;

export function enterpriseManagedAuthHref(
  provider?: string,
  view?: string,
  sectionId?: string,
): string {
  const search = new URLSearchParams({ tab: "enterprise-managed-auth" });
  if (provider) search.set("provider", provider);
  if (view) search.set("view", view);
  return `?${search.toString()}${sectionId ? `#${sectionId}` : ""}`;
}

export function identityTabHref(tab: IdentityTab, sectionId?: string): string {
  if (tab === "sso") return `?tab=sso${sectionId ? `#${sectionId}` : ""}`;
  return enterpriseManagedAuthHref(
    "okta",
    tab === "provider" ? "setup" : tab,
    sectionId,
  );
}

/** Rewrite a legacy provider URL without dropping unrelated query parameters. */
export function legacyIdentityProviderSearch(
  search: URLSearchParams,
  tab: ProviderTab,
): string {
  const canonical = new URLSearchParams(identityTabHref(tab));
  const result = new URLSearchParams(search);
  result.delete("okta");
  canonical.forEach((value, key) => result.set(key, value));
  return `?${result.toString()}`;
}

/** Old Okta-page sub-tabs (`/okta?tab=<sub>` and `identity?tab=okta&okta=<sub>`) map onto the concern tabs. */
const LEGACY_OKTA_TABS: Record<string, ProviderTab> = {
  connection: "provider",
  applications: "applications",
  "cross-app-access": "cross-app-access",
};

export function legacyOktaTab(sub: string | null): ProviderTab {
  return sub && Object.hasOwn(LEGACY_OKTA_TABS, sub)
    ? LEGACY_OKTA_TABS[sub]!
    : "provider";
}

/** After a verification the form below unmounts; bring the status card back into view. */
export function scrollToConnectionCard(): void {
  document
    .getElementById(CONNECTION_SECTION_ID)
    ?.scrollIntoView({ block: "start", behavior: "smooth" });
}

/** Hooks that render their error inline opt out of the global "Request failed" toast. */
export function inlineError(): undefined {
  return undefined;
}

export function invalidateIdentityProviderQueries(
  client: QueryClient,
): Promise<void> {
  return Promise.all([
    invalidateAllIdentityProviderConnection(client),
    invalidateAllIdentityProviderConnectionApplications(client),
    invalidateAllXaaReadiness(client),
  ]).then(() => undefined);
}
