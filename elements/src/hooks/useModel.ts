import { getApiUrl } from '@/lib/api'
import { createOpenRouter } from '@openrouter/ai-sdk-provider'
import { LanguageModel } from 'ai'
import { useAuth } from './useAuth'
import { useElements } from './useElements'

// Creates an OpenRouter client to be used for "internal Gram" usage, such as follow-on suggestions
export const useModel = (
  model: string = 'openai/gpt-4o-mini'
): LanguageModel => {
  const { config } = useElements()

  const auth = useAuth({
    auth: config.api,
    projectSlug: config.projectSlug,
  })

  const apiUrl = getApiUrl(config)

  const openRouter = createOpenRouter({
    baseURL: apiUrl,
    apiKey: 'unused, but must be set',
    headers: {
      ...auth.headers,
      'X-Gram-Source': 'gram',
    },
  })

  return openRouter.chat(model)
}
