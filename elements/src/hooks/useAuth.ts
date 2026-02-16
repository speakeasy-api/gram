import { useReplayContext } from '@/contexts/ReplayContext'
import {
  hasExplicitSessionAuth,
  isDangerousApiKeyAuth,
  isStaticSessionAuth,
  isUnifiedFunctionSession,
  isUnifiedStaticSession,
} from '@/lib/auth'
import { useMemo } from 'react'
import { ApiConfig, GetSessionFn } from '../types'
import { useSession } from './useSession'

declare const __GRAM_API_URL__: string | undefined

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

function createDangerousApiKeySessionFn(
  apiKey: string,
  apiUrl: string
): GetSessionFn {
  return async ({ projectSlug }) => {
    const response = await fetch(`${apiUrl}/rpc/chatSessions.create`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Gram-Key': apiKey,
        'Gram-Project': projectSlug,
      },
      body: JSON.stringify({
        embed_origin: window.location.origin,
        user_identifier: 'elements-dev',
      }),
    })
    const data = await response.json()
    return data.client_token
  }
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

  const apiUrl = useMemo(() => {
    const envUrl =
      typeof __GRAM_API_URL__ !== 'undefined' ? __GRAM_API_URL__ : undefined
    const url = auth?.url || envUrl || 'https://app.getgram.ai'
    return url.replace(/\/+$/, '')
  }, [auth?.url])

  const getSession = useMemo(() => {
    // In replay mode, skip session fetching entirely
    if (isReplay) {
      return null
    }
    // dangerousApiKey — exchange key for session via API
    if (isDangerousApiKeyAuth(auth)) {
      return createDangerousApiKeySessionFn(auth.dangerousApiKey, apiUrl)
    }
    // Unified session: static string
    if (isUnifiedStaticSession(auth)) {
      return () => Promise.resolve(auth.session)
    }
    // Unified session: function
    if (isUnifiedFunctionSession(auth)) {
      return auth.session
    }
    // Legacy: static sessionToken
    if (isStaticSessionAuth(auth)) {
      return () => Promise.resolve(auth.sessionToken)
    }
    // Legacy: sessionFn
    if (hasExplicitSessionAuth(auth)) {
      return auth.sessionFn
    }
    return defaultGetSession
  }, [auth, isReplay, apiUrl])

  // The session request is only necessary if we are not using static session auth
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
