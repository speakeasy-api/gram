import { useEffect, useState } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Chat, GramElementsProvider } from '@gram-ai/elements'
import type { ElementsConfig } from '@gram-ai/elements'
import '@gram-ai/elements/elements.css'
import { getSession } from '../session.functions'

export const Route = createFileRoute('/chat_/server-fn')({
  component: ChatServerFnPage,
})

function ChatServerFnPage() {
  const navigate = useNavigate()
  const [username, setUsername] = useState<string | null>(null)
  const [token, setToken] = useState<string | null>(null)

  useEffect(() => {
    const stored = localStorage.getItem('token')
    if (!stored) {
      navigate({ to: '/' })
      return
    }

    try {
      const payload = JSON.parse(atob(stored)) as { username: string }
      setUsername(payload.username)
      setToken(stored)
    } catch {
      localStorage.removeItem('token')
      navigate({ to: '/' })
    }
  }, [navigate])

  if (!username || !token) return null

  const config: ElementsConfig = {
    projectSlug: import.meta.env.VITE_GRAM_PROJECT_SLUG,
    mcp: import.meta.env.VITE_GRAM_MCP_URL,
    variant: 'standalone',
    environment: { MY_MCP_BEARER_TOKEN: token },
    api: {
      session: async ({ projectSlug }) => {
        return await getSession({ data: { projectSlug } })
      },
    },
  }

  return (
    <div className="flex h-screen flex-col">
      <header className="flex items-center justify-between border-b px-4 py-2">
        <div className="flex items-center gap-4">
          <span className="text-sm text-gray-500">Logged in as {username}</span>
          <span className="text-xs text-gray-400">(Server Function)</span>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => navigate({ to: '/chat' })}
            className="rounded border px-3 py-1 text-sm"
          >
            Try API Route
          </button>
          <button
            onClick={() => {
              localStorage.removeItem('token')
              navigate({ to: '/' })
            }}
            className="rounded border px-3 py-1 text-sm"
          >
            Logout
          </button>
        </div>
      </header>
      <div className="flex-1 overflow-hidden">
        <div className="h-full">
          <GramElementsProvider config={config}>
            <Chat />
          </GramElementsProvider>
        </div>
      </div>
    </div>
  )
}
