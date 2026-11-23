import type { Toolset } from "@/lib/toolTypes";
import type { Environment } from "@gram/client/models/components/environment.js";
import type { McpMetadata } from "@gram/client/models/components/mcpmetadata.js";
import { useMemo } from "react";

/**
 * Hook to count missing required environment variables for a specific environment
 *
 * Variables are considered "not configured" based on their state:
 * - If NOT in attachedEnvironmentVariables (user-provided): always considered configured
 * - If in attachedEnvironmentVariables (system): only configured if environment has a value
 * - If providedBy is "none" (omitted): always considered configured
 *
 * @param toolset - The toolset containing required variables
 * @param environments - Array of all environments
 * @param environmentSlug - The specific environment to check
 * @param mcpMetadata - Optional MCP metadata containing attachedEnvironmentVariables
 * @returns Number of required variables missing in the specified environment
 */
export function useMissingRequiredEnvVars(
  toolset: Toolset | undefined,
  environments: Environment[],
  environmentSlug: string,
  mcpMetadata?: McpMetadata | null,
): number {
  return useMemo(
    () =>
      countMissingRequiredEnvVars(
        toolset,
        environments,
        environmentSlug,
        mcpMetadata,
      ),
    [toolset, environments, environmentSlug, mcpMetadata],
  );
}

/** The variables a toolset needs, from either the raw or the wrapped toolset. */
type EnvVarToolset = Pick<
  Toolset,
  "securityVariables" | "serverVariables" | "functionEnvironmentVariables"
>;

/** The count behind {@link useMissingRequiredEnvVars}, for use outside a hook. */
export function countMissingRequiredEnvVars(
  toolset: EnvVarToolset | undefined,
  environments: Environment[],
  environmentSlug: string,
  mcpMetadata?: McpMetadata | null,
): number {
  if (!toolset) return 0;

  // Find the specified environment
  const environment = environments.find((env) => env.slug === environmentSlug);
  if (!environment) return 0;

  // Get the list of variables marked as "system" (in attachedEnvironmentVariables)
  const attachedEnvVars = mcpMetadata?.environmentConfigs || [];

  // Collect all required variable names
  const requiredVarNames = new Set<string>();

  // Add security variables
  toolset.securityVariables?.forEach((secVar) => {
    secVar.envVariables.forEach((envVar) => {
      if (!envVar.toLowerCase().includes("token_url")) {
        requiredVarNames.add(envVar);
      }
    });
  });

  // Add server variables
  toolset.serverVariables?.forEach((serverVar) => {
    serverVar.envVariables.forEach((envVar) => {
      requiredVarNames.add(envVar);
    });
  });

  // Add function environment variables
  toolset.functionEnvironmentVariables?.forEach((funcVar) => {
    requiredVarNames.add(funcVar.name);
  });

  const hasValidValue = (varName: string) => {
    const entry = attachedEnvVars.find(
      (entry) => entry.variableName === varName,
    );
    if (entry) {
      if (entry.providedBy === "system") {
        return environment.entries.some((entry) => entry.name === varName);
      } else if (entry.providedBy === "none") {
        // Omitted variables are considered "configured" (intentionally not included)
        return true;
      } else {
        // User-provided variables are always considered configured
        return true;
      }
    }
    return false;
  };

  // Count how many required variables are "not configured"
  // A variable is "not configured" if it's marked as "system" BUT has no value
  let missingCount = 0;
  requiredVarNames.forEach((varName) => {
    if (!hasValidValue(varName)) {
      missingCount++;
    }
  });

  return missingCount;
}
