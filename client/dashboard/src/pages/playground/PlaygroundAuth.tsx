import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { PrivateInput } from "@/components/ui/private-input";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { useMissingRequiredEnvVars } from "@/hooks/useMissingEnvironmentVariables";
import { Toolset } from "@/lib/toolTypes";
import { getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import {
  ExternalMCPToolDefinition,
  Tool as GeneratedTool,
} from "@gram/client/models/components";
import {
  useGetMcpMetadata,
  useListEnvironments,
} from "@gram/client/react-query";
import { Badge, Stack } from "@speakeasy-api/moonshine";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle, ExternalLink, Loader2, LogOut } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import {
  environmentHasValue,
  getValueForEnvironment,
} from "../mcp/environmentVariableUtils";
import { useEnvironmentVariables } from "../mcp/useEnvironmentVariables";
import { useToolset } from "@/hooks/toolTypes";
import { z } from "zod/v4";

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

/**
 * OAuth connection status component for external MCP tools discovered via MCP protocol.
 * This handles OAuth 2.1 with Dynamic Client Registration (DCR).
 */
function ExternalMcpOAuthConnection({
  toolsetSlug,
  mcpOAuthConfig,
}: {
  toolsetSlug: string;
  mcpOAuthConfig: ExternalMCPToolDefinition;
}) {
  const { data: toolset } = useToolset(toolsetSlug);
  const project = useProject();
  const queryClient = useQueryClient();
  const apiUrl = getServerURL();
  const pollTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // Clean up poll timer on unmount
  useEffect(() => {
    return () => {
      if (pollTimerRef.current) {
        clearInterval(pollTimerRef.current);
      }
    };
  }, []);

  // Query OAuth status using the shared hook
  const {
    data: oauthStatus,
    isLoading: statusLoading,
    refetch: refetchStatus,
  } = useExternalMcpOAuthStatus(toolset?.id, {
    slug: mcpOAuthConfig.slug,
  });

  // Disconnect mutation
  const disconnectMutation = useMutation({
    mutationFn: async () => {
      if (!toolset)
        throw new Error("Cannot disconnect because toolset is not loaded");

      const params = new URLSearchParams({
        toolset_id: toolset.id,
      });

      const response = await fetch(
        `${apiUrl}/oauth-external/disconnect?${params.toString()}`,
        {
          method: "DELETE",
          credentials: "include",
          headers: {
            "Gram-Project": project.slug,
          },
        },
      );

      if (!response.ok) {
        throw new Error("Failed to disconnect");
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: getExternalMcpOAuthStatusQueryKey(
          toolset?.id,
          mcpOAuthConfig.slug,
        ),
      });
      toast.success(
        `Disconnected from ${mcpOAuthConfig.name || mcpOAuthConfig.slug}`,
      );
    },
    onError: () => {
      toast.error("Failed to disconnect");
    },
  });

  // Handle connect click - initiates OAuth flow
  const handleConnect = () => {
    if (!mcpOAuthConfig.oauthAuthorizationEndpoint) return;

    if (window.location.origin !== apiUrl) {
      toast.error("OAuth configuration error: redirect origin mismatch");
      return;
    }

    const params = new URLSearchParams({
      toolset_id: toolset?.id ?? "",
      external_mcp_slug: mcpOAuthConfig.slug,
      redirect_uri: window.location.href.split("?")[0],
      project: project.slug,
    });

    const authUrl = `${apiUrl}/oauth-external/authorize?${params.toString()}`;

    const width = 600;
    const height = 700;
    const left = window.screenX + (window.outerWidth - width) / 2;
    const top = window.screenY + (window.outerHeight - height) / 2;

    // Open in popup
    const popup = window.open(
      authUrl,
      "oauth_popup",
      `width=${width},height=${height},scrollbars=yes,top=${top},left=${left}`,
    );

    if (!popup) {
      // Fallback to redirect
      window.location.href = authUrl;
      return;
    }

    // Poll for popup close and refresh status
    pollTimerRef.current = setInterval(() => {
      if (popup.closed) {
        if (pollTimerRef.current) {
          clearInterval(pollTimerRef.current);
          pollTimerRef.current = null;
        }
        // Small delay to ensure server has processed the callback
        setTimeout(() => {
          refetchStatus();
        }, 300);
      }
    }, 500);
  };

  const isConnected = oauthStatus?.status === "authenticated";
  const providerName = mcpOAuthConfig.name || mcpOAuthConfig.slug;
  const oauthVersionLabel =
    mcpOAuthConfig.oauthVersion === "2.1" ? "MCP OAuth 2.1" : "OAuth 2.0";

  return (
    <div className="border rounded-md p-3 bg-muted/30">
      <Stack gap={2}>
        <Stack
          direction="horizontal"
          align="center"
          className="justify-between"
        >
          <Type variant="small" className="font-medium">
            {oauthVersionLabel}
          </Type>
          {statusLoading ? (
            <Loader2 className="size-4 animate-spin text-muted-foreground" />
          ) : isConnected ? (
            <Badge variant="success">
              <CheckCircle className="size-3 mr-1" />
              Connected
            </Badge>
          ) : (
            <Badge variant="warning">Not Connected</Badge>
          )}
        </Stack>

        <Type variant="small" className="text-muted-foreground">
          {providerName}
        </Type>

        {isConnected ? (
          <Button
            size="sm"
            variant="outline"
            className="w-full"
            onClick={() => disconnectMutation.mutate()}
            disabled={disconnectMutation.isPending}
          >
            <LogOut className="size-3 mr-2" />
            Disconnect
          </Button>
        ) : (
          <Button
            size="sm"
            variant="default"
            className="w-full"
            onClick={handleConnect}
          >
            <ExternalLink className="size-3 mr-2" />
            Connect
          </Button>
        )}
      </Stack>
    </div>
  );
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
