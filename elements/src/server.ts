import { IncomingMessage, ServerResponse } from 'node:http'
import { createChatSession, type SessionHandlerOptions } from './server/core'

type ServerHandler<T> = (
  req: IncomingMessage,
  res: ServerResponse,
  options?: T
) => Promise<void>

interface ServerHandlers {
  /**
   * Handler to create a new chat session token.
   *
   * @example
   * ```typescript
   * import { createElementsServerHandlers } from '@gram-ai/elements/server'
   * import express from 'express'
   * const app = express()
   * const handlers = createElementsServerHandlers()
   * app.post('/chat/session', handlers.session)
   * app.listen(3000)
   * ```
   */
  session: ServerHandler<SessionHandlerOptions>
}

/**
 * @deprecated Use framework-specific adapters instead:
 * - `@gram-ai/elements/server/express` for Express
 * - `@gram-ai/elements/server/nextjs` for Next.js App Router
 * - `@gram-ai/elements/server/fastify` for Fastify
 * - `@gram-ai/elements/server/hono` for Hono
 */
export const createElementsServerHandlers = (): ServerHandlers => {
  return {
    session: sessionHandler,
  }
}

export type { SessionHandlerOptions }

const sessionHandler: ServerHandler<SessionHandlerOptions> = async (
  req,
  res,
  options
) => {
  if (req.method !== 'POST') {
    res.writeHead(405, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify({ error: 'Method not allowed' }))
    return
  }

  const projectSlug = Array.isArray(req.headers['gram-project'])
    ? req.headers['gram-project'][0]
    : req.headers['gram-project']

  if (!projectSlug || typeof projectSlug !== 'string') {
    res.writeHead(400, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify({ error: 'Missing Gram-Project header' }))
    return
  }

  if (!options) {
    res.writeHead(400, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify({ error: 'Missing session options' }))
    return
  }

  const result = await createChatSession({
    projectSlug,
    options,
  })

  res.writeHead(result.status, result.headers)
  res.end(result.body)
}
