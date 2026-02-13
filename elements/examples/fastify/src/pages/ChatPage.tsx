import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Chat, GramElementsProvider } from '@gram-ai/elements'
import type { ElementsConfig } from '@gram-ai/elements'
import '@gram-ai/elements/elements.css'

export function ChatPage() {
  const navigate = useNavigate()
  const [username, setUsername] = useState<string | null>(null)
  const [token, setToken] = useState<string | null>(null)

  useEffect(() => {
    const stored = localStorage.getItem('token')
    if (!stored) {
      navigate('/')
      return
    }

    try {
      const payload = JSON.parse(atob(stored)) as { username: string }
      setUsername(payload.username)
      setToken(stored)
    } catch {
      localStorage.removeItem('token')
      navigate('/')
    }
  }, [navigate])

  if (!username || !token) return null

  const config: ElementsConfig = {
    projectSlug: import.meta.env.VITE_GRAM_PROJECT_SLUG,
    mcp: import.meta.env.VITE_GRAM_MCP_URL,
    variant: 'standalone',
    environment: { MY_MCP_BEARER_TOKEN: token },
  }

  return (
    <div className="flex h-screen flex-col">
      <header className="flex items-center justify-between border-b px-4 py-2">
        <span className="text-sm text-gray-500">Logged in as {username}</span>
        <button
          onClick={() => {
            localStorage.removeItem('token')
            navigate('/')
          }}
          className="rounded border px-3 py-1 text-sm"
        >
          Logout
        </button>
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
