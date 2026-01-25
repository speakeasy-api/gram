import { http, HttpResponse } from 'msw'

/**
 * MSW handlers for mocking API endpoints in Storybook.
 * These are used during visual testing (Chromatic) and Playwright tests
 * when no real backend is available.
 *
 * Note: MSW v2 matches regex against the FULL URL (protocol + host + path).
 * Use wildcard '*' for paths that should match any origin.
 */

// Vega chart spec for testing chart rendering
const vegaChartSpec = {
  $schema: 'https://vega.github.io/schema/vega/v5.json',
  width: 400,
  height: 200,
  padding: 5,
  data: [
    {
      name: 'table',
      values: [
        { category: 'USA', amount: 22000 },
        { category: 'Canada', amount: 16000 },
        { category: 'Mexico', amount: 10000 },
      ],
    },
  ],
  scales: [
    {
      name: 'xscale',
      type: 'band',
      domain: { data: 'table', field: 'category' },
      range: 'width',
      padding: 0.05,
    },
    {
      name: 'yscale',
      domain: { data: 'table', field: 'amount' },
      nice: true,
      range: 'height',
    },
  ],
  axes: [
    { orient: 'bottom', scale: 'xscale' },
    { orient: 'left', scale: 'yscale' },
  ],
  marks: [
    {
      type: 'rect',
      from: { data: 'table' },
      encode: {
        enter: {
          x: { scale: 'xscale', field: 'category' },
          width: { scale: 'xscale', band: 1 },
          y: { scale: 'yscale', field: 'amount' },
          y2: { scale: 'yscale', value: 0 },
        },
        update: { fill: { value: 'steelblue' } },
        hover: { fill: { value: 'red' } },
      },
    },
  ],
}

/**
 * Creates SSE streaming response chunks for chat completions.
 * This helper enables testing different response scenarios.
 */
function createStreamingResponse(options: {
  content?: string
  toolCalls?: Array<{
    id: string
    name: string
    arguments: Record<string, unknown>
  }>
}) {
  const completionId = `chatcmpl-mock-${Date.now()}`
  const created = Math.floor(Date.now() / 1000)
  const chunks: string[] = []

  // Initial chunk with role
  chunks.push(
    `data: ${JSON.stringify({
      id: completionId,
      object: 'chat.completion.chunk',
      created,
      model: 'mock-model',
      choices: [{ index: 0, delta: { role: 'assistant' }, finish_reason: null }],
    })}\n\n`
  )

  // Tool calls if provided
  if (options.toolCalls?.length) {
    for (const tool of options.toolCalls) {
      chunks.push(
        `data: ${JSON.stringify({
          id: completionId,
          object: 'chat.completion.chunk',
          created,
          model: 'mock-model',
          choices: [
            {
              index: 0,
              delta: {
                tool_calls: [
                  {
                    index: 0,
                    id: tool.id,
                    type: 'function',
                    function: {
                      name: tool.name,
                      arguments: JSON.stringify(tool.arguments),
                    },
                  },
                ],
              },
              finish_reason: null,
            },
          ],
        })}\n\n`
      )
    }
  }

  // Content chunk if provided
  if (options.content) {
    chunks.push(
      `data: ${JSON.stringify({
        id: completionId,
        object: 'chat.completion.chunk',
        created,
        model: 'mock-model',
        choices: [
          { index: 0, delta: { content: options.content }, finish_reason: null },
        ],
      })}\n\n`
    )
  }

  // Final chunk
  chunks.push(
    `data: ${JSON.stringify({
      id: completionId,
      object: 'chat.completion.chunk',
      created,
      model: 'mock-model',
      choices: [
        {
          index: 0,
          delta: {},
          finish_reason: options.toolCalls?.length ? 'tool_calls' : 'stop',
        },
      ],
    })}\n\n`,
    'data: [DONE]\n\n'
  )

  return chunks
}

/**
 * Analyzes the user's message to determine what kind of response to generate.
 * This enables smart mocking without LLM costs.
 */
