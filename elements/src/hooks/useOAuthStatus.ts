import { getOAuthConfig, hasOAuthConfig } from '@/lib/auth'
import { ApiConfig, ExternalOAuthConfig } from '@/types'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useMemo } from 'react'

/**
 * OAuth authentication status
 */
export type OAuthStatus =
  | 'authenticated' // User has a valid OAuth token
  | 'needs_auth' // User needs to authenticate via OAuth
  | 'disconnected' // User has disconnected/revoked OAuth
  | 'loading' // Still checking status
  | 'disabled' // OAuth is not configured

/**
 * Response from the backend OAuth status endpoint
 */
interface OAuthStatusResponse {
  status: 'authenticated' | 'needs_auth' | 'disconnected'
  provider_name?: string
  expires_at?: string
  auth_url?: string
}

/**
 * Return type for the useOAuthStatus hook
 */
export interface UseOAuthStatusResult {
  /** Current OAuth authentication status */
  status: OAuthStatus
  /** Whether the status is still being loaded */
  isLoading: boolean
  /** Error message if status check failed */
  error: string | null
  /** The OAuth provider name if authenticated */
  providerName: string | null
  /** Token expiration time if authenticated */
  expiresAt: Date | null
  /** Start the OAuth authorization flow */
  startAuthorization: () => void
  /** Disconnect OAuth (revoke token) */
  disconnect: () => Promise<void>
  /** Refresh the OAuth status */
  refresh: () => Promise<void>
  /** The OAuth configuration if present */
  config: ExternalOAuthConfig | undefined
}

/**
 * Hook to check OAuth authentication status for external MCP servers.
 *
 * This hook checks whether the user has valid OAuth credentials for the
 * configured external OAuth provider. It returns the current status and
 * provides methods to initiate authorization or disconnect.
 *
 * @example
 * ```tsx
 * const { status, startAuthorization } = useOAuthStatus({
 *   apiUrl: 'https://app.getgram.ai',
 *   auth: config.api,
 *   sessionHeaders: { 'Gram-Chat-Session': sessionToken },
 * });
 *
 * if (status === 'needs_auth') {
 *   return <button onClick={startAuthorization}>Connect OAuth</button>;
 * }
 * ```
 */
export const useOAuthStatus = ({
  apiUrl,
  auth,
  sessionHeaders,
}: {
  apiUrl: string
  auth?: ApiConfig
  sessionHeaders: Record<string, string>
}): UseOAuthStatusResult => {
  const queryClient = useQueryClient()
  const oauthConfig = useMemo(() => getOAuthConfig(auth), [auth])
  const isOAuthEnabled = hasOAuthConfig(auth)

  // Query key for caching OAuth status
  const queryKey = useMemo(
    () => ['oauthStatus', oauthConfig?.issuer, oauthConfig?.toolsetId],
    [oauthConfig?.issuer, oauthConfig?.toolsetId]
  )

  // Fetch OAuth status from the backend
  const {
    data: statusResponse,
    isLoading,
    error,
  } = useQuery<OAuthStatusResponse, Error>({
    queryKey,
    queryFn: async (): Promise<OAuthStatusResponse> => {
      if (!oauthConfig) {
        throw new Error('OAuth not configured')
      }

      const params = new URLSearchParams({
        toolset_id: oauthConfig.toolsetId,
        issuer: oauthConfig.issuer,
      })

      const response = await fetch(
        `${apiUrl}/oauth-external/status?${params.toString()}`,
        {
          method: 'GET',
          headers: {
            ...sessionHeaders,
            'Content-Type': 'application/json',
          },
        }
      )

      if (!response.ok) {
        const errorText = await response.text()
        throw new Error(`Failed to get OAuth status: ${errorText}`)
      }

      return response.json()
    },
    enabled: isOAuthEnabled && Object.keys(sessionHeaders).length > 0,
    staleTime: 30 * 1000, // Consider status stale after 30 seconds
    gcTime: 5 * 60 * 1000, // Keep in cache for 5 minutes
    refetchOnWindowFocus: true, // Refetch when user returns to tab
  })

  // Start the OAuth authorization flow
  const startAuthorization = useCallback(() => {
    if (!oauthConfig) return

    const params = new URLSearchParams({
      toolset_id: oauthConfig.toolsetId,
      issuer: oauthConfig.issuer,
    })

    if (oauthConfig.redirectUri) {
      params.set('redirect_uri', oauthConfig.redirectUri)
    } else {
      // Default to current page URL
      params.set('redirect_uri', window.location.href)
    }

    const authUrl = `${apiUrl}/oauth-external/authorize?${params.toString()}`

    // Call the callback if provided, otherwise redirect
    if (oauthConfig.onAuthRequired) {
      oauthConfig.onAuthRequired(authUrl)
    } else {
      window.location.href = authUrl
    }
  }, [apiUrl, oauthConfig])

  // Disconnect OAuth
  const disconnect = useCallback(async () => {
    if (!oauthConfig) return

    const params = new URLSearchParams({
      toolset_id: oauthConfig.toolsetId,
      issuer: oauthConfig.issuer,
    })

    const response = await fetch(
      `${apiUrl}/oauth-external/disconnect?${params.toString()}`,
      {
        method: 'DELETE',
        headers: sessionHeaders,
      }
    )

    if (!response.ok) {
      const errorText = await response.text()
      throw new Error(`Failed to disconnect OAuth: ${errorText}`)
    }

    // Invalidate the status query to trigger a refetch
    await queryClient.invalidateQueries({ queryKey })
  }, [apiUrl, oauthConfig, queryClient, queryKey, sessionHeaders])

  // Refresh the OAuth status
  const refresh = useCallback(async () => {
    await queryClient.invalidateQueries({ queryKey })
  }, [queryClient, queryKey])

  // Determine the final status
  const status: OAuthStatus = useMemo(() => {
    if (!isOAuthEnabled) return 'disabled'
    if (isLoading) return 'loading'
    if (!statusResponse) return 'needs_auth'
    return statusResponse.status
  }, [isOAuthEnabled, isLoading, statusResponse])

  // Parse expiration time if present
  const expiresAt = useMemo(() => {
    if (statusResponse?.expires_at) {
      return new Date(statusResponse.expires_at)
    }
    return null
  }, [statusResponse?.expires_at])

  // Handle auth success callback when status becomes authenticated
  // This is triggered when the OAuth callback redirects back to the app
  useMemo(() => {
    if (
      status === 'authenticated' &&
      oauthConfig?.onAuthSuccess &&
      statusResponse?.provider_name
    ) {
      oauthConfig.onAuthSuccess(statusResponse.provider_name)
    }
  }, [status, oauthConfig, statusResponse?.provider_name])

  return {
    status,
    isLoading,
    error: error?.message ?? null,
    providerName: statusResponse?.provider_name ?? null,
    expiresAt,
    startAuthorization,
    disconnect,
    refresh,
    config: oauthConfig,
  }
}
