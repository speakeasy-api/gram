import { createChatSession } from '@gram-ai/elements/server/core'
import { createServerFn } from '@tanstack/react-start'
import { z } from 'zod'

const GetSessionInputSchema = z.object({
  projectSlug: z.string(),
})

export const getSession = createServerFn({ method: 'POST' })
  .inputValidator(GetSessionInputSchema)
  .handler(async ({ data }) => {
    const result = await createChatSession({
      projectSlug: data.projectSlug,
      options: {
        embedOrigin:
          import.meta.env.VITE_EMBED_ORIGIN ?? 'http://localhost:3000',
        userIdentifier: 'user-123',
      },
    })

    if (result.status !== 200) {
      throw new Error(`Failed to create chat session: ${result.body}`)
    }

    const parsed = JSON.parse(result.body) as { client_token: string }
    return parsed.client_token
  })
