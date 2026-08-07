import { clearStorageForLogout } from "@/lib/logout-storage";

/**
 * Paths that render without a session. A 401 on these is expected (auth.info
 * always 401s when logged out), so it must not trigger a login redirect.
 */
export const UNAUTHENTICATED_PATHS = [
  "/login",
  "/register",
  "/explore-demo",
  "/sign-up",
  "/invite",
  "/book-demo",
  "/shadow-mcp/request",
  "/risk-policy-bypass/request",
  "/blocks",
  "/shared",
];

let redirecting = false;

/**
 * Narrows a value taken from the URL to a same-origin path.
 *
 * Prefix checks alone are not enough: the URL parser strips ASCII tab and
 * newline characters, so `/%0A/evil.example` reaches the browser as
 * `//evil.example` — a protocol-relative URL pointing at a foreign origin.
 * Resolving against the current origin and comparing the parsed origin catches
 * those, and returning the parsed components hands the caller the normalized
 * path rather than the raw input. A leading slash is still required so the
 * value can only ever be an absolute in-app path.
 */
export function safeRedirectPath(value: string | null): string | undefined {
  if (!value || !value.startsWith("/")) return undefined;

  const origin = window.location.origin;
  let url: URL;
  try {
    url = new URL(value, origin);
  } catch {
    return undefined;
  }

  if (url.origin !== origin) return undefined;
  return `${url.pathname}${url.search}${url.hash}`;
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
  clearStorageForLogout();
  const target = safeRedirectPath(pathname + search);
  if (!target) {
    window.location.assign("/login");
    return;
  }
  window.location.assign(`/login?redirect=${encodeURIComponent(target)}`);
}
