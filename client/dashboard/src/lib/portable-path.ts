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
 * A path beginning with `/@self` stands for `/<orgSlug>` alone, for org-level
 * pages (settings, members, billing) that sit outside any project:
 *
 *   app.getgram.ai/@self/settings  →  /acme/settings
 *
 * The placeholder is resolved client-side by AuthProvider once the session is
 * known. Neither `~` nor `@` can collide with a real org slug (slugs are
 * lowercase alphanumerics and dashes), and both are legal unescaped in a path.
 */

const PORTABLE_PATH_PREFIX = "/~";
const PORTABLE_ORG_PATH_PREFIX = "/@self";

function hasPrefix(pathname: string, prefix: string): boolean {
  return pathname === prefix || pathname.startsWith(`${prefix}/`);
}

export function isPortablePath(pathname: string): boolean {
  return (
    hasPrefix(pathname, PORTABLE_PATH_PREFIX) ||
    hasPrefix(pathname, PORTABLE_ORG_PATH_PREFIX)
  );
}

type OrganizationWithProjects = {
  slug: string;
  projects: Array<{ slug: string }>;
};

/**
 * Expands a portable path into a concrete one for the given organization.
 * `/@self` paths map straight onto the org. `/~` paths prefer the project the
 * user last visited; an organization whose visible project list is empty
 * (project-level access can be filtered away) resolves to the org home, since
 * the remainder of the path is project-scoped and cannot render anywhere else.
 * Returns undefined when the path is not portable.
 */
export function resolvePortablePath(
  location: { pathname: string; search: string; hash: string },
  organization: OrganizationWithProjects,
  preferredProjectSlug?: string | null,
): string | undefined {
  const suffix = `${location.search}${location.hash}`;

  if (hasPrefix(location.pathname, PORTABLE_ORG_PATH_PREFIX)) {
    const rest = location.pathname.slice(PORTABLE_ORG_PATH_PREFIX.length);
    return `/${organization.slug}${rest}${suffix}`;
  }

  if (!hasPrefix(location.pathname, PORTABLE_PATH_PREFIX)) return undefined;

  const project =
    (preferredProjectSlug != null &&
      organization.projects.find((p) => p.slug === preferredProjectSlug)) ||
    organization.projects[0];

  if (!project) {
    return `/${organization.slug}${suffix}`;
  }

  const rest = location.pathname.slice(PORTABLE_PATH_PREFIX.length);
  return `/${organization.slug}/projects/${project.slug}${rest}${suffix}`;
}
