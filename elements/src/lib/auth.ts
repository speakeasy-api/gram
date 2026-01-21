import {
  ApiConfig,
  ExternalOAuthConfig,
  OAuthApiConfig,
  SessionAuthConfig,
  StaticSessionAuthConfig,
} from '@/types'

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

/**
 * Checks if the auth config includes OAuth configuration
 */
export function hasOAuthConfig(
  auth: ApiConfig | undefined
): auth is OAuthApiConfig {
  if (!auth) return false
  return 'oauth' in auth && auth.oauth !== undefined
}

/**
 * Extracts OAuth configuration from auth config if present
 */
export function getOAuthConfig(
  auth: ApiConfig | undefined
): ExternalOAuthConfig | undefined {
  if (hasOAuthConfig(auth)) {
    return auth.oauth
  }
  return undefined
}
