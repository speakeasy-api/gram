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
