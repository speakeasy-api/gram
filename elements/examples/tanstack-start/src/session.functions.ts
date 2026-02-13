import { createTanStackStartSessionFn } from '@gram-ai/elements/server/tanstack-start'

export const getSession = createTanStackStartSessionFn({
  embedOrigin: import.meta.env.VITE_EMBED_ORIGIN ?? 'http://localhost:3000',
  userIdentifier: 'user-123',
  expiresAfter: 3600,
})
