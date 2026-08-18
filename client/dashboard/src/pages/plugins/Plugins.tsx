import { CreateResourceCard } from "@/components/create-resource-card";
import { type FilterValue, useFilterState } from "@/components/filters";
import { InputField } from "@/components/moon/input-field";
import { ResourceListPage } from "@/components/page-templates";
import { Dialog } from "@/components/ui/Dialog";
import { Card } from "@/components/ui/Card";
import { Text } from "@/components/ui/Text";
import { RequireScope } from "@/components/require-scope";
import { useFetcher } from "@/contexts/Fetcher";
import { openSafeExternalUrl } from "@/lib/safe-external-url";
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
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Stack } from "@/components/ui/Stack";
import { Activity, Network } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";
import { Outlet, useNavigate } from "react-router";
import { toast } from "sonner";
import { PlatformInstrumentationSheet } from "../setup/components/platform-instrumentation-sheet";
import { PlatformMCPOnboardingContent } from "../org/PlatformMCP";
import { usePlatformMCPPackageStatus } from "@gram/client/react-query/platformMCPPackageStatus";
import {
  MarketplaceCard,
  UninitializedMarketplaceCard,
} from "./MarketplaceCard";
import { PluginCard } from "./PluginCard";
import { PluginInstallButton } from "./PluginInstallButton";
import { downloadResponse } from "./downloadPluginPackage";
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
  // Polled so the marketplace card and per-plugin sync badges pick up the
  // Temporal generator-rollout schedule's auto-sync without a manual refresh.
  const { data: publishStatus } = usePublishStatusSuspense(
    undefined,
    undefined,
    { refetchInterval: 5_000 },
  );
  const { data: marketplaceSettings } = useMarketplaceSettingsSuspense();
  const { fetch: authFetch } = useFetcher();
  const [isObservabilityDownloadMenuOpen, setIsObservabilityDownloadMenuOpen] =
    useState(false);
  const [isDownloadingObservability, setIsDownloadingObservability] = useState<
    "claude" | "cursor" | "codex" | "opencode" | null
  >(null);

  const handleObservabilityDownload = async (
    platform: "claude" | "cursor" | "codex" | "opencode",
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
      await downloadResponse(resp, `observability-${platform}.zip`);
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
            openSafeExternalUrl(data.repoUrl);
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
    marketplaceSettings.marketplaceName ?? marketplaceSettings.defaultName,
  );
  const updateMarketplaceSettingsMutation =
    useUpdateMarketplaceSettingsMutation({
      onSuccess: async (data) => {
        await Promise.all([
          invalidateAllMarketplaceSettings(queryClient),
          invalidateAllPublishStatus(queryClient),
        ]);
        setMarketplaceNameInput(data.settings.marketplaceName ?? "");
        if (data.hooksUpdateDeferred) {
          toast.warning(
            "Marketplace name updated, but the observability hooks plugin can't be updated yet: your organization isn't approved for the latest hooks version. It will update automatically once your org is rolled forward.",
          );
        } else {
          toast.success(
            data.republished
              ? "Marketplace name updated and republished"
              : "Marketplace name saved",
          );
        }
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
    // Reset the input to the persisted value so reopening discards unsaved
    // edits. Falls back to the computed default name (not "") when unset —
    // otherwise the field shows only ghost placeholder text with nothing to
    // actually save, which is a dead end once naming is required (Setup).
    setMarketplaceNameInput(
      marketplaceSettings.marketplaceName ?? marketplaceSettings.defaultName,
    );
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
      description="Bundle MCP servers and hooks for distribution to supported coding agents."
      onClick={() => setIsCreateDialogOpen(true)}
    />
  );

  return (
    <>
      <ResourceListPage
        title="Plugins"
        description={
          <span className={hasPlugins ? "block w-3/4" : undefined}>
            Create distributable plugin bundles that package MCP servers and
            skills together. Assign plugins to roles and publish them to
            supported agent marketplaces via GitHub.
          </span>
        }
        hideToolbar={!hasPlugins}
        search={{
          value: search,
          onChange: setSearch,
          placeholder: "Search plugins",
        }}
        filters={{
          schema: PLUGINS_FILTERS,
          values: pluginFilters.values,
          optionsById: pluginFilterOptions,
          onChange: pluginFilters.setValue as (
            id: string,
            value: FilterValue,
          ) => void,
          onClear: pluginFilters.clearValue as (id: string) => void,
          onClearAll: pluginFilters.clearAll,
        }}
      >
        <Stack direction="vertical" gap={8}>
          {publishStatus?.configured &&
            (publishStatus.connected && publishStatus.repoUrl ? (
              publishStatus.hasCollaborators === false ? (
                <>
                  <UninitializedMarketplaceCard
                    publishStatus={publishStatus}
                    defaultName={
                      marketplaceSettings.marketplaceName ??
                      marketplaceSettings.defaultName
                    }
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
                    onSync={() => handlePublish([])}
                    isSyncing={publishMutation.isPending}
                  />
                  <div className="border-border border-t" />
                </>
              )
            ) : (
              <>
                <UninitializedMarketplaceCard
                  publishStatus={publishStatus}
                  defaultName={
                    marketplaceSettings.marketplaceName ??
                    marketplaceSettings.defaultName
                  }
                  onSetup={handleStartSetup}
                  onAddCollaborators={() => setIsManageCollaboratorsOpen(true)}
                />
                <div className="border-border border-t" />
              </>
            ))}
          <Text small muted>
            The default plugin is where all newly created MCP servers will be
            automatically published to. If you have the default plugin installed
            in your coding agent, then any new MCP servers will become instantly
            available for installation.
          </Text>
          <PluginGrid
            plugins={filteredPlugins}
            publishStatus={publishStatus}
            searchQuery={hasPlugins ? search : ""}
            createCard={createCard}
          />
          <div className="flex items-center gap-3">
            <div className="border-border flex-1 border-t" />
            <Text
              small
              muted
              className="shrink-0 font-mono text-xs tracking-wide uppercase"
            >
              Platform Plugins
            </Text>
            <div className="border-border flex-1 border-t" />
          </div>
          <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
            <ObservabilityPluginCard
              publishStatus={publishStatus}
              isDownloadMenuOpen={isObservabilityDownloadMenuOpen}
              onDownloadMenuOpenChange={setIsObservabilityDownloadMenuOpen}
              isDownloading={isDownloadingObservability !== null}
              onDownload={(platform) => {
                void handleObservabilityDownload(platform);
              }}
            />
            <RequireScope scope="org:admin" level="section">
              <PlatformMCPPluginCard />
            </RequireScope>
          </div>
        </Stack>
      </ResourceListPage>

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
              plugin slug ({"<plugin>@<marketplace>"}) when installing from a
              supported agent marketplace. Applies to all plugins in this
              project.
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
            <Text small muted>
              Will publish as{" "}
              <code>
                {trimmedMarketplaceName || marketplaceSettings.defaultName}
              </code>
              .{" "}
              {publishStatus?.connected
                ? "Saving will regenerate the marketplace and push to GitHub."
                : "Will take effect on your next publish."}
            </Text>
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
    </>
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
  onDownload: (platform: "claude" | "cursor" | "codex" | "opencode") => void;
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
    <Card.Entity
      className="border-primary/30 bg-primary/[0.02]"
      icon={<Activity className="text-primary h-10 w-10 opacity-80" />}
    >
      <div className="mb-2 flex items-center gap-1.5">
        <Text
          variant="subheading"
          as="div"
          className="text-md truncate"
          title="Observability"
        >
          Observability
        </Text>
        <Badge variant="information">
          <Badge.Text>Platform</Badge.Text>
        </Badge>
      </div>

      <Text small muted className="mb-3 line-clamp-3">
        Forwards tool events from your team&apos;s coding agent installs to your
        project dashboard. Ships first in your marketplace, marked Required.
      </Text>

      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        <Text small muted>
          {isConnected
            ? "Included in your marketplace"
            : "Available as a direct download"}
        </Text>
        <DropdownMenu
          open={isDownloadMenuOpen}
          onOpenChange={onDownloadMenuOpenChange}
        >
          <DropdownMenuTrigger asChild>
            <PluginInstallButton size="sm" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              disabled={!installTarget}
              onClick={() => {
                // Defer until after the dropdown has fully closed to avoid a
                // Radix focus-trap/body-lock conflict between the closing
                // menu and the opening sheet (same pattern as MCPDetails.tsx).
                setTimeout(() => setIsInstallSheetOpen(true), 0);
              }}
            >
              <div className="flex flex-col">
                <span>GitHub installation (preferred)</span>
                {!installTarget && (
                  <span className="text-muted-foreground text-xs">
                    Requires marketplace setup
                  </span>
                )}
              </div>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              disabled={isDownloading}
              onClick={() => {
                onDownload("claude");
              }}
            >
              Download as zip — Claude
            </DropdownMenuItem>
            <DropdownMenuItem
              disabled={isDownloading}
              onClick={() => {
                onDownload("cursor");
              }}
            >
              Download as zip — Cursor
            </DropdownMenuItem>
            <DropdownMenuItem
              disabled={isDownloading}
              onClick={() => {
                onDownload("codex");
              }}
            >
              Download as zip — Codex
            </DropdownMenuItem>
            <DropdownMenuItem
              disabled={isDownloading}
              onClick={() => {
                onDownload("opencode");
              }}
            >
              Download as zip — OpenCode
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
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
    </Card.Entity>
  );
}

function PlatformMCPPluginCard(): JSX.Element {
  const { fetch: authFetch } = useFetcher();
  const [installOpen, setInstallOpen] = useState(false);
  const [isInstallMenuOpen, setIsInstallMenuOpen] = useState(false);
  const [downloadingPackage, setDownloadingPackage] = useState<
    "claude" | "cursor" | "codex" | "opencode" | "agent-plugin" | null
  >(null);
  const status = usePlatformMCPPackageStatus(undefined, undefined, {
    refetchInterval: 5_000,
  });
  const packageStatus = status.data;
  const directDownloadReady =
    packageStatus?.available === true && packageStatus.directDownloadAvailable;

  const downloadPackage = async (
    platform: "claude" | "cursor" | "codex" | "opencode" | "agent-plugin",
  ) => {
    setIsInstallMenuOpen(false);
    setDownloadingPackage(platform);
    try {
      const response = await authFetch(
        `/rpc/plugins.downloadPlatformMCPPlugin?platform=${platform}`,
        {},
      );
      if (!response.ok) throw new Error("download failed");
      await downloadResponse(
        response,
        `speakeasy-aicp-platform-mcp-${platform}.zip`,
      );
    } catch (error) {
      toast.error("Could not download the Platform MCP package");
      console.error("Platform MCP plugin download failed", error);
    } finally {
      setDownloadingPackage(null);
    }
  };

  const statusLabel = (() => {
    if (status.isLoading) return "Checking package availability…";
    if (packageStatus?.admission === "indeterminate") {
      return "Package availability temporarily unknown";
    }
    if (!packageStatus?.available) return "Not available for this organization";
    if (
      packageStatus.marketplaceConnected &&
      packageStatus.freshness === "current"
    ) {
      return "Included in your organization marketplace";
    }
    if (
      packageStatus.marketplaceConnected &&
      (packageStatus.freshness === "missing" ||
        packageStatus.freshness === "stale")
    ) {
      return "Marketplace update available";
    }
    return "Available as a direct download";
  })();

  return (
    <Card.Entity
      className="border-primary/30 bg-primary/[0.02]"
      icon={<Network className="text-primary h-10 w-10 opacity-80" />}
    >
      <div className="mb-2 flex items-center gap-1.5">
        <Text
          variant="subheading"
          as="div"
          className="text-md truncate"
          title="Speakeasy AICP Platform MCP"
        >
          Platform MCP
        </Text>
        <Badge variant="information">
          <Badge.Text>Platform</Badge.Text>
        </Badge>
      </div>

      <Text small muted className="mb-3 line-clamp-3">
        Connects your selected coding agent to Speakeasy through OAuth and
        includes a reviewed workflow for adding an MCP Catalogue server to an
        explicit project.
      </Text>

      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        <Text small muted>
          {statusLabel}
        </Text>
        <DropdownMenu
          open={isInstallMenuOpen}
          onOpenChange={setIsInstallMenuOpen}
        >
          <DropdownMenuTrigger asChild>
            <PluginInstallButton
              size="sm"
              loading={downloadingPackage !== null}
              disabled={!packageStatus?.available || status.isLoading}
            />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              onClick={() => {
                setIsInstallMenuOpen(false);
                // Let the dropdown release its focus trap before opening the
                // first sheet in the shared tracked setup flow.
                setTimeout(() => setInstallOpen(true), 0);
              }}
            >
              Guided setup (preferred)
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              disabled={!directDownloadReady || downloadingPackage !== null}
              onClick={() => void downloadPackage("claude")}
            >
              Download as zip — Claude
            </DropdownMenuItem>
            <DropdownMenuItem
              disabled={!directDownloadReady || downloadingPackage !== null}
              onClick={() => void downloadPackage("cursor")}
            >
              Download as zip — Cursor
            </DropdownMenuItem>
            <DropdownMenuItem
              disabled={!directDownloadReady || downloadingPackage !== null}
              onClick={() => void downloadPackage("codex")}
            >
              Download as zip — Codex
            </DropdownMenuItem>
            <DropdownMenuItem
              disabled={!directDownloadReady || downloadingPackage !== null}
              onClick={() => void downloadPackage("opencode")}
            >
              Download as zip — OpenCode
            </DropdownMenuItem>
            <DropdownMenuItem
              disabled={!directDownloadReady || downloadingPackage !== null}
              onClick={() => void downloadPackage("agent-plugin")}
            >
              Download as zip — Agent Plugins
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      <PlatformMCPOnboardingContent
        sheetOnly
        setupOpen={installOpen}
        onSetupOpenChange={setInstallOpen}
      />
    </Card.Entity>
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
          <Text muted>No plugins matching &ldquo;{searchQuery}&rdquo;</Text>
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
