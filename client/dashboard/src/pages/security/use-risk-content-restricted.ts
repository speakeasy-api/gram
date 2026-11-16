import { useSession } from "@/contexts/Auth";

// Keep the fabricated demo exception aligned with CheckSensitiveContentAccess.
export function useRiskContentRestricted(): boolean {
  const session = useSession();
  return (
    Boolean(session.organizationOverride || session.impersonatorEmail) &&
    session.activeOrganizationId !== "org_gram_demo_workspace"
  );
}
