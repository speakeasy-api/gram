import {
  ApiConfig,
  BaseApiConfig,
  DangerousApiKeyAuthConfig,
  GetSessionFn,
  SessionAuthConfig,
  StaticSessionAuthConfig,
  UnifiedSessionAuthConfig,
} from '@/types'

/**
 * Checks if the auth config uses a static session token (legacy).
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

export function isDangerousApiKeyAuth(
  auth: ApiConfig | undefined
): auth is DangerousApiKeyAuthConfig {
  return !!auth && 'dangerousApiKey' in auth
}

export function isUnifiedStaticSession(
  auth: ApiConfig | undefined
): auth is UnifiedSessionAuthConfig & { session: string } {
  return !!auth && 'session' in auth && typeof auth.session === 'string'
}

export function isUnifiedFunctionSession(
  auth: ApiConfig | undefined
): auth is BaseApiConfig & { session: GetSessionFn } {
  return !!auth && 'session' in auth && typeof auth.session === 'function'
}

export function isAnyStaticSession(auth: ApiConfig | undefined): boolean {
  return isStaticSessionAuth(auth) || isUnifiedStaticSession(auth)
}
