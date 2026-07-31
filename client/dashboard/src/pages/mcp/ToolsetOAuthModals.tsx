import { CodeBlock } from "@/components/code";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import { useTelemetry } from "@/contexts/Telemetry";
import { useMcpUrl } from "@/hooks/useToolsetUrl";
import { Toolset } from "@/lib/toolTypes";
import { useAddOAuthProxyServerMutation } from "@gram/client/react-query/addOAuthProxyServer.js";
import { useRemoveOAuthServerMutation } from "@gram/client/react-query/removeOAuthServer.js";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { useQueryClient } from "@tanstack/react-query";
import { Pencil, Trash2 } from "lucide-react";
import React from "react";
import { toast } from "sonner";

export function OAuthDetailsModal({
  isOpen,
  onClose,
  toolset,
  onEditRequest,
}: {
  isOpen: boolean;
  onClose: () => void;
  toolset: Toolset;
  onEditRequest: () => void;
}): React.JSX.Element {
  const { url: mcpUrl } = useMcpUrl(toolset);
  const queryClient = useQueryClient();

  const removeOAuthMutation = useRemoveOAuthServerMutation({
    onSuccess: () => {
      void invalidateAllToolset(queryClient);
      onClose();
    },
  });

  const isGramOAuth =
    toolset.oauthProxyServer?.oauthProxyProviders?.[0]?.providerType === "gram";

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="flex max-h-[80vh] max-w-2xl flex-col">
        <Dialog.Header className="shrink-0">
          <Dialog.Title>
            {toolset.externalOauthServer
              ? "External OAuth Configuration"
              : isGramOAuth
                ? "Platform OAuth Configuration"
                : "OAuth Proxy Configuration"}
          </Dialog.Title>
        </Dialog.Header>
        <div className="flex-1 overflow-y-auto">
          <Stack gap={4}>
            {toolset.oauthProxyServer && isGramOAuth && (
              <>
                <div>
                  <Text className="font-medium">Platform OAuth is Active</Text>
                </div>
                <Stack gap={2} className="">
                  <Text className="mb-2">
                    Platform users with access to your organization can use this
                    MCP server.
                  </Text>
                  {toolset.oauthProxyServer.oauthProxyProviders?.[0]
                    ?.environmentSlug && (
                    <div>
                      <Text small className="text-muted-foreground font-medium">
                        Environment:
                      </Text>
                      <CodeBlock className="mt-1">
                        {
                          toolset.oauthProxyServer.oauthProxyProviders[0]
                            .environmentSlug
                        }
                      </CodeBlock>
                    </div>
                  )}
                </Stack>
              </>
            )}
            {toolset.oauthProxyServer && !isGramOAuth && (
              <>
                <div className="flex items-center justify-between">
                  <Text className="font-medium">OAuth Proxy Server</Text>
                  <div className="flex items-center gap-2">
                    <Button
                      variant="tertiary"
                      size="sm"
                      onClick={() => {
                        onClose();
                        onEditRequest();
                      }}
                    >
                      <Button.LeftIcon>
                        <Pencil aria-hidden="true" className="h-4 w-4" />
                      </Button.LeftIcon>
                      <Button.Text>Edit</Button.Text>
                    </Button>
                    <Button
                      variant="tertiary"
                      size="sm"
                      className="hover:bg-destructive border-none hover:text-white"
                      onClick={() =>
                        removeOAuthMutation.mutate({
                          request: {
                            slug: toolset.slug,
                          },
                        })
                      }
                    >
                      <Button.LeftIcon>
                        <Trash2 aria-hidden="true" className="h-4 w-4" />
                      </Button.LeftIcon>
                      <Button.Text>Unlink</Button.Text>
                    </Button>
                  </div>
                </div>
                <Stack gap={2} className="pl-4">
                  <div>
                    <Text small className="text-muted-foreground font-medium">
                      Server Slug:
                    </Text>
                    <CodeBlock className="mt-1">
                      {toolset.oauthProxyServer.slug}
                    </CodeBlock>
                  </div>
                  {toolset.oauthProxyServer.audience && (
                    <div>
                      <Text small className="text-muted-foreground font-medium">
                        Audience:
                      </Text>
                      <CodeBlock className="mt-1">
                        {toolset.oauthProxyServer.audience}
                      </CodeBlock>
                    </div>
                  )}
                </Stack>
              </>
            )}
            {toolset.oauthProxyServer?.oauthProxyProviders?.map(
              (provider) =>
                provider.providerType !== "gram" && (
                  <Stack key={provider.id} gap={2}>
                    <Stack gap={2} className="pl-4">
                      <div>
                        <Text
                          small
                          className="text-muted-foreground font-medium"
                        >
                          Authorization Endpoint:
                        </Text>
                        <CodeBlock className="mt-1">
                          {provider.authorizationEndpoint}
                        </CodeBlock>
                      </div>
                      <div>
                        <Text
                          small
                          className="text-muted-foreground font-medium"
                        >
                          Token Endpoint:
                        </Text>
                        <CodeBlock className="mt-1">
                          {provider.tokenEndpoint}
                        </CodeBlock>
                      </div>
                      {provider.tokenEndpointAuthMethodsSupported &&
                        provider.tokenEndpointAuthMethodsSupported.length >
                          0 && (
                          <div>
                            <Text
                              small
                              className="text-muted-foreground font-medium"
                            >
                              Token Auth Method:
                            </Text>
                            <CodeBlock className="mt-1">
                              {provider.tokenEndpointAuthMethodsSupported.join(
                                ", ",
                              )}
                            </CodeBlock>
                          </div>
                        )}
                      {provider.scopesSupported &&
                        provider.scopesSupported.length > 0 && (
                          <div>
                            <Text
                              small
                              className="text-muted-foreground font-medium"
                            >
                              Supported Scopes:
                            </Text>
                            <CodeBlock className="mt-1">
                              {provider.scopesSupported.join(", ")}
                            </CodeBlock>
                          </div>
                        )}
                      {provider.environmentSlug && (
                        <div>
                          <Text
                            small
                            className="text-muted-foreground font-medium"
                          >
                            Environment:
                          </Text>
                          <CodeBlock className="mt-1">
                            {provider.environmentSlug}
                          </CodeBlock>
                        </div>
                      )}
                    </Stack>
                  </Stack>
                ),
            )}
            {toolset.externalOauthServer && (
              <Stack gap={2}>
                <div className="flex items-center justify-between">
                  <Text className="font-medium">External OAuth Server</Text>
                  <Button
                    variant="tertiary"
                    size="sm"
                    className="text-muted-foreground hover:text-destructive hover:border-destructive"
                    onClick={() =>
                      removeOAuthMutation.mutate({
                        request: { slug: toolset.slug },
                      })
                    }
                  >
                    <Button.LeftIcon>
                      <Trash2 className="h-4 w-4" />
                    </Button.LeftIcon>
                    <Button.Text className="sr-only">Remove OAuth</Button.Text>
                  </Button>
                </div>
                <Stack gap={2} className="pl-4">
                  <div>
                    <Text small className="text-muted-foreground font-medium">
                      External OAuth Server Slug:
                    </Text>
                    <CodeBlock className="mt-1">
                      {toolset.externalOauthServer.slug}
                    </CodeBlock>
                  </div>
                  <div>
                    <Text small className="text-muted-foreground font-medium">
                      OAuth Authorization Server Discovery URL:
                    </Text>
                    <CodeBlock className="mt-1">
                      {mcpUrl
                        ? `${new URL(mcpUrl).origin}/.well-known/oauth-authorization-server/mcp/${
                            toolset.mcpSlug
                          }`
                        : ""}
                    </CodeBlock>
                  </div>
                  <div>
                    <Text small className="text-muted-foreground font-medium">
                      OAuth Authorization Server Metadata:
                    </Text>
                    <CodeBlock className="mt-1">
                      {JSON.stringify(
                        toolset.externalOauthServer.metadata,
                        null,
                        2,
                      )}
                    </CodeBlock>
                  </div>
                </Stack>
              </Stack>
            )}
          </Stack>
        </div>
        {isGramOAuth && (
          <Dialog.Footer>
            <Button variant="tertiary" onClick={onClose}>
              Close
            </Button>
            <Button
              variant="destructive-primary"
              onClick={() =>
                removeOAuthMutation.mutate({
                  request: { slug: toolset.slug },
                })
              }
            >
              <Button.LeftIcon>
                <Trash2 aria-hidden="true" className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Unlink</Button.Text>
            </Button>
          </Dialog.Footer>
        )}
      </Dialog.Content>
    </Dialog>
  );
}

