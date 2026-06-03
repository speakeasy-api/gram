import { useQuery } from "@tanstack/react-query";
import { z } from "zod/v4";

const OAuthProtectedResourceMetadataSchema = z.object({
  authorization_servers: z.array(z.string()).optional(),
});

export const getMcpOAuthRequiredQueryKey = (mcpUrl: string | undefined) => [
  "mcpOAuthRequired",
  mcpUrl,
];

export function mcpOAuthProtectedResourceMetadataUrl(mcpUrl: string): string {
  const parsed = new URL(mcpUrl);
  return `${parsed.origin}/.well-known/oauth-protected-resource${parsed.pathname}`;
}

export async function isMcpOAuthRequired(mcpUrl: string): Promise<boolean> {
  let wellKnownUrl: string;
  try {
    wellKnownUrl = mcpOAuthProtectedResourceMetadataUrl(mcpUrl);
  } catch {
    return false;
  }

  const response = await fetch(wellKnownUrl, {
    headers: { Accept: "application/json" },
  });
  if (response.status === 404) return false;
  if (!response.ok) return false;

  const parsedBody = OAuthProtectedResourceMetadataSchema.safeParse(
    await response.json(),
  );
  if (!parsedBody.success) return false;
  return (parsedBody.data.authorization_servers?.length ?? 0) > 0;
}

/**
 * Standard RFC 9728 OAuth protected-resource discovery against an MCP URL.
 * Returns whether the MCP server advertises OAuth protection.
 */
export function useMcpOAuthRequired(mcpUrl: string | undefined): {
  oauthRequired: boolean;
  isLoading: boolean;
} {
  const { data, isLoading } = useQuery({
    queryKey: getMcpOAuthRequiredQueryKey(mcpUrl),
    queryFn: async (): Promise<boolean> => {
      if (!mcpUrl) return false;
      return isMcpOAuthRequired(mcpUrl);
    },
    enabled: !!mcpUrl,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });

  return { oauthRequired: data ?? false, isLoading };
}
