/**
 * Portable dashboard paths.
 *
 * A path beginning with `/@self` stands for `/<orgSlug>` for whichever
 * organization the viewer ends up in. External surfaces — marketing-site CTAs,
 * docs, emails — cannot know a visitor's slugs, but the post-login
 * `?redirect=` param carries paths verbatim, so a placeholder segment lets a
 * single URL work for every visitor:
 *
 *   app.getgram.ai/@self/webhooks                  →  /acme/webhooks
 *   app.getgram.ai/@self/projects/default/toolsets →  /acme/projects/default/toolsets
 *
 * The placeholder is resolved client-side by AuthProvider once the session is
 * known. `@` cannot collide with a real org slug (slugs are lowercase
 * alphanumerics and dashes) and is legal unescaped in a path.
 */

const PORTABLE_PATH_PREFIX = "/@self";

export function isPortablePath(pathname: string): boolean {
  return (
    pathname === PORTABLE_PATH_PREFIX ||
    pathname.startsWith(`${PORTABLE_PATH_PREFIX}/`)
  );
}

/**
 * Expands a portable path into a concrete one for the given organization,
 * keeping the destination's query and hash. Returns undefined when the path
 * is not portable.
 */
export function resolvePortablePath(
  location: { pathname: string; search: string; hash: string },
  organization: { slug: string },
): string | undefined {
  if (!isPortablePath(location.pathname)) return undefined;

  const rest = location.pathname.slice(PORTABLE_PATH_PREFIX.length);
  return `/${organization.slug}${rest}${location.search}${location.hash}`;
}
