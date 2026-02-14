import { Label } from "@/components/ui/label";
import { PrivateInput } from "@/components/ui/private-input";
import { Type } from "@/components/ui/type";
import { ExternalMcpOAuthConnection } from "@/components/mcp/ExternalMcpOAuthConnection";
import { useToolsetMissingEnvVars } from "@/hooks/useToolsetMissingEnvVars";
import type { Toolset } from "@/lib/toolTypes";
import { useRoutes } from "@/routes";
import type {
  ExternalMCPToolDefinition,
  Tool as GeneratedTool,
} from "@gram/client/models/components";
import { useEffect, useMemo, useState } from "react";
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

/**
 * Extract OAuth configuration from external MCP tools in the toolset.
 * Returns the first external MCP tool that requires OAuth, or undefined.
 */
export function getExternalMcpOAuthConfig(
  rawTools: GeneratedTool[],
): ExternalMCPToolDefinition | undefined {
  for (const tool of rawTools) {
    if (
      tool.externalMcpToolDefinition?.requiresOauth &&
      tool.externalMcpToolDefinition.oauthVersion !== "none"
    ) {
      return tool.externalMcpToolDefinition;
    }
  }
  return undefined;
}

export function PlaygroundAuth({
  toolset,
  onUserProvidedHeadersChange,
}: PlaygroundAuthProps) {
  const routes = useRoutes();

  // Check if toolset has external MCP tools that require OAuth (MCP protocol discovery)
  const mcpOAuthConfig = useMemo(
    () => getExternalMcpOAuthConfig(toolset.rawTools),
    [toolset.rawTools],
  );
  const hasExternalMcpOAuth = !!mcpOAuthConfig;

  // Use the shared hook for environment data and missing vars calculation
  const {
    environments,
    mcpMetadata,
    defaultEnvironmentSlug,
    missingEnvVarCount: missingRequiredCount,
  } = useToolsetMissingEnvVars(toolset.slug);

  // Load environment variables using the same hook as MCPAuthenticationTab
  const envVars = useEnvironmentVariables(toolset, environments, mcpMetadata);

  // Track user-provided header values
  const [userProvidedValues, setUserProvidedValues] = useState<
    Record<string, string>
  >({});

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
  }, [userProvidedValues, onUserProvidedHeadersChange]);

  // Show "no auth required" only if there are no env vars AND no MCP OAuth
  if (envVars.length === 0 && !hasExternalMcpOAuth) {
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
      {/* External MCP OAuth Connection UI (discovered via MCP protocol) */}
      {hasExternalMcpOAuth && mcpOAuthConfig && (
        <ExternalMcpOAuthConnection
          toolsetSlug={toolset.slug}
          mcpOAuthConfig={mcpOAuthConfig}
        />
      )}

      {/* Environment Variables */}
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
