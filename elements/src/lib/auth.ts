import { ApiConfig, SessionAuthConfig, StaticSessionAuthConfig } from '@/types'

/**
 * Checks if the auth config is an API key auth config
 */
export function isStaticSessionAuth(
  auth: ApiConfig | undefined
): auth is StaticSessionAuthConfig {
  return !!auth && 'sessionToken' in auth
}

export function hasExplicitSessionAuth(
  auth: ApiConfig | undefined
): auth is SessionAuthConfig {
  if (!auth) return false
  return 'sessionFn' in auth
}
