/**
 * Paths that render without a session. A 401 on these is expected (auth.info
 * always 401s when logged out), so it must not trigger a login redirect.
 */
export const UNAUTHENTICATED_PATHS = [
  "/login",
  "/register",
  "/invite",
  "/book-demo",
  "/shadow-mcp/request",
  "/risk-policy-bypass/request",
  "/blocks",
  "/shared",
];

let redirecting = false;

/**
 * Narrows a value taken from the URL to a same-origin path. `//evil.com` and
 * `/\evil.com` are protocol-relative URLs that the browser (and react-router's
 * `Navigate`) resolve to a foreign origin, so anything but a single leading
 * slash is rejected and the caller falls back to the app root.
 */
export function safeRedirectPath(value: string | null): string | undefined {
  if (!value) return undefined;
  if (!value.startsWith("/")) return undefined;
  if (value.startsWith("//") || value.startsWith("/\\")) return undefined;
  return value;
}

/**
 * Bounce to /login after a query comes back 401. The session cookie is gone,
 * expired, or (in local dev, where the cookie is scoped to `localhost` and
 * ports are ignored) belongs to another worktree's stack. Without this, the
 * 401 throws to AuthProvider's error boundary and the user gets a dead
 * "Something went wrong" screen instead of a login page.
 *
 * A hard navigation rather than a router navigate: it drops the React Query
 * cache built up under the dead session and re-runs the auth bootstrap.
 */
export function redirectToLoginOnUnauthorized(): void {
  if (redirecting) return;

  const { pathname, search } = window.location;
  if (UNAUTHENTICATED_PATHS.some((p) => pathname.startsWith(p))) return;

  redirecting = true;
  const target = safeRedirectPath(pathname + search);
  if (!target) {
    window.location.assign("/login");
    return;
  }
  window.location.assign(`/login?redirect=${encodeURIComponent(target)}`);
}
