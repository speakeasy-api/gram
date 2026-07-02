import { useProject, useSession } from "@/contexts/Auth";
import { useMintUserSessionMutation } from "@gram/client/react-query/mintUserSession.js";
import { useQuery } from "@tanstack/react-query";

export interface UseRemoteMcpUserSessionTokenResult {
  /** The minted user-session JWT, or undefined while loading / not gated. */
  accessToken: string | undefined;
  isLoading: boolean;
}

/**
 * Mints a user-session JWT scoped to a remote MCP server. The token matches
 * what /x/mcp/{slug}/token would emit after an OAuth dance, so the runtime
 * gateway resolves the dashboard user's stored upstream credentials through
 * the same path a real MCP client would.
 *
 * Minting persists a user_sessions row visible in the server's "User sessions"
 * panel, so mounting this hook is the gate: render it only for users who have
 * already connected (see RemoteMcpConnection), never from a component that
 * mounts merely because a page was viewed.
 */
export function useRemoteMcpUserSessionToken({
  mcpServerId,
}: {
  mcpServerId: string | undefined;
}): UseRemoteMcpUserSessionTokenResult {
  const session = useSession();
  const project = useProject();
  const mintMutation = useMintUserSessionMutation();

  const enabled = !!mcpServerId;

  const query = useQuery({
    queryKey: [
      "remoteMcpUserSessionToken",
      project.id,
      mcpServerId,
      session.user.id,
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
    accessToken: enabled ? query.data?.accessToken : undefined,
    isLoading: enabled && query.isLoading && query.fetchStatus !== "idle",
  };
}
