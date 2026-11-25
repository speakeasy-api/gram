import { useMemo } from "react";
import { useOrganization } from "@/contexts/Auth";

interface LiteLLMInstanceProject {
  id: string;
  name: string;
  slug: string;
}

/**
 * The projects a LiteLLM instance can be bound to, and the one preselected.
 * Shared by the AI Integrations row and the setup board's LiteLLM card so
 * both surfaces preselect alike.
 */
export function useLiteLLMInstanceProjects(): {
  projects: LiteLLMInstanceProject[];
  defaultProject: LiteLLMInstanceProject | undefined;
} {
  const organization = useOrganization();
  const projects = useMemo(
    () =>
      [...organization.projects].sort((a, b) => a.name.localeCompare(b.name)),
    [organization.projects],
  );
  const defaultProject =
    projects.find((project) => project.slug === "default") ?? projects[0];
  return { projects, defaultProject };
}
