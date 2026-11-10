import { CodeBlock } from "@/components/code";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useMcpUrl } from "@/hooks/useToolsetUrl";
import type { Toolset } from "@/lib/toolTypes";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useRemoveOAuthServerMutation } from "@gram/client/react-query/removeOAuthServer.js";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import { useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import type React from "react";

export function OAuthDetailsModal({
  isOpen,
  onClose,
  onManageMetadata,
  toolset,
}: {
  isOpen: boolean;
  onClose: () => void;
  onManageMetadata?: () => void;
  toolset: Toolset;
}): React.JSX.Element {
  const { url: mcpUrl } = useMcpUrl(toolset);
  const queryClient = useQueryClient();

  const removeOAuthMutation = useRemoveOAuthServerMutation({
    onSuccess: () => {
      void invalidateAllToolset(queryClient);
      void invalidateAllMcpServers(queryClient);
      onClose();
    },
  });

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="max-w-2xl">
        <Dialog.Header>
          <Dialog.Title>External OAuth Configuration</Dialog.Title>
        </Dialog.Header>
        <div>
          <Stack gap={4}>
            {toolset.externalOauthServer && (
              <Stack gap={2}>
                <Text className="font-medium">External OAuth Server</Text>
                <Stack gap={2} className="pl-4">
                  <div>
                    <Text small className="text-muted-foreground font-medium">
                      External OAuth Server Slug:
                    </Text>
                    <CodeBlock className="mt-1">
                      {toolset.externalOauthServer.slug}
                    </CodeBlock>
                  </div>
                  {!toolset.externalOauthServer.authorizationServerIssuer && (
                    <div>
                      <Text small className="text-muted-foreground font-medium">
                        OAuth Authorization Server Discovery URL:
                      </Text>
                      <CodeBlock
                        className="mt-1"
                        innerClassName="max-h-24 overflow-auto"
                      >
                        {mcpUrl
                          ? `${new URL(mcpUrl).origin}/.well-known/oauth-authorization-server/mcp/${
                              toolset.mcpSlug
                            }`
                          : ""}
                      </CodeBlock>
                    </div>
                  )}
                  <div>
                    <Text small className="text-muted-foreground font-medium">
                      Metadata Source:
                    </Text>
                    <CodeBlock className="mt-1">
                      {toolset.externalOauthServer.authorizationServerIssuer
                        ? "Provider hosted"
                        : "Speakeasy hosted"}
                    </CodeBlock>
                  </div>
                  {toolset.externalOauthServer.authorizationServerIssuer ? (
                    <div>
                      <Text small className="text-muted-foreground font-medium">
                        Issuer URL:
                      </Text>
                      <CodeBlock
                        className="mt-1"
                        innerClassName="max-h-24 overflow-auto"
                      >
                        {toolset.externalOauthServer.authorizationServerIssuer}
                      </CodeBlock>
                    </div>
                  ) : (
                    <div>
                      <Text small className="text-muted-foreground font-medium">
                        OAuth Authorization Server Metadata:
                      </Text>
                      <CodeBlock
                        className="mt-1"
                        innerClassName="max-h-64 overflow-auto"
                      >
                        {JSON.stringify(
                          toolset.externalOauthServer.metadata,
                          null,
                          2,
                        )}
                      </CodeBlock>
                    </div>
                  )}
                </Stack>
              </Stack>
            )}
          </Stack>
        </div>
        {toolset.externalOauthServer && (
          <Dialog.Footer>
            {onManageMetadata && (
              <Button variant="secondary" onClick={onManageMetadata}>
                Manage metadata source
              </Button>
            )}
            <Button
              variant="destructive-secondary"
              disabled={removeOAuthMutation.isPending}
              onClick={() =>
                removeOAuthMutation.mutate({
                  request: { slug: toolset.slug },
                })
              }
            >
              <Button.LeftIcon>
                <Trash2 className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Remove OAuth</Button.Text>
            </Button>
          </Dialog.Footer>
        )}
      </Dialog.Content>
    </Dialog>
  );
}
