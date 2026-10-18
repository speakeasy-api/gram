import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";

export const PROJECT_SPECIFIC_ISSUER_VALUE = "project-specific";

export function defaultCreationUserSessionIssuerValue(
  organizationIssuers: UserSessionIssuer[],
): string {
  if (organizationIssuers.length === 1) {
    return organizationIssuers[0]!.id;
  }
  if (organizationIssuers.length === 0) {
    return PROJECT_SPECIFIC_ISSUER_VALUE;
  }
  return "";
}

/**
 * Names an issuer by slug and owner, and marks one whose servers share a single
 * authorization server, since attaching to it changes where clients sign in. A
 * shared issuer whose authorization server is not served is not marked: its
 * servers keep their own.
 */
export function userSessionIssuerLabel(issuer: UserSessionIssuer): string {
  const ownership = issuer.projectId === "" ? "Organization" : "Project";
  const shared = issuer.issuerUrl ? " · Shared authorization server" : "";
  return `${issuer.slug} — ${ownership}${shared}`;
}