function analyzePromptForResponse(messages: Array<{ role: string; content: string }>) {
  const lastUserMessage = messages
    .filter((m) => m.role === 'user')
    .pop()
    ?.content?.toLowerCase() || ''

  // Chart-related prompts
  if (
    lastUserMessage.includes('chart') ||
    lastUserMessage.includes('visualize') ||
    lastUserMessage.includes('gdp')
  ) {
    return {
      content: `Here's a bar chart showing the data:\n\n\`\`\`vega\n${JSON.stringify(vegaChartSpec, null, 2)}\n\`\`\``,
    }
  }

  // Tool calling prompts - salutation
  if (lastUserMessage.includes('salutation') || lastUserMessage.includes('greeting')) {
    return {
      toolCalls: [
        {
          id: `call_${Date.now()}`,
          name: 'kitchen_sink_get_salutation',
          arguments: {},
        },
      ],
    }
  }

  // Tool calling prompts - card details
  if (lastUserMessage.includes('card') && lastUserMessage.includes('details')) {
    return {
      toolCalls: [
        {
          id: `call_${Date.now()}`,
          name: 'kitchen_sink_get_get_card_details',
          arguments: { queryParameters: { cardNumber: '4532 •••• •••• 1234' } },
        },
      ],
    }
  }

  // Tool calling prompts - fetch URL
  if (lastUserMessage.includes('fetch') && lastUserMessage.includes('http')) {
    const urlMatch = lastUserMessage.match(/https?:\/\/[^\s]+/)
    return {
      toolCalls: [
        {
          id: `call_${Date.now()}`,
          name: 'fetchUrl',
          arguments: { url: urlMatch?.[0] || 'https://httpbin.org/html' },
        },
      ],
    }
  }

  // Tool calling prompts - delete file
  if (lastUserMessage.includes('delete') && lastUserMessage.includes('file')) {
    const idMatch = lastUserMessage.match(/id\s*(\d+)/i)
    return {
      toolCalls: [
        {
          id: `call_${Date.now()}`,
          name: 'deleteFile',
          arguments: { fileId: idMatch?.[1] || '123' },
        },
      ],
    }
  }

  // Tool calling prompts - call both tools
  if (lastUserMessage.includes('call both')) {
    return {
      toolCalls: [
        {
          id: `call_${Date.now()}_1`,
          name: 'kitchen_sink_get_salutation',
          arguments: {},
        },
        {
          id: `call_${Date.now()}_2`,
          name: 'kitchen_sink_get_get_card_details',
          arguments: { queryParameters: { cardNumber: '4532 •••• •••• 1234' } },
        },
      ],
    }
  }

  // Default response
  return {
    content: 'This is a mock response for visual testing.',
  }
}

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

      // Handle tools/list request - return mock tools for testing
      if (body.method === 'tools/list') {
        return HttpResponse.json({
          jsonrpc: '2.0',
          id: body.id,
          result: {
            tools: [
              {
                name: 'kitchen_sink_get_salutation',
                description: 'Get a friendly greeting',
                inputSchema: {
                  type: 'object',
                  properties: {},
                },
              },
              {
                name: 'kitchen_sink_get_get_card_details',
                description: 'Get card details including PIN',
                inputSchema: {
                  type: 'object',
                  properties: {
                    queryParameters: {
                      type: 'object',
                      properties: {
                        cardNumber: { type: 'string' },
                      },
                    },
                  },
                },
              },
              {
                name: 'kitchen_sink_post_create_user',
                description: 'Create a new user',
                inputSchema: {
                  type: 'object',
                  properties: {
                    name: { type: 'string' },
                    email: { type: 'string' },
                  },
                },
              },
            ],
          },
        })
      }

      // Handle tools/call request - return mock results
      if (body.method === 'tools/call') {
        const params = (body as { params?: { name?: string } }).params
        const toolName = params?.name

        if (toolName === 'kitchen_sink_get_salutation') {
          return HttpResponse.json({
            jsonrpc: '2.0',
            id: body.id,
            result: {
              content: [{ type: 'text', text: JSON.stringify({ greeting: 'Hello, welcome!' }) }],
            },
          })
        }

        if (toolName === 'kitchen_sink_get_get_card_details') {
          return HttpResponse.json({
            jsonrpc: '2.0',
            id: body.id,
            result: {
              content: [{ type: 'text', text: JSON.stringify({ pin: '4321' }) }],
            },
          })
        }

        return HttpResponse.json({
          jsonrpc: '2.0',
          id: body.id,
          result: {
            content: [{ type: 'text', text: '{"status": "success"}' }],
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
  // Uses smart prompt analysis to generate appropriate mock responses
  http.post(/\/chat\/completions$/, async ({ request }) => {
    const body = (await request.json()) as {
      stream?: boolean
      messages?: Array<{ role: string; content: string }>
    }

    // Analyze the prompt to determine appropriate response
    const responseConfig = analyzePromptForResponse(body.messages || [])

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
              content:
                responseConfig.content ||
                'This is a mock response for visual testing.',
            },
            finish_reason: 'stop',
          },
        ],
      })
    }

    // Streaming response using the helper
    const chunks = createStreamingResponse(responseConfig)

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

  // Mock tool execution results (for when tools are called)
  http.post('*/tools/call', async ({ request }) => {
    const body = (await request.json()) as { toolName?: string }

    // Return mock tool results based on tool name
    if (body.toolName === 'kitchen_sink_get_salutation') {
      return HttpResponse.json({
        content: [{ type: 'text', text: JSON.stringify({ greeting: 'Hello!' }) }],
      })
    }

    if (body.toolName === 'kitchen_sink_get_get_card_details') {
      return HttpResponse.json({
        content: [{ type: 'text', text: JSON.stringify({ pin: '1234' }) }],
      })
    }

    return HttpResponse.json({
      content: [{ type: 'text', text: '{"result": "success"}' }],
    })
  }),
]
