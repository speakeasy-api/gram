import { useProject, useSession } from "@/contexts/Auth";
import { useMintUserSessionMutation } from "@gram/client/react-query/mintUserSession.js";
import type { MintUserSessionRequestBody } from "@gram/client/models/components/mintusersessionrequestbody.js";
import { useQuery } from "@tanstack/react-query";

/**
 * The resource a minted JWT's audience binds to. The unified mint RPC takes
 * exactly one of these:
 *
 * - `toolset` binds to the toolset's `/mcp/{slug}` surface.
 * - `mcpServer` binds to the server's user_session_issuer (the /x/mcp
 *   convention) — proxied MCP servers are mcp_servers-backed and carry no
 *   toolset.
 */
export type UserSessionTokenTarget =
  | { kind: "toolset"; id: string | undefined }
  | { kind: "mcpServer"; id: string | undefined };

export interface UseUserSessionTokenResult {
  /** The minted user-session JWT, or undefined while loading / not gated. */
  accessToken: string | undefined;
  isLoading: boolean;
}

function mintRequestBody(
  target: UserSessionTokenTarget,
): MintUserSessionRequestBody {
  switch (target.kind) {
    case "toolset":
      return { toolsetId: target.id };
    case "mcpServer":
      return { mcpServerId: target.id };
  }
}

/**
 * Mints a user-session JWT scoped to a toolset or a proxied MCP server. The
 * minted token is what the dashboard's MCP clients send as
 * `Authorization: Bearer` on the target's MCP surface, so the runtime gateway
 * resolves the dashboard user's stored upstream credentials through the same
 * path a real MCP client would after an OAuth dance — for remote MCP servers
 * it matches what /x/mcp/{slug}/token would emit.
 *
 * Only issuer-gated targets get a token; the mint RPC 400s for the rest, so we
 * gate on `isIssuerGated` and leave `accessToken` undefined otherwise, letting
 * the connection proceed unauthenticated.
 *
 * Consumers sharing a target share the query key, so React Query dedupes the
 * mint across them — e.g. the playground's auth panel and chat surface issue a
 * single request between them.
 */
export function useUserSessionToken({
  target,
  isIssuerGated,
}: {
  target: UserSessionTokenTarget;
  isIssuerGated: boolean;
}): UseUserSessionTokenResult {
  const session = useSession();
  const project = useProject();
  const mintMutation = useMintUserSessionMutation();

  const { kind, id } = target;
  const enabled = !!id && isIssuerGated;

  const query = useQuery({
    queryKey: ["userSessionToken", kind, project.id, id, session.user.id],
    queryFn: async () => {
      if (!id) return null;
      const result = await mintMutation.mutateAsync({
        request: {
          gramProject: project.id,
          mintUserSessionRequestBody: mintRequestBody(target),
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
    // Gate on `enabled` so a non-issuer-gated target never surfaces a token
    // (stale or otherwise) that would get attached to an unauthenticated
    // connection.
    accessToken: enabled ? query.data?.accessToken : undefined,
    isLoading: enabled && query.isLoading && query.fetchStatus !== "idle",
  };
}
