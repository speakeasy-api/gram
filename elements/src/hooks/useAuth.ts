import { hasExplicitSessionAuth, isApiKeyAuth } from '@/lib/auth'
import { ApiConfig } from '../types'
import { useSession } from './useSession'
import { useMemo } from 'react'

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
  const getSession = useMemo(() => {
    if (isApiKeyAuth(auth)) {
      return null
    }
    return !isApiKeyAuth(auth) && hasExplicitSessionAuth(auth)
      ? auth.sessionFn
      : defaultGetSession
  }, [auth])
  // The session request is only neccessary if we are not using an API key auth
  // configuration. If a custom session fetcher is provided, we use it,
  // otherwise we fallback to the default session fetcher
  const session = useSession({
    // We want to check it's NOT API key auth, as the default auth scheme is session auth (if the user hasn't provided an explicit API config, we have a session auth config by default)
    enabled: !isApiKeyAuth(auth),
    getSession,
    projectSlug,
  })

  if (isApiKeyAuth(auth)) {
    return {
      headers: {
        'Gram-Project': projectSlug,
        'Gram-Key': auth.UNSAFE_apiKey,
      },
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
