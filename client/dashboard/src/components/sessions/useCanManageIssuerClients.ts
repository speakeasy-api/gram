import { useUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";

import { useOrganization } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";

/**
 * Whether the viewer may register agents against an issuer or edit their
 * redirect URIs. Mirrors the server: a project-owned issuer needs
 * project:write on its project, and an organization-owned issuer, which every
 * project in the organization shares, needs org:admin.
 *
 * Reads false until the issuer has loaded, so write controls never flash in
 * for someone who cannot use them.
 */
export function useCanManageIssuerClients(
  issuerId: string | undefined,
  gramProject?: string,
): boolean {
  const { hasScope } = useRBAC();
  const organization = useOrganization();
  const issuerQuery = useUserSessionIssuer(
    { id: issuerId ?? "", gramProject },
    undefined,
    { enabled: Boolean(issuerId) },
  );

  const issuer = issuerQuery.data;
  if (!issuer) return false;

  return issuer.projectId
    ? hasScope("project:write", issuer.projectId)
    : hasScope("org:admin", organization.id);
}
