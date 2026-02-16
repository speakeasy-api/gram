import { useState } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'

export const Route = createFileRoute('/')({
  component: LoginPage,
})

function LoginPage() {
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setLoading(true)

    try {
      const res = await fetch('/api/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      })

      const data = await res.json()

      if (!res.ok) {
        setError(data.error || 'Login failed')
        return
      }

      localStorage.setItem('token', data.token)
      navigate({ to: '/chat' })
    } catch {
      setError('Something went wrong. Please try again.')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center">
      <form onSubmit={handleSubmit} className="flex w-72 flex-col gap-3">
        <h1 className="text-xl font-semibold">Sign in</h1>
        <input
          type="text"
          placeholder="Username"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          required
          className="rounded border px-3 py-2"
        />
        <input
          type="password"
          placeholder="Password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
          className="rounded border px-3 py-2"
        />
        {error && <p className="text-sm text-red-600">{error}</p>}
        <button
          type="submit"
          disabled={loading}
          className="rounded bg-black px-3 py-2 text-white disabled:opacity-50"
        >
          {loading ? 'Signing in...' : 'Sign in'}
        </button>
      </form>
    </div>
  )
}
