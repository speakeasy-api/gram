import { ApiKeyAuthConfig, ApiConfig, SessionAuthConfig } from '@/types'

/**
 * Checks if the auth config is an API key auth config
 */
export function isApiKeyAuth(
  auth: ApiConfig | undefined
): auth is ApiKeyAuthConfig {
  return !!auth && 'UNSAFE_apiKey' in auth
}

export function hasExplicitSessionAuth(
  auth: ApiConfig | undefined
): auth is SessionAuthConfig {
  if (!auth) return false
  return 'sessionFn' in auth
}
