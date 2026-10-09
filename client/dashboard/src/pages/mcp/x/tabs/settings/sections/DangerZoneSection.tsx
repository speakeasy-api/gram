import { RequireScope } from "@/components/require-scope";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useIsSpeakeasyStaff } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { UNPROXIED_DELETE_STAFF_ONLY_MESSAGE } from "@/pages/sources/unproxied-mcp/hooks";
import { useRoutes } from "@/routes";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type {
  McpServer,
  McpServerVisibility,
} from "@gram/client/models/components/mcpserver.js";
import { useDeleteMcpServerMutation } from "@gram/client/react-query/deleteMcpServer.js";
import { buildGetMcpServerQuery } from "@gram/client/react-query/getMcpServer.js";
import { invalidateAllMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import {
  invalidateAllMcpServers,
  useMcpServers,
} from "@gram/client/react-query/mcpServers.js";
import { useUpdateMcpServerMutation } from "@gram/client/react-query/updateMcpServer.js";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Stack } from "@/components/ui/Stack";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";
import { DangerSettingsSection } from "@/components/detail/settings-section";
import {
  invalidateMcpServerQueries,
  mcpServerVisibilityToast,
  mcpServerVisibilityUpdateForm,
} from "@/lib/mcp-server-visibility";
import { DeleteSourceBackedServerDialogContent } from "./DeleteSourceBackedServerDialogContent";
import { DeleteTunnelDialogContent } from "./DeleteTunnelDialogContent";
import {
  linkedMcpServersFilter,
  serversBackedBySameSource,
  type CascadeDeleteTarget,
  type SourceBackedDeleteTarget,
} from "./sourceDelete";
import { invalidateWrapperDeleteAuthViews } from "./sourceInvalidation";

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
        <Text small className="font-medium">
          {title}
        </Text>
        <Text muted small className="max-w-2xl">
          {description}
        </Text>
      </div>
      <div className="flex shrink-0 items-center gap-2">{children}</div>
    </div>
  );
}

const SOURCE_KIND_LABEL: Record<CascadeDeleteTarget["kind"], string> = {
  remote: "remote MCP source",
  unproxied: "unproxied MCP source",
};

const DELETE_SERVER_ON_TUNNEL_DESCRIPTION =
  "Permanently remove this MCP server and its endpoints. The tunnel and any other MCP servers on it keep working. A tunnel no MCP server uses still counts toward your tunnel limit; reuse or delete it from Add MCP server, Existing tunnel.";

const DELETE_TUNNEL_DESCRIPTION =
  "Permanently remove the tunnel behind this server and the MCP servers on it that you review and confirm, with their endpoints. Running tunnel agents are disconnected.";

type DeleteDialogKind = "server" | "tunnel";

function deleteRowCopy(
  deleteTarget: CascadeDeleteTarget | undefined,
  sourceUnavailable: boolean,
  staffOnly: boolean,
): {
  title: string;
  description: string;
} {
  if (staffOnly) {
    return {
      title: "Delete MCP Server and Source",
      description: `${UNPROXIED_DELETE_STAFF_ONLY_MESSAGE} Deleting this server would also delete the unproxied MCP source behind it.`,
    };
  }
  if (sourceUnavailable && !deleteTarget) {
    return {
      title: "Delete MCP Server",
      description:
        "The source behind this server could not be loaded, so deleting removes only this server and its endpoints. This action cannot be undone.",
    };
  }
  if (!deleteTarget) {
    return {
      title: "Delete MCP Server",
      description:
        "Permanently remove this server and all of its endpoints. This action cannot be undone.",
    };
  }
  return {
    title: "Delete MCP Server and Source",
    description: `Permanently remove this server, the ${SOURCE_KIND_LABEL[deleteTarget.kind]} behind it, every other server backed by that source, and all of their endpoints. This action cannot be undone.`,
  };
}

