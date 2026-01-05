import { IncomingMessage, ServerResponse } from 'node:http'

const GRAM_API_URL = 'https://app.getgram.ai'

type Handler = (req: IncomingMessage, res: ServerResponse) => Promise<void>

// eslint-disable-next-line @typescript-eslint/no-unused-vars, unused-imports/no-unused-vars
const handlers = ['session'] as const

type HandlerName = (typeof handlers)[number]

type ServerHandlers = Record<HandlerName, Handler>

// expose a set of http handlers that is agnostic of the underlying node.js framework
export const createElementsServerHandlers = (): ServerHandlers => {
  return {
    session: sessionHandler,
  }
}

const sessionHandler: Handler = async (req, res) => {
  if (req.method === 'POST') {
    fetch(GRAM_API_URL + '/rpc/chatSessions.create', {
      method: 'POST',
      body: JSON.stringify({
        embed_origin: 'http://localhost:6006',
        user_identifier: 'test',
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
