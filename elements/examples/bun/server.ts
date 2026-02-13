import { createBunHandler } from '@gram-ai/elements/server/bun'

const port = Number(process.env.PORT ?? 3001)

const sessionHandler = createBunHandler({
  embedOrigin: process.env.EMBED_ORIGIN ?? 'http://localhost:3000',
  userIdentifier: 'user-123',
  expiresAfter: 3600,
})

Bun.serve({
  port,
  routes: {
    '/api/login': {
      POST: async (req) => {
        const { username, password } = (await req.json()) as {
          username: string
          password: string
        }
        if (!username || !password) {
          return Response.json(
            { error: 'Username and password are required' },
            { status: 401 }
          )
        }
        const token = btoa(JSON.stringify({ username, timestamp: Date.now() }))
        return Response.json({ token })
      },
    },
    '/chat/session': {
      POST: sessionHandler,
    },
  },
  fetch() {
    return new Response('Not Found', { status: 404 })
  },
})

console.log(`Bun server running on http://localhost:${port}`)