export function DangerZoneSection({
  mcpServer,
  endpoints,
  deleteTarget,
  sourceUnavailable = false,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  /**
   * The source row behind a remote/tunneled/unproxied server, once loaded.
   * Deleting such a server must delete its source too, so the button waits
   * for this rather than fall back to a wrapper-only delete that would
   * orphan the source.
   */
  deleteTarget?: SourceBackedDeleteTarget;
  /**
   * The source row failed to load (already gone, or not readable). The
   * wrapper-only delete is then the only way to clear the server, so it is
   * allowed rather than leaving the button disabled forever.
   */
  sourceUnavailable?: boolean;
}): JSX.Element {
  const navigate = useNavigate();
  const routes = useRoutes();
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  // The dialog last opened, kept after it closes so its content stays put
  // while it animates out.
  const [deleteDialog, setDeleteDialog] = useState<DeleteDialogKind>("server");
  const openDeleteDialog = (kind: DeleteDialogKind) => {
    setDeleteDialog(kind);
    setDeleteDialogOpen(true);
  };
  // A delete in flight keeps the dialog open, so its outcome is never lost.
  const [deleteBusy, setDeleteBusy] = useState(false);
  const closeDeleteDialog = () => setDeleteDialogOpen(false);
  const leavePage = (href: string = routes.mcp.href()) => {
    setDeleteDialogOpen(false);
    void navigate(href);
  };

  // A tunnel is shared on purpose: its MCP servers are deleted one at a time,
  // and the tunnel through its own reviewed flow. Every other source deletes
  // its siblings along with the server, through the cascade below.
  const isTunneled = !!mcpServer.tunneledMcpServerId;
  const tunnelTarget =
    deleteTarget?.kind === "tunneled" ? deleteTarget : undefined;
  const cascadeTarget =
    deleteTarget?.kind === "tunneled" ? undefined : deleteTarget;
  const linkedFilter = isTunneled ? null : linkedMcpServersFilter(mcpServer);
  const isSourceBacked = linkedFilter !== null;
  const linkedQuery = useMcpServers(linkedFilter ?? undefined, undefined, {
    enabled: isSourceBacked,
  });
  const linkedMcpServers = serversBackedBySameSource(
    mcpServer,
    linkedQuery.data?.mcpServers ?? [],
  );
  // The unproxied source delete is staff-only server-side. Refuse up front
  // rather than let the cascade delete the wrappers and then be turned away.
  const isSpeakeasyStaff = useIsSpeakeasyStaff();
  const staffOnly = cascadeTarget?.kind === "unproxied" && !isSpeakeasyStaff;
  // A refetch in flight means the sibling list may be about to change; wait
  // for it so the dialog opens on the current set.
  const deleteReady =
    !staffOnly &&
    (!isSourceBacked ||
      (!!cascadeTarget && linkedQuery.isSuccess && !linkedQuery.isFetching) ||
      sourceUnavailable);
  const deleteRow = deleteRowCopy(cascadeTarget, sourceUnavailable, staffOnly);
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
      await invalidateMcpServerQueries(queryClient);
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
                <Text muted small>
                  {enabled ? "Enabled" : "Disabled"}
                </Text>
                <RequireScope
                  scope="mcp:write"
                  resourceId={mcpServer.projectId}
                  level="component"
                >
                  <Switch
                    checked={enabled}
                    disabled={isUpdatingVisibility}
                    aria-label="Enable MCP server"
                    onCheckedChange={requestAvailabilityChange}
                  />
                </RequireScope>
              </ServerControlRow>

              {isTunneled ? (
                <>
                  <ServerControlRow
                    title="Delete this MCP server"
                    description={DELETE_SERVER_ON_TUNNEL_DESCRIPTION}
                  >
                    <RequireScope
                      scope="mcp:write"
                      resourceId={mcpServer.id}
                      projectId={mcpServer.projectId}
                      level="component"
                    >
                      <Button
                        variant="destructive-primary"
                        size="md"
                        onClick={() => openDeleteDialog("server")}
                      >
                        <Button.LeftIcon>
                          <Trash2 className="h-4 w-4" />
                        </Button.LeftIcon>
                        <Button.Text>Delete MCP server</Button.Text>
                      </Button>
                    </RequireScope>
                  </ServerControlRow>
                  {tunnelTarget ? (
                    <ServerControlRow
                      title="Delete tunnel and its MCP servers"
                      description={DELETE_TUNNEL_DESCRIPTION}
                    >
                      <RequireScope
                        scope="mcp:write"
                        resourceId={tunnelTarget.source.projectId}
                        projectId={tunnelTarget.source.projectId}
                        level="component"
                      >
                        <Button
                          variant="destructive-primary"
                          size="md"
                          onClick={() => openDeleteDialog("tunnel")}
                        >
                          <Button.LeftIcon>
                            <Trash2 className="h-4 w-4" />
                          </Button.LeftIcon>
                          <Button.Text>Delete tunnel</Button.Text>
                        </Button>
                      </RequireScope>
                    </ServerControlRow>
                  ) : null}
                </>
              ) : (
                <ServerControlRow
                  title={deleteRow.title}
                  description={deleteRow.description}
                >
                  <RequireScope
                    scope="mcp:write"
                    resourceId={mcpServer.projectId}
                    level="component"
                  >
                    <Button
                      variant="destructive-primary"
                      size="md"
                      disabled={!deleteReady}
                      onClick={() => openDeleteDialog("server")}
                    >
                      <Button.LeftIcon>
                        <Trash2 className="h-4 w-4" />
                      </Button.LeftIcon>
                      <Button.Text>Delete MCP server</Button.Text>
                    </Button>
                  </RequireScope>
                </ServerControlRow>
              )}
            </div>
            {linkedQuery.isError && (
              // The delete stays disabled until the sibling list loads, since
              // the dialog must show what the cascade removes; say why, and
              // offer a way out other than reloading the page.
              <Alert variant="error" dismissible={false}>
                <Stack gap={2}>
                  <Text small>
                    Could not load the other servers backed by this source, so
                    deletion is unavailable.
                  </Text>
                  <div>
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={linkedQuery.isFetching}
                      onClick={() => void linkedQuery.refetch()}
                    >
                      <Button.Text>Retry</Button.Text>
                    </Button>
                  </div>
                </Stack>
              </Alert>
            )}
            {updateVisibility.isError && (
              <Alert variant="error" dismissible={false}>
                {updateVisibility.error.message}
              </Alert>
            )}
          </DangerSettingsSection.Body>
        </DangerSettingsSection.Panel>
      </DangerSettingsSection>
      <Dialog
        open={deleteDialogOpen}
        onOpenChange={(open) => {
          if (!open && !deleteBusy) closeDeleteDialog();
        }}
      >
        <Dialog.Content className="max-w-2xl!" closeable={!deleteBusy}>
          <DeleteDialogBody
            dialog={deleteDialog}
            mcpServer={mcpServer}
            endpoints={endpoints}
            tunnelTarget={tunnelTarget}
            cascadeTarget={cascadeTarget}
            linkedMcpServers={linkedMcpServers}
            onClose={closeDeleteDialog}
            onLeave={leavePage}
            onBusyChange={setDeleteBusy}
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

function DeleteDialogBody({
  dialog,
  mcpServer,
  endpoints,
  tunnelTarget,
  cascadeTarget,
  linkedMcpServers,
  onClose,
  onLeave,
  onBusyChange,
}: {
  dialog: DeleteDialogKind;
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  tunnelTarget:
    | Extract<SourceBackedDeleteTarget, { kind: "tunneled" }>
    | undefined;
  cascadeTarget: CascadeDeleteTarget | undefined;
  linkedMcpServers: McpServer[];
  onClose: () => void;
  onLeave: (href?: string) => void;
  onBusyChange: (busy: boolean) => void;
}): JSX.Element {
  if (dialog === "tunnel") {
    // Never fall through to another delete: the user asked to delete the
    // tunnel, and the server-only delete would keep it.
    if (!tunnelTarget) return <TunnelUnavailable onClose={onClose} />;
    return (
      <DeleteTunnelDialogContent
        tunnel={tunnelTarget.source}
        mcpServerId={mcpServer.id}
        onClose={onClose}
        onLeave={onLeave}
        onBusyChange={onBusyChange}
      />
    );
  }
  if (cascadeTarget) {
    return (
      <DeleteSourceBackedServerDialogContent
        target={cascadeTarget}
        linkedMcpServers={linkedMcpServers}
        onClose={onClose}
        onSuccess={() => onLeave()}
        onBusyChange={onBusyChange}
      />
    );
  }
  return (
    <DeleteMcpServerDialogContent
      mcpServer={mcpServer}
      endpoints={endpoints}
      keepsTunnel={!!mcpServer.tunneledMcpServerId}
      onBusyChange={onBusyChange}
      onClose={onClose}
      onSuccess={() => onLeave()}
    />
  );
}

// The tunnel row went away while its delete dialog was open, for example
// because it was deleted in another tab or could no longer be read.
function TunnelUnavailable({ onClose }: { onClose: () => void }) {
  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Tunnel unavailable</Dialog.Title>
        <Dialog.Description>
          The tunnel behind this server could not be loaded, so it cannot be
          deleted from here. Nothing was deleted.
        </Dialog.Description>
      </Dialog.Header>
      <Dialog.Footer>
        <Button variant="secondary" onClick={onClose}>
          <Button.Text>Close</Button.Text>
        </Button>
      </Dialog.Footer>
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
            {isLoading && (
              <Button.LeftIcon>
                <Loader2 aria-hidden="true" className="size-4 animate-spin" />
              </Button.LeftIcon>
            )}
            <Button.Text>{isLoading ? "Saving" : "Continue"}</Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

function DeleteMcpServerDialogContent({
  mcpServer,
  endpoints,
  keepsTunnel,
  onBusyChange,
  onClose,
  onSuccess,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  /** The server is on a tunnel, which this delete leaves in place. */
  keepsTunnel: boolean;
  onBusyChange: (busy: boolean) => void;
  onClose: () => void;
  onSuccess: () => void;
}) {
  const queryClient = useQueryClient();
  const remove = useDeleteMcpServerMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
        invalidateWrapperDeleteAuthViews(queryClient, { refetchType: "all" }),
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

  useEffect(
    () => onBusyChange(remove.isPending),
    [remove.isPending, onBusyChange],
  );

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
        <Text>
          This will soft-delete the MCP server <strong>{mcpServer.name}</strong>{" "}
          and the following endpoints. The action cannot be undone.
        </Text>
        {endpoints.length > 0 ? (
          <ul className="list-disc pl-6">
            {endpoints.map((endpoint) => (
              <li key={endpoint.id}>
                <Text small className="font-mono">
                  {endpoint.slug}
                  {endpoint.customDomainId
                    ? " (custom domain)"
                    : " (platform-hosted)"}
                </Text>
              </li>
            ))}
          </ul>
        ) : (
          <Text muted small>
            No endpoints are currently associated with this MCP server.
          </Text>
        )}
        {keepsTunnel ? (
          <Text small muted>
            The tunnel and any other MCP servers on it are not affected. If no
            MCP server uses the tunnel afterwards, it still counts toward your
            tunnel limit until you reuse or delete it from Add MCP server,
            Existing tunnel.
          </Text>
        ) : null}
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
