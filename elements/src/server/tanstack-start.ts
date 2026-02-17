/**
 * TanStack Start adapter for Gram Elements server handlers.
 *
 * Use `createTanStackStartHandler` to create a handler for TanStack Start
 * server routes that manages chat session creation.
 *
 * @example
 * ```typescript
 * // routes/api/chat.session.ts
 * import { createTanStackStartHandler } from '@gram-ai/elements/server/tanstack-start'
 *
 * export const POST = createTanStackStartHandler({
 *   embedOrigin: 'http://localhost:3000',
 *   userIdentifier: 'user-123',
 *   expiresAfter: 3600,
 * })
 * ```
 *
 * @example Dynamic configuration
 * ```typescript
 * // routes/api/chat.session.ts
 * export const POST = createTanStackStartHandler(async (request) => {
 *   const user = await getUserFromRequest(request)
 *   return {
 *     embedOrigin: 'http://localhost:3000',
 *     userIdentifier: user.id,
 *     expiresAfter: 3600,
 *   }
 * })
 * ```
 */

import { createChatSession, type SessionHandlerOptions } from './core'

/**
 * Create a TanStack Start server route handler for the chat session endpoint.
 *
 * Returns a function compatible with TanStack Start server route handlers
 * that accepts a Web API `Request` and returns a `Response`.
 *
 * @param options - Session configuration options
 * @returns Handler function `(request: Request) => Promise<Response>`
 */
export function createTanStackStartHandler(
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
