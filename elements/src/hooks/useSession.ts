import { GetSessionFn } from '@/types'
import { useQuery, useQueryClient } from '@tanstack/react-query'

export function getChatSessionQueryKey(projectSlug: string) {
  return ['chatSession', projectSlug] as const
}

/**
 * Hook to fetch or retrieve the session token for the chat.
 * @returns The session token string or null
 */
export const useSession = ({
  getSession,
  projectSlug,
}: {
  getSession: GetSessionFn | null
  projectSlug: string
}): string | null => {
  const queryClient = useQueryClient()
  const queryKey = getChatSessionQueryKey(projectSlug)

  const queryState = queryClient.getQueryState(queryKey)
  const hasData = queryState?.data !== undefined
  // Check if data is stale - with staleTime: Infinity, data never becomes stale
  // but we check dataUpdatedAt to determine if we should refetch
  const dataUpdatedAt = queryState?.dataUpdatedAt ?? 0
  const staleTime = Infinity // Matches the staleTime in useQuery options
  const isStale = hasData && Date.now() - dataUpdatedAt > staleTime
  const shouldFetch = !hasData || isStale

  const { data: fetchedSessionToken } = useQuery({
    queryKey,
    queryFn: () => getSession!({ projectSlug }),
    enabled: shouldFetch && getSession !== null,
    staleTime: Infinity, // Session tokens don't need to be refetched
    gcTime: Infinity, // Keep in cache indefinitely
  })

  return fetchedSessionToken ?? null
}
