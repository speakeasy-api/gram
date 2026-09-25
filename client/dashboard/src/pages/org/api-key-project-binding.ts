/** Sentinel for a key that is not bound to any single project. */
export const ORGANIZATION_WIDE = "organization-wide";

type OrganizationProject = { id: string; name: string };

/**
 * How a key's project binding reads in the list and on the created key. A
 * binding the caller can no longer see is named rather than blanked, so a key
 * bound to a project outside the viewer's access never looks unbound.
 */
export function projectBindingLabel(
  projects: OrganizationProject[],
  id?: string,
): string {
  if (!id) return "Organization-wide";
  return (
    projects.find((project) => project.id === id)?.name ?? "Unavailable project"
  );
}
