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
}: {
  auth?: AuthConfig
  projectSlug: string
}): Auth => {
  let sessionFn = auth && 'sessionFn' in auth ? auth.sessionFn : null
  if (sessionFn === undefined) {
    sessionFn = defaultGetSession
  }

  const session = useSession({
    getSession: sessionFn,
    projectSlug,
  })

  if (auth && 'UNSAFE_apiKey' in auth) {
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
