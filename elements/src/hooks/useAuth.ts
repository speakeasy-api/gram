import { useReplayContext } from '@/contexts/ReplayContext'
import { hasExplicitSessionAuth, isStaticSessionAuth } from '@/lib/auth'
import { useMemo } from 'react'
import { ApiConfig } from '../types'
import { useSession } from './useSession'

export type Auth =
  | {
      headers: Record<string, string>
      isLoading: false
    }
  | {
      headers?: Record<string, string>
      isLoading: true
    }

async function defaultGetSession(init: {
  projectSlug: string
}): Promise<string> {
  const response = await fetch('/chat/session', {
    method: 'POST',
    headers: {
      'Gram-Project': init.projectSlug,
    },
    body: JSON.stringify({
      embedOrigin: window.location.origin,
    }),
  })
  const data = await response.json()
  return data.client_token
}

/**
 * Hook to fetch or retrieve the session token for the chat.
 * @returns The session token string or null
 */
export const useAuth = ({
  projectSlug,
  auth,
}: {
  auth?: ApiConfig
  projectSlug: string
}): Auth => {
  const replayCtx = useReplayContext()
  const isReplay = replayCtx?.isReplay ?? false

  const getSession = useMemo(() => {
    // In replay mode, skip session fetching entirely
    if (isReplay) {
      return null
    }
    if (isStaticSessionAuth(auth)) {
      return () => Promise.resolve(auth.sessionToken)
    }
    return !isStaticSessionAuth(auth) && hasExplicitSessionAuth(auth)
      ? auth.sessionFn
      : defaultGetSession
  }, [auth, isReplay])

  // The session request is only neccessary if we are not using static session auth
  // configuration. If a custom session fetcher is provided, we use it,
  // otherwise we fallback to the default session fetcher
  const session = useSession({
    getSession,
    projectSlug,
  })

  // In replay mode, return immediately without waiting for session
  if (isReplay) {
    return {
      headers: {},
      isLoading: false,
    }
  }

  return !session
    ? {
        isLoading: true,
      }
    : {
        headers: {
          'Gram-Project': projectSlug,
          'Gram-Chat-Session': session,
        },
        isLoading: false,
      }
}
