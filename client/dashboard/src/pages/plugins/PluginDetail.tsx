import { InputField } from "@/components/moon/input-field";
import { Page } from "@/components/page-layout";
import { MCPStatusIndicator } from "@/components/mcp/MCPStatusIndicator";
import { ToolCollectionBadge } from "@/components/tool-collection-badge";
import { Button as UiButton } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { DotCard } from "@/components/ui/dot-card";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import {
  invalidateAllPlugin,
  usePluginSuspense,
} from "@gram/client/react-query/plugin";
import { invalidateAllPlugins } from "@gram/client/react-query/plugins";
import {
  invalidateAllPublishStatus,
  usePublishStatus,
} from "@gram/client/react-query/publishStatus";
import { usePublishPluginsMutation } from "@gram/client/react-query/publishPlugins";
import { useUpdatePluginMutation } from "@gram/client/react-query/updatePlugin";
import { useDeletePluginMutation } from "@gram/client/react-query/deletePlugin";
import { useAddPluginServerMutation } from "@gram/client/react-query/addPluginServer";
import { useRemovePluginServerMutation } from "@gram/client/react-query/removePluginServer";
import { useListToolsets } from "@gram/client/react-query/listToolsets";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers";
import type { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";
import {
  Badge,
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  Icon,
  Stack,
} from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { formatDistanceToNow } from "date-fns";
import { Network, Puzzle, Sparkles, Trash2 } from "lucide-react";
import { useCallback, useMemo, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { PluginServer } from "@gram/client/models/components/pluginserver.js";
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import { useSdkClient } from "@/contexts/Sdk";
import { toast } from "sonner";
import {
  DEFAULT_PLUGIN_DESCRIPTION,
  isDefaultPluginSlug,
} from "./default-plugin";
import { downloadPluginPackage } from "./downloadPluginPackage";
import { InstallInstructionsDialog } from "./InstallInstructionsDialog";
import { PublishDialog } from "./PublishDialog";

// A selectable server for a plugin, sourced from either a toolset (Hosted) or
// a Remote MCP-backed mcp_server. The kind determines whether it is submitted
// as a toolset_id or an mcp_server_id, mirroring the collections picker.
type ServerOptionKind = "toolset" | "mcpServer";
type ServerOption = {
  kind: ServerOptionKind;
  id: string;
  name: string;
};

function serverOptionKey(kind: ServerOptionKind, id: string): string {
  return `${kind}:${id}`;
}

export default function PluginDetail(): JSX.Element | null {
  const { pluginId } = useParams<{ pluginId: string }>();
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const navigate = useNavigate();
  const [isEditOpen, setIsEditOpen] = useState(false);
  const [isAddServerOpen, setIsAddServerOpen] = useState(false);
  const [isDownloadMenuOpen, setIsDownloadMenuOpen] = useState(false);
  const [isInstallSheetOpen, setIsInstallSheetOpen] = useState(false);
  const [isPublishDialogOpen, setIsPublishDialogOpen] = useState(false);
  const [isDeleteOpen, setIsDeleteOpen] = useState(false);

  const { data: plugin } = usePluginSuspense({ id: pluginId! });
  // Polled so the publish-freshness badges/banner pick up the Temporal
  // generator-rollout schedule's auto-sync without a manual refresh.
  const { data: publishStatus } = usePublishStatus(undefined, undefined, {
    refetchInterval: 5_000,
  });

  const client = useSdkClient();

  const { data: toolsetsData, isLoading: isLoadingToolsets } =
    useListToolsets();
  const toolsets = useMemo(
    () => toolsetsData?.toolsets ?? [],
    [toolsetsData?.toolsets],
  );

  // Remote MCP-backed mcp_servers for this project. All of them back the
  // cards of already-attached plugin servers (an attached server that's been
  // disabled or lost its endpoints must still resolve for display), while
  // only non-disabled servers with at least one endpoint are publishable —
  // mirroring the backend's AddPluginServer check, so the picker never
  // offers a server the API would reject.
  const { data: mcpServersData, isLoading: isLoadingMcpServers } =
    useMcpServers({});
  const { data: mcpEndpointsData, isLoading: isLoadingMcpEndpoints } =
    useMcpEndpoints({});
  const mcpServers = useMemo(
    () =>
      (mcpServersData?.mcpServers ?? []).filter((s) => !!s.remoteMcpServerId),
    [mcpServersData],
  );
  const publishableMcpServers = useMemo(() => {
    const serverIdsWithEndpoint = new Set(
      (mcpEndpointsData?.mcpEndpoints ?? []).map((e) => e.mcpServerId),
    );
    return mcpServers.filter(
      (s) => s.visibility !== "disabled" && serverIdsWithEndpoint.has(s.id),
    );
  }, [mcpServers, mcpEndpointsData]);

  const isLoadingServers =
    isLoadingToolsets || isLoadingMcpServers || isLoadingMcpEndpoints;

  // Invalidate publish status too so the dirty/up-to-date affordance reflects
  // the edit the moment a mutation lands.
  const invalidateAll = async () => {
    await invalidateAllPlugin(queryClient);
    await invalidateAllPlugins(queryClient);
    await invalidateAllPublishStatus(queryClient);
  };

  const publishMutation = usePublishPluginsMutation({
    onSuccess: (data) => {
      setIsPublishDialogOpen(false);
      void invalidateAllPublishStatus(queryClient);
      toast.success("Plugins published to GitHub", {
        description: data.repoUrl,
        action: {
          label: "Open",
          onClick: () => {
            void window.open(data.repoUrl, "_blank", "noopener,noreferrer");
          },
        },
      });
    },
    onError: () => {
      toast.error("Failed to publish plugins to GitHub");
    },
  });

  // Destructure mutate so callbacks depend on the stable function rather than
  // the fresh-per-render wrapper object (mirrors Plugins.tsx).
  const { mutate: publishMutate } = publishMutation;
  // Mirror the in-flight flag into a ref so detached callbacks can gate on the
  // current pending state. The "Publish now" toast action closure is created
  // when offerPublish runs (before any publish starts), so it can't read a live
  // isPending — without this guard, stacking edits into multiple toasts lets a
  // user fire concurrent publishes that the disabled header button prevents.
  const isPublishingRef = useRef(publishMutation.isPending);
  isPublishingRef.current = publishMutation.isPending;
  const handlePublish = useCallback(
    (githubUsernames: string[]) => {
      if (isPublishingRef.current) return;
      publishMutate({
        security: { sessionHeaderGramSession: "" },
        request: { publishPluginsRequestBody: { githubUsernames } },
      });
    },
    [publishMutate],
  );

  // Nudge the user to publish straight after an edit instead of hunting for
  // Re-publish on the list page. A connected project republishes in one click;
  // a configured-but-unconnected project needs the first-publish dialog (it
  // collects collaborators). Unconfigured projects get no nudge — there's
  // nowhere to publish to.
  const offerPublish = useCallback(
    (message: string) => {
      if (!publishStatus?.configured) return;
      toast.success(message, {
        action: {
          label: "Publish now",
          onClick: () => {
            if (publishStatus.connected) {
              handlePublish([]);
            } else {
              setIsPublishDialogOpen(true);
            }
          },
        },
      });
    },
    [publishStatus?.configured, publishStatus?.connected, handlePublish],
  );

  const updateMutation = useUpdatePluginMutation({
    onSuccess: () => {
      setIsEditOpen(false);
      void invalidateAll();
      offerPublish("Plugin updated");
    },
  });

  const deleteMutation = useDeletePluginMutation({
    onSuccess: async () => {
      setIsDeleteOpen(false);
      await invalidateAll();
      offerPublish("Plugin deleted");
      void navigate(routes.plugins.href());
    },
  });

  const addServerMutation = useAddPluginServerMutation({
    onSuccess: () => {
      setIsAddServerOpen(false);
      void invalidateAll();
      offerPublish("Server added to plugin");
    },
  });

  const removeServerMutation = useRemovePluginServerMutation({
    onSuccess: () => {
      void invalidateAll();
      offerPublish("Server removed from plugin");
    },
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

  const handleDelete = () => {
    deleteMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: { id: pluginId! },
    });
  };

  const handleAddServer: React.FormEventHandler<HTMLFormElement> = (e) => {
    e.preventDefault();
    const fd = new FormData(e.currentTarget);
    const key = fd.get("serverKey") as string;
    if (!key) return;
    const option = serverOptions.find(
      (o) => serverOptionKey(o.kind, o.id) === key,
    );
    if (!option) return;
    addServerMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        addPluginServerForm: {
          pluginId: pluginId!,
          // Submit exactly one backend id per the toolset_id XOR mcp_server_id
          // contract.
          ...(option.kind === "mcpServer"
            ? { mcpServerId: option.id }
            : { toolsetId: option.id }),
          displayName: option.name,
          policy: "required",
        },
      },
    });
  };

  const handleDownload = async (platform: "claude" | "cursor" | "codex") => {
    setIsDownloadMenuOpen(false);
    try {
      await downloadPluginPackage(client, pluginId!, platform);
    } catch (_err) {
      toast.error("Failed to download plugin package");
    }
  };

  const toolsetById = useMemo(() => {
    const map = new Map<string, ToolsetEntry>();
    for (const t of toolsets) map.set(t.id, t);
    return map;
  }, [toolsets]);

  const mcpServerById = useMemo(() => {
    const map = new Map<string, McpServer>();
    for (const s of mcpServers) map.set(s.id, s);
    return map;
  }, [mcpServers]);

  // Merge toolsets and publishable Remote MCP-backed servers into one
  // selectable list.
  const serverOptions = useMemo<ServerOption[]>(() => {
    const opts: ServerOption[] = toolsets.map((t) => ({
      kind: "toolset",
      id: t.id,
      name: t.name,
    }));
    for (const s of publishableMcpServers) {
      opts.push({
        kind: "mcpServer",
        id: s.id,
        name: s.name ?? s.slug ?? "Untitled server",
      });
    }
    return opts;
  }, [toolsets, publishableMcpServers]);

  if (!plugin) return null;

  const isDefaultPlugin = isDefaultPluginSlug(plugin.slug);
  const description =
    plugin.description ??
    (isDefaultPlugin ? DEFAULT_PLUGIN_DESCRIPTION : "No description");

  const servers = plugin.servers ?? [];

  // Exclude servers already added to the plugin, keyed per backend.
  const addedToolsetIds = new Set(
    servers.map((s) => s.toolsetId).filter((id): id is string => !!id),
  );
  const addedMcpServerIds = new Set(
    servers.map((s) => s.mcpServerId).filter((id): id is string => !!id),
  );
  const availableServerOptions = serverOptions.filter((o) =>
    o.kind === "toolset"
      ? !addedToolsetIds.has(o.id)
      : !addedMcpServerIds.has(o.id),
  );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [pluginId ?? ""]: plugin.name }}
        />
      </Page.Header>
      <Page.Body>
        {/* Hero */}
        <div className="mb-8 flex flex-wrap items-start justify-between gap-6">
          <div className="flex min-w-0 items-start gap-4">
            <div className="bg-primary/5 flex h-14 w-14 shrink-0 items-center justify-center rounded-xl dark:bg-neutral-800">
              <Puzzle className="text-muted-foreground h-7 w-7" />
            </div>
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <h1 className="text-2xl font-bold">{plugin.name}</h1>
                {isDefaultPlugin && (
                  <Badge variant="information">
                    <Badge.Text>Default</Badge.Text>
                  </Badge>
                )}
              </div>
              <Type muted small className="mt-1 font-mono">
                {plugin.slug}
              </Type>
              <Type small className="mt-4 max-w-xl">
                {description}
              </Type>
              <PublishFreshnessIndicator publishStatus={publishStatus} />
            </div>
          </div>
          <Stack
            direction="horizontal"
            gap={2}
            align="center"
            className="shrink-0"
          >
            <DropdownMenu
              open={isDownloadMenuOpen}
              onOpenChange={setIsDownloadMenuOpen}
            >
              <DropdownMenuTrigger asChild>
                <Button variant="primary">
                  <Button.Text>Install</Button.Text>
                  <span className="bg-primary-foreground/25 mx-1 h-4 w-px self-center" />
                  <Button.RightIcon>
                    <Icon name="chevron-down" className="h-4 w-4" />
                  </Button.RightIcon>
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem
                  onClick={() => {
                    // Defer until after the dropdown has fully closed to
                    // avoid a Radix focus-trap/body-lock conflict between
                    // the closing menu and the opening sheet (same pattern
                    // as MCPDetails.tsx).
                    setTimeout(() => setIsInstallSheetOpen(true), 0);
                  }}
                  disabled={
                    !publishStatus?.connected ||
                    !publishStatus.repoOwner ||
                    !publishStatus.repoName
                  }
                >
                  <div className="flex flex-col">
                    <span>GitHub installation (preferred)</span>
                    {(!publishStatus?.connected ||
                      !publishStatus.repoOwner ||
                      !publishStatus.repoName) && (
                      <span className="text-muted-foreground text-xs">
                        Requires marketplace setup
                      </span>
                    )}
                  </div>
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  onClick={() => {
                    void handleDownload("claude");
                  }}
                >
                  Download as zip — Claude
                </DropdownMenuItem>
                <DropdownMenuItem
                  onClick={() => {
                    void handleDownload("cursor");
                  }}
                >
                  Download as zip — Cursor
                </DropdownMenuItem>
                <DropdownMenuItem
                  onClick={() => {
                    void handleDownload("codex");
                  }}
                >
                  Download as zip — Codex
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            {publishStatus?.connected &&
              publishStatus.repoOwner &&
              publishStatus.repoName && (
                <InstallInstructionsDialog
                  open={isInstallSheetOpen}
                  onOpenChange={setIsInstallSheetOpen}
                  repoOwner={publishStatus.repoOwner}
                  repoName={publishStatus.repoName}
                  marketplaceUrl={publishStatus.marketplaceUrl}
                  candidatePlugins={[
                    {
                      name: plugin.name,
                      slug: plugin.slug,
                      description: plugin.description,
                    },
                  ]}
                />
              )}
            <PublishStatusControl
              publishStatus={publishStatus}
              isPending={publishMutation.isPending}
              onRepublish={() => handlePublish([])}
              onOpenDialog={() => setIsPublishDialogOpen(true)}
            />
            <Button variant="secondary" onClick={() => setIsEditOpen(true)}>
              Edit name
            </Button>
            <Button
              variant="destructive-primary"
              onClick={() => setIsDeleteOpen(true)}
            >
              Delete
            </Button>
          </Stack>
        </div>

        {/* Servers section */}
        <div className="mb-3 flex items-center gap-3">
          <div className="border-border flex-1 border-t" />
          <Type
            small
            muted
            className="shrink-0 font-mono text-xs tracking-wide uppercase"
          >
            MCP Servers
          </Type>
          <div className="border-border flex-1 border-t" />
        </div>
        <div className="mb-3 flex justify-end">
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setIsAddServerOpen(true)}
          >
            <Button.LeftIcon>
              <Icon name="plus" className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Add Server</Button.Text>
          </Button>
        </div>
        <div className="mb-8">
          {servers.length === 0 ? (
            <Stack
              gap={2}
              className="border-border rounded-xl border py-8"
              align="center"
              justify="center"
            >
              <Type variant="body" muted>
                No servers added yet
              </Type>
            </Stack>
          ) : (
            <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
              {servers.map((server) => (
                <PluginServerCard
                  key={server.id}
                  server={server}
                  toolset={
                    server.toolsetId
                      ? toolsetById.get(server.toolsetId)
                      : undefined
                  }
                  mcpServer={
                    server.mcpServerId
                      ? mcpServerById.get(server.mcpServerId)
                      : undefined
                  }
                  isLoading={isLoadingServers}
                  onRemove={() => handleRemoveServer(server)}
                  lastPublishedAt={publishStatus?.lastPublishedAt}
                />
              ))}
            </div>
          )}
        </div>

        {/* Skills section — no plugin support yet, coming soon */}
        <div className="mb-3 flex items-center gap-3">
          <div className="border-border flex-1 border-t" />
          <Type
            small
            muted
            className="shrink-0 font-mono text-xs tracking-wide uppercase"
          >
            Skills
          </Type>
          <div className="border-border flex-1 border-t" />
        </div>
        <div className="mb-8">
          <div className="border-border flex items-center gap-4 rounded-xl border border-dashed p-6 opacity-60">
            <div className="bg-muted flex h-14 w-14 shrink-0 items-center justify-center rounded-xl">
              <Sparkles className="text-muted-foreground h-7 w-7" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <Type variant="subheading" as="div">
                  Skills
                </Type>
                <Badge variant="neutral">
                  <Badge.Text>Coming soon</Badge.Text>
                </Badge>
              </div>
              <Type small muted>
                Bundle reusable skills alongside your MCP servers in this
                plugin.
              </Type>
            </div>
          </div>
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

        {/* Delete Confirmation Dialog */}
        <Dialog open={isDeleteOpen} onOpenChange={setIsDeleteOpen}>
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>Delete Plugin</Dialog.Title>
              <Dialog.Description>
                Are you sure you want to delete &quot;{plugin.name}&quot;? This
                will remove it from all assigned users on the next publish.
              </Dialog.Description>
            </Dialog.Header>
            <Dialog.Footer>
              <Button
                variant="secondary"
                onClick={() => setIsDeleteOpen(false)}
              >
                Cancel
              </Button>
              <Button
                variant="destructive-primary"
                onClick={handleDelete}
                disabled={deleteMutation.isPending}
              >
                Delete
              </Button>
            </Dialog.Footer>
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
                {isLoadingServers ? (
                  <Skeleton className="h-9 w-full" />
                ) : availableServerOptions.length > 0 ? (
                  <select
                    name="serverKey"
                    className="bg-background rounded-md border px-3 py-2 text-sm"
                    required
                  >
                    <option value="">Select an MCP server</option>
                    {availableServerOptions.map((o) => (
                      <option
                        key={serverOptionKey(o.kind, o.id)}
                        value={serverOptionKey(o.kind, o.id)}
                      >
                        {o.name}
                        {o.kind === "mcpServer" ? " (Remote MCP)" : ""}
                      </option>
                    ))}
                  </select>
                ) : serverOptions.length > 0 ? (
                  <Type muted small>
                    All available MCP servers have already been added to this
                    plugin.
                  </Type>
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
                    isLoadingServers ||
                    availableServerOptions.length === 0
                  }
                >
                  Add
                </Button>
              </Dialog.Footer>
            </form>
          </Dialog.Content>
        </Dialog>
        <PublishDialog
          open={isPublishDialogOpen}
          onOpenChange={setIsPublishDialogOpen}
          onPublish={handlePublish}
          isPending={publishMutation.isPending}
        />
      </Page.Body>
    </Page>
  );
}

