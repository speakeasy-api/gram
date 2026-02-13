import { createFileRoute } from '@tanstack/react-router'
import { createTanStackStartHandler } from '@gram-ai/elements/server/tanstack-start'

const handler = createTanStackStartHandler({
  embedOrigin: import.meta.env.VITE_EMBED_ORIGIN ?? 'http://localhost:3000',
  userIdentifier: 'user-123',
  expiresAfter: 3600,
})

export const Route = createFileRoute('/api/chat/session')({
  server: {
    handlers: {
      POST: async ({ request }) => handler(request),
    },
  },
})
