import { useOrganization, useProject } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { generatePath } from "react-router";

/** Resolve organization-wide provider links into their owning project. */
export function useIssuerHref(): (issuer: {
  id: string;
  projectId?: string | null;
}) => string | undefined {
  const organization = useOrganization();
  const activeProject = useProject();
  const routes = useRoutes({ projectSlug: ":issuerProjectSlug" });

  return (issuer: { id: string; projectId?: string | null }) => {
    const project = issuer.projectId
      ? organization.projects.find((project) => project.id === issuer.projectId)
      : activeProject;
    if (!project) return undefined;
    return generatePath(
      routes.remoteIdentityProviders.issuerDetail.href(issuer.id),
      {
        issuerProjectSlug: project.slug,
      },
    );
  };
}
