import { experimental_createMCPClient as createMCPClient } from '@ai-sdk/mcp'
import { createOpenRouter } from '@openrouter/ai-sdk-provider'
import { convertToModelMessages, stepCountIs, streamText, ToolSet } from 'ai'
import { IncomingMessage, ServerResponse } from 'node:http'
import { z } from 'zod'
import { MODELS } from './lib/models'
import { frontendTools } from '@assistant-ui/react-ai-sdk'

const GRAM_API_URL = 'https://app.getgram.ai'

type Handler = (req: IncomingMessage, res: ServerResponse) => Promise<void>

// eslint-disable-next-line @typescript-eslint/no-unused-vars, unused-imports/no-unused-vars
const handlers = ['chat'] as const

type HandlerName = (typeof handlers)[number]

type ServerHandlers = Record<HandlerName, Handler>

// expose a set of http handlers that is agnostic of the underlying node.js framework
export const createElementsServerHandlers = (): ServerHandlers => {
  return {
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
