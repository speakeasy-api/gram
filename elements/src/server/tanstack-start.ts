/**
 * TanStack Start adapter for Gram Elements server handlers.
 *
 * Provides two approaches for session creation:
 *
 * 1. **Server Function (RPC)** — use `createTanStackStartSessionFn` to create
 *    a `createServerFn` that can be called directly from client code and passed
 *    to `sessionFn` in the Elements config.
 *
 * 2. **API Route** — use `createTanStackStartHandler` to create a handler for
 *    TanStack Start server routes (similar to the Next.js adapter).
 *
 * @example Server Function approach
 * ```typescript
 * // session.functions.ts
 * import { createTanStackStartSessionFn } from '@gram-ai/elements/server/tanstack-start'
 *
 * export const getSession = createTanStackStartSessionFn({
 *   embedOrigin: 'http://localhost:3000',
 *   userIdentifier: 'user-123',
 *   expiresAfter: 3600,
 * })
 * ```
 *
 * @example API Route approach
 * ```typescript
 * // routes/api/chat.session.ts
 * import { createTanStackStartHandler } from '@gram-ai/elements/server/tanstack-start'
 *
 * const handler = createTanStackStartHandler({
 *   embedOrigin: 'http://localhost:3000',
 *   userIdentifier: 'user-123',
 *   expiresAfter: 3600,
 * })
 * ```
 */

import { createServerFn } from '@tanstack/react-start'
import { createChatSession, type SessionHandlerOptions } from './core'

/**
 * Create a TanStack Start server function for session creation.
 *
 * The returned function can be called from client code (RPC-style) and passed
 * to `sessionFn` in the Gram Elements config.
 *
 * @param options - Session configuration options
 * @returns A `createServerFn` instance callable from the client
 */
export function createTanStackStartSessionFn(options: SessionHandlerOptions) {
  return createServerFn({ method: 'POST' })
    .inputValidator((data: { projectSlug: string }) => data)
    .handler(async ({ data }) => {
      const result = await createChatSession({
        projectSlug: data.projectSlug,
        options,
      })

      if (result.status !== 200) {
        throw new Error(`Failed to create chat session: ${result.body}`)
      }

      const parsed = JSON.parse(result.body) as { client_token: string }
      return parsed.client_token
    })
}

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
