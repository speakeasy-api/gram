import { useOrganization } from "@/contexts/Auth";
import { resolveLandingProject } from "@/lib/preferredProject";
import { Navigate } from "react-router";

/**
 * The organization URL has no page of its own: org-level pages are global
 * settings, and the product lives in a project. Open the landing project, or
 * the project list when the organization has none yet.
 */
export function OrgHomeRedirect(): JSX.Element {
  const organization = useOrganization();
  const project = resolveLandingProject(organization.projects);

  return (
    <Navigate
      replace
      to={
        project
          ? `/${organization.slug}/projects/${project.slug}`
          : `/${organization.slug}/projects`
      }
    />
  );
}
