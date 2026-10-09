import { useRBAC } from "@/hooks/useRBAC";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";

export type SourceDestinationLock = {
  /** True when the caller may not move this source's destination. */
  locked: boolean;
  /** Why, for the disabled control's hint; null when unlocked. */
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
 * The sibling list is complete for anyone who can make these changes at all:
 * both need mcp:write across the project, which lists every server in it.
 * While the list is loading or failed, a caller without the grant is treated
 * as locked rather than offered a change the server may refuse.
 */
export function useSourceDestinationLock(
  source:
    | { kind: "remote"; id: string; projectId: string }
    | { kind: "tunneled"; id: string; projectId: string },
): SourceDestinationLock {
  const { hasScope, isLoading: rbacLoading } = useRBAC();
  const canReadEnvironments =
    !rbacLoading &&
    hasScope("environment:read", source.projectId, source.projectId);

  const siblings = useMcpServers(
    source.kind === "remote"
      ? { remoteMcpServerId: source.id }
      : { tunneledMcpServerId: source.id },
    undefined,
    { throwOnError: false, enabled: !rbacLoading && !canReadEnvironments },
  );

  if (canReadEnvironments) return { locked: false, reason: null };
  if (rbacLoading || siblings.isLoading || siblings.isError || !siblings.data) {
    return { locked: true, reason: SOURCE_DESTINATION_UNKNOWN_REASON };
  }
  const linked = siblings.data.mcpServers.some(
    (server) =>
      !!server.environmentId &&
      (source.kind === "remote"
        ? server.remoteMcpServerId === source.id
        : server.tunneledMcpServerId === source.id),
  );
  return linked
    ? { locked: true, reason: SOURCE_DESTINATION_LOCK_REASON }
    : { locked: false, reason: null };
}
