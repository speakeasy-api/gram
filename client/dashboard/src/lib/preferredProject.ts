// Slug of the last-visited project, written by ProjectProvider. AuthProvider
// and CliCallback still hold local copies of this key.
export const PREFERRED_PROJECT_KEY = "preferredProject";

/**
 * The stored slug is global across organizations, so it only resolves when it
 * exists in the given organization's projects.
 */
export function getPreferredProject<T extends { slug: string }>(
  projects: readonly T[],
): T | undefined {
  const preferredSlug = localStorage.getItem(PREFERRED_PROJECT_KEY);
  if (!preferredSlug) return undefined;
  return projects.find((p) => p.slug === preferredSlug);
}
