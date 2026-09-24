import { useOrganization } from "@/contexts/Auth";
import { getPreferredProject } from "@/lib/preferredProject";
import { useOrgRoutes, useRoutes } from "@/routes";

/** Labels for the dashboard areas a step sends the admin to. */
export function destinationLabel(destination: string): string {
  switch (destination) {
    case "plugins":
      return "Open Plugins";
    case "integrations":
      return "Open AI Integrations";
    case "policies":
      return "Open Guardrails";
    case "mcp":
      return "Open MCP";
    case "devices":
      return "Open Device Agent";
    default:
      return "Open settings";
  }
}

/**
 * Resolves a step's destination to a dashboard URL. Project-level areas use
 * the organization's preferred project, the same one org home starts in.
 */
export function useDestinationHref(): (destination: string) => string {
  const organization = useOrganization();
  const orgRoutes = useOrgRoutes();
  const project =
    getPreferredProject(organization.projects) ??
    organization.projects.find((candidate) => candidate.slug === "default") ??
    organization.projects[0];
  const routes = useRoutes({ projectSlug: project?.slug });

  return (destination: string) => {
    switch (destination) {
      case "plugins":
        return routes.plugins.href();
      case "integrations":
        return orgRoutes.aiIntegrations.href();
      case "policies":
        return routes.policyCenter.href();
      case "mcp":
        return routes.mcp.href();
      case "devices":
        return orgRoutes.deviceAgent.href();
      default:
        return orgRoutes.onboardingSettings.href();
    }
  };
}
