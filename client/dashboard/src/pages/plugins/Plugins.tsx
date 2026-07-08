import { CreateResourceCard } from "@/components/create-resource-card";
import { type FilterValue, useFilterState } from "@/components/filters";
import { InputField } from "@/components/moon/input-field";
import { Page } from "@/components/page-layout";
import { Dialog } from "@/components/ui/dialog";
import { DotCard } from "@/components/ui/dot-card";
import { Type } from "@/components/ui/type";
import { useFetcher } from "@/contexts/Fetcher";
import { useRoutes } from "@/routes";
import type { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";
import { Plugin } from "@gram/client/models/components/plugin.js";
import { useCreatePluginMutation } from "@gram/client/react-query/createPlugin";
import {
  invalidateAllPlugins,
  usePluginsSuspense,
} from "@gram/client/react-query/plugins";
import {
  invalidateAllPublishStatus,
  usePublishStatusSuspense,
} from "@gram/client/react-query/publishStatus";
import { usePublishPluginsMutation } from "@gram/client/react-query/publishPlugins";
import {
  invalidateAllMarketplaceSettings,
  useMarketplaceSettingsSuspense,
} from "@gram/client/react-query/marketplaceSettings";
import { useUpdateMarketplaceSettingsMutation } from "@gram/client/react-query/updateMarketplaceSettings";
import {
  Badge,
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
import { PlatformInstrumentationSheet } from "../setup/components/platform-instrumentation-sheet";
import {
  MarketplaceCard,
  UninitializedMarketplaceCard,
} from "./MarketplaceCard";
import { PluginCard } from "./PluginCard";
import {
  matchesPluginFilters,
  PLUGINS_FILTERS,
  pluginServerFilterOptions,
} from "./plugins-filter-schema";
import { PublishDialog } from "./PublishDialog";

export function PluginsRoot(): JSX.Element {
  return <Outlet />;
}

export default function Plugins(): JSX.Element {
  const [isCreateDialogOpen, setIsCreateDialogOpen] = useState(false);
  const [isPublishDialogOpen, setIsPublishDialogOpen] = useState(false);
  const [isManageCollaboratorsOpen, setIsManageCollaboratorsOpen] =
    useState(false);
  const [search, setSearch] = useState("");
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const navigate = useNavigate();

  const { data } = usePluginsSuspense();
  const { data: publishStatus } = usePublishStatusSuspense();
  const { data: marketplaceSettings } = useMarketplaceSettingsSuspense();
  const { fetch: authFetch } = useFetcher();
  const [isObservabilityDownloadMenuOpen, setIsObservabilityDownloadMenuOpen] =
    useState(false);
  const [isDownloadingObservability, setIsDownloadingObservability] = useState<
    "claude" | "cursor" | "codex" | null
  >(null);

  const handleObservabilityDownload = async (
    platform: "claude" | "cursor" | "codex",
  ) => {
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
      setIsManageCollaboratorsOpen(false);
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

  const hasPlugins = (data?.plugins ?? []).length > 0;

  const pluginFilters = useFilterState(PLUGINS_FILTERS);
  const pluginFilterOptions = useMemo(
    () => pluginServerFilterOptions(data?.plugins ?? []),
    [data?.plugins],
  );

  const filteredPlugins = useMemo(() => {
    const plugins = data?.plugins ?? [];
    const q = search.trim().toLowerCase();
    return plugins.filter((p) => {
      if (!matchesPluginFilters(p, pluginFilters.values)) return false;
      if (!q) return true;
      return (
        p.name.toLowerCase().includes(q) || p.slug.toLowerCase().includes(q)
      );
    });
  }, [data?.plugins, search, pluginFilters.values]);

  const createMutation = useCreatePluginMutation({
    onSuccess: async (data) => {
      setIsCreateDialogOpen(false);
      await invalidateAllPlugins(queryClient);
      void navigate(routes.plugins.detail.href(data.id));
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

  // Destructure mutate so the dep array references the stable function
  // directly (TanStack Query keeps mutate referentially stable, but the
  // wrapper object is fresh per render). Keeps memo() on PublishDialog
  // effective and satisfies react-hooks/exhaustive-deps.
  const { mutate: publishMutate } = publishMutation;
  const handlePublish = useCallback(
    (githubUsernames: string[]) => {
      publishMutate({
        security: { sessionHeaderGramSession: "" },
        request: {
          publishPluginsRequestBody: { githubUsernames },
        },
      });
    },
    [publishMutate],
  );

  const [marketplaceNameInput, setMarketplaceNameInput] = useState(
    marketplaceSettings.marketplaceName ?? "",
  );
  const updateMarketplaceSettingsMutation =
    useUpdateMarketplaceSettingsMutation({
      onSuccess: async (data) => {
        await Promise.all([
          invalidateAllMarketplaceSettings(queryClient),
          invalidateAllPublishStatus(queryClient),
        ]);
        setMarketplaceNameInput(data.settings.marketplaceName ?? "");
        toast.success(
          data.republished
            ? "Marketplace name updated and republished"
            : "Marketplace name saved",
        );
      },
      onError: () => {
        toast.error("Failed to update marketplace name");
      },
    });

  const [isMarketplaceSettingsDialogOpen, setIsMarketplaceSettingsDialogOpen] =
    useState(false);
  // Set when the settings dialog was opened from the uninitialized card's
  // "Setup" CTA rather than "Rename" on an already-connected marketplace —
  // saving the name should then chain straight into the publish dialog so
  // Setup reads as one flow (name, then collaborators) instead of two.
  const [chainToPublishAfterSave, setChainToPublishAfterSave] = useState(false);

  const trimmedMarketplaceName = marketplaceNameInput.trim();
  const currentMarketplaceName = marketplaceSettings.marketplaceName ?? "";
  const marketplaceNameDirty =
    trimmedMarketplaceName !== currentMarketplaceName.trim();

  const handleOpenMarketplaceSettings = () => {
    // Reset the input to the persisted value so reopening discards unsaved edits.
    setMarketplaceNameInput(marketplaceSettings.marketplaceName ?? "");
    setIsMarketplaceSettingsDialogOpen(true);
  };

  const handleStartSetup = () => {
    setChainToPublishAfterSave(true);
    handleOpenMarketplaceSettings();
  };

  const handleSaveMarketplaceName = () => {
    updateMarketplaceSettingsMutation.mutate(
      {
        security: { sessionHeaderGramSession: "" },
        request: {
          updateMarketplaceSettingsRequestBody: {
            marketplaceName: trimmedMarketplaceName || undefined,
          },
        },
      },
      {
        onSuccess: () => {
          setIsMarketplaceSettingsDialogOpen(false);
          if (chainToPublishAfterSave) {
            setChainToPublishAfterSave(false);
            setIsPublishDialogOpen(true);
          }
        },
      },
    );
  };

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
            skills together. Assign plugins to roles and publish them to Claude
            Code, Cursor, and Codex marketplaces via GitHub.
          </Page.Section.Description>
          <Page.Section.Body>
            <Stack direction="vertical" gap={8}>
              {publishStatus?.configured &&
                (publishStatus.connected && publishStatus.repoUrl ? (
                  publishStatus.hasCollaborators === false ? (
                    <>
                      <UninitializedMarketplaceCard
                        publishStatus={publishStatus}
                        onSetup={handleStartSetup}
                        onAddCollaborators={() =>
                          setIsManageCollaboratorsOpen(true)
                        }
                      />
                      <div className="border-border border-t" />
                    </>
                  ) : (
                    <>
                      <MarketplaceCard
                        publishStatus={publishStatus}
                        onManageCollaborators={() =>
                          setIsManageCollaboratorsOpen(true)
                        }
                        onRename={handleOpenMarketplaceSettings}
                      />
                      <div className="border-border border-t" />
                    </>
                  )
                ) : (
                  <>
                    <UninitializedMarketplaceCard
                      publishStatus={publishStatus}
                      onSetup={handleStartSetup}
                      onAddCollaborators={() =>
                        setIsManageCollaboratorsOpen(true)
                      }
                    />
                    <div className="border-border border-t" />
                  </>
                ))}
              {hasPlugins && (
                <Page.Toolbar>
                  <Page.Toolbar.Search
                    value={search}
                    onChange={setSearch}
                    placeholder="Search plugins"
                  />
                  <Page.Toolbar.Filters
                    schema={PLUGINS_FILTERS}
                    values={pluginFilters.values}
                    optionsById={pluginFilterOptions}
                    onChange={
                      pluginFilters.setValue as (
                        id: string,
                        value: FilterValue,
                      ) => void
                    }
                    onClear={pluginFilters.clearValue as (id: string) => void}
                    onClearAll={pluginFilters.clearAll}
                  />
                </Page.Toolbar>
              )}
              <PluginGrid
                plugins={filteredPlugins}
                publishStatus={publishStatus}
                searchQuery={hasPlugins ? search : ""}
                createCard={createCard}
              />
              <div className="flex items-center gap-3">
                <div className="border-border flex-1 border-t" />
                <Type
                  small
                  muted
                  className="shrink-0 font-mono text-xs tracking-wide uppercase"
                >
                  Platform Plugins
                </Type>
                <div className="border-border flex-1 border-t" />
              </div>
              <div className="grid grid-cols-2 gap-6">
                <ObservabilityPluginCard
                  publishStatus={publishStatus}
                  isDownloadMenuOpen={isObservabilityDownloadMenuOpen}
                  onDownloadMenuOpenChange={setIsObservabilityDownloadMenuOpen}
                  isDownloading={isDownloadingObservability !== null}
                  onDownload={(platform) => {
                    void handleObservabilityDownload(platform);
                  }}
                />
              </div>
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

        <PublishDialog
          open={isPublishDialogOpen}
          onOpenChange={setIsPublishDialogOpen}
          onPublish={handlePublish}
          isPending={publishMutation.isPending}
        />
        <PublishDialog
          mode="manage"
          open={isManageCollaboratorsOpen}
          onOpenChange={setIsManageCollaboratorsOpen}
          onPublish={handlePublish}
          isPending={publishMutation.isPending}
        />

        {/* Marketplace Settings Dialog */}
        <Dialog
          open={isMarketplaceSettingsDialogOpen}
          onOpenChange={setIsMarketplaceSettingsDialogOpen}
        >
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>Marketplace settings</Dialog.Title>
              <Dialog.Description>
                The marketplace name is the identifier your team types after the
                plugin slug ({"<plugin>@<marketplace>"}) when installing from
                Claude Code or Codex. Leave blank to use the default (
                <code>{marketplaceSettings.defaultName}</code>
                ). Applies to all plugins in this project.
              </Dialog.Description>
            </Dialog.Header>
            <form
              className="flex flex-col gap-4"
              onSubmit={(e) => {
                e.preventDefault();
                handleSaveMarketplaceName();
              }}
            >
              <InputField
                label="Marketplace name"
                name="marketplace_name"
                value={marketplaceNameInput}
                onChange={(e) => setMarketplaceNameInput(e.target.value)}
                placeholder={marketplaceSettings.defaultName}
                pattern="^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$"
                title="Lowercase letters, digits, and hyphens. May not start or end with a hyphen."
                // Renaming an already-published marketplace can fall back to
                // the default name, so it's genuinely optional there — but
                // mid-Setup this is the one deliberate naming step, so it
                // reads as required (also hides the "optional" label via
                // AnyField's group-has-[[required]] rule).
                required={chainToPublishAfterSave}
                autoFocus
              />
              <Type small muted>
                Will publish as{" "}
                <code>
                  {trimmedMarketplaceName || marketplaceSettings.defaultName}
                </code>
                .{" "}
                {publishStatus?.connected
                  ? "Saving will regenerate the marketplace and push to GitHub."
                  : "Will take effect on your next publish."}
              </Type>
              <Dialog.Footer>
                <Button
                  variant="secondary"
                  type="button"
                  onClick={() => setIsMarketplaceSettingsDialogOpen(false)}
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  disabled={
                    !marketplaceNameDirty ||
                    updateMarketplaceSettingsMutation.isPending
                  }
                >
                  <Button.Text>
                    {updateMarketplaceSettingsMutation.isPending
                      ? publishStatus?.connected
                        ? "Republishing..."
                        : "Saving..."
                      : "Save"}
                  </Button.Text>
                </Button>
              </Dialog.Footer>
            </form>
          </Dialog.Content>
        </Dialog>
      </Page.Body>
    </Page>
  );
}

// Platform-provided plugin, not a real `Plugin` row — always ships first in
// the marketplace and has no detail page, so it gets its own card rather than
// reusing PluginCard. The accent border + "Platform" badge are its "special
// affordance" distinguishing it from user-created plugins in the grid.
function ObservabilityPluginCard({
  publishStatus,
  isDownloadMenuOpen,
  onDownloadMenuOpenChange,
  isDownloading,
  onDownload,
}: {
  publishStatus: PublishStatusResult | undefined;
  isDownloadMenuOpen: boolean;
  onDownloadMenuOpenChange: (open: boolean) => void;
  isDownloading: boolean;
  onDownload: (platform: "claude" | "cursor" | "codex") => void;
}) {
  const [isInstallSheetOpen, setIsInstallSheetOpen] = useState(false);
  const isConnected = !!publishStatus?.connected;
  const installTarget =
    isConnected && publishStatus?.repoOwner && publishStatus.repoName
      ? {
          repoOwner: publishStatus.repoOwner,
          repoName: publishStatus.repoName,
          marketplaceUrl: publishStatus.marketplaceUrl,
        }
      : undefined;

  return (
    <DotCard
      className="border-primary/30 bg-primary/[0.02]"
      icon={<Activity className="text-primary h-10 w-10 opacity-80" />}
    >
      <div className="mb-2 flex items-center gap-1.5">
        <Type
          variant="subheading"
          as="div"
          className="text-md truncate"
          title="Observability"
        >
          Observability
        </Type>
        <Badge variant="information">
          <Badge.Text>Platform</Badge.Text>
        </Badge>
      </div>

      <Type small muted className="mb-3 line-clamp-3">
        Forwards tool events from your team&apos;s Claude Code, Cursor and Codex
        installs to your project dashboard. Ships first in your marketplace,
        marked Required.
      </Type>

      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        <Type small muted>
          {isConnected
            ? "Included in your marketplace"
            : "Available as a direct download"}
        </Type>
        {/* Split button (GitHub "Merge pull request" style): primary click
            launches the install sheet, the attached caret opens the zip
            download's platform picker. */}
        <div className="flex items-stretch">
          <Button
            variant="primary"
            size="sm"
            className="rounded-r-none"
            disabled={!installTarget}
            onClick={() => setIsInstallSheetOpen(true)}
          >
            <Button.Text>Install</Button.Text>
          </Button>
          <DropdownMenu
            open={isDownloadMenuOpen}
            onOpenChange={onDownloadMenuOpenChange}
          >
            <DropdownMenuTrigger asChild>
              <Button
                variant="primary"
                size="sm"
                className="border-primary-foreground/25 -ml-px rounded-l-none border-l px-1.5"
                disabled={isDownloading}
                aria-label="Download as zip"
              >
                <Icon name="chevron-down" className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                onClick={() => {
                  onDownload("claude");
                }}
              >
                Download as zip — Claude
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => {
                  onDownload("cursor");
                }}
              >
                Download as zip — Cursor
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => {
                  onDownload("codex");
                }}
              >
                Download as zip — Codex
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      {/* Reuses the onboarding wizard's platform-by-platform setup sheet
          (real per-platform slugs, API key minting, full instructions)
          instead of the generic single-plugin install dialog — no
          preselected platform, so the sheet opens on its own platform
          picker first. */}
      <PlatformInstrumentationSheet
        open={isInstallSheetOpen}
        onOpenChange={setIsInstallSheetOpen}
      />
    </DotCard>
  );
}

function PluginGrid({
  plugins,
  publishStatus,
  searchQuery,
  createCard,
}: {
  plugins: Plugin[];
  publishStatus: PublishStatusResult | undefined;
  searchQuery: string;
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
        <PluginCard
          key={plugin.id}
          plugin={plugin}
          publishStatus={publishStatus}
        />
      ))}
      {createCard}
    </div>
  );
}
