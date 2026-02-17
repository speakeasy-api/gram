import { describe, expect, it } from 'vitest'
import { getTokenExpiry, isTokenExpired } from './token'

/** Helper: build a JWT with a given payload (no signature verification needed). */
function makeJwt(payload: Record<string, unknown>): string {
  const header = btoa(JSON.stringify({ alg: 'HS256', typ: 'JWT' }))
  const body = btoa(JSON.stringify(payload))
  return `${header}.${body}.fake-signature`
}

describe('getTokenExpiry', () => {
  it('returns exp for a valid JWT', () => {
    const exp = 1700000000
    expect(getTokenExpiry(makeJwt({ exp }))).toBe(exp)
  })

  it('returns null when exp is missing', () => {
    expect(getTokenExpiry(makeJwt({ sub: 'user' }))).toBeNull()
  })

  it('returns null for a non-JWT string', () => {
    expect(getTokenExpiry('not-a-jwt')).toBeNull()
  })

  it('returns null for an empty string', () => {
    expect(getTokenExpiry('')).toBeNull()
  })

  it('returns null when payload is not valid JSON', () => {
    // Two dots but the middle segment is not valid base64 JSON
    expect(getTokenExpiry('a.!!!.b')).toBeNull()
  })

  it('handles base64url characters (- and _)', () => {
    // Manually craft a payload with base64url-specific chars
    const payload = { exp: 1700000000 }
    const json = JSON.stringify(payload)
    const b64 = btoa(json)
      .replace(/\+/g, '-')
      .replace(/\//g, '_')
      .replace(/=+$/, '')
    const token = `header.${b64}.sig`
    expect(getTokenExpiry(token)).toBe(1700000000)
  })
})

describe('isTokenExpired', () => {
  it('returns true for an expired token', () => {
    // exp = 1 second ago
    const exp = Math.floor(Date.now() / 1000) - 1
    expect(isTokenExpired(makeJwt({ exp }))).toBe(true)
  })

  it('returns true when token is within the buffer window', () => {
    // exp = 20 seconds from now (within 30s default buffer)
    const exp = Math.floor(Date.now() / 1000) + 20
    expect(isTokenExpired(makeJwt({ exp }))).toBe(true)
  })

  it('returns false when token is well outside the buffer', () => {
    // exp = 5 minutes from now
    const exp = Math.floor(Date.now() / 1000) + 300
    expect(isTokenExpired(makeJwt({ exp }))).toBe(false)
  })

  it('respects a custom buffer', () => {
    // exp = 20 seconds from now, buffer = 10s → should NOT be expired
    const exp = Math.floor(Date.now() / 1000) + 20
    expect(isTokenExpired(makeJwt({ exp }), 10_000)).toBe(false)
  })

  it('returns false (fail-open) for a non-JWT string', () => {
    expect(isTokenExpired('opaque-session-token')).toBe(false)
  })

  it('returns false (fail-open) for an empty string', () => {
    expect(isTokenExpired('')).toBe(false)
  })
})