// Durable publish affordance for the plugin detail header. Renders nothing when
// the server has no GitHub publishing configured; otherwise it surfaces the
// project's publish freshness (sourced from getPublishStatus) and a one-click
// path to publish without returning to the plugins list.
function PublishStatusControl({
  publishStatus,
  isPending,
  onRepublish,
  onOpenDialog,
}: {
  publishStatus: PublishStatusResult | undefined;
  isPending: boolean;
  onRepublish: () => void;
  onOpenDialog: () => void;
}): JSX.Element | null {
  if (!publishStatus?.configured) return null;

  // Never published: the first publish needs the dialog (it collects repo
  // collaborators), so there's no freshness to show yet.
  if (!publishStatus.connected) {
    return (
      <Button variant="secondary" onClick={onOpenDialog} disabled={isPending}>
        <Button.LeftIcon>
          <Icon name="upload" className="h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>
          {isPending ? "Publishing..." : "Publish Private Marketplace"}
        </Button.Text>
      </Button>
    );
  }

  // up_to_date is absent when freshness can't be determined (connection
  // predates fingerprinting) — treat only an explicit false as dirty.
  const hasUnpublishedChanges = publishStatus.upToDate === false;

  return (
    <Button
      variant={hasUnpublishedChanges ? "primary" : "secondary"}
      onClick={onRepublish}
      disabled={isPending}
    >
      <Button.LeftIcon>
        <Icon name="refresh-cw" className="h-4 w-4" />
      </Button.LeftIcon>
      <Button.Text>
        {isPending
          ? "Publishing..."
          : hasUnpublishedChanges
            ? "Publish changes"
            : "Sync"}
      </Button.Text>
    </Button>
  );
}

