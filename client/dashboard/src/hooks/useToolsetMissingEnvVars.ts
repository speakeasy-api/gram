import type { Toolset } from "@/lib/toolTypes";
import { useToolset } from "@/hooks/toolTypes";
import { useMissingRequiredEnvVars } from "@/hooks/useMissingEnvironmentVariables";
import type { Environment, McpMetadata } from "@gram/client/models/components";
import {
  useGetMcpMetadata,
  useListEnvironments,
} from "@gram/client/react-query";

interface MissingEnvVarsResult {
  missingEnvVarCount: number;
  hasEnvVarRequirements: boolean;
  isLoading: boolean;
  toolset: Toolset | undefined;
  environments: Environment[];
  mcpMetadata: McpMetadata | undefined;
  defaultEnvironmentSlug: string;
}

/**
 * Hook to check missing environment variables for a toolset.
 */
export function useToolsetMissingEnvVars(
  toolsetSlug: string,
): MissingEnvVarsResult {
  const { data: toolset, isLoading: toolsetLoading } = useToolset(toolsetSlug);

  const { data: environmentsData, isLoading: envLoading } =
    useListEnvironments();
  const environments = environmentsData?.environments ?? [];

  const { data: mcpMetadataData, isLoading: metadataLoading } =
    useGetMcpMetadata({ toolsetSlug: toolsetSlug }, undefined, {
      enabled: !!toolsetSlug,
      throwOnError: false,
      retry: false,
    });

  const mcpMetadata = mcpMetadataData?.metadata;

  // Determine default environment slug
  const defaultEnvironmentSlug =
    environments.find((env) => env.id === mcpMetadata?.defaultEnvironmentId)
      ?.slug ?? "default";

  const missingEnvVarCount = useMissingRequiredEnvVars(
    toolset,
    environments,
    defaultEnvironmentSlug,
    mcpMetadata,
  );

  // Determine if there are any env var requirements
  const hasEnvVarRequirements =
    (toolset?.securityVariables?.length ?? 0) > 0 ||
    (toolset?.serverVariables?.length ?? 0) > 0 ||
    (toolset?.functionEnvironmentVariables?.length ?? 0) > 0 ||
    (toolset?.externalMcpHeaderDefinitions?.length ?? 0) > 0;

  const isLoading = toolsetLoading || envLoading || metadataLoading;

  return {
    missingEnvVarCount,
    hasEnvVarRequirements,
    isLoading,
    toolset,
    environments,
    mcpMetadata,
    defaultEnvironmentSlug,
  };
}
