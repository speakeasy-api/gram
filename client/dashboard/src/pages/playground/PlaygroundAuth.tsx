import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Type } from "@/components/ui/type";
import { useMissingRequiredEnvVars } from "@/hooks/useEnvironmentVariables";
import { Toolset } from "@/lib/toolTypes";
import { useRoutes } from "@/routes";
import {
  useGetMcpMetadata,
  useListEnvironments,
} from "@gram/client/react-query";
import { useMemo } from "react";
import {
  environmentHasValue,
  getValueForEnvironment,
} from "../mcp/environmentVariableUtils";
import { useEnvironmentVariables } from "../mcp/useEnvironmentVariables";

interface PlaygroundAuthProps {
  toolset: Toolset;
  environment?: {
    slug: string;
    entries?: Array<{ name: string; value: string }>;
  };
}

const SECRET_FIELD_INDICATORS = ["SECRET", "KEY", "TOKEN", "PASSWORD"] as const;
const PASSWORD_MASK = "••••••••";

export function getAuthStatus(
  toolset: Pick<
    Toolset,
    "securityVariables" | "serverVariables" | "functionEnvironmentVariables"
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

export function PlaygroundAuth({ toolset, environment }: PlaygroundAuthProps) {
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

  // Load environment variables using the same hook as MCPAuthenticationTab
  const envVars = useEnvironmentVariables(toolset, environments, mcpMetadata);

  // Get the currently selected environment slug
  const currentEnvironmentSlug = environment?.slug || "default";

  // Filter to only show required variables that are relevant for the playground
  // (exclude server_url variables as they can't be configured in playground)
  const relevantEnvVars = useMemo(() => {
    return envVars.filter((envVar) => {
      // Only show required variables
      if (!envVar.isRequired) return false;

      // Filter out server_url variables
      if (envVar.key.toLowerCase().includes("server_url")) return false;

      return true;
    });
  }, [envVars]);

  // Calculate missing required variables using the same hook as MCPAuthenticationTab
  const missingRequiredCount = useMissingRequiredEnvVars(
    toolset,
    environments,
    currentEnvironmentSlug,
    mcpMetadata,
  );

  if (relevantEnvVars.length === 0) {
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
      {relevantEnvVars.map((envVar) => {
        // Use the same utilities as MCPAuthenticationTab to get values
        const hasValue = environmentHasValue(envVar, currentEnvironmentSlug);
        const value = getValueForEnvironment(envVar, currentEnvironmentSlug);

        // Determine if this is a secret field
        const isSecret = SECRET_FIELD_INDICATORS.some((indicator) =>
          envVar.key.toUpperCase().includes(indicator),
        );

        // Determine display value based on state
        let displayValue = "";
        let placeholder = "Not set";

        if (envVar.state === "user-provided") {
          displayValue = "";
          placeholder = "Provided by user at runtime";
        } else if (envVar.state === "omitted") {
          displayValue = "";
          placeholder = "Omitted";
        } else if (envVar.state === "system" && hasValue && value) {
          displayValue = isSecret ? PASSWORD_MASK : value;
          placeholder = "Configured";
        }

        return (
          <div key={envVar.id} className="space-y-1.5">
            <Label
              htmlFor={`auth-${envVar.id}`}
              className="text-xs font-medium"
            >
              {envVar.key}
              {envVar.state === "user-provided" && (
                <span className="ml-1 text-muted-foreground">(user)</span>
              )}
              {envVar.state === "omitted" && (
                <span className="ml-1 text-muted-foreground">(omitted)</span>
              )}
            </Label>
            <Input
              id={`auth-${envVar.id}`}
              value={displayValue}
              placeholder={placeholder}
              type={isSecret ? "password" : "text"}
              className="font-mono text-xs h-7"
              readOnly
              disabled
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
        Configure auth in the{" "}
        <routes.mcp.details.Link
          params={[toolset.slug]}
          hash="authentication"
          className="underline hover:text-foreground"
        >
          toolset settings
        </routes.mcp.details.Link>
      </Type>
    </div>
  );
}
