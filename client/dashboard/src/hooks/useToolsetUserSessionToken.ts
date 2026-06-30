import { useProject, useSession } from "@/contexts/Auth";
import { useMintUserSessionMutation } from "@gram/client/react-query/mintUserSession.js";
import { useQuery } from "@tanstack/react-query";

export interface UseToolsetUserSessionTokenResult {
  /** The minted user-session JWT, or undefined while loading / not gated. */
  accessToken: string | undefined;
  isLoading: boolean;
}

/**
 * Mints a user-session JWT scoped to a toolset, the toolset-side twin of
 * useRemoteMcpUserSessionToken: it passes `toolsetId` to the unified mint, so
 * the JWT audience binds to the toolset's `/mcp/{slug}` surface. The minted
 * token is what the playground's elements MCP client sends as
 * `Authorization: Bearer` on /mcp/{slug} requests, so the runtime gateway
 * resolves the dashboard user's stored upstream credentials through the same
 * path a real MCP client would after an OAuth dance.
 *
 * Only issuer-gated toolsets get a token; the mint RPC 400s for the rest, so we
 * gate on `isIssuerGated` and leave `accessToken` undefined otherwise.
 */
export function useToolsetUserSessionToken({
  toolsetId,
  isIssuerGated,
}: {
  toolsetId: string | undefined;
  isIssuerGated: boolean;
}): UseToolsetUserSessionTokenResult {
  const session = useSession();
  const project = useProject();
  const mintMutation = useMintUserSessionMutation();

  const enabled = !!toolsetId && isIssuerGated;

  const query = useQuery({
    queryKey: [
      "toolsetUserSessionToken",
      project.id,
      toolsetId,
      session.user.id,
    ],
    queryFn: async () => {
      if (!toolsetId) return null;
      const result = await mintMutation.mutateAsync({
        request: {
          gramProject: project.id,
          mintUserSessionRequestBody: { toolsetId },
        },
        security: {
          sessionHeaderGramSession: session.session,
          projectSlugHeaderGramProject: project.slug,
        },
      });
      return result;
    },
    enabled,
    // The minted JWT is good for ~1h; refresh well before expiry so the
    // connection always has headroom.
    staleTime: 1000 * 60 * 45,
    refetchInterval: 1000 * 60 * 45,
    refetchOnWindowFocus: false,
    retry: false,
  });

  return {
    // Gate on `enabled` so a non-issuer-gated toolset never surfaces a token
    // (stale or otherwise) that would get attached to an unauthenticated
    // connection.
    accessToken: enabled ? query.data?.accessToken : undefined,
    isLoading: enabled && query.isLoading && query.fetchStatus !== "idle",
  };
}
