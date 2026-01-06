import { GetSessionFn } from '@/types'
import { experimental_createMCPClient as createMCPClient } from '@ai-sdk/mcp'
import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { useSession } from './useSession'
import { assert } from '@/lib/utils'

type MCPToolsResult = Awaited<
  ReturnType<Awaited<ReturnType<typeof createMCPClient>>['tools']>
>

export function useMCPTools({
  getSession,
  projectSlug,
  mcp,
  environment,
}: {
  getSession: GetSessionFn
  projectSlug: string
  mcp: string
  environment: Record<string, unknown>
}): UseQueryResult<MCPToolsResult, Error> {
  const session = useSession({
    getSession,
    projectSlug,
  })

  const queryResult = useQuery({
    queryKey: ['mcpTools', projectSlug, mcp, session],
    queryFn: async () => {
      assert(session, 'No session found')
      const mcpClient = await createMCPClient({
        name: 'gram-elements-mcp-client',
        transport: {
          type: 'http',
          url: mcp,
          headers: {
            ...transformEnvironmentToHeaders(environment ?? {}),
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
