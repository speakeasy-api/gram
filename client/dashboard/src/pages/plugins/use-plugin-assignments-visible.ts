import { useRBAC } from "@/hooks/useRBAC";
import { useOrganization } from "@/contexts/Auth";

// Assignment reach is admin-only, including after an authorization downgrade.
export function usePluginAssignmentsVisible(): boolean {
  const organization = useOrganization();
  const { hasScope, isLoading } = useRBAC();
  return !isLoading && hasScope("org:admin", organization.id);
}
