import { RequireScope } from "@/components/require-scope";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import type {
  McpEndpoint,
  McpServer,
  McpServerVisibility,
} from "@gram/client/models/components";
import {
  buildGetMcpServerQuery,
  invalidateAllGetMcpServer,
  invalidateAllMcpEndpoints,
  invalidateAllMcpServers,
  useDeleteMcpServerMutation,
  useUpdateMcpServerMutation,
} from "@gram/client/react-query/index.js";
import { Alert, Button, Dialog, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, Trash2 } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";
import { DangerSettingsSection } from "../SettingsSection";

function mcpServerVisibilityUpdateForm(
  mcpServer: McpServer,
  visibility: McpServerVisibility,
) {
  return {
    id: mcpServer.id,
    name: mcpServer.name ?? undefined,
    remoteMcpServerId: mcpServer.remoteMcpServerId ?? undefined,
    toolsetId: mcpServer.toolsetId ?? undefined,
    environmentId: mcpServer.environmentId ?? undefined,
    userSessionIssuerId: mcpServer.userSessionIssuerId ?? undefined,
    toolVariationsGroupId: mcpServer.toolVariationsGroupId ?? undefined,
    visibility,
  };
}

function mcpServerVisibilityToast(visibility: McpServerVisibility) {
  switch (visibility) {
    case "disabled":
      return "MCP server disabled";
    case "private":
      return "MCP server enabled";
    case "public":
      return "MCP server set to public";
    default:
      return "MCP server updated";
  }
}

function ServerControlRow({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-3 py-4 first:pt-0 last:pb-0 sm:flex-row sm:items-center sm:justify-between">
      <div className="min-w-0 space-y-1">
        <Type small className="font-medium">
          {title}
        </Type>
        <Type muted small className="max-w-2xl">
          {description}
        </Type>
      </div>
      <div className="flex shrink-0 items-center gap-2">{children}</div>
    </div>
  );
}

