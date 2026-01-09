import { IncomingMessage, ServerResponse } from 'node:http'

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

export const createElementsServerHandlers = (): ServerHandlers => {
  return {
    session: sessionHandler,
  }
}

interface SessionHandlerOptions {
  /**
   * The origin from which the token will be used
   */
  embedOrigin: string

  /**
   * Free-form user identifier
   */
  userIdentifier: string

  /**
   * Token expiration in seconds (max / default 3600)
   * @default 3600
   */
  expiresAfter?: number
}

const sessionHandler: ServerHandler<SessionHandlerOptions> = async (
  req,
  res,
  options
) => {
  const base = process.env.GRAM_API_URL ?? 'https://app.getgram.ai'
  if (req.method === 'POST') {
    const projectSlug = Array.isArray(req.headers['gram-project'])
      ? req.headers['gram-project'][0]
      : req.headers['gram-project']

    fetch(base + '/rpc/chatSessions.create', {
      method: 'POST',
      body: JSON.stringify({
        embed_origin: options?.embedOrigin,
        user_identifier: options?.userIdentifier,
        expires_after: options?.expiresAfter,
      }),
      headers: {
        'Content-Type': 'application/json',
        'Gram-Project': typeof projectSlug === 'string' ? projectSlug : '',
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
