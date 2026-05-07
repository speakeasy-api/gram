import { CreateResourceCard } from "@/components/create-resource-card";
import { InputField } from "@/components/moon/input-field";
import { Page } from "@/components/page-layout";
import { Dialog } from "@/components/ui/dialog";
import { SearchBar } from "@/components/ui/search-bar";
import { Type } from "@/components/ui/type";
import { InstallInstructionsButton } from "./InstallInstructionsDialog";
import { useFetcher } from "@/contexts/Fetcher";
import { useRoutes } from "@/routes";
import { Plugin } from "@gram/client/models/components";
import { useCreatePluginMutation } from "@gram/client/react-query/createPlugin";
import {
  invalidateAllPlugins,
  usePluginsSuspense,
} from "@gram/client/react-query/plugins";
import { useDeletePluginMutation } from "@gram/client/react-query/deletePlugin";
import {
  invalidateAllPublishStatus,
  usePublishStatusSuspense,
} from "@gram/client/react-query/publishStatus";
import { usePublishPluginsMutation } from "@gram/client/react-query/publishPlugins";
import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Icon,
  Stack,
} from "@speakeasy-api/moonshine";
import { Activity } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";
import { Outlet, useNavigate } from "react-router";
import { toast } from "sonner";
import { PluginCard } from "./PluginCard";
import { PublishDialog } from "./PublishDialog";

export function PluginsRoot() {
  return <Outlet />;
}

