import { Hono } from 'hono'
import { cors } from 'hono/cors'
import { serve } from '@hono/node-server'
import { createHonoHandler } from '@gram-ai/elements/server/hono'

const app = new Hono()
const port = Number(process.env.PORT ?? 3001)

app.use('*', cors({ origin: 'http://localhost:3000' }))

// Stub login endpoint
app.post('/api/login', async (c) => {
  const { username, password } = await c.req.json<{
    username: string
    password: string
  }>()
  if (!username || !password) {
    return c.json({ error: 'Username and password are required' }, 401)
  }
  const token = btoa(JSON.stringify({ username, timestamp: Date.now() }))
  return c.json({ token })
})

// Chat session endpoint — uses the Hono server adapter
app.post(
  '/chat/session',
  createHonoHandler({
    embedOrigin: process.env.EMBED_ORIGIN ?? 'http://localhost:3000',
    userIdentifier: 'user-123',
    expiresAfter: 3600,
  })
)

serve({ fetch: app.fetch, port }, () => {
  console.log(`Hono server running on http://localhost:${port}`)
})
