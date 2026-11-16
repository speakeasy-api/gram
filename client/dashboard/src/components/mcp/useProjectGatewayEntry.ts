import { useOrganization } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useProjectGateway } from "@gram/client/react-query/projectGateway.js";

/**
 * Whether the MCP Servers list offers the project's gateway, and its URL as
 * the server builds it. It is on and off with the Organization Gateway, so
 * everyone who can see the project sees it while that is served. Organization
 * admins also see it while it is turned off, so the project page stays a way to
 * turn it on.
 */
export function useProjectGatewayEntry(): {
  show: boolean;
  isOff: boolean;
  url: string | undefined;
} {
  const organization = useOrganization();
  const { hasScope } = useRBAC();
  // Discoverability only: a failed read hides the entry rather than the page.
  const { data } = useProjectGateway(undefined, undefined, {
    throwOnError: false,
  });
  const isOff = data?.unavailableReason === "turned_off";
  const show =
    !!data?.available || (isOff && hasScope("org:admin", organization.id));
  return { show, isOff, url: data?.available ? data.url : undefined };
}
