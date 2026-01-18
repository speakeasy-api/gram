import { describe, expect, it } from 'vitest'
import { isStaticSessionAuth, hasExplicitSessionAuth } from './auth'
import type { ApiConfig } from '@/types'

describe('isStaticSessionAuth', () => {
  it('returns true when auth has sessionToken', () => {
    const auth: ApiConfig = {
      sessionToken: 'test-token-123',
    }

    expect(isStaticSessionAuth(auth)).toBe(true)
  })

  it('returns false when auth has sessionFn instead', () => {
    const auth: ApiConfig = {
      sessionFn: async () => 'dynamic-token',
    }

    expect(isStaticSessionAuth(auth)).toBe(false)
  })

  it('returns false when auth is undefined', () => {
    expect(isStaticSessionAuth(undefined)).toBe(false)
  })

  it('returns false when auth has only url', () => {
    const auth: ApiConfig = {
      url: 'https://api.example.com',
    }

    expect(isStaticSessionAuth(auth)).toBe(false)
  })

  it('returns true when auth has both url and sessionToken', () => {
    const auth: ApiConfig = {
      url: 'https://api.example.com',
      sessionToken: 'test-token',
    }

    expect(isStaticSessionAuth(auth)).toBe(true)
  })
})

describe('hasExplicitSessionAuth', () => {
  it('returns true when auth has sessionFn', () => {
    const auth: ApiConfig = {
      sessionFn: async () => 'dynamic-token',
    }

    expect(hasExplicitSessionAuth(auth)).toBe(true)
  })

  it('returns false when auth has sessionToken instead', () => {
    const auth: ApiConfig = {
      sessionToken: 'static-token',
    }

    expect(hasExplicitSessionAuth(auth)).toBe(false)
  })

  it('returns false when auth is undefined', () => {
    expect(hasExplicitSessionAuth(undefined)).toBe(false)
  })

  it('returns false when auth has only url', () => {
    const auth: ApiConfig = {
      url: 'https://api.example.com',
    }

    expect(hasExplicitSessionAuth(auth)).toBe(false)
  })

  it('returns true when auth has both url and sessionFn', () => {
    const auth: ApiConfig = {
      url: 'https://api.example.com',
      sessionFn: async ({ projectSlug }) => `token-for-${projectSlug}`,
    }

    expect(hasExplicitSessionAuth(auth)).toBe(true)
  })
})

describe('auth config type guards work together', () => {
  it('static session and explicit session are mutually exclusive', () => {
    const staticAuth: ApiConfig = { sessionToken: 'token' }
    const explicitAuth: ApiConfig = { sessionFn: async () => 'token' }
    const noAuth: ApiConfig = { url: 'https://api.example.com' }

    // Static session auth
    expect(isStaticSessionAuth(staticAuth)).toBe(true)
    expect(hasExplicitSessionAuth(staticAuth)).toBe(false)

    // Explicit session auth
    expect(isStaticSessionAuth(explicitAuth)).toBe(false)
    expect(hasExplicitSessionAuth(explicitAuth)).toBe(true)

    // No specific auth (uses default)
    expect(isStaticSessionAuth(noAuth)).toBe(false)
    expect(hasExplicitSessionAuth(noAuth)).toBe(false)
  })

  it('undefined auth falls back to default behavior', () => {
    expect(isStaticSessionAuth(undefined)).toBe(false)
    expect(hasExplicitSessionAuth(undefined)).toBe(false)
  })
})
