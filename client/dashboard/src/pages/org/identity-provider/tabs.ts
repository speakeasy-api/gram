export const IDENTITY_TABS = [
  "sso",
  "identity-providers",
  "slack-workspaces",
] as const;
export type IdentityPageTab = (typeof IDENTITY_TABS)[number];

export const PROVIDER_IDS = ["okta"] as const;
export type ProviderId = (typeof PROVIDER_IDS)[number];

export const OKTA_VIEWS = [
  "setup",
  "applications",
  "cross-app-access",
] as const;
export type OktaView = (typeof OKTA_VIEWS)[number];

export const CONNECTION_SECTION_ID = "connection";
export const AGENT_SECTION_ID = "agent";
export const CHECKLIST_SECTION_ID = "checklist";
export const CLIENT_ID_SECTION_ID = "client-id";
export const READINESS_SECTION_ID = "readiness";

export function identityProvidersHref(): string {
  return "?tab=identity-providers";
}

export function oktaViewHref(view: OktaView, sectionId?: string): string {
  const search = new URLSearchParams({
    tab: "identity-providers",
    provider: "okta",
    view,
  });
  return `?${search.toString()}${sectionId ? `#${sectionId}` : ""}`;
}

/** After a verification the form below unmounts; bring the status card back into view. */
export function scrollToConnectionCard(): void {
  document
    .getElementById(CONNECTION_SECTION_ID)
    ?.scrollIntoView({ block: "start", behavior: "smooth" });
}
