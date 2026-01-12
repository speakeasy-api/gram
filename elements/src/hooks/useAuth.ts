import { isApiKeyAuth } from '@/lib/auth'
import { AuthConfig } from '../types'
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
  chatId,
}: {
  auth?: AuthConfig
  projectSlug: string
  chatId?: string
}): Auth => {
  // The session request is only neccessary if we are not using an API key auth
  // configuration. If a custom session fetcher is provided, we use it,
  // otherwise we fallback to the default session fetcher
  const session = useSession({
    enabled: !isApiKeyAuth(auth),
    getSession: !isApiKeyAuth(auth)
      ? (auth?.sessionFn ?? defaultGetSession)
      : null,
    projectSlug,
  })

  if (isApiKeyAuth(auth)) {
    return {
      headers: {
        'Gram-Project': projectSlug,
        'Gram-Key': auth.UNSAFE_apiKey,
        ...(chatId && { 'Gram-Chat-ID': chatId }),
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
          ...(chatId && { 'Gram-Chat-ID': chatId }),
        },
        isLoading: false,
      }
}
