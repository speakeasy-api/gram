import { createFileRoute, useRouter } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { Chat, GramElementsProvider } from '@gram-ai/elements'
import type { ElementsConfig } from '@gram-ai/elements'
import { Button } from '@/components/ui/button'
import '@gram-ai/elements/elements.css'

export const Route = createFileRoute('/chat')({ component: ChatPage })

/**
 * Chat page — embeds Gram Elements and passes the user's credential to the
 * MCP server. The token stored during login is forwarded via the `environment`
 * config field, so the end-user never has to manage it directly.
 */
function ChatPage() {
  const router = useRouter()
  const [username, setUsername] = useState<string | null>(null)
  const [token, setToken] = useState<string | null>(null)

  // Guard: redirect to login if there's no token in localStorage.
  useEffect(() => {
    const stored = localStorage.getItem('token')
    if (!stored) {
      router.navigate({ to: '/' })
      return
    }

    try {
      const payload = JSON.parse(atob(stored)) as { username: string }
      setUsername(payload.username)
      setToken(stored)
    } catch {
      localStorage.removeItem('token')
      router.navigate({ to: '/' })
    }
  }, [router])

  function handleLogout() {
    localStorage.removeItem('token')
    router.navigate({ to: '/' })
  }

  if (!username || !token) return null

  const config: ElementsConfig = {
    projectSlug: import.meta.env.VITE_GRAM_PROJECT_SLUG,

    // session obtains a short-lived client token from our server-side proxy
    // (/api/chat/session), which in turn calls Gram's session API using our
    // secret GRAM_API_KEY. This keeps the API key off the client.
    api: {
      session: async () => {
        const request = new Request('/api/chat/session', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            embed_origin: import.meta.env.DEV
              ? 'http://localhost:3000'
              : 'https://my-app.com',
            user_identifier: 'some-user-uuid', // optional
            expires_after: 60 * 60 * 24, // optional, in seconds
          }),
        })

        const response = await fetch(request)

        if (!response.ok) {
          throw new Error('Failed to create chat session')
        }

        const json: { client_token: string } = await response.json()
        return json.client_token
      },
    },

    mcp: import.meta.env.VITE_GRAM_MCP_URL,

    // 'standalone' renders a full-page chat UI (as opposed to 'widget' or 'sidecar').
    variant: 'standalone',

    // This is the key integration point: `environment` values are forwarded to the
    // MCP server as headers. The env var name (MY_MCP_BEARER_TOKEN) must match what
    // is configured on the MCP server's settings page in the Gram dashboard.
    environment: { MY_MCP_BEARER_TOKEN: token },
  }

  return (
    <div className="flex h-screen flex-col">
      <header className="flex items-center justify-between border-b px-4 py-2">
        <span className="text-sm text-muted-foreground">
          Logged in as {username}
        </span>
        <Button variant="outline" size="sm" onClick={handleLogout}>
          Logout
        </Button>
      </header>
      <div className="flex-1 overflow-hidden">
        <GramElementsProvider config={config}>
          <Chat />
        </GramElementsProvider>
      </div>
    </div>
  )
}
