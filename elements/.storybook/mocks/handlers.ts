import { http, HttpResponse } from 'msw'

/**
 * MSW handlers for mocking API endpoints in Storybook.
 * These are used during visual testing (Chromatic) when no real backend is available.
 *
 * Note: MSW v2 matches regex against the FULL URL (protocol + host + path).
 * Use wildcard '*' for paths that should match any origin.
 */
export const handlers = [
  // Mock the session endpoint - returns a fake client token
  http.post('*/chat/session', () => {
    return HttpResponse.json({
      client_token: 'mock-session-token-for-storybook',
    })
  }),

  // Mock the chat sessions create endpoint (used by server.ts)
  http.post('*/rpc/chatSessions.create', () => {
    return HttpResponse.json({
      client_token: 'mock-session-token-for-storybook',
    })
  }),

  // Mock chat list endpoint for history feature
  http.get('*/rpc/chat.list', () => {
    return HttpResponse.json({
      threads: [],
    })
  }),

  // Mock chat load endpoint
  http.get('*/rpc/chat.load', () => {
    return HttpResponse.json({
      messages: [],
    })
  }),

  // Mock MCP endpoint - GET (SSE connection)
  // Matches: https://chat.speakeasy.com/mcp/speakeasy-team-my_api
  http.get(/^https:\/\/chat\.speakeasy\.com\/mcp\/.*$/, () => {
    // Return empty SSE stream
    return new HttpResponse(null, {
      status: 200,
      headers: {
        'Content-Type': 'text/event-stream',
        'Cache-Control': 'no-cache',
        Connection: 'keep-alive',
      },
    })
  }),

  // Mock MCP endpoint - POST (JSON-RPC requests)
  // Matches: https://chat.speakeasy.com/mcp/speakeasy-team-my_api
  http.post(
    /^https:\/\/chat\.speakeasy\.com\/mcp\/.*$/,
    async ({ request }) => {
      const body = (await request.json()) as { method?: string; id?: number }

      // Handle MCP initialize request
      if (body.method === 'initialize') {
        return HttpResponse.json({
          jsonrpc: '2.0',
          id: body.id,
          result: {
            protocolVersion: '2025-06-18',
            capabilities: {},
            serverInfo: {
              name: 'mock-mcp-server',
              version: '1.0.0',
            },
          },
        })
      }

      // Handle tools/list request
      if (body.method === 'tools/list') {
        return HttpResponse.json({
          jsonrpc: '2.0',
          id: body.id,
          result: {
            tools: [],
          },
        })
      }

      // Default response for other methods
      return HttpResponse.json({
        jsonrpc: '2.0',
        id: body.id,
        result: {},
      })
    }
  ),

  // Mock chat completions endpoint - matches any host (localhost, production, etc.)
  // Returns SSE streaming format for AI SDK compatibility
  http.post(/\/chat\/completions$/, async ({ request }) => {
    const body = (await request.json()) as { stream?: boolean }

    // If not streaming, return regular JSON response
    if (!body.stream) {
      return HttpResponse.json({
        id: 'mock-completion',
        object: 'chat.completion',
        created: Math.floor(Date.now() / 1000),
        model: 'mock-model',
        choices: [
          {
            index: 0,
            message: {
              role: 'assistant',
              content: 'This is a mock response for visual testing.',
            },
            finish_reason: 'stop',
          },
        ],
      })
    }

    // Streaming response for AI SDK
    const completionId = `chatcmpl-mock-${Date.now()}`
    const created = Math.floor(Date.now() / 1000)
    const content = 'This is a mock response for visual testing.'

    // Build SSE chunks
    const chunks = [
      // Initial chunk with role
      `data: ${JSON.stringify({
        id: completionId,
        object: 'chat.completion.chunk',
        created,
        model: 'mock-model',
        choices: [
          { index: 0, delta: { role: 'assistant' }, finish_reason: null },
        ],
      })}\n\n`,
      // Content chunk
      `data: ${JSON.stringify({
        id: completionId,
        object: 'chat.completion.chunk',
        created,
        model: 'mock-model',
        choices: [{ index: 0, delta: { content }, finish_reason: null }],
      })}\n\n`,
      // Final chunk
      `data: ${JSON.stringify({
        id: completionId,
        object: 'chat.completion.chunk',
        created,
        model: 'mock-model',
        choices: [{ index: 0, delta: {}, finish_reason: 'stop' }],
      })}\n\n`,
      'data: [DONE]\n\n',
    ]

    const stream = new ReadableStream({
      start(controller) {
        for (const chunk of chunks) {
          controller.enqueue(new TextEncoder().encode(chunk))
        }
        controller.close()
      },
    })

    return new HttpResponse(stream, {
      status: 200,
      headers: {
        'Content-Type': 'text/event-stream',
        'Cache-Control': 'no-cache',
        Connection: 'keep-alive',
      },
    })
  }),
]
