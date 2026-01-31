/**
 * Shared session server for integration test apps.
 *
 * Each test app imports this and starts it on a unique port.
 */
import http from 'node:http'
import { createElementsServerHandlers } from '@gram-ai/elements/server'

export function startTestServer(
  port: number,
  label: string,
  embedOrigin = 'http://localhost:5173',
) {
  const handlers = createElementsServerHandlers()

  const server = http.createServer(async (req, res) => {
    res.setHeader('Access-Control-Allow-Origin', '*')
    res.setHeader('Access-Control-Allow-Methods', 'GET, POST, OPTIONS')
    res.setHeader('Access-Control-Allow-Headers', 'Content-Type, Gram-Project')

    if (req.method === 'OPTIONS') {
      res.writeHead(204)
      res.end()
      return
    }

    if (req.url === '/chat/session' && req.method === 'POST') {
      await handlers.session(req, res, {
        embedOrigin,
        userIdentifier: `${label}-test-user`,
      })
      return
    }

    res.writeHead(404)
    res.end('Not found')
  })

  server.listen(port, () => {
    console.log(`${label} test server running on http://localhost:${port}`)
    console.log(`Session endpoint: POST http://localhost:${port}/chat/session`)
  })

  return server
}
