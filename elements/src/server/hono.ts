/**
 * Hono adapter for Gram Elements server handlers.
 *
 * @example
 * ```typescript
 * import { createHonoHandler } from '@gram-ai/elements/server/hono'
 * import { Hono } from 'hono'
 *
 * const app = new Hono()
 *
 * app.post('/chat/session', createHonoHandler({
 *   embedOrigin: 'http://localhost:3000',
 *   userIdentifier: 'user-123',
 *   expiresAfter: 3600,
 * }))
 *
 * export default app
 * ```
 */

import type { Context } from 'hono'
import { createChatSession, type SessionHandlerOptions } from './core'

/**
 * Create a Hono route handler for the chat session endpoint.
 *
 * @param options - Session configuration options
 * @returns Hono route handler
 */
export function createHonoHandler(
  options:
    | SessionHandlerOptions
    | ((c: Context) => SessionHandlerOptions | Promise<SessionHandlerOptions>)
) {
  return async (c: Context) => {
    const projectSlug = c.req.header('gram-project')

    if (!projectSlug) {
      return c.json({ error: 'Missing Gram-Project header' }, 400)
    }

    const sessionOptions =
      typeof options === 'function' ? await options(c) : options

    const result = await createChatSession({
      projectSlug,
      options: sessionOptions,
    })

    return new Response(result.body, {
      status: result.status,
      headers: result.headers,
    })
  }
}
