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
    queryKey: ["inspectChatSession", project.id, session.user.id],
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
