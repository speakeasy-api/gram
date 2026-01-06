import { IncomingMessage, ServerResponse } from 'node:http'
import { experimental_createMCPClient as createMCPClient } from '@ai-sdk/mcp'
import { createOpenRouter } from '@openrouter/ai-sdk-provider'
import { convertToModelMessages, stepCountIs, streamText, ToolSet } from 'ai'
import { frontendTools } from '@assistant-ui/react-ai-sdk'
import { z } from 'zod'
import { MODELS } from './lib/models'

const GRAM_API_URL = 'https://app.getgram.ai'

type Handler = (req: IncomingMessage, res: ServerResponse) => Promise<void>

// eslint-disable-next-line @typescript-eslint/no-unused-vars, unused-imports/no-unused-vars
const handlers = ['session', 'chat'] as const

type HandlerName = (typeof handlers)[number]

type ServerHandlers = Record<HandlerName, Handler>

// expose a set of http handlers that is agnostic of the underlying node.js framework
export const createElementsServerHandlers = (): ServerHandlers => {
  return {
    session: sessionHandler,
    chat: chatHandler,
  }
}

const configSchema = z.object({
  // currently only supports serverURL, however we will support other config shapes for this via a union type in the future
  mcp: z.union([z.string()]),
  environment: z.record(z.string(), z.unknown()).optional(),
  projectSlug: z.string(),
  model: z.enum(MODELS),
})

/**
 * @deprecated This handler is deprecated and will be removed in the near future. The
 * chat request is now performed client side directly with the Gram API
 */
const chatHandler: Handler = async (req, res) => {
  if (req.method === 'POST') {
    try {
      const chunks: Buffer[] = []
      for await (const chunk of req) {
        chunks.push(chunk)
      }
      const body = Buffer.concat(chunks).toString()
      const { messages, config, system, tools: clientTools } = JSON.parse(body)

      const parsedConfig = configSchema.parse(config)

      const mcpClient = await createMCPClient({
        transport: {
          type: 'http',
          url: parsedConfig.mcp,
          headers: {
            ...transformEnvironmentToHeaders(parsedConfig.environment ?? {}),
            // Always send the Gram-Key header last so that it isn't clobbered by the environment variables.
            Authorization: `Bearer ${process.env.GRAM_API_KEY ?? ''}`,
          },
        },
      })

      const tools = await mcpClient.tools()

      const openRouterModel = createOpenRouter({
        baseURL: GRAM_API_URL,

        // We do not use the apiKey field as the requests are proxied through the Gram API.
        // Instead we use the Gram-Key header to authenticate the request.
        apiKey: 'must be set',
        headers: {
          'Gram-Project': parsedConfig.projectSlug,
          'Gram-Key': process.env.GRAM_API_KEY ?? '',
        },
      })

      const result = streamText({
        system,
        model: openRouterModel.chat(parsedConfig.model),
        messages: convertToModelMessages(messages),
        tools: {
          ...tools,
          ...frontendTools(clientTools),
        } as ToolSet,
        stopWhen: stepCountIs(10),
      })

      res.setHeader('Content-Type', 'text/event-stream')
      res.setHeader('Cache-Control', 'no-cache')
      res.setHeader('Connection', 'keep-alive')
      res.setHeader('Transfer-Encoding', 'chunked')

      res.statusCode = 200
      result.pipeUIMessageStreamToResponse(res, {
        sendReasoning: true,
        sendSources: true,
      })
    } catch (error) {
      res.statusCode = 500
      res.end(
        JSON.stringify({
          error: error instanceof Error ? error.message : 'Unknown error',
        })
      )
    }
  }
}

const HEADER_PREFIX = 'MCP-'

function transformEnvironmentToHeaders(environment: Record<string, unknown>) {
  if (typeof environment !== 'object' || environment === null) {
    return {}
  }
  return Object.entries(environment).reduce(
    (acc, [key, value]) => {
      // Normalize key: replace underscores with dashes
      const normalizedKey = key.replace(/_/g, '-')

      // Add MCP- prefix if it doesn't already have it
      const headerKey = normalizedKey.startsWith(HEADER_PREFIX)
        ? normalizedKey
        : `${HEADER_PREFIX}${normalizedKey}`

      acc[headerKey] = value as string
      return acc
    },
    {} as Record<string, string>
  )
}

const sessionHandler: Handler = async (req, res) => {
  if (req.method === 'POST') {
    fetch(GRAM_API_URL + '/rpc/chatSessions.create', {
      method: 'POST',
      body: JSON.stringify({
        embed_origin: 'http://localhost:6006',
        user_identifier: 'test',
      }),
      headers: {
        'Content-Type': 'application/json',
        'Gram-Project': 'default',
        'Gram-Key': process.env.GRAM_API_KEY ?? '',
      },
    })
      .then(async (response) => {
        const body = await response.text()
        res.writeHead(response.status, { 'Content-Type': 'application/json' })
        res.end(body)
      })
      .catch((error) => {
        console.error('Failed to create chat session:', error)
        res.writeHead(500, { 'Content-Type': 'application/json' })
        res.end(
          JSON.stringify({
            error: 'Failed to create chat session: ' + error.message,
          })
        )
      })
  }
}
