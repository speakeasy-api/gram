/**
 * Bun adapter for Gram Elements server handlers.
 *
 * @example
 * ```typescript
 * import { createBunHandler } from '@gram-ai/elements/server/bun'
 *
 * const handler = createBunHandler({
 *   embedOrigin: 'http://localhost:3000',
 *   userIdentifier: 'user-123',
 *   expiresAfter: 3600,
 * })
 *
 * Bun.serve({
 *   routes: {
 *     '/chat/session': { POST: handler },
 *   },
 * })
 * ```
 */

import { createChatSession, type SessionHandlerOptions } from './core'

/**
 * Create a Bun route handler for the chat session endpoint.
 *
 * @param options - Session configuration options
 * @returns Bun route handler
 */
export function createBunHandler(
  options:
    | SessionHandlerOptions
    | ((
        request: Request
      ) => SessionHandlerOptions | Promise<SessionHandlerOptions>)
) {
  return async (request: Request) => {
    const projectSlug = request.headers.get('gram-project')

    if (!projectSlug) {
      return new Response(
        JSON.stringify({ error: 'Missing Gram-Project header' }),
        {
          status: 400,
          headers: { 'Content-Type': 'application/json' },
        }
      )
    }

    const sessionOptions =
      typeof options === 'function' ? await options(request) : options

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
