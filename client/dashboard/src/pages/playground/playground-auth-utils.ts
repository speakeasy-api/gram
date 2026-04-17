import { useProject } from "@/contexts/Auth";
import { Toolset } from "@/lib/toolTypes";
import { getServerURL } from "@/lib/utils";
import { useQuery } from "@tanstack/react-query";
import { z } from "zod/v4";

export { getToolsetOAuthMode, type OAuthMode } from "./playgroundOAuthMode";

const ExternalMcpOAuthStatusResponseSchema = z.object({
  status: z.enum(["authenticated", "needs_auth", "disconnected"]),
  provider_name: z.string().optional(),
  expires_at: z.string().optional(),
});

export type ExternalOAuthStatusResponse = z.infer<
  typeof ExternalMcpOAuthStatusResponseSchema
>;

export const getExternalMcpOAuthStatusQueryKey = (
  toolsetId: string | undefined,
  slug?: string,
) => {
  const result = ["oauthExternalStatus"];
  if (toolsetId) result.push(toolsetId);
  if (slug) result.push(slug);
  return result;
};

/**
 * Shared hook for querying OAuth status from the /oauth-external/status endpoint.
 * Used by external MCP OAuth (2.1).
 */
export function useExternalMcpOAuthStatus(
  toolsetId: string | undefined,
  options?: {
    slug?: string; // For query key uniqueness
    enabled?: boolean;
  },
) {
  const { enabled = true } = options || {};

  const project = useProject();
  const apiUrl = getServerURL();

  const queryKey = getExternalMcpOAuthStatusQueryKey(toolsetId, options?.slug);

  return useQuery({
    queryKey: queryKey,
    queryFn: async (): Promise<ExternalOAuthStatusResponse> => {
      if (!toolsetId) return { status: "needs_auth" };

      const params = new URLSearchParams({
        toolset_id: toolsetId,
      });

      const response = await fetch(
        `${apiUrl}/oauth-external/status?${params.toString()}`,
        {
          credentials: "include",
          headers: {
            "Gram-Project": project.slug,
          },
        },
      );

      if (!response.ok) {
        if (response.status === 404) {
          return { status: "needs_auth" };
        }
        throw new Error("Failed to get OAuth status");
      }

      const parseResult = ExternalMcpOAuthStatusResponseSchema.safeParse(
        await response.json(),
      );

      if (!parseResult.success) {
        throw new Error("Invalid OAuth status response");
      }

      return parseResult.data;
    },
    enabled: enabled && !!toolsetId,
    staleTime: 30 * 1000,
    refetchOnWindowFocus: true,
  });
}

export function getAuthStatus(
  toolset: Pick<
    Toolset,
    | "securityVariables"
    | "serverVariables"
    | "functionEnvironmentVariables"
    | "externalMcpHeaderDefinitions"
  >,
  environment?: { entries?: Array<{ name: string; value: string }> },
): { hasMissingAuth: boolean; missingCount: number } {
  // In playground, always filter out server_url variables since they can't be configured here
  const relevantEnvVars = [
    ...(toolset?.securityVariables?.flatMap((secVar) => secVar.envVariables) ??
      []),
    ...(toolset?.serverVariables?.flatMap((serverVar) =>
      serverVar.envVariables.filter(
        (v) => !v.toLowerCase().includes("server_url"),
      ),
    ) ?? []),
    ...(toolset?.functionEnvironmentVariables?.map((fnVar) => fnVar.name) ??
      []),
    ...(toolset?.externalMcpHeaderDefinitions?.map(
      (headerDef) => headerDef.name,
    ) ?? []),
  ];

  const missingCount = relevantEnvVars.filter((varName) => {
    const entry = environment?.entries?.find((e) => e.name === varName);
    return !entry?.value || entry.value.trim() === "";
  }).length;

  return {
    hasMissingAuth: missingCount > 0,
    missingCount,
  };
}
