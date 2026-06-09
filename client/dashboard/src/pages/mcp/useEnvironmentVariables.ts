import { Toolset } from "@/lib/toolTypes";
import { useMemo } from "react";
import { EnvironmentVariable, EnvVarState } from "./environmentVariableUtils";

interface Environment {
  id: string;
  slug: string;
  name: string;
  entries: Array<{
    name: string;
    value: string;
    createdAt: Date;
  }>;
}

interface McpMetadata {
  defaultEnvironmentId?: string;
  environmentConfigs?: Array<{
    variableName: string;
    providedBy: string;
    headerDisplayName?: string;
  }>;
  externalDocumentationUrl?: string;
  instructions?: string;
  logoAssetId?: string;
}

export function useEnvironmentVariables(
  toolset: Toolset,
  environments: Environment[],
  mcpMetadata?: McpMetadata,
): EnvironmentVariable[] {
  return useMemo(() => {
    const existingVars: EnvironmentVariable[] = [];
    const requiredVarNames = new Set<string>();

    // Get environment entries from MCP metadata
    const envEntries = mcpMetadata?.environmentConfigs || [];

    // Helper to find environment entry for a variable
    const findEnvEntry = (varName: string) => {
      return envEntries.find((e) => e.variableName === varName);
    };

    // Helper to collect a variable's (redacted) value in each environment that defines it
    const getEnvironmentValues = (varName: string) =>
      environments.flatMap((env) => {
        const entry = env.entries.find((e) => e.name === varName);
        return entry ? [{ environmentSlug: env.slug, value: entry.value }] : [];
      });

    // Get env vars from security variables (these are required auth credentials)
    toolset.securityVariables?.forEach((secVar) => {
      secVar.envVariables.forEach((envVar) => {
        if (!envVar.toLowerCase().includes("token_url")) {
          requiredVarNames.add(envVar);
          const environmentValues = getEnvironmentValues(envVar);
          const id = `sec-${secVar.id}-${envVar}`;
          // Check if this variable has an environment entry
          const entry = findEnvEntry(envVar);
          const state: EnvVarState =
            entry?.providedBy === "user"
              ? "user-provided"
              : entry?.providedBy === "none"
                ? "omitted"
                : "system";
          existingVars.push({
            id,
            key: envVar,
            environmentValues,
            state,
            isRequired: true,
            description: `Authentication credential for ${secVar.name || "API access"}`,
            createdAt: new Date(),
          });
        }
      });
    });

    // Get env vars from server variables (these are required server config)
    toolset.serverVariables?.forEach((serverVar) => {
      serverVar.envVariables.forEach((envVar) => {
        requiredVarNames.add(envVar);
        const environmentValues = getEnvironmentValues(envVar);
        const id = `srv-${envVar}`;
        // Check if this variable has an environment entry
        const entry = findEnvEntry(envVar);
        const state: EnvVarState =
          entry?.providedBy === "user"
            ? "user-provided"
            : entry?.providedBy === "none"
              ? "omitted"
              : "system";
        existingVars.push({
          id,
          key: envVar,
          environmentValues,
          state,
          isRequired: true,
          description: "Server configuration variable",
          createdAt: new Date(),
        });
      });
    });

    // Get env vars from function environment variables (these are required for functions)
    toolset.functionEnvironmentVariables?.forEach((funcVar) => {
      requiredVarNames.add(funcVar.name);
      const environmentValues = getEnvironmentValues(funcVar.name);
      const id = `func-${funcVar.name}`;
      // Check if this variable has an environment entry
      const entry = findEnvEntry(funcVar.name);
      const state: EnvVarState =
        entry?.providedBy === "user"
          ? "user-provided"
          : entry?.providedBy === "none"
            ? "omitted"
            : "system";
      existingVars.push({
        id,
        key: funcVar.name,
        environmentValues,
        state,
        isRequired: true,
        description: funcVar.description || "Function environment variable",
        createdAt: new Date(),
      });
    });

    // Get env   vars from external MCP header definitions (these are required for external MCP servers)
    toolset.externalMcpHeaderDefinitions?.forEach((headerDef) => {
      requiredVarNames.add(headerDef.name);
      const environmentValues = getEnvironmentValues(headerDef.name);
      const id = `ext-${headerDef.name}`;
      // Check if this variable has an environment entry
      const entry = findEnvEntry(headerDef.name);
      const state: EnvVarState =
        entry?.providedBy === "user"
          ? "user-provided"
          : entry?.providedBy === "none"
            ? "omitted"
            : "system";
      existingVars.push({
        id,
        key: headerDef.name,
        environmentValues,
        state,
        isRequired: true,
        description: headerDef.description || "External MCP header",
        createdAt: new Date(),
      });
    });

    // Load custom variables from environments (variables not in the required list)
    const customVarMap = new Map<
      string,
      {
        environmentValues: Array<{ environmentSlug: string; value: string }>;
        createdAt: Date;
      }
    >();

    environments.forEach((env) => {
      env.entries.forEach((entry) => {
        // Skip if this is a required variable or a token_url
        if (
          !requiredVarNames.has(entry.name) &&
          !entry.name.toLowerCase().includes("token_url")
        ) {
          const value = { environmentSlug: env.slug, value: entry.value };
          const varData = customVarMap.get(entry.name);
          if (varData) {
            varData.environmentValues.push(value);
          } else {
            customVarMap.set(entry.name, {
              environmentValues: [value],
              createdAt: entry.createdAt,
            });
          }
        }
      });
    });

    // Add custom variables to the list (only those with environment entries)
    customVarMap.forEach((info, varName) => {
      // Only include custom variables that have an environment entry
      const entry = findEnvEntry(varName);
      if (!entry) {
        return;
      }

      const id = `custom-${varName}`;
      const environmentValues = info.environmentValues;
      const state: EnvVarState =
        entry.providedBy === "user"
          ? "user-provided"
          : entry.providedBy === "none"
            ? "omitted"
            : "system";
      existingVars.push({
        id,
        key: varName,
        environmentValues,
        state,
        isRequired: false,
        description: "Custom environment variable",
        createdAt: info.createdAt,
      });
    });

    return existingVars;
  }, [
    toolset.securityVariables,
    toolset.serverVariables,
    toolset.functionEnvironmentVariables,
    toolset.externalMcpHeaderDefinitions,
    environments,
    mcpMetadata,
  ]);
}
