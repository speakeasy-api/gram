import { InputField } from "@/components/moon/input-field";
import { Page } from "@/components/page-layout";
import { MCPStatusIndicator } from "@/components/mcp/MCPStatusIndicator";
import { ToolCollectionBadge } from "@/components/tool-collection-badge";
import { Badge } from "@/components/ui/badge";
import { Button as UiButton } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { DotCard } from "@/components/ui/dot-card";
import { Heading } from "@/components/ui/heading";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import {
  invalidateAllPlugin,
  usePluginSuspense,
} from "@gram/client/react-query/plugin";
import { invalidateAllPlugins } from "@gram/client/react-query/plugins";
import { useUpdatePluginMutation } from "@gram/client/react-query/updatePlugin";
import { useAddPluginServerMutation } from "@gram/client/react-query/addPluginServer";
import { useRemovePluginServerMutation } from "@gram/client/react-query/removePluginServer";
import { useListToolsets } from "@gram/client/react-query/listToolsets";
import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Icon,
  Stack,
} from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Network, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { useParams } from "react-router";
import type {
  PluginServer,
  ToolsetEntry,
} from "@gram/client/models/components";
import { useSdkClient } from "@/contexts/Sdk";
import { toast } from "sonner";

export default function PluginDetail() {
  const { pluginId } = useParams<{ pluginId: string }>();
  const queryClient = useQueryClient();
  const [isEditOpen, setIsEditOpen] = useState(false);
  const [isAddServerOpen, setIsAddServerOpen] = useState(false);
  const [isDownloadMenuOpen, setIsDownloadMenuOpen] = useState(false);

  const { data: plugin } = usePluginSuspense({ id: pluginId! });

  const client = useSdkClient();
  const { data: toolsetsData, isLoading: isLoadingToolsets } =
    useListToolsets();
  const toolsets = useMemo(
    () => toolsetsData?.toolsets ?? [],
    [toolsetsData?.toolsets],
  );

  const invalidateAll = async () => {
    await invalidateAllPlugin(queryClient);
    await invalidateAllPlugins(queryClient);
  };

  const updateMutation = useUpdatePluginMutation({
    onSuccess: () => {
      setIsEditOpen(false);
      invalidateAll();
    },
  });

  const addServerMutation = useAddPluginServerMutation({
    onSuccess: () => {
      setIsAddServerOpen(false);
      invalidateAll();
    },
  });

  const removeServerMutation = useRemovePluginServerMutation({
    onSuccess: () => invalidateAll(),
  });

  const handleRemoveServer = (server: PluginServer) => {
    removeServerMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: { id: server.id, pluginId: pluginId! },
    });
  };

  const handleUpdate: React.FormEventHandler<HTMLFormElement> = (e) => {
    e.preventDefault();
    const fd = new FormData(e.currentTarget);
    updateMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        updatePluginForm: {
          id: pluginId!,
          name: fd.get("name") as string,
          slug: fd.get("slug") as string,
          description: (fd.get("description") as string) || undefined,
        },
      },
    });
  };

  const handleAddServer: React.FormEventHandler<HTMLFormElement> = (e) => {
    e.preventDefault();
    const fd = new FormData(e.currentTarget);
    const toolsetId = fd.get("toolsetId") as string;
    const toolset = toolsets.find((t) => t.id === toolsetId);
    addServerMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        addPluginServerForm: {
          pluginId: pluginId!,
          toolsetId,
          displayName: toolset?.name ?? toolsetId,
          policy: "required",
        },
      },
    });
  };

  const handleDownload = async (platform: "claude" | "cursor" | "codex") => {
    setIsDownloadMenuOpen(false);
    try {
      const { headers, result } = await client.plugins.downloadPluginPackage({
        pluginId: pluginId!,
        platform,
      });
      const blob = await new Response(result).blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download =
        headers["content-disposition"]?.[0]?.match(/filename="(.+)"/)?.[1] ??
        "plugin.zip";
      a.click();
      URL.revokeObjectURL(url);
    } catch (_err) {
      toast.error("Failed to download plugin package");
    }
  };

  const toolsetById = useMemo(() => {
    const map = new Map<string, ToolsetEntry>();
    for (const t of toolsets) map.set(t.id, t);
    return map;
  }, [toolsets]);

  if (!plugin) return null;

  const servers = plugin.servers ?? [];

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [pluginId ?? ""]: plugin.name }}
        />
      </Page.Header>
      <Page.Body>
        {/* Plugin metadata */}
        <Stack
          direction="horizontal"
          justify="space-between"
          align="start"
          className="mb-6"
        >
          <div>
            <Heading variant="h4">{plugin.name}</Heading>
            <Type muted small className="mt-1">
              {plugin.description ?? "No description"}
            </Type>
            <Type muted small className="mt-1">
              Slug: <code>{plugin.slug}</code>
            </Type>
          </div>
          <Button variant="secondary" onClick={() => setIsEditOpen(true)}>
            Edit
          </Button>
        </Stack>

        {/* Servers section */}
        <Stack
          direction="horizontal"
          justify="space-between"
          align="center"
          className="mb-3"
        >
          <Heading variant="h5">MCP Servers</Heading>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setIsAddServerOpen(true)}
          >
            Add Server
          </Button>
        </Stack>
        {servers.length === 0 ? (
          <Stack
            gap={2}
            className="bg-background mb-8 rounded-md border p-8"
            align="center"
            justify="center"
          >
            <Type variant="body">No servers added yet</Type>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => setIsAddServerOpen(true)}
            >
              <Button.LeftIcon>
                <Icon name="plus" className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Add Server</Button.Text>
            </Button>
          </Stack>
        ) : (
          <div className="mb-8 grid grid-cols-1 gap-6 xl:grid-cols-2">
            {servers.map((server) => (
              <PluginServerCard
                key={server.id}
                server={server}
                toolset={toolsetById.get(server.toolsetId)}
                isLoadingToolset={isLoadingToolsets}
                onRemove={() => handleRemoveServer(server)}
              />
            ))}
          </div>
        )}

        {/* Download section */}
        <Heading variant="h5" className="mb-3">
          Download
        </Heading>
        <div>
          <DropdownMenu
            open={isDownloadMenuOpen}
            onOpenChange={setIsDownloadMenuOpen}
          >
            <DropdownMenuTrigger asChild>
              <Button variant="secondary" size="sm">
                <Button.LeftIcon>
                  <Icon name="download" className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>Download Plugin</Button.Text>
                <Button.RightIcon>
                  <Icon name="chevron-down" className="h-4 w-4" />
                </Button.RightIcon>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start">
              <DropdownMenuItem onClick={() => handleDownload("claude")}>
                Claude
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => handleDownload("cursor")}>
                Cursor
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => handleDownload("codex")}>
                Codex
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>

        {/* Edit Dialog */}
        <Dialog open={isEditOpen} onOpenChange={setIsEditOpen}>
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>Edit Plugin</Dialog.Title>
            </Dialog.Header>
            <form onSubmit={handleUpdate} className="flex flex-col gap-4">
              <InputField
                label="Name"
                name="name"
                defaultValue={plugin.name}
                required
              />
              <InputField
                label="Slug"
                name="slug"
                defaultValue={plugin.slug}
                required
              />
              <InputField
                label="Description"
                name="description"
                defaultValue={plugin.description ?? ""}
              />
              <Dialog.Footer>
                <Button
                  variant="secondary"
                  onClick={() => setIsEditOpen(false)}
                  type="button"
                >
                  Cancel
                </Button>
                <Button type="submit" disabled={updateMutation.isPending}>
                  Save
                </Button>
              </Dialog.Footer>
            </form>
          </Dialog.Content>
        </Dialog>

        {/* Add Server Dialog */}
        <Dialog open={isAddServerOpen} onOpenChange={setIsAddServerOpen}>
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>Add MCP Server</Dialog.Title>
              <Dialog.Description>
                Add an MCP server to this plugin bundle.
              </Dialog.Description>
            </Dialog.Header>
            <form onSubmit={handleAddServer} className="flex flex-col gap-4">
              <div className="flex flex-col gap-2">
                <label className="text-sm font-medium">MCP Server</label>
                {isLoadingToolsets ? (
                  <Skeleton className="h-9 w-full" />
                ) : toolsets.length > 0 ? (
                  <select
                    name="toolsetId"
                    className="bg-background rounded-md border px-3 py-2 text-sm"
                    required
                  >
                    <option value="">Select an MCP server</option>
                    {toolsets.map((t) => (
                      <option key={t.id} value={t.id}>
                        {t.name}
                      </option>
                    ))}
                  </select>
                ) : (
                  <Type muted small>
                    No MCP servers available. Create an MCP server in this
                    project first.
                  </Type>
                )}
              </div>
              <Dialog.Footer>
                <Button
                  variant="secondary"
                  onClick={() => setIsAddServerOpen(false)}
                  type="button"
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  disabled={
                    addServerMutation.isPending ||
                    isLoadingToolsets ||
                    toolsets.length === 0
                  }
                >
                  Add
                </Button>
              </Dialog.Footer>
            </form>
          </Dialog.Content>
        </Dialog>
      </Page.Body>
    </Page>
  );
}

