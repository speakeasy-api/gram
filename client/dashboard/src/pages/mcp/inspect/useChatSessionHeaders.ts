import { useProject, useSession } from "@/contexts/Auth";
import { useChatSessionsCreateMutation } from "@gram/client/react-query/chatSessionsCreate.js";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

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
} {
  const session = useSession();
  const project = useProject();
  const createSession = useChatSessionsCreateMutation();

  const query = useQuery({
    queryKey: ["inspectChatSession", project.id, session.user.id],
    enabled,
    staleTime: 30 * 60 * 1000,
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

  return {
    headers,
    isLoading: enabled && query.isLoading,
    error: query.error,
  };
}
