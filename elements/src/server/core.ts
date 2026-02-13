/**
 * Core session handler logic shared across all server adapters.
 * This module contains the framework-agnostic business logic for creating chat sessions.
 */

export interface SessionHandlerOptions {
  /**
   * The origin from which the token will be used
   */
  embedOrigin: string

  /**
   * Free-form user identifier
   */
  userIdentifier: string

  /**
   * Token expiration in seconds (max / default 3600)
   * @default 3600
   */
  expiresAfter?: number

  /**
   * Gram API key. If not provided, falls back to the `GRAM_API_KEY` environment variable.
   */
  apiKey?: string
}

export interface CreateSessionRequest {
  projectSlug: string
  options: SessionHandlerOptions
}

export interface CreateSessionResponse {
  status: number
  body: string
  headers: Record<string, string>
}

/**
 * Core function to create a chat session by calling Gram's API.
 * This is framework-agnostic and can be used by any adapter.
 */
export async function createChatSession(
  request: CreateSessionRequest
): Promise<CreateSessionResponse> {
  const base = process.env.GRAM_API_URL ?? 'https://app.getgram.ai'

  try {
    const response = await fetch(base + '/rpc/chatSessions.create', {
      method: 'POST',
      body: JSON.stringify({
        embed_origin: request.options.embedOrigin,
        user_identifier: request.options.userIdentifier,
        expires_after: request.options.expiresAfter,
      }),
      headers: {
        'Content-Type': 'application/json',
        'Gram-Project': request.projectSlug,
        'Gram-Key': request.options.apiKey ?? process.env.GRAM_API_KEY ?? '',
      },
    })

    const body = await response.text()

    return {
      status: response.status,
      body,
      headers: { 'Content-Type': 'application/json' },
    }
  } catch (error) {
    const errorMessage =
      error instanceof Error ? error.message : 'Unknown error'
    console.error('Failed to create chat session:', error)

    return {
      status: 500,
      body: JSON.stringify({
        error: 'Failed to create chat session: ' + errorMessage,
      }),
      headers: { 'Content-Type': 'application/json' },
    }
  }
}
