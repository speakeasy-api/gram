import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Type } from "@/components/ui/type";
import { useSession } from "@/contexts/Auth";
import { Toolset } from "@/lib/toolTypes";
import { getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { ExternalMCPToolDefinition } from "@gram/client/models/components";
import { Badge, Stack } from "@speakeasy-api/moonshine";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle, ExternalLink, Loader2, LogOut } from "lucide-react";
import { useMemo } from "react";
import { toast } from "sonner";

/**
 * Extract OAuth configuration from external MCP tools in the toolset.
 * Returns the first external MCP tool that requires OAuth, or undefined.
 */
function getExternalMcpOAuthConfig(
  toolset: Toolset,
): ExternalMCPToolDefinition | undefined {
  for (const tool of toolset.rawTools ?? []) {
    if (
      tool.externalMcpToolDefinition?.requiresOauth &&
      tool.externalMcpToolDefinition.oauthVersion !== "none"
    ) {
      return tool.externalMcpToolDefinition;
    }
  }
  return undefined;
}

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

/**
 * OAuth connection status component for external MCP tools discovered via MCP protocol.
 * This handles OAuth 2.1 with Dynamic Client Registration (DCR).
 */
function ExternalMcpOAuthConnection({
  toolset,
  mcpOAuthConfig,
}: {
  toolset: Toolset;
  mcpOAuthConfig: ExternalMCPToolDefinition;
}) {
  const queryClient = useQueryClient();
  const apiUrl = getServerURL();
  const session = useSession();

  // Use the authorization endpoint as the issuer for querying status
  const issuer = mcpOAuthConfig.oauthAuthorizationEndpoint
    ? new URL(mcpOAuthConfig.oauthAuthorizationEndpoint).origin
    : undefined;

  // Query OAuth status
  const { data: oauthStatus, isLoading: statusLoading } = useQuery({
    queryKey: ["mcpOauthStatus", toolset.id, mcpOAuthConfig.slug],
    queryFn: async () => {
      if (!issuer) return { status: "needs_auth" as const };

      const params = new URLSearchParams({
        toolset_id: toolset.id,
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
    enabled: !!issuer,
    staleTime: 30 * 1000,
    refetchOnWindowFocus: true,
  });

  // Disconnect mutation
  const disconnectMutation = useMutation({
    mutationFn: async () => {
      if (!issuer) throw new Error("No issuer configured");

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
        queryKey: ["mcpOauthStatus", toolset.id, mcpOAuthConfig.slug],
      });
      toast.success(`Disconnected from ${mcpOAuthConfig.name || mcpOAuthConfig.slug}`);
    },
    onError: () => {
      toast.error("Failed to disconnect");
    },
  });

  // Handle connect click - initiates OAuth flow
  const handleConnect = () => {
    if (!mcpOAuthConfig.oauthAuthorizationEndpoint) return;

    const params = new URLSearchParams({
      toolset_id: toolset.id,
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
        queryClient.invalidateQueries({
          queryKey: ["mcpOauthStatus", toolset.id, mcpOAuthConfig.slug],
        });
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
        <Stack direction="horizontal" align="center" className="justify-between">
          <Type variant="small" className="font-medium">
            {oauthVersionLabel}
          </Type>
          {statusLoading ? (
            <Loader2 className="size-4 animate-spin text-muted-foreground" />
          ) : isConnected ? (
            <Badge variant="success" size="sm">
              <CheckCircle className="size-3 mr-1" />
              Connected
            </Badge>
          ) : (
            <Badge variant="warning" size="sm">
              Not Connected
            </Badge>
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
function OAuthConnection({ toolset }: { toolset: Toolset }) {
  const session = useSession();
  const queryClient = useQueryClient();
  const apiUrl = getServerURL();

  // Extract OAuth metadata from toolset
  const oauthMetadata = toolset.externalOauthServer?.metadata as {
    issuer?: string;
  } | undefined;
  const issuer = oauthMetadata?.issuer;

  // Query OAuth status
  const { data: oauthStatus, isLoading: statusLoading } = useQuery({
    queryKey: ["oauthStatus", toolset.id, issuer],
    queryFn: async () => {
      if (!issuer) return null;

      const params = new URLSearchParams({
        toolset_id: toolset.id,
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
        toolset_id: toolset.id,
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
        queryKey: ["oauthStatus", toolset.id, issuer],
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
      toolset_id: toolset.id,
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
          queryKey: ["oauthStatus", toolset.id, issuer],
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
        <Stack direction="horizontal" align="center" className="justify-between">
          <Type variant="small" className="font-medium">
            External OAuth
          </Type>
          {statusLoading ? (
            <Loader2 className="size-4 animate-spin text-muted-foreground" />
          ) : isConnected ? (
            <Badge variant="success" size="sm">
              <CheckCircle className="size-3 mr-1" />
              Connected
            </Badge>
          ) : (
            <Badge variant="warning" size="sm">
              Not Connected
            </Badge>
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

export function PlaygroundAuth({ toolset, environment }: PlaygroundAuthProps) {
  const routes = useRoutes();

  // Check if toolset has external OAuth configured (legacy path)
  const hasExternalOAuth = !!toolset.externalOauthServer?.metadata;

  // Check if toolset has external MCP tools that require OAuth (MCP protocol discovery)
  const mcpOAuthConfig = useMemo(
    () => getExternalMcpOAuthConfig(toolset),
    [toolset],
  );
  const hasExternalMcpOAuth = !!mcpOAuthConfig;

  const relevantEnvVars = useMemo(() => {
    const securityVars =
      toolset?.securityVariables?.flatMap((secVar) => secVar.envVariables) ??
      [];
    // In playground, always filter out server_url variables since they can't be configured here
    const serverVars =
      toolset?.serverVariables?.flatMap((serverVar) =>
        serverVar.envVariables.filter(
          (v) => !v.toLowerCase().includes("server_url"),
        ),
      ) ?? [];
    const functionEnvVars =
      toolset?.functionEnvironmentVariables?.map((fnVar) => fnVar.name) ?? [];

    return [...securityVars, ...serverVars, ...functionEnvVars];
  }, [
    toolset?.securityVariables,
    toolset?.serverVariables,
    toolset.functionEnvironmentVariables,
  ]);

  // Show "no auth required" only if there are no env vars AND no OAuth of any kind
  if (relevantEnvVars.length === 0 && !hasExternalOAuth && !hasExternalMcpOAuth) {
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
          toolset={toolset}
          mcpOAuthConfig={mcpOAuthConfig}
        />
      )}

      {/* External OAuth Connection UI (legacy path) */}
      {hasExternalOAuth && !hasExternalMcpOAuth && (
        <OAuthConnection toolset={toolset} />
      )}

      {/* Environment Variables */}
      {relevantEnvVars.map((varName) => {
        const entry =
          environment?.entries?.find((e) => e.name === varName) ?? null;
        const isSecret = SECRET_FIELD_INDICATORS.some((indicator) =>
          varName.toUpperCase().includes(indicator),
        );
        const hasExistingValue =
          entry?.value != null && entry.value.trim() !== "";
        const displayValue = hasExistingValue
          ? isSecret
            ? PASSWORD_MASK
            : entry.value
          : "";

        return (
          <div key={varName} className="space-y-1.5">
            <Label htmlFor={`auth-${varName}`} className="text-xs font-medium">
              {varName}
            </Label>
            <Input
              id={`auth-${varName}`}
              value={displayValue}
              placeholder={hasExistingValue ? "Configured" : "Not set"}
              type={isSecret ? "password" : "text"}
              className="font-mono text-xs h-7"
              readOnly
              disabled
            />
          </div>
        );
      })}

      {relevantEnvVars.length > 0 && (
        <Type variant="small" className="text-muted-foreground pt-2">
          Configure auth in the{" "}
          <routes.toolsets.toolset.Link
            params={[toolset.slug]}
            hash="auth"
            className="underline hover:text-foreground"
          >
            toolset settings
          </routes.toolsets.toolset.Link>
        </Type>
      )}
    </div>
  );
}
