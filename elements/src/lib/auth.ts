import {
  ApiConfig,
  BaseApiConfig,
  DangerousApiKeyAuthConfig,
  GetSessionFn,
  SessionAuthConfig,
  StaticSessionAuthConfig,
  UnifiedSessionAuthConfig,
} from '@/types'

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

/** @deprecated Legacy check for `{ sessionToken }` configs. */
export function isStaticSessionAuth(
  auth: ApiConfig | undefined
): auth is StaticSessionAuthConfig {
  return !!auth && 'sessionToken' in auth
}

/** @deprecated Legacy check for `{ sessionFn }` configs. */
export function hasExplicitSessionAuth(
  auth: ApiConfig | undefined
): auth is SessionAuthConfig {
  if (!auth) return false
  return 'sessionFn' in auth
}

/** Returns true when either the legacy `sessionToken` or the unified static `session` string is used. */
export function isAnyStaticSession(auth: ApiConfig | undefined): boolean {
  return isStaticSessionAuth(auth) || isUnifiedStaticSession(auth)
}
