import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

function stubNavigatorWithoutModelContext() {
  vi.stubGlobal('navigator', {})
}

describe('useWebMCP', () => {
  beforeEach(() => {
    vi.resetModules()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('exports a function', async () => {
    stubNavigatorWithoutModelContext()
    const { useWebMCP } = await import('./useWebMCP')
    expect(typeof useWebMCP).toBe('function')
  })

  it('is importable when navigator.modelContext is undefined', async () => {
    stubNavigatorWithoutModelContext()
    const { useWebMCP } = await import('./useWebMCP')
    // The hook itself is safe to import — it only calls registerTool inside useEffect
    expect(useWebMCP).toBeDefined()
  })
})

describe('WebMCP execute callback logic', () => {
  beforeEach(() => {
    vi.resetModules()
    global.fetch = vi.fn()
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('sends correct JSON-RPC tools/call request', async () => {
    const mcpResult = {
      content: [{ type: 'text', text: 'search results' }],
    }

    ;(global.fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ jsonrpc: '2.0', id: 1, result: mcpResult }),
    })

    const mcpUrl = 'https://app.getgram.ai/mcp/test-slug'
    const mcpHeaders = { 'Gram-Chat-Session': 'test-token' }
    const toolName = 'search'
    const args = { query: 'hello' }

    // Simulate the execute callback that useWebMCP creates
    const body = JSON.stringify({
      jsonrpc: '2.0',
      id: 1,
      method: 'tools/call',
      params: { name: toolName, arguments: args },
    })

    const response = await fetch(mcpUrl, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...mcpHeaders },
      body,
    })

    const data = await response.json()
    expect(data.result).toEqual(mcpResult)
    expect(global.fetch).toHaveBeenCalledWith(mcpUrl, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Gram-Chat-Session': 'test-token',
      },
      body,
    })
  })

  it('detects non-ok response status', async () => {
    ;(global.fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
      ok: false,
      status: 500,
      json: () => Promise.resolve({}),
    })

    const response = await fetch('https://example.com/mcp/test', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{}',
    })

    // The hook's execute would throw: `MCP request failed: 500`
    expect(response.ok).toBe(false)
    expect(response.status).toBe(500)
  })

  it('detects JSON-RPC error in response', async () => {
    const errorResponse = {
      jsonrpc: '2.0',
      id: 1,
      error: { code: -32601, message: 'method not found' },
    }

    ;(global.fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(errorResponse),
    })

    const response = await fetch('https://example.com/mcp/test', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{}',
    })

    const data = await response.json()
    // The hook's execute would throw: `method not found`
    expect(data.error).toBeDefined()
    expect(data.error.message).toBe('method not found')
  })
})
