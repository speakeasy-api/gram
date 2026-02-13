import { createBunHandler } from '@gram-ai/elements/server/bun'

const port = Number(process.env.PORT ?? 3001)

const sessionHandler = createBunHandler({
  embedOrigin: process.env.EMBED_ORIGIN ?? 'http://localhost:3000',
  userIdentifier: 'user-123',
  expiresAfter: 3600,
})

const corsHeaders = {
  'Access-Control-Allow-Origin': 'http://localhost:3000',
  'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
  'Access-Control-Allow-Headers': 'Content-Type, Gram-Project',
}

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
            { status: 401, headers: corsHeaders }
          )
        }
        const token = btoa(JSON.stringify({ username, timestamp: Date.now() }))
        return Response.json({ token }, { headers: corsHeaders })
      },
    },
    '/chat/session': {
      POST: async (req) => {
        const res = await sessionHandler(req)
        // Append CORS headers to the adapter response
        const headers = new Headers(res.headers)
        for (const [key, value] of Object.entries(corsHeaders)) {
          headers.set(key, value)
        }
        return new Response(res.body, {
          status: res.status,
          headers,
        })
      },
    },
  },
  fetch(req) {
    if (req.method === 'OPTIONS') {
      return new Response(null, { status: 204, headers: corsHeaders })
    }
    return new Response('Not Found', { status: 404 })
  },
})

console.log(`Bun server running on http://localhost:${port}`)
