import { getApiUrl } from '@/lib/api'
import { GramCore } from '@gram/client/core'
import { useMemo } from 'react'
import { useAuth } from './useAuth'
import { useElements } from './useElements'

interface SdkClientResult {
  client: GramCore
  options: {
    headers?: Record<string, string>
  }
}

/**
 * Hook to create and configure the SDK client with authentication
 * @returns SDK client instance and security configuration
 */
export const useSdkClient = (): SdkClientResult => {
  const { config } = useElements()
  const auth = useAuth({
    projectSlug: config.projectSlug,
    auth: config.api,
  })

  // Create SDK client with server URL
  const client = useMemo(() => {
    const apiUrl = getApiUrl(config)

    return new GramCore({
      serverURL: apiUrl,
    })
  }, [config.api])

  return { client, options: { headers: auth.headers } }
}
