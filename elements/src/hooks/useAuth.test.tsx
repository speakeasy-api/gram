import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useAuth } from './useAuth'
import type { ApiConfig } from '@/types'
import type { ReactNode } from 'react'

// Create a fresh QueryClient for each test
function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    )
  }
}

describe('useAuth', () => {
  beforeEach(() => {
    vi.resetAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  describe('with static session token', () => {
    it('returns headers immediately without loading state', async () => {
      const auth: ApiConfig = {
        sessionToken: 'static-token-123',
      }

      const { result } = renderHook(
        () => useAuth({ projectSlug: 'test-project', auth }),
        { wrapper: createWrapper() }
      )

      // With static token, should never be in loading state
      await waitFor(() => {
        expect(result.current.isLoading).toBe(false)
      })

      expect(result.current.headers).toEqual({
        'Gram-Project': 'test-project',
        'Gram-Chat-Session': 'static-token-123',
      })
    })

    it('uses the provided static token directly', async () => {
      const auth: ApiConfig = {
        sessionToken: 'my-custom-token',
      }

      const { result } = renderHook(
        () => useAuth({ projectSlug: 'my-project', auth }),
        { wrapper: createWrapper() }
      )

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false)
      })

      expect(result.current.headers?.['Gram-Chat-Session']).toBe(
        'my-custom-token'
      )
    })
  })

  describe('with custom session function', () => {
    it('calls the custom sessionFn and returns headers', async () => {
      const sessionFn = vi.fn().mockResolvedValue('custom-session-token')
      const auth: ApiConfig = {
        sessionFn,
      }

      const { result } = renderHook(
        () => useAuth({ projectSlug: 'test-project', auth }),
        { wrapper: createWrapper() }
      )

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false)
      })

      expect(sessionFn).toHaveBeenCalledWith({ projectSlug: 'test-project' })
      expect(result.current.headers).toEqual({
        'Gram-Project': 'test-project',
        'Gram-Chat-Session': 'custom-session-token',
      })
    })

    it('passes projectSlug to the sessionFn', async () => {
      const sessionFn = vi
        .fn()
        .mockImplementation(({ projectSlug }) => `token-for-${projectSlug}`)
      const auth: ApiConfig = {
        sessionFn,
      }

      const { result } = renderHook(
        () => useAuth({ projectSlug: 'special-project', auth }),
        { wrapper: createWrapper() }
      )

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false)
      })

      expect(result.current.headers?.['Gram-Chat-Session']).toBe(
        'token-for-special-project'
      )
    })
  })

  describe('with default session fetcher', () => {
    it('fetches session from /chat/session when no auth config provided', async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        json: () => Promise.resolve({ client_token: 'fetched-token' }),
      })
      vi.stubGlobal('fetch', mockFetch)

      const { result } = renderHook(
        () => useAuth({ projectSlug: 'test-project', auth: undefined }),
        { wrapper: createWrapper() }
      )

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false)
      })

      expect(mockFetch).toHaveBeenCalledWith('/chat/session', {
        method: 'POST',
        headers: {
          'Gram-Project': 'test-project',
        },
      })
      expect(result.current.headers).toEqual({
        'Gram-Project': 'test-project',
        'Gram-Chat-Session': 'fetched-token',
      })
    })

    it('uses default fetcher when auth has only url', async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        json: () => Promise.resolve({ client_token: 'default-token' }),
      })
      vi.stubGlobal('fetch', mockFetch)

      const auth: ApiConfig = {
        url: 'https://api.example.com',
      }

      const { result } = renderHook(
        () => useAuth({ projectSlug: 'my-project', auth }),
        { wrapper: createWrapper() }
      )

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false)
      })

      expect(mockFetch).toHaveBeenCalledWith('/chat/session', {
        method: 'POST',
        headers: {
          'Gram-Project': 'my-project',
        },
      })
    })
  })

  describe('loading state', () => {
    it('starts in loading state when fetching session', async () => {
      // Create a deferred promise to control when the fetch resolves
      let resolveSession: (value: string) => void
      const sessionPromise = new Promise<string>((resolve) => {
        resolveSession = resolve
      })

      const sessionFn = vi.fn().mockReturnValue(sessionPromise)
      const auth: ApiConfig = {
        sessionFn,
      }

      const { result } = renderHook(
        () => useAuth({ projectSlug: 'test-project', auth }),
        { wrapper: createWrapper() }
      )

      // Should be loading initially
      expect(result.current.isLoading).toBe(true)
      expect(result.current.headers).toBeUndefined()

      // Resolve the session
      resolveSession!('resolved-token')

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false)
      })

      expect(result.current.headers?.['Gram-Chat-Session']).toBe(
        'resolved-token'
      )
    })
  })

  describe('header format', () => {
    it('always includes Gram-Project header', async () => {
      const auth: ApiConfig = {
        sessionToken: 'token',
      }

      const { result } = renderHook(
        () => useAuth({ projectSlug: 'my-project-slug', auth }),
        { wrapper: createWrapper() }
      )

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false)
      })

      expect(result.current.headers?.['Gram-Project']).toBe('my-project-slug')
    })

    it('always includes Gram-Chat-Session header with the token', async () => {
      const auth: ApiConfig = {
        sessionToken: 'my-session-token',
      }

      const { result } = renderHook(
        () => useAuth({ projectSlug: 'project', auth }),
        { wrapper: createWrapper() }
      )

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false)
      })

      expect(result.current.headers?.['Gram-Chat-Session']).toBe(
        'my-session-token'
      )
    })
  })
})
