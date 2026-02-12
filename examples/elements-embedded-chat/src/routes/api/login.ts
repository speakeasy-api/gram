import { createFileRoute } from '@tanstack/react-router'
import { json } from '@tanstack/react-start'

/**
 * Stub login endpoint. In a real app, replace this with your actual auth
 * provider (OAuth callback, credential validation, etc). The token returned
 * here gets stored in localStorage and eventually passed to the MCP server
 * via the GramElementsProvider `environment` config.
 */
export const Route = createFileRoute('/api/login')({
  server: {
    handlers: {
      POST: async ({ request }: { request: Request }) => {
        const body = await request.json()
        const { username, password } = body as {
          username: string
          password: string
        }

        if (!username || !password) {
          return json(
            { error: 'Username and password are required' },
            { status: 401 },
          )
        }

        const token = btoa(JSON.stringify({ username, timestamp: Date.now() }))
        return json({ token })
      },
    },
  },
})