// Shows the published freshness of a connected project under the plugin
// metadata: an explicit "Unpublished changes" vs "Up to date" badge, paired
// with the last-published time. The timestamp shows in both states (it's still
// useful to know when the last publish happened while there are pending
// changes). Renders nothing when not connected, or when freshness is unknown
// and there's no publish timestamp to show.
function PublishFreshnessIndicator({
  publishStatus,
}: {
  publishStatus: PublishStatusResult | undefined;
}): JSX.Element | null {
  if (!publishStatus?.connected) return null;

  // up_to_date is absent when freshness can't be determined (connection
  // predates fingerprinting) — treat only the explicit booleans as known.
  const hasUnpublishedChanges = publishStatus.upToDate === false;
  const isUpToDate = publishStatus.upToDate === true;

  const lastPublished = publishStatus.lastPublishedAt ? (
    <Type muted small>
      Published{" "}
      {formatDistanceToNow(publishStatus.lastPublishedAt, {
        addSuffix: true,
      })}
    </Type>
  ) : null;

  // Freshness unknown and nothing published yet — nothing meaningful to show.
  if (!hasUnpublishedChanges && !isUpToDate && !lastPublished) return null;

  return (
    <Stack direction="horizontal" gap={2} align="center" className="mt-4">
      {hasUnpublishedChanges ? (
        <Badge variant="warning">Unpublished changes</Badge>
      ) : isUpToDate ? (
        <Badge variant="success">Up to date</Badge>
      ) : null}
      {lastPublished}
    </Stack>
  );
}

