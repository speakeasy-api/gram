import { useOrganization } from "@/contexts/Auth";

/** A project hook events can be sent to. */
export interface HooksProject {
  id: string;
  name: string;
  slug: string;
}

/** The organization's projects, as choices for where hooks go. */
export function useHooksProjects(): HooksProject[] {
  const organization = useOrganization();
  return (organization.projects ?? []).map((project) => ({
    id: project.id,
    name: project.name,
    slug: project.slug,
  }));
}
