import { GramError } from "@gram/client/models/errors/gramerror.js";
import { queryKeySessionInfo } from "@gram/client/react-query/sessionInfo.core.js";

function getHttpStatusCode(error: unknown): number | undefined {
  if (error instanceof GramError) {
    return error.statusCode;
  }

  if (error && typeof error === "object") {
    const statusCode = (error as { statusCode?: unknown }).statusCode;
    if (typeof statusCode === "number") {
      return statusCode;
    }

    const status = (error as { status?: unknown }).status;
    if (typeof status === "number") {
      return status;
    }
  }

  return undefined;
}

export function isNotFoundError(error: unknown): boolean {
  return getHttpStatusCode(error) === 404;
}

export function isUnauthorizedError(error: unknown): boolean {
  return getHttpStatusCode(error) === 401;
}

/**
 * A 401 from the Gram API itself, meaning the dashboard session is dead.
 *
 * This is deliberately narrower than {@link isUnauthorizedError}: non-Gram
 * clients also surface errors with a 401 status — e.g. the AI SDK's
 * MCPClientError when a proxied MCP upstream rejects its credentials — and
 * those say nothing about the Gram session. Treating them as session expiry
 * causes a redirect loop: /login sees a valid session and bounces straight
 * back to the page whose query 401s again.
 */
export function isGramSessionUnauthorizedError(error: unknown): boolean {
  return error instanceof GramError && error.statusCode === 401;
}

// The static part of the auth.info query key; the final element is the
// per-call parameters object and never participates in the match.
const SESSION_INFO_KEY_PREFIX = queryKeySessionInfo({}).slice(0, -1);

/**
 * True for the session-bootstrap query (auth.info). Its 401 is the expected
 * "logged out" answer — AuthProvider renders the logged-out tree and
 * LoginCheck redirects in-SPA — so the global 401 handler must not also
 * force a full-page navigation for it. Reacting to both produced a double
 * page load on every logged-out visit: SPA-rendered login screen, then a
 * hard reload of the same screen.
 */
export function isSessionInfoQueryKey(queryKey: readonly unknown[]): boolean {
  return SESSION_INFO_KEY_PREFIX.every((part, i) => queryKey[i] === part);
}

const UUID_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function isUuidRouteParam(value: string | undefined): value is string {
  return typeof value === "string" && UUID_PATTERN.test(value);
}
