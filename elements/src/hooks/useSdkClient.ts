import { getApiUrl } from '@/lib/api'
import { GramCore } from '@gram/client/core'
import { HTTPClient } from '@gram/client/lib/http.js'
import { useMemo } from 'react'
import { useAuth } from './useAuth'
import { useElements } from './useElements'

/**
 * Hook to create and configure the SDK client with authentication
 * @returns SDK client instance and security configuration
 */
export const useSdkClient = (): GramCore => {
  const { config } = useElements()
  const auth = useAuth({
    projectSlug: config.projectSlug,
    auth: config.api,
  })

  // Create SDK client with server URL
  const client = useMemo(() => {
    const apiUrl = getApiUrl(config)

    const httpClient = new HTTPClient({
      fetcher: (request) => {
        const newRequest = new Request(request, {
          credentials: 'include',
        })

        for (const [key, value] of Object.entries(auth.headers ?? {})) {
          newRequest.headers.set(key, value)
        }

        return fetch(newRequest)
      },
    })

    return new GramCore({
      serverURL: apiUrl,
      httpClient,
    })
  }, [config.api, auth.headers])

  return client
}
