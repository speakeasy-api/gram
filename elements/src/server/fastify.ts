/**
 * Fastify adapter for Gram Elements server handlers.
 *
 * @example
 * ```typescript
 * import { createFastifyHandler } from '@gram-ai/elements/server/fastify'
 * import Fastify from 'fastify'
 *
 * const fastify = Fastify()
 *
 * fastify.post('/chat/session', createFastifyHandler({
 *   embedOrigin: 'http://localhost:3000',
 *   userIdentifier: 'user-123',
 *   expiresAfter: 3600,
 * }))
 *
 * fastify.listen({ port: 3000 })
 * ```
 */

import type { FastifyRequest, FastifyReply } from 'fastify'
import { createChatSession, type SessionHandlerOptions } from './core'

/**
 * Create a Fastify route handler for the chat session endpoint.
 *
 * @param options - Session configuration options
 * @returns Fastify route handler
 */
export function createFastifyHandler(
  options:
    | SessionHandlerOptions
    | ((
        request: FastifyRequest
      ) => SessionHandlerOptions | Promise<SessionHandlerOptions>)
) {
  return async (request: FastifyRequest, reply: FastifyReply) => {
    const projectSlug = Array.isArray(request.headers['gram-project'])
      ? request.headers['gram-project'][0]
      : request.headers['gram-project']

    if (!projectSlug) {
      reply.code(400).send({ error: 'Missing Gram-Project header' })
      return
    }

    const sessionOptions =
      typeof options === 'function' ? await options(request) : options

    const result = await createChatSession({
      projectSlug,
      options: sessionOptions,
    })

    reply
      .code(result.status)
      .headers(result.headers)
      .type('application/json')
      .send(result.body)
  }
}
