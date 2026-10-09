import { GramError } from "@gram/client/models/errors/gramerror.js";

export type SourceDestinationLock = {
  /** Why the caller may not move this source's destination; null when it may. */
  reason: string | null;
};

export const SOURCE_DESTINATION_LOCK_REASON =
  "An MCP server on this source has a linked environment you can't read, so you can't change where it sends requests. Ask for environment:read on the project and on that environment.";

export const SOURCE_DESTINATION_UNKNOWN_REASON =
  "Couldn't confirm whether you can change where this source sends requests: an MCP server on it may have a linked environment you can't read.";

/**
 * Mirrors the server rule for changing a remote source's URL or rotating a
 * tunnel key: while any MCP server on the source has a linked environment
 * (disabled and hidden servers included), the caller needs read access to
 * each of those environments. The source's getServer response answers that
 * for the current caller, honouring exclusions the dashboard cannot see, so
 * this only reads it. The server stays authoritative.
 *
 * environmentLinkAuthorized is true when allowed, false when refused, and
 * missing when unknown (an older server, or a failed refresh): unknown locks.
 */
export function sourceDestinationLock(source: {
  environmentLinkAuthorized?: boolean | undefined;
}): SourceDestinationLock {
  if (source.environmentLinkAuthorized === true) return { reason: null };
  if (source.environmentLinkAuthorized === false) {
    return { reason: SOURCE_DESTINATION_LOCK_REASON };
  }
  return { reason: SOURCE_DESTINATION_UNKNOWN_REASON };
}

type EnvironmentLinkFields = {
  environmentLinked?: boolean | undefined;
  environmentLinkAuthorized?: boolean | undefined;
};

/**
 * A source query whose refetch failed still holds its last good row, but that
 * row's environment-link answer can no longer be trusted: drop it so the lock
 * falls back to unknown. Everything else is kept so forms do not reset.
 */
export function withUnconfirmedEnvironmentLink<T extends EnvironmentLinkFields>(
  source: T | undefined,
  failed: boolean,
): (Omit<T, keyof EnvironmentLinkFields> & EnvironmentLinkFields) | undefined {
  if (!source || !failed) return source;
  return {
    ...source,
    environmentLinked: undefined,
    environmentLinkAuthorized: undefined,
  };
}

/** True for the refusal a stale lock lets through. */
export function isForbidden(error: unknown): boolean {
  return error instanceof GramError && error.statusCode === 403;
}