export function GramOAuthProxyModal({
  isOpen,
  onClose,
  toolset,
}: {
  isOpen: boolean;
  onClose: () => void;
  toolset: Toolset;
}): React.JSX.Element {
  const telemetry = useTelemetry();
  const queryClient = useQueryClient();

  const addOAuthProxyMutation = useAddOAuthProxyServerMutation({
    onSuccess: () => {
      void invalidateAllToolset(queryClient);
      toast.success("Platform OAuth configured successfully");
      telemetry.capture("mcp_event", {
        action: "gram_oauth_proxy_configured",
        slug: toolset.slug,
      });
      onClose();
    },
    onError: (error) => {
      console.error("Failed to configure Platform OAuth:", error);
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to configure Platform OAuth",
      );
    },
  });

  const handleSubmit = () => {
    addOAuthProxyMutation.mutate({
      request: {
        slug: toolset.slug,
        addOAuthProxyServerRequestBody: {
          oauthProxyServer: {
            providerType: "gram",
            slug: "gram-oauth-proxy",
          },
        },
      },
    });
  };

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="max-h-[90vh] max-w-2xl overflow-hidden">
        <Dialog.Header>
          <Dialog.Title>Platform OAuth</Dialog.Title>
        </Dialog.Header>

        <div className="max-h-[60vh] space-y-4 overflow-auto">
          <div>
            <Text className="mb-2 font-medium">
              Platform OAuth Configuration
            </Text>
            <Text small className="mb-4">
              Configure Platform OAuth to let users with access to your
              organization use this MCP server. Users will authenticate using
              their platform credentials.
            </Text>
          </div>
        </div>

        <Dialog.Footer className="flex justify-end">
          <Button
            onClick={handleSubmit}
            disabled={addOAuthProxyMutation.isPending}
          >
            {addOAuthProxyMutation.isPending
              ? "Enabling..."
              : "Enable Platform OAuth"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
