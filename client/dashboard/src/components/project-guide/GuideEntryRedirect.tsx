import { useOrganization } from "@/contexts/Auth";
import { getPreferredProject } from "@/lib/preferredProject";
import { Navigate } from "react-router";

export const PROJECT_GUIDE_ENTRY_PATH = "/guide";

export function GuideEntryRedirect(): JSX.Element {
  const organization = useOrganization();
  const project =
    getPreferredProject(organization.projects) ??
    organization.projects.find((candidate) => candidate.slug === "default") ??
    organization.projects[0];

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
