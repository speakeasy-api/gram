import { InputField } from "@/components/moon/input-field";
import { Page } from "@/components/page-layout";
import { ToolCollectionBadge } from "@/components/tool-collection-badge";
import { Badge } from "@/components/ui/badge";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { UpdatedAt } from "@/components/updated-at";
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
import { ChevronRight, Globe, Lock, Server, Unplug } from "lucide-react";
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

  const handleRemoveServer = (server: PluginServer) => {
    removeServerMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: { id: server.id, pluginId: pluginId! },
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
          <div className="mb-8 space-y-2">
            {servers.map((server) => (
              <PluginServerRow
                key={server.id}
                server={server}
                toolset={toolsetById.get(server.toolsetId)}
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
                <label className="text-sm font-medium">Toolset</label>
                {isLoadingToolsets ? (
                  <Skeleton className="h-9 w-full" />
                ) : toolsets.length > 0 ? (
                  <select
                    name="toolsetId"
                    className="bg-background rounded-md border px-3 py-2 text-sm"
                    required
                  >
                    <option value="">Select a toolset</option>
                    {toolsets.map((t) => (
                      <option key={t.id} value={t.id}>
                        {t.name}
                      </option>
                    ))}
                  </select>
                ) : (
                  <Type muted small>
                    No toolsets available. Create a toolset in this project
                    first.
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

function PluginServerRow({
  server,
  toolset,
  onRemove,
}: {
  server: PluginServer;
  toolset: ToolsetEntry | undefined;
  onRemove: () => void;
}) {
  const routes = useRoutes();
  const toolNames = toolset?.tools.map((t) => t.name) ?? [];
  const description = toolset?.description ?? null;
  const isMissing = !toolset;
  const isRequired = server.policy === "required";

  const mcpBadge = (() => {
    if (!toolset) return null;
    if (!toolset.mcpEnabled) {
      return (
        <Badge variant="outline" className="text-muted-foreground">
          <Unplug />
          MCP off
        </Badge>
      );
    }
    if (toolset.mcpIsPublic) {
      return (
        <Badge variant="secondary">
          <Globe />
          Public MCP
        </Badge>
      );
    }
    return (
      <Badge variant="outline">
        <Lock />
        Private MCP
      </Badge>
    );
  })();

  const body = (
    <div className="flex min-w-0 flex-1 flex-col gap-1">
      <div className="flex items-center gap-2">
        <Type className="truncate font-medium">{server.displayName}</Type>
        {isMissing && (
          <Badge variant="destructive" className="text-xs">
            Toolset missing
          </Badge>
        )}
      </div>
      {description && (
        <Type className="text-muted-foreground line-clamp-1 text-sm">
          {description}
        </Type>
      )}
    </div>
  );

  const leftSide = toolset ? (
    <routes.mcp.details.Link
      params={[toolset.slug]}
      className="group hover:text-primary flex min-w-0 flex-1 items-center gap-3 hover:no-underline"
    >
      <Server className="text-muted-foreground size-5 shrink-0" />
      {body}
      <ChevronRight className="text-muted-foreground size-4 shrink-0 opacity-0 transition-opacity group-hover:opacity-100" />
    </routes.mcp.details.Link>
  ) : (
    <div className="flex min-w-0 flex-1 items-center gap-3">
      <Server className="text-muted-foreground size-5 shrink-0" />
      {body}
    </div>
  );

  return (
    <div className="bg-surface-secondary hover:bg-surface-tertiary flex items-center gap-3 rounded-md border p-3 transition-colors">
      {leftSide}
      <div className="flex shrink-0 items-center gap-2">
        {toolset && <ToolCollectionBadge toolNames={toolNames} />}
        {mcpBadge}
        <Badge variant={isRequired ? "secondary" : "outline"}>
          {isRequired ? "Required" : "Optional"}
        </Badge>
        {toolset && (
          <UpdatedAt
            date={new Date(toolset.updatedAt)}
            italic={false}
            className="hidden md:flex"
          />
        )}
        <Button
          variant="tertiary"
          size="sm"
          onClick={onRemove}
          className="hover:text-destructive"
        >
          <Button.LeftIcon>
            <Icon name="trash-2" className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text className="sr-only">Remove</Button.Text>
        </Button>
      </div>
    </div>
  );
}
