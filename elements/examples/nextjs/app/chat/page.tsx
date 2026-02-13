'use client'

import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'
import { Chat, GramElementsProvider } from '@gram-ai/elements'
import type { ElementsConfig } from '@gram-ai/elements'
import '@gram-ai/elements/elements.css'

export default function ChatPage() {
  const router = useRouter()
  const [username, setUsername] = useState<string | null>(null)
  const [token, setToken] = useState<string | null>(null)

  useEffect(() => {
    const stored = localStorage.getItem('token')
    if (!stored) {
      router.push('/')
      return
    }

    try {
      const payload = JSON.parse(atob(stored)) as { username: string }
      setUsername(payload.username)
      setToken(stored)
    } catch {
      localStorage.removeItem('token')
      router.push('/')
    }
  }, [router])

  if (!username || !token) return null

  const config: ElementsConfig = {
    projectSlug: process.env.NEXT_PUBLIC_GRAM_PROJECT_SLUG!,
    mcp: process.env.NEXT_PUBLIC_GRAM_MCP_URL!,
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
            router.push('/')
          }}
          className="rounded border px-3 py-1 text-sm"
        >
          Logout
        </button>
      </header>
      <div className="flex-1 overflow-hidden">
        <GramElementsProvider config={config}>
          <Chat />
        </GramElementsProvider>
      </div>
    </div>
  )
}
