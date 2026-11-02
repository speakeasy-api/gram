import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import type { Toolset } from "@/lib/toolTypes";
import { useToolsets } from "@/pages/toolsets/useToolsets";
import { useRoutes } from "@/routes";
import { invalidateAllGetPeriodUsage } from "@gram/client/react-query/getPeriodUsage.js";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import { useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import React, { useState } from "react";
import { toast } from "sonner";

/** Deletes a canonical hosted server with its toolset, as the toolset page did. */
export function DeleteHostedServerButton({
  toolset,
}: {
  toolset: Toolset;
}): React.JSX.Element {
  const telemetry = useTelemetry();
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const client = useSdkClient();
  const toolsets = useToolsets();
  const { data: deploymentResult, refetch: refetchDeployment } =
    useLatestDeployment();
  const deployment = deploymentResult?.deployment;
  const [isDeleteDialogOpen, setIsDeleteDialogOpen] = useState(false);
  const [isDeletingMcpServer, setIsDeletingMcpServer] = useState(false);

  const handleDeleteMcpServer = async () => {
    if (!toolset) return;

    setIsDeletingMcpServer(true);

    try {
      const externalMcpUrn = toolset.toolUrns?.find((urn) =>
        urn.includes(":externalmcp:"),
      );

      if (externalMcpUrn && deployment) {
        const parts = externalMcpUrn.split(":");
        const externalMcpSlug = parts[2];

        if (externalMcpSlug) {
          await client.deployments.evolveDeployment({
            evolveForm: {
              deploymentId: deployment.id,
              nonBlocking: true,
              excludeExternalMcps: [externalMcpSlug],
            },
          });
        }
      }

      await client.toolsets.deleteBySlug({ slug: toolset.slug });

      telemetry.capture("mcp_event", {
        action: "mcp_server_deleted",
        slug: toolset.slug,
      });

      void invalidateAllToolset(queryClient, { refetchType: "none" });
      void invalidateAllGetPeriodUsage(queryClient);
      void refetchDeployment();
      // Refresh both lists before navigating so the listing never shows the deleted server.
      await Promise.all([
        toolsets.refetch(),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);

      toast.success(`MCP server "${toolset.slug}" deleted`);
      setIsDeleteDialogOpen(false);
      routes.mcp.goTo();
    } catch (error) {
      console.error("Failed to delete MCP server:", error);
      toast.error(`Failed to delete MCP server "${toolset.slug}"`);
    } finally {
      setIsDeletingMcpServer(false);
    }
  };

  return (
    <>
      <Button
        variant="destructive-primary"
        size="md"
        onClick={() => setIsDeleteDialogOpen(true)}
        disabled={isDeleteDialogOpen}
      >
        <Button.LeftIcon>
          <Trash2 className="h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>Delete MCP server</Button.Text>
      </Button>
      <Dialog
        open={isDeleteDialogOpen}
        onOpenChange={(open) => {
          if (!isDeletingMcpServer) setIsDeleteDialogOpen(open);
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Delete MCP Server</Dialog.Title>
          </Dialog.Header>
          <div className="space-y-4 py-4">
            <Text variant="body">
              <code className="bg-muted px-1 py-0.5 font-mono font-bold">
                {toolset.name}
              </code>{" "}
              and all its configuration will be permanently deleted. Connected
              clients will immediately lose access. This action cannot be
              undone.
            </Text>
            <div className="flex justify-end space-x-2">
              <Button
                variant="secondary"
                onClick={() => setIsDeleteDialogOpen(false)}
                disabled={isDeletingMcpServer}
              >
                Cancel
              </Button>
              <Button
                variant="destructive-primary"
                onClick={() => void handleDeleteMcpServer()}
                disabled={isDeletingMcpServer}
              >
                Delete MCP Server
              </Button>
            </div>
          </div>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
