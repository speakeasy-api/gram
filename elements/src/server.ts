import { IncomingMessage, ServerResponse } from 'node:http'

const GRAM_API_URL = 'https://app.getgram.ai'

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
  if (req.method === 'POST') {
    fetch(GRAM_API_URL + '/rpc/chatSessions.create', {
      method: 'POST',
      body: JSON.stringify({
        embed_origin: options?.embedOrigin,
        user_identifier: options?.userIdentifier,
        expires_after: options?.expiresAfter,
      }),
      headers: {
        'Content-Type': 'application/json',
        'Gram-Project': 'default',
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
