import { ElementsConfig } from '@/types'
import { experimental_createMCPClient as createMCPClient } from '@ai-sdk/mcp'
import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { useSession } from './useSession'
import { assert } from '@/lib/utils'

type MCPToolsResult = Awaited<
  ReturnType<Awaited<ReturnType<typeof createMCPClient>>['tools']>
>

export function useMCPTools(
  config: ElementsConfig
): UseQueryResult<MCPToolsResult, Error> {
  const session = useSession(config)

  const queryResult = useQuery({
    queryKey: ['mcpTools', config.projectSlug, config.mcp, session],
    queryFn: async () => {
      assert(session, 'No session found')
      const mcpClient = await createMCPClient({
        name: 'gram-elements-mcp-client',
        transport: {
          type: 'http',
          url: config.mcp,
          headers: {
            ...transformEnvironmentToHeaders(config.environment ?? {}),
            'Gram-Chat-Session': session,
          },
        },
      })

      const mcpTools = await mcpClient.tools()
      return mcpTools
    },
    enabled: !!session,
    staleTime: Infinity,
    gcTime: Infinity,
  })

  return queryResult
}
const HEADER_PREFIX = 'MCP-'

function transformEnvironmentToHeaders(environment: Record<string, unknown>) {
  if (typeof environment !== 'object' || environment === null) {
    return {}
  }
  return Object.entries(environment).reduce(
    (acc, [key, value]) => {
      // Normalize key: replace underscores with dashes
      const normalizedKey = key.replace(/_/g, '-')

      // Add MCP- prefix if it doesn't already have it
      const headerKey = normalizedKey.startsWith(HEADER_PREFIX)
        ? normalizedKey
        : `${HEADER_PREFIX}${normalizedKey}`

      acc[headerKey] = value as string
      return acc
    },
    {} as Record<string, string>
  )
}
