import { Block, BlockInner } from "@/components/block";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpEndpoint, McpServer } from "@gram/client/models/components";
import {
  invalidateAllGetMcpServer,
  invalidateAllMcpEndpoints,
  invalidateAllMcpServers,
  useDeleteMcpServerMutation,
  useUpdateMcpServerMutation,
} from "@gram/client/react-query/index.js";
import { Alert, Button, Dialog, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";

export function SettingsTab({
  mcpServer,
  endpoints,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
}) {
  return (
    <div className="mx-auto w-full max-w-[1270px] space-y-8 px-8 py-8">
      <GeneralSection mcpServer={mcpServer} />
      {/* TODO(AGE-2238): wire the Publishing section in once collections
          attachments learn about mcp_server_id. */}
      <DangerZoneSection mcpServer={mcpServer} endpoints={endpoints} />
    </div>
  );
}

function GeneralSection({ mcpServer }: { mcpServer: McpServer }) {
  const [nameDraft, setNameDraft] = useState(mcpServer.name ?? "");

  // Re-sync draft when the upstream record changes (e.g. another tab edited
  // it or a refetch landed). Without this a stale draft survives the refetch.
  useEffect(() => {
    setNameDraft(mcpServer.name ?? "");
  }, [mcpServer.id, mcpServer.name]);

  const queryClient = useQueryClient();
  const update = useUpdateMcpServerMutation();
  const navigate = useNavigate();
  const routes = useRoutes();

  const trimmedDraft = nameDraft.trim();
  const dirty = trimmedDraft !== (mcpServer.name ?? "").trim();
  const saveDisabled = !dirty || trimmedDraft === "" || update.isPending;

  const handleSave = async () => {
    try {
      const updated = await update.mutateAsync({
        request: {
          updateMcpServerForm: {
            id: mcpServer.id,
            name: trimmedDraft,
            remoteMcpServerId: mcpServer.remoteMcpServerId ?? undefined,
            toolsetId: mcpServer.toolsetId ?? undefined,
            environmentId: mcpServer.environmentId ?? undefined,
            visibility: mcpServer.visibility,
          },
        },
      });
      // The server recomputes slug on every update, so a name change produces
      // a new slug. Replace the route param with the new slug *before*
      // invalidating queries so the refetch uses the new lookup args and the
      // page-level not-found guard doesn't bounce the user back to /mcp.
      const nextParam = mcpServerRouteParam(updated);
      navigate(routes.mcp.x.href(nextParam), { replace: true });
      await Promise.all([
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
      toast.success("MCP server updated");
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to update MCP server";
      toast.error(message);
    }
  };

  return (
    <div className="space-y-6">
      <Heading variant="h4">General</Heading>

      <Block label="Display name" className="p-0">
        <BlockInner>
          <Stack direction="horizontal" align="center" gap={2}>
            <Input
              value={nameDraft}
              onChange={(value) => setNameDraft(value)}
              placeholder="My MCP server"
            />
            <RequireScope scope="mcp:write" level="component">
              <Button
                variant="primary"
                size="md"
                disabled={saveDisabled}
                onClick={handleSave}
              >
                {update.isPending ? (
                  <>
                    <Button.LeftIcon>
                      <Loader2 className="size-4 animate-spin" />
                    </Button.LeftIcon>
                    <Button.Text>Saving</Button.Text>
                  </>
                ) : (
                  <Button.Text>Save</Button.Text>
                )}
              </Button>
            </RequireScope>
          </Stack>
        </BlockInner>
      </Block>
      {update.isError && (
        <Alert variant="error" dismissible={false}>
          {update.error.message}
        </Alert>
      )}
    </div>
  );
}

function DangerZoneSection({
  mcpServer,
  endpoints,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
}) {
  const navigate = useNavigate();
  const routes = useRoutes();
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);

  return (
    <div className="border-destructive/30 rounded-lg border p-6">
      <Type variant="subheading" className="text-destructive mb-1">
        Danger Zone
      </Type>
      <Type muted small className="mb-4">
        Deleting this MCP server also removes its endpoints. This action cannot
        be undone.
      </Type>
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

      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <Dialog.Content className="max-w-2xl!">
          <DeleteMcpServerDialogContent
            mcpServer={mcpServer}
            endpoints={endpoints}
            onClose={() => setDeleteDialogOpen(false)}
            onSuccess={() => {
              setDeleteDialogOpen(false);
              navigate(routes.mcp.href());
            }}
          />
        </Dialog.Content>
      </Dialog>
    </div>
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
            {remove.isPending ? (
              <>
                <Button.LeftIcon>
                  <Loader2 className="size-4 animate-spin" />
                </Button.LeftIcon>
                <Button.Text>Deleting</Button.Text>
              </>
            ) : (
              <Button.Text>Delete MCP server</Button.Text>
            )}
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
