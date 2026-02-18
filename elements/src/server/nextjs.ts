/**
 * Next.js App Router adapter for Gram Elements server handlers.
 *
 * @example
 * ```typescript
 * // app/api/chat/session/route.ts
 * import { createNextHandler } from '@gram-ai/elements/server/nextjs'
 *
 * export const POST = createNextHandler({
 *   embedOrigin: 'http://localhost:3000',
 *   userIdentifier: 'user-123',
 *   expiresAfter: 3600,
 * })
 * ```
 */

import { createChatSession, type SessionHandlerOptions } from './core'

/**
 * Create a Next.js App Router route handler for the chat session endpoint.
 *
 * @param options - Session configuration options
 * @returns Next.js route handler
 */
export function createNextHandler(
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
