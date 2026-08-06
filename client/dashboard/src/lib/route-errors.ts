import { GramError } from "@gram/client/models/errors/gramerror.js";

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

const UUID_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function isUuidRouteParam(value: string | undefined): value is string {
  return typeof value === "string" && UUID_PATTERN.test(value);
}