export function DangerZoneSection({
  mcpServer,
  endpoints,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
}): JSX.Element {
  const navigate = useNavigate();
  const routes = useRoutes();
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [pendingAvailability, setPendingAvailability] =
    useState<McpServerVisibility | null>(null);
  const queryClient = useQueryClient();
  const client = useSdkClient();
  const [isFetchingLatestMcpServer, setIsFetchingLatestMcpServer] =
    useState(false);
  const notifyVisibilityUpdateError = (error: unknown) => {
    toast.error(
      error instanceof Error ? error.message : "Failed to update MCP server",
    );
  };
  const updateVisibility = useUpdateMcpServerMutation({
    onSuccess: async (_data, variables) => {
      await Promise.all([
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
      const next = variables.request.updateMcpServerForm.visibility;
      toast.success(mcpServerVisibilityToast(next));
    },
    onError: notifyVisibilityUpdateError,
  });
  const isUpdatingVisibility =
    isFetchingLatestMcpServer || updateVisibility.isPending;

  const applyVisibility = async (visibility: McpServerVisibility) => {
    if (visibility === mcpServer.visibility) return;
    setIsFetchingLatestMcpServer(true);
    try {
      const latestMcpServer = await queryClient.fetchQuery({
        ...buildGetMcpServerQuery(client, { id: mcpServer.id }),
        staleTime: 0,
      });

      if (visibility === latestMcpServer.visibility) return;

      updateVisibility.mutate({
        request: {
          updateMcpServerForm: mcpServerVisibilityUpdateForm(
            latestMcpServer,
            visibility,
          ),
        },
      });
    } catch (error) {
      notifyVisibilityUpdateError(error);
    } finally {
      setIsFetchingLatestMcpServer(false);
    }
  };

  const requestAvailabilityChange = (checked: boolean) => {
    setPendingAvailability(checked ? "private" : "disabled");
  };

  const confirmAvailabilityChange = () => {
    if (!pendingAvailability) return;
    void applyVisibility(pendingAvailability);
    setPendingAvailability(null);
  };

  const enabled = mcpServer.visibility !== "disabled";

  return (
    <>
      <DangerSettingsSection>
        <DangerSettingsSection.Header>
          <DangerSettingsSection.Title>Danger Zone</DangerSettingsSection.Title>
          <DangerSettingsSection.Description>
            Manage server availability and destructive actions.
          </DangerSettingsSection.Description>
        </DangerSettingsSection.Header>
        <DangerSettingsSection.Panel>
          <DangerSettingsSection.Body>
            <div className="divide-y">
              <ServerControlRow
                title="Server Availability"
                description={
                  enabled
                    ? "This MCP server is currently serving traffic on configured URLs."
                    : "This MCP server is offline and will not serve client traffic."
                }
              >
                <Type muted small>
                  {enabled ? "Enabled" : "Disabled"}
                </Type>
                <RequireScope scope="mcp:write" level="component">
                  <Switch
                    checked={enabled}
                    disabled={isUpdatingVisibility}
                    aria-label="Enable MCP server"
                    onCheckedChange={requestAvailabilityChange}
                  />
                </RequireScope>
              </ServerControlRow>

              <ServerControlRow
                title="Delete MCP Server"
                description="Permanently remove this server and all of its endpoints. This action cannot be undone."
              >
                <RequireScope scope="mcp:write" level="component">
                  <Button
                    variant="destructive-primary"
                    size="md"
                    onClick={() => setDeleteDialogOpen(true)}
                  >
                    <Button.LeftIcon>
                      <Trash2 className="h-4 w-4" />
                    </Button.LeftIcon>
                    <Button.Text>Delete MCP server</Button.Text>
                  </Button>
                </RequireScope>
              </ServerControlRow>
            </div>
            {updateVisibility.isError && (
              <Alert variant="error" dismissible={false}>
                {updateVisibility.error.message}
              </Alert>
            )}
          </DangerSettingsSection.Body>
        </DangerSettingsSection.Panel>
      </DangerSettingsSection>
      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <Dialog.Content className="max-w-2xl!">
          <DeleteMcpServerDialogContent
            mcpServer={mcpServer}
            endpoints={endpoints}
            onClose={() => setDeleteDialogOpen(false)}
            onSuccess={() => {
              setDeleteDialogOpen(false);
              void navigate(routes.mcp.href());
            }}
          />
        </Dialog.Content>
      </Dialog>
      <ServerAvailabilityDialog
        targetVisibility={pendingAvailability}
        isLoading={isUpdatingVisibility}
        onClose={() => setPendingAvailability(null)}
        onConfirm={confirmAvailabilityChange}
      />
    </>
  );
}

function ServerAvailabilityDialog({
  targetVisibility,
  isLoading,
  onClose,
  onConfirm,
}: {
  targetVisibility: McpServerVisibility | null;
  isLoading: boolean;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const isOpen = targetVisibility != null;
  const enabling = targetVisibility !== "disabled";
  let title = "Disable MCP server?";
  let message =
    "You are about to disable this MCP server. Users will no longer be able to connect to it. Continue?";

  if (enabling) {
    title = "Enable MCP server?";
    message =
      "You are about to enable this MCP server. Users will be able to connect to it and perform tool calls. Continue?";
  }

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="max-w-md">
        <Dialog.Header>
          <Dialog.Title>{title}</Dialog.Title>
          <Dialog.Description>{message}</Dialog.Description>
        </Dialog.Header>
        <Dialog.Footer>
          <Button variant="secondary" disabled={isLoading} onClick={onClose}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant={enabling ? "primary" : "destructive-primary"}
            disabled={isLoading}
            onClick={onConfirm}
          >
            {isLoading ? (
              <>
                <Button.LeftIcon>
                  <Loader2 className="size-4 animate-spin" />
                </Button.LeftIcon>
                <Button.Text>Saving</Button.Text>
              </>
            ) : (
              <Button.Text>Continue</Button.Text>
            )}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

function DeleteMcpServerDialogContent({
  mcpServer,
  endpoints,
  onClose,
  onSuccess,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  onClose: () => void;
  onSuccess: () => void;
}) {
  const queryClient = useQueryClient();
  const remove = useDeleteMcpServerMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
      ]);
      toast.success("MCP server deleted");
      onSuccess();
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to delete MCP server",
      );
    },
  });

  const handleConfirm = () => {
    remove.mutate({ request: { id: mcpServer.id } });
  };

  let deleteButtonContent = <Button.Text>Delete MCP server</Button.Text>;
  if (remove.isPending) {
    deleteButtonContent = (
      <>
        <Button.LeftIcon>
          <Loader2 className="size-4 animate-spin" />
        </Button.LeftIcon>
        <Button.Text>Deleting</Button.Text>
      </>
    );
  }

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Delete this MCP server?</Dialog.Title>
      </Dialog.Header>
      <Stack gap={3}>
        <Type>
          This will soft-delete the MCP server <strong>{mcpServer.name}</strong>{" "}
          and the following endpoints. The action cannot be undone.
        </Type>
        {endpoints.length > 0 ? (
          <ul className="list-disc pl-6">
            {endpoints.map((endpoint) => (
              <li key={endpoint.id}>
                <Type small className="font-mono">
                  {endpoint.slug}
                  {endpoint.customDomainId
                    ? " (custom domain)"
                    : " (platform-hosted)"}
                </Type>
              </li>
            ))}
          </ul>
        ) : (
          <Type muted small>
            No endpoints are currently associated with this MCP server.
          </Type>
        )}
        {remove.isError && (
          <Alert variant="error" dismissible={false}>
            {remove.error.message}
          </Alert>
        )}
        <Stack direction="horizontal" gap={2}>
          <Button
            variant="destructive-primary"
            disabled={remove.isPending}
            onClick={handleConfirm}
          >
            {deleteButtonContent}
          </Button>
          <Button
            variant="secondary"
            disabled={remove.isPending}
            onClick={onClose}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
        </Stack>
      </Stack>
    </>
  );
}
