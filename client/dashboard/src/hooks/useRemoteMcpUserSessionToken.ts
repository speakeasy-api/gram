import { useProject, useSession } from "@/contexts/Auth";
import { useMintUserSessionMutation } from "@gram/client/react-query/mintUserSession.js";
import { useQuery } from "@tanstack/react-query";

export interface UseRemoteMcpUserSessionTokenResult {
  /** The minted user-session JWT, or undefined while loading / not gated. */
  accessToken: string | undefined;
  isLoading: boolean;
}

/**
 * Mints a user-session JWT scoped to a remote MCP server, mirroring the
 * playground (see PlaygroundElements.tsx) but passing `mcpServerId` to the
 * unified mint: remote MCP servers are mcp_servers-backed and carry no toolset,
 * so the JWT audience binds to the server's user_session_issuer (the /x/mcp
 * convention).
 * The minted token matches what /x/mcp/{slug}/token would emit after an OAuth
 * dance, so the runtime gateway resolves the dashboard user's stored upstream
 * credentials through the same path a real MCP client would.
 *
 * Only issuer-gated servers get a token; the mint RPC 400s for the rest, so we
 * leave `accessToken` undefined and let the connection proceed unauthenticated.
 */
export function useRemoteMcpUserSessionToken({
  mcpServerId,
  isIssuerGated,
  userSessionIssuerId,
}: {
  mcpServerId: string | undefined;
  isIssuerGated: boolean;
  /**
   * Explicit issuer id, or undefined when implicitly gated. Part of the
   * query key so issuer changes drop cached JWTs with a stale audience.
   */
  userSessionIssuerId: string | undefined;
}): UseRemoteMcpUserSessionTokenResult {
  const session = useSession();
  const project = useProject();
  const mintMutation = useMintUserSessionMutation();

  const enabled = !!mcpServerId && isIssuerGated;

  const query = useQuery({
    queryKey: [
      "remoteMcpUserSessionToken",
      project.id,
      mcpServerId,
      session.user.id,
      userSessionIssuerId ?? "implicit",
    ],
    queryFn: async () => {
      if (!mcpServerId) return null;
      const result = await mintMutation.mutateAsync({
        request: {
          gramProject: project.id,
          mintUserSessionRequestBody: { mcpServerId },
        },
        security: {
          sessionHeaderGramSession: session.session,
          projectSlugHeaderGramProject: project.slug,
        },
      });
      return result;
    },
    enabled,
    // The minted JWT is good for ~1h; refresh well before expiry so the tools
    // connection always has headroom.
    staleTime: 1000 * 60 * 45,
    refetchInterval: 1000 * 60 * 45,
    refetchOnWindowFocus: false,
    retry: false,
  });

  return {
    // Gate on `enabled` so a non-issuer-gated server never surfaces a token
    // (stale or otherwise) that would get attached to an unauthenticated
    // connection.
    accessToken: enabled ? query.data?.accessToken : undefined,
    isLoading: enabled && query.isLoading && query.fetchStatus !== "idle",
  };
}
