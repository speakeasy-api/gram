import { useRBAC } from "@/hooks/useRBAC";

export type SourceDestinationLock = {
  /** Why the caller may not move this source's destination; null when it may. */
  reason: string | null;
};

export const SOURCE_DESTINATION_LOCK_REASON =
  "An MCP server on this source has a linked environment, so changing where it sends requests needs environment:read for every environment in the project.";

export const SOURCE_DESTINATION_UNKNOWN_REASON =
  "Couldn't check whether an MCP server on this source has a linked environment. Changing where it sends requests may need environment:read for every environment in the project.";

/**
 * Mirrors the server rule for changing a remote source's URL or rotating a
 * tunnel key: while any MCP server on the source has a linked environment
 * (disabled servers included), the caller also needs project-wide
 * environment:read. The server stays authoritative; this only explains a
 * refusal before the user tries.
 *
 * environmentLinked comes from the source's getServer response, which checks
 * every server on the source, including ones this caller cannot list. When it
 * is missing, a caller without the grant is treated as locked rather than
 * offered a change the server may refuse.
 */
export function useSourceDestinationLock(source: {
  projectId: string;
  environmentLinked: boolean | undefined;
}): SourceDestinationLock {
  const { hasScope, isLoading } = useRBAC();
  if (
    !isLoading &&
    hasScope("environment:read", source.projectId, source.projectId)
  ) {
    return { reason: null };
  }
  if (isLoading || source.environmentLinked === undefined) {
    return { reason: SOURCE_DESTINATION_UNKNOWN_REASON };
  }
  return {
    reason: source.environmentLinked ? SOURCE_DESTINATION_LOCK_REASON : null,
  };
}

/**
 * A source query whose refetch failed still holds its last good row, but that
 * row's environmentLinked can no longer be trusted: drop it so the lock falls
 * back to unknown. Everything else is kept so forms do not reset.
 */
export function withUnconfirmedEnvironmentLink<
  T extends { environmentLinked?: boolean | undefined },
>(source: T | undefined, failed: boolean): T | undefined {
  if (!source || !failed) return source;
  return { ...source, environmentLinked: undefined };
}
