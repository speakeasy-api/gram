/**
 * Express adapter for Gram Elements server handlers.
 *
 * @example
 * ```typescript
 * import { createExpressHandler } from '@gram-ai/elements/server/express'
 * import express from 'express'
 *
 * const app = express()
 * app.use(express.json())
 *
 * app.post('/chat/session', createExpressHandler({
 *   embedOrigin: 'http://localhost:3000',
 *   userIdentifier: 'user-123',
 *   expiresAfter: 3600,
 * }))
 *
 * app.listen(3000)
 * ```
 */

import type { Request, Response } from 'express'
import { createChatSession, type SessionHandlerOptions } from './core'

/**
 * Create an Express request handler for the chat session endpoint.
 *
 * @param options - Session configuration options
 * @returns Express request handler
 */
export function createExpressHandler(
  options:
    | SessionHandlerOptions
    | ((
        req: Request
      ) => SessionHandlerOptions | Promise<SessionHandlerOptions>)
) {
  return async (req: Request, res: Response) => {
    const projectSlug = Array.isArray(req.headers['gram-project'])
      ? req.headers['gram-project'][0]
      : req.headers['gram-project']

    if (!projectSlug) {
      res.status(400).json({ error: 'Missing Gram-Project header' })
      return
    }

    const sessionOptions =
      typeof options === 'function' ? await options(req) : options

    const result = await createChatSession({
      projectSlug,
      options: sessionOptions,
    })

    res.status(result.status)
    Object.entries(result.headers).forEach(([key, value]) => {
      res.setHeader(key, value)
    })
    res.send(result.body)
  }
}