export default function Plugins() {
  const [isCreateDialogOpen, setIsCreateDialogOpen] = useState(false);
  const [isPublishDialogOpen, setIsPublishDialogOpen] = useState(false);
  const [pluginToDelete, setPluginToDelete] = useState<Plugin | null>(null);
  const [search, setSearch] = useState("");
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const navigate = useNavigate();

  const { data } = usePluginsSuspense();
  const { data: publishStatus } = usePublishStatusSuspense();
  const { fetch: authFetch } = useFetcher();
  const [isObservabilityDownloadMenuOpen, setIsObservabilityDownloadMenuOpen] =
    useState(false);
  const [isDownloadingObservability, setIsDownloadingObservability] = useState<
    "claude" | "cursor" | null
  >(null);

  const handleObservabilityDownload = async (platform: "claude" | "cursor") => {
    setIsObservabilityDownloadMenuOpen(false);
    setIsDownloadingObservability(platform);
    try {
      const resp = await authFetch(
        `/rpc/plugins.downloadObservabilityPlugin?platform=${platform}`,
        {},
      );
      if (!resp.ok) {
        toast.error("Failed to download observability plugin");
        return;
      }
      const blob = await resp.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download =
        resp.headers
          .get("Content-Disposition")
          ?.match(/filename="(.+)"/)?.[1] ?? `observability-${platform}.zip`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      toast.error("Failed to download observability plugin");
      console.error("observability plugin download failed", err);
    } finally {
      setIsDownloadingObservability(null);
    }
  };

  const publishMutation = usePublishPluginsMutation({
    onSuccess: (data) => {
      setIsPublishDialogOpen(false);
      invalidateAllPublishStatus(queryClient);
      toast.success("Plugins published to GitHub", {
        description: data.repoUrl,
        action: {
          label: "Open",
          onClick: () =>
            window.open(data.repoUrl, "_blank", "noopener,noreferrer"),
        },
      });
    },
    onError: () => {
      toast.error("Failed to publish plugins to GitHub");
    },
  });

  const hasPlugins = (data?.plugins ?? []).length > 0;

  const filteredPlugins = useMemo(() => {
    const plugins = data?.plugins ?? [];
    const q = search.trim().toLowerCase();
    if (!q) return plugins;
    return plugins.filter(
      (p) =>
        p.name.toLowerCase().includes(q) || p.slug.toLowerCase().includes(q),
    );
  }, [data?.plugins, search]);

  const createMutation = useCreatePluginMutation({
    onSuccess: async (data) => {
      setIsCreateDialogOpen(false);
      await invalidateAllPlugins(queryClient);
      navigate(routes.plugins.detail.href(data.id));
    },
  });

  const deleteMutation = useDeletePluginMutation({
    onSuccess: async () => {
      setPluginToDelete(null);
      await invalidateAllPlugins(queryClient);
    },
  });

  const handleCreate: React.FormEventHandler<HTMLFormElement> = (e) => {
    e.preventDefault();
    const formData = new FormData(e.currentTarget);
    const name = formData.get("name") as string;
    const description = formData.get("description") as string;

    createMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        createPluginForm: {
          name,
          description: description || undefined,
        },
      },
    });
  };

  const handleDelete = () => {
    if (!pluginToDelete) return;
    deleteMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: { id: pluginToDelete.id },
    });
  };

  // Destructure mutate so the dep array references the stable function
  // directly (TanStack Query keeps mutate referentially stable, but the
  // wrapper object is fresh per render). Keeps memo() on PublishDialog
  // effective and satisfies react-hooks/exhaustive-deps.
  const { mutate: publishMutate } = publishMutation;
  const handlePublish = useCallback(
    (githubUsername?: string) => {
      publishMutate({
        security: { sessionHeaderGramSession: "" },
        request: {
          publishPluginsRequestBody: { githubUsername },
        },
      });
    },
    [publishMutate],
  );

  const createCard = (
    <CreateResourceCard
      title="New Plugin"
      description="Bundle MCP servers and hooks for distribution to Claude Code, Cursor, and Codex."
      onClick={() => setIsCreateDialogOpen(true)}
    />
  );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>Plugins</Page.Section.Title>
          <Page.Section.Description className={hasPlugins ? "w-3/4" : ""}>
            Create distributable plugin bundles that package MCP servers and
            hooks together. Assign plugins to roles and publish them to Claude
            Code, Cursor, and Codex marketplaces via GitHub.
          </Page.Section.Description>
          <Page.Section.CTA>
            {publishStatus?.configured && (
              <Button
                variant="secondary"
                onClick={() => setIsPublishDialogOpen(true)}
                disabled={publishMutation.isPending}
              >
                <Button.LeftIcon>
                  <Icon
                    name={publishStatus.connected ? "refresh-cw" : "upload"}
                    className="h-4 w-4"
                  />
                </Button.LeftIcon>
                <Button.Text>
                  {publishMutation.isPending
                    ? "Publishing..."
                    : publishStatus.connected
                      ? "Re-publish"
                      : "Publish to GitHub"}
                </Button.Text>
              </Button>
            )}
          </Page.Section.CTA>
          <Page.Section.Body>
            <Stack direction="vertical" gap={4}>
              {publishStatus?.connected && publishStatus.repoUrl && (
                <div className="bg-muted/30 border-border/60 flex flex-wrap items-center justify-between gap-3 rounded-lg border px-4 py-3">
                  <div className="flex flex-col gap-0.5">
                    <span className="text-muted-foreground text-xs font-medium tracking-wide uppercase">
                      Marketplace
                    </span>
                    <a
                      href={publishStatus.repoUrl}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="hover:text-primary text-foreground font-mono text-sm hover:underline"
                    >
                      {publishStatus.repoOwner && publishStatus.repoName
                        ? `${publishStatus.repoOwner}/${publishStatus.repoName}`
                        : publishStatus.repoUrl}
                    </a>
                  </div>
                  {publishStatus.repoOwner && publishStatus.repoName && (
                    <InstallInstructionsButton
                      repoOwner={publishStatus.repoOwner}
                      repoName={publishStatus.repoName}
                      marketplaceUrl={publishStatus.marketplaceUrl}
                    />
                  )}
                </div>
              )}
              {hasPlugins && (
                <SearchBar
                  value={search}
                  onChange={setSearch}
                  placeholder="Search plugins"
                  className="w-64"
                />
              )}
              <PluginGrid
                plugins={filteredPlugins}
                searchQuery={hasPlugins ? search : ""}
                onDelete={setPluginToDelete}
                createCard={createCard}
              />
            </Stack>
          </Page.Section.Body>
        </Page.Section>

        <Page.Section>
          <Page.Section.Title>Observability hooks</Page.Section.Title>
          <Page.Section.Description className="w-3/4">
            The observability plugin forwards tool events from your team's
            Claude Code and Cursor installs to your Gram dashboard. When you
            publish to GitHub, it ships first in your marketplace marked
            Required. You can also download a single-plugin ZIP per platform
            here for direct install — each download mints a fresh hooks-scoped
            API key.
          </Page.Section.Description>
          <Page.Section.Body>
            <Stack direction="horizontal" gap={3} align="center">
              <div className="bg-primary/10 flex h-10 w-10 items-center justify-center rounded-lg">
                <Activity className="text-primary h-5 w-5" />
              </div>
              <Stack direction="vertical" gap={1} className="flex-1">
                <Type variant="body" className="font-medium">
                  Observability plugin
                </Type>
                <Type small muted>
                  {publishStatus?.connected
                    ? "Included in your published marketplace as required."
                    : "Available as a direct ZIP download. Connect GitHub publishing to also ship it via the marketplace."}
                </Type>
              </Stack>
              <DropdownMenu
                open={isObservabilityDownloadMenuOpen}
                onOpenChange={setIsObservabilityDownloadMenuOpen}
              >
                <DropdownMenuTrigger asChild>
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={isDownloadingObservability !== null}
                  >
                    <Button.LeftIcon>
                      <Icon name="download" className="h-4 w-4" />
                    </Button.LeftIcon>
                    <Button.Text>
                      {isDownloadingObservability
                        ? "Downloading..."
                        : "Download Observability Plugin"}
                    </Button.Text>
                    <Button.RightIcon>
                      <Icon name="chevron-down" className="h-4 w-4" />
                    </Button.RightIcon>
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem
                    onClick={() => handleObservabilityDownload("claude")}
                  >
                    Claude
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    onClick={() => handleObservabilityDownload("cursor")}
                  >
                    Cursor
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </Stack>
          </Page.Section.Body>
        </Page.Section>

        {/* Create Dialog */}
        <Dialog open={isCreateDialogOpen} onOpenChange={setIsCreateDialogOpen}>
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>Create Plugin</Dialog.Title>
              <Dialog.Description>
                Create a new plugin bundle for distributing MCP servers.
              </Dialog.Description>
            </Dialog.Header>
            <form onSubmit={handleCreate} className="flex flex-col gap-4">
              <InputField label="Name" name="name" required autoFocus />
              <InputField label="Description" name="description" />
              <Dialog.Footer>
                <Button
                  variant="secondary"
                  onClick={() => setIsCreateDialogOpen(false)}
                  type="button"
                >
                  Cancel
                </Button>
                <Button type="submit" disabled={createMutation.isPending}>
                  Create
                </Button>
              </Dialog.Footer>
            </form>
          </Dialog.Content>
        </Dialog>

        {/* Delete Confirmation Dialog */}
        <Dialog
          open={!!pluginToDelete}
          onOpenChange={() => setPluginToDelete(null)}
        >
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>Delete Plugin</Dialog.Title>
              <Dialog.Description>
                Are you sure you want to delete &quot;{pluginToDelete?.name}
                &quot;? This will remove it from all assigned users on the next
                publish.
              </Dialog.Description>
            </Dialog.Header>
            <Dialog.Footer>
              <Button
                variant="secondary"
                onClick={() => setPluginToDelete(null)}
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

function PluginGrid({
  plugins,
  searchQuery,
  onDelete,
  createCard,
}: {
  plugins: Plugin[];
  searchQuery: string;
  onDelete: (plugin: Plugin) => void;
  createCard: React.ReactNode;
}) {
  if (plugins.length === 0) {
    return (
      <div className="space-y-4">
        {searchQuery ? (
          <Type muted>No plugins matching &ldquo;{searchQuery}&rdquo;</Type>
        ) : null}
        <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
          {createCard}
        </div>
      </div>
    );
  }

  return (
    <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
      {plugins.map((plugin) => (
        <PluginCard key={plugin.id} plugin={plugin} onDelete={onDelete} />
      ))}
      {createCard}
    </div>
  );
}
