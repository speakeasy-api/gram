import Fastify from 'fastify'
import cors from '@fastify/cors'
import { createFastifyHandler } from '@gram-ai/elements/server/fastify'

const fastify = Fastify({ logger: true })
const port = Number(process.env.PORT ?? 3001)

await fastify.register(cors, { origin: 'http://localhost:3000' })

// Stub login endpoint
fastify.post('/api/login', async (request, reply) => {
  const { username, password } = request.body as {
    username: string
    password: string
  }
  if (!username || !password) {
    return reply.code(401).send({ error: 'Username and password are required' })
  }
  const token = btoa(JSON.stringify({ username, timestamp: Date.now() }))
  return reply.send({ token })
})

// Chat session endpoint — uses the Fastify server adapter
fastify.post(
  '/chat/session',
  createFastifyHandler({
    embedOrigin: process.env.EMBED_ORIGIN ?? 'http://localhost:3000',
    userIdentifier: 'user-123',
    expiresAfter: 3600,
  })
)

fastify.listen({ port }, (err) => {
  if (err) throw err
})
