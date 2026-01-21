import { getOAuthConfig, hasOAuthConfig } from '@/lib/auth'
import { ApiConfig } from '@/types'
import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'

/**
 * Response from the backend OAuth token endpoint
 */
interface OAuthTokenResponse {
  access_token: string
  token_type: string
  expires_at?: string
  scope?: string
}

/**
 * Return type for the useOAuthToken hook
 */
export interface UseOAuthTokenResult {
  /** The OAuth access token (only available when authenticated) */
  accessToken: string | null
  /** Token type (e.g., 'Bearer') */
  tokenType: string | null
  /** Whether the token is being fetched */
  isLoading: boolean
  /** Error message if token fetch failed */
  error: string | null
  /** Token expiration time */
  expiresAt: Date | null
  /** OAuth scopes granted */
  scope: string | null
  /** Refetch the token */
  refetch: () => Promise<void>
}

/**
 * Hook to fetch the OAuth access token for authenticated users.
 *
 * This hook retrieves the decrypted OAuth access token from the backend,
 * which can be used for making authenticated requests to external APIs.
 *
 * **Note:** For MCP tool execution, you typically don't need this hook
 * as the backend automatically retrieves and uses the OAuth token.
 * This hook is useful when you need to make direct authenticated
 * requests from the frontend or display token information.
 *
 * @example
 * ```tsx
 * const { accessToken, isLoading, error } = useOAuthToken({
 *   apiUrl: 'https://app.getgram.ai',
 *   auth: config.api,
 *   sessionHeaders: { 'Gram-Chat-Session': sessionToken },
 * });
 *
 * if (accessToken) {
 *   // Use token for authenticated API calls
 *   fetch('https://api.example.com/data', {
 *     headers: { Authorization: `Bearer ${accessToken}` }
 *   });
 * }
 * ```
 */
export const useOAuthToken = ({
  apiUrl,
  auth,
  sessionHeaders,
  enabled = true,
}: {
  apiUrl: string
  auth?: ApiConfig
  sessionHeaders: Record<string, string>
  /** Whether to fetch the token. Defaults to true. */
  enabled?: boolean
}): UseOAuthTokenResult => {
  const oauthConfig = useMemo(() => getOAuthConfig(auth), [auth])
  const isOAuthEnabled = hasOAuthConfig(auth)

  // Query key for caching OAuth token
  const queryKey = useMemo(
    () => ['oauthToken', oauthConfig?.issuer, oauthConfig?.toolsetId],
    [oauthConfig?.issuer, oauthConfig?.toolsetId]
  )

  // Fetch OAuth token from the backend
  const {
    data: tokenResponse,
    isLoading,
    error,
    refetch,
  } = useQuery<OAuthTokenResponse, Error>({
    queryKey,
    queryFn: async (): Promise<OAuthTokenResponse> => {
      if (!oauthConfig) {
        throw new Error('OAuth not configured')
      }

      const params = new URLSearchParams({
        toolset_id: oauthConfig.toolsetId,
        issuer: oauthConfig.issuer,
      })

      const response = await fetch(
        `${apiUrl}/oauth-external/token?${params.toString()}`,
        {
          method: 'GET',
          headers: {
            ...sessionHeaders,
            'Content-Type': 'application/json',
          },
        }
      )

      if (!response.ok) {
        if (response.status === 401 || response.status === 404) {
          throw new Error('Not authenticated')
        }
        const errorText = await response.text()
        throw new Error(`Failed to get OAuth token: ${errorText}`)
      }

      return response.json()
    },
    enabled:
      enabled &&
      isOAuthEnabled &&
      Object.keys(sessionHeaders).length > 0,
    staleTime: 5 * 60 * 1000, // Token considered stale after 5 minutes
    gcTime: 10 * 60 * 1000, // Keep in cache for 10 minutes
  })

  // Parse expiration time if present
  const expiresAt = useMemo(() => {
    if (tokenResponse?.expires_at) {
      return new Date(tokenResponse.expires_at)
    }
    return null
  }, [tokenResponse?.expires_at])

  const handleRefetch = async () => {
    await refetch()
  }

  return {
    accessToken: tokenResponse?.access_token ?? null,
    tokenType: tokenResponse?.token_type ?? null,
    isLoading,
    error: error?.message ?? null,
    expiresAt,
    scope: tokenResponse?.scope ?? null,
    refetch: handleRefetch,
  }
}
