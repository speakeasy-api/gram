/**
 * Extracts the `exp` claim from a JWT token without verifying the signature.
 * Returns the expiry as a Unix timestamp (seconds), or null if the token
 * is not a valid JWT or has no `exp` claim.
 */
export function getTokenExpiry(token: string): number | null {
  try {
    const parts = token.split('.')
    if (parts.length !== 3) return null

    // base64url → base64 → decode
    let payload = parts[1].replace(/-/g, '+').replace(/_/g, '/')
    while (payload.length % 4) payload += '='

    const json = atob(payload)
    const parsed = JSON.parse(json)

    if (typeof parsed.exp === 'number') {
      return parsed.exp
    }
    return null
  } catch {
    return null
  }
}

/**
 * Returns true when the token is expired or within `bufferMs` milliseconds
 * of expiry. Fails open (returns false) for non-JWT tokens or tokens
 * without an `exp` claim so they pass through unchanged.
 */
export function isTokenExpired(
  token: string,
  bufferMs: number = 30_000
): boolean {
  const exp = getTokenExpiry(token)
  if (exp === null) return false // fail-open for non-JWT tokens
  return Date.now() >= exp * 1000 - bufferMs
}
