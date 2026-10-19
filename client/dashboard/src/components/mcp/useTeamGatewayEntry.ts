import { useOrganization } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useOrganizationGateway } from "@gram/client/react-query/organizationGateway.js";

/**
 * Whether the MCP Servers list offers the Team Gateway. Everyone sees it while
 * it is served. Organization admins also see it while it is turned off, so the
 * page they turned it off from stays the way back to turning it on.
 */
export function useTeamGatewayEntry(): {
  show: boolean;
  isOff: boolean;
  url: string | undefined;
} {
  const organization = useOrganization();
  const { hasScope } = useRBAC();
  // Discoverability only: a failed read hides the entry rather than the page.
  const { data } = useOrganizationGateway(undefined, undefined, {
    throwOnError: false,
  });
  const isOff = data?.unavailableReason === "turned_off";
  const show =
    !!data?.available || (isOff && hasScope("org:admin", organization.id));
  return { show, isOff, url: data?.available ? data.url : undefined };
}
