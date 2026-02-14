import { Button } from "@/components/ui/button";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { useToolset } from "@/hooks/toolTypes";
import { getServerURL } from "@/lib/utils";
import type { ExternalMCPToolDefinition } from "@gram/client/models/components";
import { Badge, Stack } from "@speakeasy-api/moonshine";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle, ExternalLink, Loader2, LogOut } from "lucide-react";
import { useEffect, useRef } from "react";
import { toast } from "sonner";
import { z } from "zod/v4";

const ExternalMcpOAuthStatusResponseSchema = z.object({
  status: z.enum(["authenticated", "needs_auth", "disconnected"]),
  provider_name: z.string().optional(),
  expires_at: z.string().optional(),
});

type ExternalOAuthStatusResponse = z.infer<
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
    slug?: string;
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

/**
 * OAuth connection status component for external MCP tools discovered via MCP protocol.
 * This handles OAuth 2.1 with Dynamic Client Registration (DCR).
 */
export function ExternalMcpOAuthConnection({
  toolsetSlug,
  mcpOAuthConfig,
  variant = "compact",
}: {
  toolsetSlug: string;
  mcpOAuthConfig: ExternalMCPToolDefinition;
  variant?: "compact" | "wide";
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

  const statusBadge = statusLoading ? (
    <Loader2 className="size-4 animate-spin text-muted-foreground" />
  ) : isConnected ? (
    <Badge variant="success">
      <CheckCircle className="size-3 mr-1" />
      Connected
    </Badge>
  ) : (
    <Badge variant="warning">Not Connected</Badge>
  );

  if (variant === "wide") {
    const actionButton = isConnected ? (
      <Button
        size="sm"
        variant="outline"
        onClick={() => disconnectMutation.mutate()}
        disabled={disconnectMutation.isPending}
      >
        <LogOut className="size-3 mr-2" />
        Disconnect
      </Button>
    ) : (
      <Button size="sm" variant="default" onClick={handleConnect}>
        <ExternalLink className="size-3 mr-2" />
        Connect
      </Button>
    );

    return (
      <div className="border rounded-lg p-4 bg-muted/30">
        <Stack
          direction="horizontal"
          align="center"
          className="justify-between"
        >
          <Stack direction="horizontal" align="center" gap={3}>
            {statusBadge}
            <Type variant="small" className="font-medium">
              {oauthVersionLabel}
            </Type>
            <Type variant="small" className="text-muted-foreground">
              {providerName}
            </Type>
          </Stack>
          {actionButton}
        </Stack>
      </div>
    );
  }

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
          {statusBadge}
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
