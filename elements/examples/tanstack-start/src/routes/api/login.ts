import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/api/login')({
  server: {
    handlers: {
      POST: async ({ request }) => {
        const { username, password } = (await request.json()) as {
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
  },
})