function PluginServerCard({
  server,
  toolset,
  mcpServer,
  isLoading,
  onRemove,
  lastPublishedAt,
}: {
  server: PluginServer;
  toolset: ToolsetEntry | undefined;
  mcpServer: McpServer | undefined;
  isLoading: boolean;
  onRemove: () => void;
  /** Undefined when the marketplace has never been published. */
  lastPublishedAt: Date | undefined;
}) {
  const routes = useRoutes();

  // Remote MCP-backed servers reference an mcp_server; toolset-backed servers
  // reference a toolset. Exactly one backend is set per row.
  const isRemote = !!server.mcpServerId;
  // Approximates per-server publish freshness from the project-wide
  // fingerprint: publish itself isn't scoped to a single server, but a
  // server added after the last publish timestamp can't possibly be in the
  // pushed repo yet.
  const notYetPublished =
    !lastPublishedAt || server.createdAt > lastPublishedAt;
  // The card is clickable only once its backing resource resolves.
  const isClickable = isRemote ? !!mcpServer : !!toolset;

  const handleClick = () => {
    // Remote MCP servers live on the mcp_servers-backed details page (x/);
    // toolset-backed servers use the toolset details page.
    if (isRemote) {
      if (mcpServer) routes.mcp.x.overview.goTo(mcpServerRouteParam(mcpServer));
    } else if (toolset) {
      routes.mcp.details.goTo(toolset.slug);
    }
  };

  return (
    <DotCard
      className={cn(isClickable && "cursor-pointer")}
      onClick={isClickable ? handleClick : undefined}
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
        <div className="flex items-center gap-2">
          {notYetPublished ? (
            <Badge
              variant="warning"
              className="text-xs"
              title="Added since the marketplace was last published"
            >
              Unpublished
            </Badge>
          ) : (
            <Badge variant="success" className="text-xs">
              Published
            </Badge>
          )}
          {isRemote ? (
            // Remote MCP servers have no Gram-side tool catalog, so the
            // tool-collection badge is omitted.
            <Badge variant="neutral" className="text-xs">
              Remote MCP
            </Badge>
          ) : toolset ? (
            <ToolCollectionBadge toolNames={toolset.tools.map((t) => t.name)} />
          ) : isLoading ? (
            <Skeleton className="h-5 w-16" />
          ) : (
            <Badge variant="destructive" className="text-xs">
              Toolset missing
            </Badge>
          )}
        </div>
      </div>

      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        {isRemote ? (
          <span />
        ) : toolset ? (
          <MCPStatusIndicator
            mcpEnabled={toolset.mcpEnabled}
            mcpIsPublic={toolset.mcpIsPublic}
          />
        ) : isLoading ? (
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
