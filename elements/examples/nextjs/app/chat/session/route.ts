import { createNextHandler } from '@gram-ai/elements/server/nextjs'

export const POST = createNextHandler({
  embedOrigin: process.env.EMBED_ORIGIN ?? 'http://localhost:3000',
  userIdentifier: 'user-123',
  expiresAfter: 3600,
})
