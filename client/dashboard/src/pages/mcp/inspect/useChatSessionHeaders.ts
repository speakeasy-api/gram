import { useProject, useSession } from "@/contexts/Auth";
import { useChatSessionsCreateMutation } from "@gram/client/react-query/chatSessionsCreate.js";
import { useQuery } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";

const TOKEN_REFRESH_MS = 45 * 60 * 1000;

/**
 * Headers that let the dashboard call a hosted MCP server as the signed-in
 * user, the way the chat does: a short-lived chat session token plus the
 * project. Hosted servers without a sign-in issuer accept these instead of a
 * user-session JWT.
 */
export function useChatSessionHeaders(enabled: boolean): {
  headers: Record<string, string> | undefined;
  isLoading: boolean;
  error: Error | null;
  refetch: () => void;
} {
  const session = useSession();
  const project = useProject();
  const createSession = useChatSessionsCreateMutation();

  const query = useQuery({
    queryKey: [
      "inspectChatSession",
      project.id,
      session.user.id,
      sessionKey(session.session),
    ],
    enabled,
    // The token lives for an hour (expiresAfter below). Mint a new one well
    // before then so a tab left open never sends an expired token; a tab
    // that was hidden past the interval refetches on focus once stale.
    staleTime: TOKEN_REFRESH_MS,
    refetchInterval: TOKEN_REFRESH_MS,
    retry: false,
    throwOnError: false,
    queryFn: async () => {
      const result = await createSession.mutateAsync({
        request: {
          gramProject: project.id,
          createRequestBody: {
            embedOrigin: window.location.origin,
            expiresAfter: 3600,
            userIdentifier: session.user.id,
          },
        },
        security: {
          option1: {
            sessionHeaderGramSession: session.session,
            projectSlugHeaderGramProject: project.slug,
          },
        },
      });
      return result.clientToken;
    },
  });

  const headers = useMemo(
    () =>
      query.data
        ? { "Gram-Chat-Session": query.data, "Gram-Project": project.slug }
        : undefined,
    [query.data, project.slug],
  );

  const { refetch } = query;
  const retry = useCallback(() => void refetch(), [refetch]);

  return {
    headers,
    isLoading: enabled && query.isLoading,
    error: query.error,
    refetch: retry,
  };
}

/**
 * A short, non-reversible key for the auth session, so a new sign-in by the
 * same user mints a fresh chat token without putting the session in the cache.
 */
function sessionKey(value: string): string {
  // FNV-1a, 32-bit.
  let hash = 0x811c9dc5;
  for (let i = 0; i < value.length; i++) {
    hash ^= value.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193);
  }
  return (hash >>> 0).toString(36);
}
