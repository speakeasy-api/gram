import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'

// Mock @assistant-ui/react before importing the hook
vi.mock('@assistant-ui/react', () => ({
  unstable_RemoteThreadListAdapter: vi.fn(),
  RuntimeAdapterProvider: ({ children }: { children: ReactNode }) => children,
  useAssistantApi: vi.fn(() => ({
    threadListItem: () => ({
      getState: () => ({ remoteId: 'test-remote-id' }),
    }),
  })),
}))

// Import after mocking
import { useGramThreadListAdapter } from './useGramThreadListAdapter'

// Create wrapper with QueryClient
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

describe('useGramThreadListAdapter', () => {
  const defaultOptions = {
    apiUrl: 'https://api.example.com',
    headers: {
      'Gram-Project': 'test-project',
      'Gram-Chat-Session': 'test-session',
    },
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  describe('list()', () => {
    it('fetches and returns chat list', async () => {
      const mockChats = {
        chats: [
          {
            id: 'chat-1',
            title: 'First Chat',
            userId: 'user-1',
            numMessages: 5,
          },
          {
            id: 'chat-2',
            title: 'Second Chat',
            userId: 'user-1',
            numMessages: 3,
          },
        ],
      }

      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve(mockChats),
      })
      vi.stubGlobal('fetch', mockFetch)

      const { result } = renderHook(
        () => useGramThreadListAdapter(defaultOptions),
        { wrapper: createWrapper() }
      )

      const listResult = await result.current.list()

      expect(mockFetch).toHaveBeenCalledWith(
        'https://api.example.com/rpc/chat.list',
        {
          headers: defaultOptions.headers,
        }
      )
      expect(listResult.threads).toHaveLength(2)
      expect(listResult.threads[0]).toEqual({
        remoteId: 'chat-1',
        externalId: 'chat-1',
        status: 'regular',
        title: 'First Chat',
      })
    })

    it('returns empty threads on fetch failure', async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
      })
      vi.stubGlobal('fetch', mockFetch)

      const consoleError = vi
        .spyOn(console, 'error')
        .mockImplementation(() => {})

      const { result } = renderHook(
        () => useGramThreadListAdapter(defaultOptions),
        { wrapper: createWrapper() }
      )

      const listResult = await result.current.list()

      expect(listResult.threads).toEqual([])
      expect(consoleError).toHaveBeenCalledWith('Failed to list chats:', 500)
    })

    it('returns empty threads on network error', async () => {
      const mockFetch = vi.fn().mockRejectedValue(new Error('Network error'))
      vi.stubGlobal('fetch', mockFetch)

      const consoleError = vi
        .spyOn(console, 'error')
        .mockImplementation(() => {})

      const { result } = renderHook(
        () => useGramThreadListAdapter(defaultOptions),
        { wrapper: createWrapper() }
      )

      const listResult = await result.current.list()

      expect(listResult.threads).toEqual([])
      expect(consoleError).toHaveBeenCalled()
    })

    it('uses "New Chat" as fallback title when title is empty', async () => {
      const mockChats = {
        chats: [{ id: 'chat-1', title: '', userId: 'user-1', numMessages: 0 }],
      }

      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve(mockChats),
      })
      vi.stubGlobal('fetch', mockFetch)

      const { result } = renderHook(
        () => useGramThreadListAdapter(defaultOptions),
        { wrapper: createWrapper() }
      )

      const listResult = await result.current.list()

      expect(listResult.threads[0].title).toBe('New Chat')
    })
  })

  describe('initialize()', () => {
    it('returns thread initialization data', async () => {
      const { result } = renderHook(
        () => useGramThreadListAdapter(defaultOptions),
        { wrapper: createWrapper() }
      )

      const initResult = await result.current.initialize('thread-123')

      expect(initResult).toEqual({
        remoteId: 'thread-123',
        externalId: 'thread-123',
      })
    })
  })

  describe('fetch()', () => {
    it('fetches and returns thread details', async () => {
      const mockChat = {
        id: 'chat-123',
        title: 'Test Chat',
        messages: [],
      }

      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve(mockChat),
      })
      vi.stubGlobal('fetch', mockFetch)

      const { result } = renderHook(
        () => useGramThreadListAdapter(defaultOptions),
        { wrapper: createWrapper() }
      )

      const fetchResult = await result.current.fetch('chat-123')

      expect(mockFetch).toHaveBeenCalledWith(
        'https://api.example.com/rpc/chat.load?id=chat-123',
        {
          headers: defaultOptions.headers,
        }
      )
      expect(fetchResult).toEqual({
        remoteId: 'chat-123',
        externalId: 'chat-123',
        status: 'regular',
        title: 'Test Chat',
      })
    })

    it('returns fallback data on fetch failure', async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
      })
      vi.stubGlobal('fetch', mockFetch)

      const consoleError = vi
        .spyOn(console, 'error')
        .mockImplementation(() => {})

      const { result } = renderHook(
        () => useGramThreadListAdapter(defaultOptions),
        { wrapper: createWrapper() }
      )

      const fetchResult = await result.current.fetch('missing-thread')

      expect(fetchResult).toEqual({
        remoteId: 'missing-thread',
        status: 'regular',
      })
      expect(consoleError).toHaveBeenCalledWith('Failed to fetch thread:', 404)
    })

    it('returns fallback data on network error', async () => {
      const mockFetch = vi.fn().mockRejectedValue(new Error('Network error'))
      vi.stubGlobal('fetch', mockFetch)

      const consoleError = vi
        .spyOn(console, 'error')
        .mockImplementation(() => {})

      const { result } = renderHook(
        () => useGramThreadListAdapter(defaultOptions),
        { wrapper: createWrapper() }
      )

      const fetchResult = await result.current.fetch('error-thread')

      expect(fetchResult).toEqual({
        remoteId: 'error-thread',
        status: 'regular',
      })
      expect(consoleError).toHaveBeenCalled()
    })

    it('uses "New Chat" as fallback title when title is empty', async () => {
      const mockChat = {
        id: 'chat-123',
        title: '',
        messages: [],
      }

      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve(mockChat),
      })
      vi.stubGlobal('fetch', mockFetch)

      const { result } = renderHook(
        () => useGramThreadListAdapter(defaultOptions),
        { wrapper: createWrapper() }
      )

      const fetchResult = await result.current.fetch('chat-123')

      expect(fetchResult.title).toBe('New Chat')
    })

    it('encodes thread ID in URL', async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ id: 'special/id', title: 'Test' }),
      })
      vi.stubGlobal('fetch', mockFetch)

      const { result } = renderHook(
        () => useGramThreadListAdapter(defaultOptions),
        { wrapper: createWrapper() }
      )

      await result.current.fetch('special/id')

      expect(mockFetch).toHaveBeenCalledWith(
        'https://api.example.com/rpc/chat.load?id=special%2Fid',
        expect.any(Object)
      )
    })
  })

  describe('no-op methods', () => {
    it('rename() does nothing', async () => {
      const { result } = renderHook(
        () => useGramThreadListAdapter(defaultOptions),
        { wrapper: createWrapper() }
      )

      // Should not throw
      await expect(
        result.current.rename('id', 'new name')
      ).resolves.toBeUndefined()
    })

    it('archive() does nothing', async () => {
      const { result } = renderHook(
        () => useGramThreadListAdapter(defaultOptions),
        { wrapper: createWrapper() }
      )

      await expect(result.current.archive('id')).resolves.toBeUndefined()
    })

    it('unarchive() does nothing', async () => {
      const { result } = renderHook(
        () => useGramThreadListAdapter(defaultOptions),
        { wrapper: createWrapper() }
      )

      await expect(result.current.unarchive('id')).resolves.toBeUndefined()
    })

    it('delete() does nothing', async () => {
      const { result } = renderHook(
        () => useGramThreadListAdapter(defaultOptions),
        { wrapper: createWrapper() }
      )

      await expect(result.current.delete('id')).resolves.toBeUndefined()
    })
  })

  describe('generateTitle()', () => {
    it('returns an empty stream that closes immediately', async () => {
      const { result } = renderHook(
        () => useGramThreadListAdapter(defaultOptions),
        { wrapper: createWrapper() }
      )

      const stream = await result.current.generateTitle('thread-id', [])

      expect(stream).toBeInstanceOf(ReadableStream)

      // Verify the stream closes immediately
      const reader = stream.getReader()
      const { done, value } = await reader.read()

      expect(done).toBe(true)
      expect(value).toBeUndefined()
    })
  })

  describe('options updates', () => {
    it('uses updated options for subsequent API calls', async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ chats: [] }),
      })
      vi.stubGlobal('fetch', mockFetch)

      const initialOptions = {
        apiUrl: 'https://initial.example.com',
        headers: { 'Gram-Project': 'initial' },
      }

      const { result, rerender } = renderHook(
        (props) => useGramThreadListAdapter(props),
        {
          wrapper: createWrapper(),
          initialProps: initialOptions,
        }
      )

      // First call with initial options
      await result.current.list()

      expect(mockFetch).toHaveBeenLastCalledWith(
        'https://initial.example.com/rpc/chat.list',
        { headers: { 'Gram-Project': 'initial' } }
      )

      // Update options
      const updatedOptions = {
        apiUrl: 'https://updated.example.com',
        headers: { 'Gram-Project': 'updated' },
      }
      rerender(updatedOptions)

      // Second call should use updated options
      await result.current.list()

      expect(mockFetch).toHaveBeenLastCalledWith(
        'https://updated.example.com/rpc/chat.list',
        { headers: { 'Gram-Project': 'updated' } }
      )
    })
  })

  describe('unstable_Provider', () => {
    it('returns a Provider component', () => {
      const { result } = renderHook(
        () => useGramThreadListAdapter(defaultOptions),
        { wrapper: createWrapper() }
      )

      expect(result.current.unstable_Provider).toBeDefined()
      expect(typeof result.current.unstable_Provider).toBe('function')
    })
  })
})
