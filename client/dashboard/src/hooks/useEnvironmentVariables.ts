import { Toolset } from "@/lib/toolTypes";
import { Environment } from "@gram/client/models/components";
import { useMemo } from "react";

/**
 * Hook to count missing required environment variables for a specific environment
 *
 * Security variables are considered "user-provided" (end-users provide them at runtime)
 * and don't count as missing. Only server variables and function environment variables
 * count as missing if they don't have values set.
 *
 * @param toolset - The toolset containing required variables
 * @param environments - Array of all environments
 * @param environmentSlug - The specific environment to check
 * @returns Number of required variables missing in the specified environment
 */
export function useMissingRequiredEnvVars(
  toolset: Toolset | undefined,
  environments: Environment[],
  environmentSlug: string
): number {
  return useMemo(() => {
    if (!toolset) return 0;

    // Find the specified environment
    const environment = environments.find(env => env.slug === environmentSlug);
    if (!environment) return 0;

    // Collect required variable names (excluding user-provided security variables)
    const requiredVarNames = new Set<string>();

    // Security variables are user-provided, so we skip them
    // (end-users provide API keys at runtime, not in environment config)

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

    // Count how many required variables are missing in the environment
    let missingCount = 0;
    requiredVarNames.forEach((varName) => {
      const hasValue = environment.entries.some(entry => entry.name === varName);
      if (!hasValue) {
        missingCount++;
      }
    });

    return missingCount;
  }, [toolset, environments, environmentSlug]);
}
