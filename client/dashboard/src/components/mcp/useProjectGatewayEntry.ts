import { useOrganization, useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useOrganizationGateway } from "@gram/client/react-query/organizationGateway.js";

/**
 * The project's gateway: the Organization Gateway limited to one project's
 * servers, served beneath the organization's URL.
 */
export function projectGatewayUrl(
  organizationGatewayUrl: string,
  projectSlug: string,
): string {
  return `${organizationGatewayUrl.replace(/\/+$/, "")}/projects/${encodeURIComponent(projectSlug)}`;
}

/**
 * Whether the MCP Servers list offers the project's gateway. It is on and off
 * with the Organization Gateway, so everyone sees it while that is served.
 * Organization admins also see it while it is turned off, so the project page
 * stays a way to turn it on.
 */
export function useProjectGatewayEntry(): {
  show: boolean;
  isOff: boolean;
  url: string | undefined;
} {
  const organization = useOrganization();
  const project = useProject();
  const { hasScope } = useRBAC();
  // Discoverability only: a failed read hides the entry rather than the page.
  const { data } = useOrganizationGateway(undefined, undefined, {
    throwOnError: false,
  });
  const isOff = data?.unavailableReason === "turned_off";
  // The gateway setting needs org:read, so the entry does too.
  const show =
    hasScope("org:read", organization.id) &&
    (!!data?.available || (isOff && hasScope("org:admin", organization.id)));
  return {
    show,
    isOff,
    url:
      data?.available && data.url
        ? projectGatewayUrl(data.url, project.slug)
        : undefined,
  };
}
