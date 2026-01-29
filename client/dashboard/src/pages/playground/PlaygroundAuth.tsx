import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { PrivateInput } from "@/components/ui/private-input";
import { Type } from "@/components/ui/type";
import { useSession } from "@/contexts/Auth";
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
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  environmentHasValue,
  getValueForEnvironment,
} from "../mcp/environmentVariableUtils";
import { useEnvironmentVariables } from "../mcp/useEnvironmentVariables";
import { useToolset } from "@/hooks/toolTypes";

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

  const queryClient = useQueryClient();
  const apiUrl = getServerURL();
  const session = useSession();

  // Use the authorization endpoint as the issuer for querying status
  const issuer = mcpOAuthConfig.oauthAuthorizationEndpoint
    ? new URL(mcpOAuthConfig.oauthAuthorizationEndpoint).origin
    : undefined;

  // Query OAuth status
  const {
    data: oauthStatus,
    isLoading: statusLoading,
    refetch: refetchStatus,
  } = useQuery({
    queryKey: ["mcpOauthStatus", toolset?.id ?? "", mcpOAuthConfig.slug],
    queryFn: async () => {
      if (!issuer) return { status: "needs_auth" as const };
      if (!toolset) return { status: "needs_auth" as const };

      const params = new URLSearchParams({
        toolset_id: toolset?.id ?? "",
        issuer: issuer,
      });

      const response = await fetch(
        `${apiUrl}/oauth-external/status?${params.toString()}`,
        {
          credentials: "include",
          headers: {
            "Gram-Session": session.session,
          },
        },
      );

      if (!response.ok) {
        if (response.status === 404) {
          return { status: "needs_auth" as const };
        }
        throw new Error("Failed to get OAuth status");
      }

      return response.json() as Promise<{
        status: "authenticated" | "needs_auth" | "disconnected";
        provider_name?: string;
        expires_at?: string;
      }>;
    },
    enabled: !!issuer && !!toolset,
    staleTime: 30 * 1000,
    refetchOnWindowFocus: true,
  });

  // Disconnect mutation
  const disconnectMutation = useMutation({
    mutationFn: async () => {
      if (!issuer)
        throw new Error("Cannot disconnect because no issuer is configured");
      if (!toolset)
        throw new Error("Cannot disconnect because toolset is not loaded");

      const params = new URLSearchParams({
        toolset_id: toolset.id,
        issuer: issuer,
      });

      const response = await fetch(
        `${apiUrl}/oauth-external/disconnect?${params.toString()}`,
        {
          method: "DELETE",
          credentials: "include",
          headers: {
            "Gram-Session": session.session,
          },
        },
      );

      if (!response.ok) {
        throw new Error("Failed to disconnect");
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["mcpOauthStatus", toolset?.id ?? "", mcpOAuthConfig.slug],
      });
      queryClient.invalidateQueries({
        queryKey: ["playground.oauthToken"],
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

    const params = new URLSearchParams({
      toolset_id: toolset?.id ?? "",
      external_mcp_slug: mcpOAuthConfig.slug,
      redirect_uri: window.location.href.split("?")[0],
      // Pass session token for popup windows that don't share cookies
      session: session.session,
    });

    const authUrl = `${apiUrl}/oauth-external/authorize?${params.toString()}`;

    // Open in popup
    const popup = window.open(
      authUrl,
      "oauth_popup",
      "width=600,height=700,scrollbars=yes",
    );

    if (!popup) {
      // Fallback to redirect
      window.location.href = authUrl;
      return;
    }

    // Poll for popup close and refresh status
    const pollTimer = setInterval(() => {
      if (popup.closed) {
        clearInterval(pollTimer);
        // Small delay to ensure server has processed the callback
        setTimeout(() => {
          refetchStatus();
          // Also invalidate the token query so GramElementsProvider
          // re-renders with the new OAuth access token
          queryClient.invalidateQueries({
            queryKey: ["playground.oauthToken"],
          });
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

/**
 * OAuth connection status component for external OAuth servers (legacy path)
 */
function OAuthConnection({
  toolsetId,
  toolset,
}: {
  toolsetId: string;
  toolset: Toolset;
}) {
  const session = useSession();
  const queryClient = useQueryClient();
  const apiUrl = getServerURL();

  // Extract OAuth metadata from toolset
  const oauthMetadata = toolset.externalOauthServer?.metadata as
    | {
        issuer?: string;
      }
    | undefined;
  const issuer = oauthMetadata?.issuer;

  // Query OAuth status
  const { data: oauthStatus, isLoading: statusLoading } = useQuery({
    queryKey: ["oauthStatus", toolsetId, issuer],
    queryFn: async () => {
      if (!issuer) return null;

      const params = new URLSearchParams({
        toolset_id: toolsetId,
        issuer: issuer,
      });

      const response = await fetch(
        `${apiUrl}/oauth-external/status?${params.toString()}`,
        {
          headers: {
            "Gram-Session": session.session,
          },
        },
      );

      if (!response.ok) {
        if (response.status === 404) {
          return { status: "needs_auth" as const };
        }
        throw new Error("Failed to get OAuth status");
      }

      return response.json() as Promise<{
        status: "authenticated" | "needs_auth" | "disconnected";
        provider_name?: string;
        expires_at?: string;
      }>;
    },
    enabled: !!issuer,
    staleTime: 30 * 1000,
    refetchOnWindowFocus: true,
  });

  // Disconnect mutation
  const disconnectMutation = useMutation({
    mutationFn: async () => {
      if (!issuer) throw new Error("No issuer configured");

      const params = new URLSearchParams({
        toolset_id: toolsetId,
        issuer: issuer,
      });

      const response = await fetch(
        `${apiUrl}/oauth-external/disconnect?${params.toString()}`,
        {
          method: "DELETE",
          headers: {
            "Gram-Session": session.session,
          },
        },
      );

      if (!response.ok) {
        throw new Error("Failed to disconnect");
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["oauthStatus", toolsetId, issuer],
      });
      toast.success("Disconnected from OAuth provider");
    },
    onError: () => {
      toast.error("Failed to disconnect");
    },
  });

  // Handle connect click
  const handleConnect = () => {
    if (!issuer) return;

    const params = new URLSearchParams({
      toolset_id: toolsetId,
      issuer: issuer,
      redirect_uri: window.location.href.split("?")[0],
    });

    const authUrl = `${apiUrl}/oauth-external/authorize?${params.toString()}`;

    // Open in popup
    const popup = window.open(
      authUrl,
      "oauth_popup",
      "width=600,height=700,scrollbars=yes",
    );

    if (!popup) {
      // Fallback to redirect
      window.location.href = authUrl;
      return;
    }

    // Poll for popup close and refresh status
    const pollTimer = setInterval(() => {
      if (popup.closed) {
        clearInterval(pollTimer);
        queryClient.invalidateQueries({
          queryKey: ["oauthStatus", toolsetId, issuer],
        });
      }
    }, 500);
  };

  if (!issuer) return null;

  const isConnected = oauthStatus?.status === "authenticated";
  const providerName =
    oauthStatus?.provider_name ||
    toolset.externalOauthServer?.slug ||
    "OAuth Provider";

  return (
    <div className="border rounded-md p-3 bg-muted/30">
      <Stack gap={2}>
        <Stack
          direction="horizontal"
          align="center"
          className="justify-between"
        >
          <Type variant="small" className="font-medium">
            External OAuth
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

  // Check if toolset has external OAuth configured (legacy path)
  const hasExternalOAuth = !!toolset.externalOauthServer?.metadata;

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

  // Show "no auth required" only if there are no env vars AND no OAuth of any kind
  if (envVars.length === 0 && !hasExternalOAuth && !hasExternalMcpOAuth) {
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

      {/* External OAuth Connection UI (legacy path) */}
      {hasExternalOAuth && !hasExternalMcpOAuth && (
        <OAuthConnection toolsetId={toolset.id} toolset={toolset} />
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
