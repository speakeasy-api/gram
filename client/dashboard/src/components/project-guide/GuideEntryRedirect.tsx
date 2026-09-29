import { useOrganization } from "@/contexts/Auth";
import { resolveLandingProject } from "@/lib/preferredProject";
import { Navigate } from "react-router";

export const PROJECT_GUIDE_ENTRY_PATH = "/guide";

export function GuideEntryRedirect(): JSX.Element {
  const organization = useOrganization();
  const project = resolveLandingProject(organization.projects);

  return (
    <Navigate
      replace
      to={
        project
          ? `/${organization.slug}/projects/${project.slug}/guide`
          : `/${organization.slug}`
      }
    />
  );
}
