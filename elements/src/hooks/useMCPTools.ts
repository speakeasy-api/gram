import { assert } from '@/lib/utils'
import { ToolsFilter } from '@/types'
import { experimental_createMCPClient as createMCPClient } from '@ai-sdk/mcp'
import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { Auth } from './useAuth'

type MCPToolsResult = Awaited<
  ReturnType<Awaited<ReturnType<typeof createMCPClient>>['tools']>
>

export function useMCPTools({
  auth,
  mcp,
  environment,
  toolsToInclude,
  gramEnvironment,
  chatId,
}: {
  auth: Auth
  mcp: string | undefined
  environment: Record<string, unknown>
  toolsToInclude?: ToolsFilter
  gramEnvironment?: string
  chatId?: string
}): UseQueryResult<MCPToolsResult, Error> {
  const envQueryKey = Object.entries(environment ?? {}).map(
    (k, v) => `${k}:${v}`
  )
  const authQueryKey = Object.entries(auth.headers ?? {}).map(
    (k, v) => `${k}:${v}`
  )

  const queryResult = useQuery({
    queryKey: [
      'mcpTools',
      mcp,
      gramEnvironment,
      ...envQueryKey,
      ...authQueryKey,
    ],
    queryFn: async () => {
      assert(!auth.isLoading, 'No auth found')
      assert(mcp, 'No MCP URL found')

      const mcpClient = await createMCPClient({
        name: 'gram-elements-mcp-client',
        transport: {
          type: 'http',
          url: mcp,
          headers: {
            ...transformEnvironmentToHeaders(environment ?? {}),
            ...auth.headers,
            ...(gramEnvironment && { 'Gram-Environment': gramEnvironment }),
            ...(chatId && { 'Gram-Chat-ID': chatId }),
          },
        },
      })

      const mcpTools = await mcpClient.tools()
      if (!toolsToInclude) {
        return mcpTools
      }

      return Object.fromEntries(
        Object.entries(mcpTools).filter(([name]) =>
          typeof toolsToInclude === 'function'
            ? toolsToInclude({ toolName: name })
            : toolsToInclude.includes(name)
        )
      )
    },
    enabled: !auth.isLoading && !!mcp,
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
