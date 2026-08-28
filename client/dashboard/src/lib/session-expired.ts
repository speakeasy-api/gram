import { clearStorageForLogout } from "@/lib/logout-storage";
import { getApiBaseURL } from "@/lib/utils";

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

const SESSION_CHECK_TIMEOUT_MS = 5_000;

let redirecting = false;
let sessionCheck: Promise<void> | null = null;

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
 * Bounce to /login after a query comes back 401, but only after auth.info
 * confirms the dashboard session itself is gone. Project-scoped endpoints can
 * also return 401 when the session is valid but the requested project context
 * is unavailable; treating those as logout creates a /login redirect loop.
 *
 * Concurrent query failures share one session check. A hard navigation after
 * confirmed expiry drops the React Query cache built under the dead session
 * and re-runs the auth bootstrap.
 */
export function redirectToLoginOnUnauthorized(): Promise<void> {
  if (redirecting) return Promise.resolve();

  const { pathname, search } = window.location;
  if (UNAUTHENTICATED_PATHS.some((p) => pathname.startsWith(p))) {
    return Promise.resolve();
  }

  if (sessionCheck) return sessionCheck;

  sessionCheck = (async () => {
    const controller = new AbortController();
    let timeout: ReturnType<typeof setTimeout> | undefined;

    try {
      const response = await Promise.race([
        fetch(`${getApiBaseURL()}/rpc/auth.info`, {
          credentials: "include",
          headers: { Accept: "application/json" },
          signal: controller.signal,
        }),
        new Promise<undefined>((resolve) => {
          timeout = setTimeout(() => {
            controller.abort();
            resolve(undefined);
          }, SESSION_CHECK_TIMEOUT_MS);
        }),
      ]);
      if (response?.status !== 401) return;

      redirecting = true;
      clearStorageForLogout();
      const target = safeRedirectPath(pathname + search);
      if (!target) {
        window.location.assign("/login");
        return;
      }
      window.location.assign(`/login?redirect=${encodeURIComponent(target)}`);
    } catch {
      // A failed verification request is not proof that the session expired.
      // Leave the user in place so a transient network error cannot log them out.
    } finally {
      clearTimeout(timeout);
      sessionCheck = null;
    }
  })();

  return sessionCheck;
}
