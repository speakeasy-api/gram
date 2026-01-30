import { Label } from "@/components/ui/label";
import { PrivateInput } from "@/components/ui/private-input";
import { Type } from "@/components/ui/type";
import { useMissingRequiredEnvVars } from "@/hooks/useMissingEnvironmentVariables";
import { Toolset } from "@/lib/toolTypes";
import { useRoutes } from "@/routes";
import {
  useGetMcpMetadata,
  useListEnvironments,
} from "@gram/client/react-query";
import { useEffect, useState } from "react";
import {
  environmentHasValue,
  getValueForEnvironment,
} from "../mcp/environmentVariableUtils";
import { useEnvironmentVariables } from "../mcp/useEnvironmentVariables";

interface PlaygroundAuthProps {
  toolset: Toolset;
  onUserProvidedHeadersChange?: (headers: Record<string, string>) => void;
}

const PASSWORD_MASK = "••••••••";

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

export function PlaygroundAuth({
  toolset,
  onUserProvidedHeadersChange,
}: PlaygroundAuthProps) {
  const routes = useRoutes();

  // Use the same environment data fetching as MCPAuthenticationTab
  const { data: environmentsData } = useListEnvironments();
  const environments = environmentsData?.environments ?? [];

  const { data: mcpMetadataData } = useGetMcpMetadata(
    { toolsetSlug: toolset.slug },
    undefined,
    {
      throwOnError: false,
      retry: false,
    },
  );
  const mcpMetadata = mcpMetadataData?.metadata;
  const defaultEnvironmentSlug =
    environments.find((env) => env.id === mcpMetadata?.defaultEnvironmentId)
      ?.slug ?? "default";

  // Load environment variables using the same hook as MCPAuthenticationTab
  const envVars = useEnvironmentVariables(toolset, environments, mcpMetadata);

  // Track user-provided header values
  const [userProvidedValues, setUserProvidedValues] = useState<
    Record<string, string>
  >({});

  // Calculate missing required variables using the same hook as MCPAuthenticationTab
  const missingRequiredCount = useMissingRequiredEnvVars(
    toolset,
    environments,
    defaultEnvironmentSlug,
    mcpMetadata,
  );

  // Notify parent component when user-provided values change
  useEffect(() => {
    if (onUserProvidedHeadersChange) {
      // Build headers object with MCP- prefix and proper header names
      const headers: Record<string, string> = {};
      Object.entries(userProvidedValues).forEach(([varKey, value]) => {
        if (value.trim()) {
          // Use MCP- prefix with the header name
          const headerKey = `MCP-${varKey.replace(/\s+/g, "-").replace(/_/g, "-")}`;
          headers[headerKey] = value;
        }
      });
      onUserProvidedHeadersChange(headers);
    }
  }, [userProvidedValues, onUserProvidedHeadersChange, mcpMetadata]);

  if (envVars.length === 0) {
    return (
      <div className="text-center py-4">
        <Type variant="small" className="text-muted-foreground">
          No authentication required
        </Type>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {envVars.map((envVar) => {
        // Use the same utilities as MCPAuthenticationTab to get values
        const hasValue = environmentHasValue(envVar, defaultEnvironmentSlug);
        const value = getValueForEnvironment(envVar, defaultEnvironmentSlug);

        // Get header display name override if it exists
        const envConfig = mcpMetadata?.environmentConfigs?.find(
          (config) => config.variableName === envVar.key,
        );
        const displayName = envConfig?.headerDisplayName || envVar.key;

        // Determine display value and editability based on state
        let displayValue = "";
        let placeholder = "Not set";
        let isEditable = false;

        if (envVar.state === "user-provided") {
          displayValue = userProvidedValues[envVar.key] || "";
          placeholder = "Enter value here";
          isEditable = true;
        } else if (envVar.state === "omitted") {
          displayValue = "";
          placeholder = "Omitted";
          isEditable = false;
        } else if (envVar.state === "system" && hasValue && value) {
          displayValue = PASSWORD_MASK;
          placeholder = "Configured";
          isEditable = false;
        }

        return (
          <div key={envVar.id} className="space-y-1.5">
            <Label
              htmlFor={`auth-${envVar.id}`}
              className="text-xs font-medium"
            >
              {displayName}
            </Label>
            <PrivateInput
              id={`auth-${envVar.id}`}
              value={displayValue}
              onChange={(newValue) => {
                if (isEditable) {
                  setUserProvidedValues((prev) => ({
                    ...prev,
                    [envVar.key]: newValue,
                  }));
                }
              }}
              placeholder={placeholder}
              className="font-mono text-xs h-7"
              readOnly={!isEditable}
              disabled={!isEditable}
            />
          </div>
        );
      })}
      {missingRequiredCount > 0 && (
        <Type variant="small" className="text-warning pt-2">
          {missingRequiredCount} required variable
          {missingRequiredCount !== 1 ? "s" : ""} not configured
        </Type>
      )}
      <Type variant="small" className="text-muted-foreground pt-2">
        <routes.mcp.details.Link
          params={[toolset.slug]}
          hash="authentication"
          className="underline hover:text-foreground"
        >
          Configure auth
        </routes.mcp.details.Link>
      </Type>
    </div>
  );
}
