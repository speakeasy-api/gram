/**
 * Portable dashboard paths.
 *
 * A path beginning with `/~` stands for `/<orgSlug>/projects/<projectSlug>`
 * for whichever organization and project the viewer ends up in. External
 * surfaces — marketing-site CTAs, docs, emails — cannot know a visitor's
 * slugs, but the post-login `?redirect=` param carries paths verbatim, so a
 * placeholder segment lets a single URL work for every visitor:
 *
 *   app.getgram.ai/~/toolsets  →  /acme/projects/default/toolsets
 *
 * The placeholder is resolved client-side by AuthProvider once the session is
 * known. `~` cannot collide with a real org slug (slugs are lowercase
 * alphanumerics and dashes) and needs no escaping in a URL.
 */

const PORTABLE_PATH_PREFIX = "/~";

export function isPortablePath(pathname: string): boolean {
  return (
    pathname === PORTABLE_PATH_PREFIX ||
    pathname.startsWith(`${PORTABLE_PATH_PREFIX}/`)
  );
}

type OrganizationWithProjects = {
  slug: string;
  projects: Array<{ slug: string }>;
};

/**
 * Expands a portable path into a concrete one for the given organization,
 * preferring the project the user last visited. Returns undefined when the
 * path is not portable. An organization whose visible project list is empty
 * (project-level access can be filtered away) resolves to the org home, since
 * the remainder of the path is project-scoped and cannot render anywhere else.
 */
export function resolvePortablePath(
  location: { pathname: string; search: string; hash: string },
  organization: OrganizationWithProjects,
  preferredProjectSlug?: string | null,
): string | undefined {
  if (!isPortablePath(location.pathname)) return undefined;

  const project =
    (preferredProjectSlug != null &&
      organization.projects.find((p) => p.slug === preferredProjectSlug)) ||
    organization.projects[0];

  if (!project) {
    return `/${organization.slug}${location.search}${location.hash}`;
  }

  const rest = location.pathname.slice(PORTABLE_PATH_PREFIX.length);
  return `/${organization.slug}/projects/${project.slug}${rest}${location.search}${location.hash}`;
}
