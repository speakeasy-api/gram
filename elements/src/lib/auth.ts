import { ApiKeyAuthConfig, AuthConfig } from '@/types'

/**
 * Checks if the auth config is an API key auth config
 */
export function isApiKeyAuth(
  auth: AuthConfig | undefined
): auth is ApiKeyAuthConfig {
  return !!auth && 'UNSAFE_apiKey' in auth
}
