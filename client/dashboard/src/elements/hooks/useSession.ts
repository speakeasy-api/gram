import { GetSessionFn } from "@/elements/types";
import { useQuery, useQueryClient } from "@tanstack/react-query";

export function getChatSessionQueryKey(
  projectSlug: string,
): readonly ["chatSession", string] {
  return ["chatSession", projectSlug] as const;
}

/**
 * Hook to fetch or retrieve the session token for the chat.
 * @returns The session token string or null
 */
export const useSession = ({
  getSession,
  projectSlug,
}: {
  getSession: GetSessionFn | null;
  projectSlug: string;
}): string | null => {
  const queryClient = useQueryClient();
  const queryKey = getChatSessionQueryKey(projectSlug);

  const hasData = queryClient.getQueryState(queryKey)?.data !== undefined;

  const { data: fetchedSessionToken } = useQuery({
    queryKey,
    queryFn: () => getSession!({ projectSlug }),
    enabled: !hasData && getSession !== null,
    staleTime: Infinity, // Session tokens don't need to be refetched
    gcTime: Infinity, // Keep in cache indefinitely
    // A host QueryClient may default to throwing; a missing token is handled
    // inline (null), not by an error boundary.
    throwOnError: false,
  });

  return fetchedSessionToken ?? null;
};