function PluginServerCard({
  server,
  toolset,
  isLoadingToolset,
  onRemove,
}: {
  server: PluginServer;
  toolset: ToolsetEntry | undefined;
  isLoadingToolset: boolean;
  onRemove: () => void;
}) {
  const routes = useRoutes();

  const handleClick = () => {
    if (toolset) routes.mcp.details.goTo(toolset.slug);
  };

  return (
    <DotCard
      className={cn(toolset && "cursor-pointer")}
      onClick={toolset ? handleClick : undefined}
      icon={<Network className="text-muted-foreground h-8 w-8" />}
    >
      <div className="mb-2 flex items-start justify-between gap-2">
        <Type
          variant="subheading"
          as="div"
          className="text-md group-hover:text-primary flex-1 truncate transition-colors"
          title={server.displayName}
        >
          {server.displayName}
        </Type>
        <div className="flex items-center gap-1">
          {toolset ? (
            <ToolCollectionBadge toolNames={toolset.tools.map((t) => t.name)} />
          ) : isLoadingToolset ? (
            <Skeleton className="h-5 w-16" />
          ) : (
            <Badge variant="destructive" className="text-xs">
              Toolset missing
            </Badge>
          )}
        </div>
      </div>

      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        {toolset ? (
          <MCPStatusIndicator
            mcpEnabled={toolset.mcpEnabled}
            mcpIsPublic={toolset.mcpIsPublic}
          />
        ) : isLoadingToolset ? (
          <Skeleton className="h-3.5 w-20" />
        ) : (
          <span />
        )}
        <UiButton
          type="button"
          variant="ghost"
          size="icon-sm"
          tooltip="Remove server"
          aria-label="Remove server"
          className="hover:text-destructive"
          onClick={(e) => {
            e.stopPropagation();
            onRemove();
          }}
        >
          <Trash2 className="h-4 w-4" />
        </UiButton>
      </div>
    </DotCard>
  );
}
