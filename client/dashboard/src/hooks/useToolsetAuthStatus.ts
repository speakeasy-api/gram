import { useToolset } from "@/hooks/toolTypes";
import { useToolsetMissingEnvVars } from "@/hooks/useToolsetMissingEnvVars";
import { useExternalMcpOAuthStatus } from "@/components/mcp/ExternalMcpOAuthConnection";
import { getExternalMcpOAuthConfig } from "@/pages/playground/PlaygroundAuth";
import type { ToolsetEntry } from "@gram/client/models/components";

interface AuthStatusResult {
  requiresOAuth: boolean;
  oauthConnected: boolean;
  missingEnvVarCount: number;
  hasAuthRequirements: boolean;
  isComplete: boolean;
  isLoading: boolean;
}

/**
 * Hook to check auth status for a toolset.
 * Uses the same logic as PlaygroundAuth to determine:
 * 1. If external MCP OAuth is required and connected
 * 2. If environment variables are configured
 */
export function useToolsetAuthStatus(
  toolsetEntry: ToolsetEntry,
): AuthStatusResult {
  const toolsetSlug = toolsetEntry.slug;

  const { data: toolset, isLoading: toolsetLoading } = useToolset(toolsetSlug);

  const mcpOAuthConfig = toolset
    ? getExternalMcpOAuthConfig(toolset.rawTools)
    : undefined;
  const requiresOAuth = !!mcpOAuthConfig;

  const { data: oauthStatus, isLoading: oauthLoading } =
    useExternalMcpOAuthStatus(toolset?.id, {
      slug: mcpOAuthConfig?.slug,
      enabled: requiresOAuth,
    });

  const oauthConnected = oauthStatus?.status === "authenticated";

  const {
    missingEnvVarCount,
    hasEnvVarRequirements,
    isLoading: envVarsLoading,
  } = useToolsetMissingEnvVars(toolsetSlug);

  const hasAuthRequirements = hasEnvVarRequirements || requiresOAuth;

  const oauthComplete = !requiresOAuth || oauthConnected;
  const envVarsComplete = missingEnvVarCount === 0;
  const isComplete = oauthComplete && envVarsComplete;

  const isLoading =
    toolsetLoading || envVarsLoading || (requiresOAuth && oauthLoading);

  return {
    requiresOAuth,
    oauthConnected,
    missingEnvVarCount,
    hasAuthRequirements,
    isComplete,
    isLoading,
  };
}
