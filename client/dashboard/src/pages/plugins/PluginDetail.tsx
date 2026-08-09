import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
import { InputField } from "@/components/moon/input-field";
import { Page } from "@/components/page-layout";
import { MCPStatusIndicator } from "@/components/mcp/MCPStatusIndicator";
import { RequireScope } from "@/components/require-scope";
import { ToolCollectionBadge } from "@/components/tool-collection-badge";
import { Button as UiButton } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Dialog } from "@/components/ui/Dialog";
import { Card } from "@/components/ui/Card";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { openSafeExternalUrl } from "@/lib/safe-external-url";
import { cn } from "@/lib/utils";
import { mcpServerRouteParam } from "@/lib/sources";
import {
  DangerSettingsSection,
  SettingsSection,
} from "@/components/detail/settings-section";
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
import { useMembers } from "@gram/client/react-query/members";
import { useRoles } from "@gram/client/react-query/roles";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useSyncedAgentUsers } from "@gram/client/react-query/syncedAgentUsers.js";
import type { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Icon } from "@/components/ui/Icon";
import { SearchBar } from "@/components/ui/SearchBar";
import { Stack } from "@/components/ui/Stack";
import { useQueryClient } from "@tanstack/react-query";
import { formatDistanceToNow } from "date-fns";
import { Network, Trash2 } from "lucide-react";
import {
  Fragment,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useLocation, useNavigate, useParams } from "react-router";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { PluginServer } from "@gram/client/models/components/pluginserver.js";
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import { useProject } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { toast } from "sonner";
import { DEFAULT_PLUGIN_DESCRIPTION } from "./default-plugin";
import {
  type PluginPackagePlatform,
  usePluginPackageDownload,
} from "./downloadPluginPackage";
import { InstallInstructionsDialog } from "./InstallInstructionsDialog";
import { PluginInstallButton } from "./PluginInstallButton";
import { PluginAssignmentsSheet } from "./PluginAssignmentsSheet";
import { PluginSkillsSection } from "./PluginSkillsSection";
import { PluginAssignmentsList } from "./PluginAssignmentsList";
import {
  activePluginSection,
  PLUGIN_ASSIGNMENTS_SECTION_ID,
  PLUGIN_OVERVIEW_SECTION_ID,
  PLUGIN_SERVERS_SECTION_ID,
  PLUGIN_SETTINGS_SECTION_ID,
  PLUGIN_SKILLS_SECTION_ID,
} from "./plugin-detail-sections";
import { countPluginInstalls } from "./plugin-reach";
import { describePrincipal, memberMapByUrn, roleMapByUrn } from "./principals";
import { PublishDialog } from "./PublishDialog";
import { SectionEmptyState } from "./SectionEmptyState";
import { usePluginAssignmentsVisible } from "./use-plugin-assignments-visible";

// A selectable server for a plugin, sourced from either a toolset (Hosted) or
// a Remote- or unproxied-MCP-backed mcp_server. The kind determines
// whether it is submitted as a toolset_id or an mcp_server_id, mirroring the
// collections picker.
type ServerOptionKind = "toolset" | "mcpServer";
type ServerOption = {
  kind: ServerOptionKind;
  id: string;
  name: string;
  isUnproxied?: boolean;
};

function serverOptionSuffix(option: ServerOption): string {
  if (option.kind !== "mcpServer") return "";
  return option.isUnproxied ? " (Unproxied MCP)" : " (Remote MCP)";
}

function serverOptionKey(kind: ServerOptionKind, id: string): string {
  return `${kind}:${id}`;
}

export default function PluginDetail(): JSX.Element | null {
  const { pluginId } = useParams<{ pluginId: string }>();
  const location = useLocation();
  const project = useProject();
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const navigate = useNavigate();
  // Each nav entry is its own subpage, so only the section named by the path
  // renders.
  const section = activePluginSection(location.pathname, pluginId);
  const [isEditOpen, setIsEditOpen] = useState(false);
  const [isAddServerOpen, setIsAddServerOpen] = useState(false);
  const [isDownloadMenuOpen, setIsDownloadMenuOpen] = useState(false);
  const [isInstallSheetOpen, setIsInstallSheetOpen] = useState(false);
  const [isPublishDialogOpen, setIsPublishDialogOpen] = useState(false);
  const [isManageCollaboratorsOpen, setIsManageCollaboratorsOpen] =
    useState(false);
  const [isDeleteOpen, setIsDeleteOpen] = useState(false);
  const [isAssignmentsOpen, setIsAssignmentsOpen] = useState(false);
  const [serverSearch, setServerSearch] = useState("");
  const [assignmentSearch, setAssignmentSearch] = useState("");
  // The component stays mounted when only :pluginId changes, so stale searches
  // would filter the new plugin's lists.
  useEffect(() => {
    setServerSearch("");
    setAssignmentSearch("");
  }, [pluginId]);

  const { data: plugin } = usePluginSuspense({ id: pluginId! });
  // Polled so the publish-freshness badges/banner pick up the Temporal
  // generator-rollout schedule's auto-sync without a manual refresh.
  const { data: publishStatus } = usePublishStatus(undefined, undefined, {
    refetchInterval: 5_000,
  });

  const client = useSdkClient();
  const { isDownloading, download } = usePluginPackageDownload(
    client,
    pluginId!,
    setIsDownloadMenuOpen,
  );

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
      (mcpServersData?.mcpServers ?? []).filter(
        (s) => !!s.remoteMcpServerId || !!s.unproxiedMcpServerId,
      ),
    [mcpServersData],
  );
  const publishableMcpServers = useMemo(() => {
    const serverIdsWithEndpoint = new Set(
      (mcpEndpointsData?.mcpEndpoints ?? []).map((e) => e.mcpServerId),
    );
    // Unproxied-backed servers are never proxied, so they never gain an
    // mcp_endpoints row — exempt them from the endpoint requirement, mirroring
    // the backend's AddPluginServer check (server/internal/plugins/impl.go).
    return mcpServers.filter(
      (s) =>
        s.visibility !== "disabled" &&
        (serverIdsWithEndpoint.has(s.id) || !!s.unproxiedMcpServerId),
    );
  }, [mcpServers, mcpEndpointsData]);

  const isLoadingServers =
    isLoadingToolsets || isLoadingMcpServers || isLoadingMcpEndpoints;

  // Roles and members resolve the plugin's assignment principal URNs to human
  // names in the assignments section (and seed the assignment sheet). React
  // Query dedupes these with the sheet's own calls.
  const { data: rolesData } = useRoles();
  const { data: membersData } = useMembers();
  const roleByUrn = useMemo(
    () => roleMapByUrn(rolesData?.roles ?? []),
    [rolesData?.roles],
  );
  const memberByUrn = useMemo(
    () => memberMapByUrn(membersData?.members ?? []),
    [membersData?.members],
  );

  const showAssignments = usePluginAssignmentsVisible();
  const { data: productFeatures } = useProductFeatures();

  // Device-agent reach powers the Installs stat. It's an admin-only, org-scoped
  // list, so it's fetched only for device-agent orgs and degrades quietly
  // (throwOnError:false) when the viewer can't read it.
  const {
    data: syncedUsersData,
    isLoading: isLoadingSynced,
    error: syncedUsersError,
  } = useSyncedAgentUsers(undefined, undefined, {
    throwOnError: false,
    enabled: showAssignments,
  });
  // The synced-users list is admin-only; a non-admin viewer's request is
  // forbidden, leaving reach unknown. Distinguish that from a genuine zero so
  // the Installs metric doesn't misreport "no data" as "no installs". Loading
  // is a separate state, surfaced via the card's refreshing spinner.
  const installsUnavailable = !isLoadingSynced && !!syncedUsersError;

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

  // Destructure mutate so callbacks depend on the stable function rather than
  // the fresh-per-render wrapper object (mirrors Plugins.tsx).
  const { mutate: publishMutate } = publishMutation;
  // Mirror the in-flight flag into a ref so detached callbacks can gate on the
  // current pending state. The "Publish now" toast action closure is created
  // when offerPublish runs (before any publish starts), so it can't read a live
  // isPending — without this guard, stacking edits into multiple toasts lets a
  // user fire concurrent publishes that the disabled buttons prevent.
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
  // Sync on the marketplace banner. A connected project republishes in one
  // click; a configured-but-unconnected project needs the first-publish dialog
  // (it collects collaborators). Unconfigured projects get no nudge — there's
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
        isUnproxied: !!s.unproxiedMcpServerId,
      });
    }
    return opts;
  }, [toolsets, publishableMcpServers]);

  if (!plugin) return null;

  const isDefaultPlugin = plugin.isDefault ?? false;
  const description =
    plugin.description ??
    (isDefaultPlugin ? DEFAULT_PLUGIN_DESCRIPTION : "No description");

  const servers = plugin.servers ?? [];
  const assignments = plugin.assignments ?? [];
  const installs = countPluginInstalls(
    assignments,
    syncedUsersData?.users ?? [],
    membersData?.members ?? [],
    rolesData?.roles ?? [],
  );

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

  // Client-side search over the server section, matching on the displayed name.
  const normalizedServerSearch = serverSearch.trim().toLowerCase();
  const filteredServers = normalizedServerSearch
    ? servers.filter((s) =>
        s.displayName.toLowerCase().includes(normalizedServerSearch),
      )
    : servers;

  // Distinguish "nothing added yet" from "no search matches".
  let serversContent: JSX.Element;
  if (servers.length === 0) {
    serversContent = <SectionEmptyState title="No servers added yet" />;
  } else if (filteredServers.length === 0) {
    serversContent = <SectionEmptyState title="No servers match your search" />;
  } else {
    serversContent = (
      <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
        {filteredServers.map((server) => (
          <PluginServerCard
            key={server.id}
            server={server}
            toolset={
              server.toolsetId ? toolsetById.get(server.toolsetId) : undefined
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
    );
  }

  // Client-side search over the assignments section, matching on the resolved
  // principal label (role name, member name, email, "Everyone") plus email.
  const normalizedAssignmentSearch = assignmentSearch.trim().toLowerCase();
  const filteredAssignments = normalizedAssignmentSearch
    ? assignments.filter((a) => {
        const { label } = describePrincipal(
          a.principalUrn,
          roleByUrn,
          memberByUrn,
        );
        const email = memberByUrn.get(a.principalUrn)?.email ?? "";
        return (
          label.toLowerCase().includes(normalizedAssignmentSearch) ||
          email.toLowerCase().includes(normalizedAssignmentSearch)
        );
      })
    : assignments;

  let assignmentsContent: JSX.Element;
  if (assignments.length === 0) {
    assignmentsContent = (
      <SectionEmptyState
        title="Not assigned to anyone yet"
        subtitle="Assign this plugin to roles, users, or emails to deliver it to their devices."
      />
    );
  } else if (filteredAssignments.length === 0) {
    assignmentsContent = (
      <SectionEmptyState title="No assignments match your search" />
    );
  } else {
    assignmentsContent = (
      <PluginAssignmentsList
        assignments={filteredAssignments}
        roleByUrn={roleByUrn}
        memberByUrn={memberByUrn}
      />
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [pluginId ?? ""]: plugin.name }}
        />
      </Page.Header>
      <Page.Body fullWidth className="gap-0">
        <div className="mx-auto w-full max-w-[1270px] flex-1 space-y-10 px-8 py-8">
          {section === PLUGIN_OVERVIEW_SECTION_ID && (
            <section className="space-y-8">
              <div className="flex flex-wrap items-start justify-between gap-4">
                <SettingsSection.Header>
                  <SettingsSection.Title>Overview</SettingsSection.Title>
                  <SettingsSection.Description>
                    {description}
                  </SettingsSection.Description>
                </SettingsSection.Header>
                <Stack
                  direction="horizontal"
                  gap={2}
                  align="center"
                  className="shrink-0"
                >
                  <MarketplaceSyncButton
                    publishStatus={publishStatus}
                    isPending={publishMutation.isPending}
                    onSync={() => handlePublish([])}
                  />
                  <PluginInstallControl
                    plugin={{
                      name: plugin.name,
                      slug: plugin.slug,
                      description: plugin.description,
                      agentPluginsV1Compatible: plugin.agentPluginsV1Compatible,
                    }}
                    publishStatus={publishStatus}
                    isDownloadMenuOpen={isDownloadMenuOpen}
                    onDownloadMenuOpenChange={setIsDownloadMenuOpen}
                    onDownload={(platform) => void download(platform)}
                    isDownloading={isDownloading}
                    isInstallSheetOpen={isInstallSheetOpen}
                    onInstallSheetOpenChange={setIsInstallSheetOpen}
                  />
                </Stack>
              </div>

              <StatTileGroup>
                <StatTile
                  title="MCP servers"
                  value={plugin.serverCount ?? servers.length}
                  tone="information"
                  format="number"
                  icon="network"
                />
                <StatTile
                  title="Skills"
                  value={plugin.skillCount ?? 0}
                  tone="information"
                  format="number"
                  icon="sparkles"
                />
                {showAssignments && (
                  <>
                    <StatTile
                      title="Assignments"
                      value={plugin.assignmentCount ?? assignments.length}
                      tone="information"
                      format="number"
                      icon="users"
                      subtext="Roles, users, and emails"
                    />
                    <StatTile
                      title="Installs"
                      value={installs}
                      tone="information"
                      displayValue={installsUnavailable ? "—" : undefined}
                      format="number"
                      icon="download"
                      isRefreshing={isLoadingSynced}
                      subtext={
                        installsUnavailable
                          ? "Requires admin access"
                          : "Running the device agent"
                      }
                      tooltip={
                        installsUnavailable
                          ? "Install counts require organization admin access."
                          : undefined
                      }
                    />
                  </>
                )}
              </StatTileGroup>

              <PublishFreshnessIndicator publishStatus={publishStatus} />
            </section>
          )}

          {section === PLUGIN_SERVERS_SECTION_ID && (
            <SettingsSection>
              <div className="flex flex-wrap items-start justify-between gap-4">
                <SettingsSection.Header>
                  <div className="flex items-center gap-2">
                    <SettingsSection.Title>MCP Servers</SettingsSection.Title>
                    {servers.length > 0 && (
                      <span className="border-border text-muted-foreground border px-1.5 py-0.5 font-mono text-xs tabular-nums">
                        {servers.length}
                      </span>
                    )}
                  </div>
                  <SettingsSection.Description>
                    MCP servers bundled in this plugin. Everyone who installs
                    the plugin gets these servers.
                  </SettingsSection.Description>
                </SettingsSection.Header>
                <div className="flex items-center gap-2">
                  {servers.length > 0 && (
                    <SearchBar
                      value={serverSearch}
                      onChange={setServerSearch}
                      placeholder="Search servers"
                      className="h-9 w-56"
                    />
                  )}
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
              </div>
              {serversContent}
            </SettingsSection>
          )}

          {section === PLUGIN_SKILLS_SECTION_ID && (
            <RequireScope
              scope="skill:read"
              resourceId={project.id}
              level="page"
            >
              <PluginSkillsSection
                key={pluginId!}
                pluginId={pluginId!}
                viewMode="grid"
                onMutated={(message) => offerPublish(message)}
              />
            </RequireScope>
          )}

          {/* Assignments only affect device-agent delivery, so the section is
              hidden for marketplace-only orgs (see usePluginAssignmentsVisible).
              Now that it's a route, a bookmark can still land here — say why the
              page is empty rather than rendering nothing. Wait for
              productFeatures before doing so, since the visibility flag reads
              false while it loads. */}
          {section === PLUGIN_ASSIGNMENTS_SECTION_ID &&
            !showAssignments &&
            !!productFeatures && (
              <SettingsSection>
                <SettingsSection.Header>
                  <SettingsSection.Title>Assignments</SettingsSection.Title>
                  <SettingsSection.Description>
                    Assignments control delivery to devices running the
                    Speakeasy agent.
                  </SettingsSection.Description>
                </SettingsSection.Header>
                <SectionEmptyState
                  title="Assignments aren't available for this project"
                  subtitle="Marketplace installs receive every published plugin, so there's nothing to assign."
                />
              </SettingsSection>
            )}

          {section === PLUGIN_ASSIGNMENTS_SECTION_ID && showAssignments && (
            <SettingsSection>
              <div className="flex flex-wrap items-start justify-between gap-4">
                <SettingsSection.Header>
                  <div className="flex items-center gap-2">
                    <SettingsSection.Title>Assignments</SettingsSection.Title>
                    {assignments.length > 0 && (
                      <span className="border-border text-muted-foreground border px-1.5 py-0.5 font-mono text-xs tabular-nums">
                        {assignments.length}
                      </span>
                    )}
                  </div>
                  <SettingsSection.Description>
                    Controls delivery to devices running the Speakeasy agent.
                    Marketplace installs receive every published plugin
                    regardless.
                  </SettingsSection.Description>
                </SettingsSection.Header>
                <div className="flex items-center gap-2">
                  {assignments.length > 0 && (
                    <SearchBar
                      value={assignmentSearch}
                      onChange={setAssignmentSearch}
                      placeholder="Search assignments"
                      className="h-9 w-56"
                    />
                  )}
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => setIsAssignmentsOpen(true)}
                  >
                    <Button.LeftIcon>
                      <Icon name="users" className="h-4 w-4" />
                    </Button.LeftIcon>
                    <Button.Text>Manage assignments</Button.Text>
                  </Button>
                </div>
              </div>
              {assignmentsContent}
            </SettingsSection>
          )}

          {section === PLUGIN_SETTINGS_SECTION_ID && (
            <>
              <SettingsSection>
                <SettingsSection.Header>
                  <SettingsSection.Title>Plugin details</SettingsSection.Title>
                  <SettingsSection.Description>
                    Registry identity and presentation metadata.
                  </SettingsSection.Description>
                </SettingsSection.Header>
                <SettingsSection.Panel>
                  <SettingsSection.Body>
                    <dl className="grid gap-4 sm:grid-cols-3">
                      <div>
                        <dt className="text-muted-foreground text-xs">Name</dt>
                        <dd className="mt-1 text-sm">{plugin.name}</dd>
                      </div>
                      <div>
                        <dt className="text-muted-foreground text-xs">Slug</dt>
                        <dd className="mt-1 font-mono text-sm">
                          {plugin.slug}
                        </dd>
                      </div>
                      <div>
                        <dt className="text-muted-foreground text-xs">
                          Description
                        </dt>
                        <dd className="mt-1 text-sm">
                          {plugin.description || "None"}
                        </dd>
                      </div>
                    </dl>
                  </SettingsSection.Body>
                  <SettingsSection.Footer>
                    <SettingsSection.FooterHint>
                      Editing republishes the plugin on your next sync.
                    </SettingsSection.FooterHint>
                    <SettingsSection.FooterActions>
                      <Button size="sm" onClick={() => setIsEditOpen(true)}>
                        Edit details
                      </Button>
                    </SettingsSection.FooterActions>
                  </SettingsSection.Footer>
                </SettingsSection.Panel>
              </SettingsSection>

              <DangerSettingsSection>
                <DangerSettingsSection.Header>
                  <DangerSettingsSection.Title>
                    Danger zone
                  </DangerSettingsSection.Title>
                </DangerSettingsSection.Header>
                <DangerSettingsSection.Panel>
                  <DangerSettingsSection.Body className="flex flex-wrap items-center justify-between gap-4">
                    <div className="space-y-1">
                      <Text className="text-sm font-semibold">
                        Delete this plugin
                      </Text>
                      <Text small muted className="max-w-xl">
                        {isDefaultPlugin
                          ? "This is the default plugin new MCP servers publish to. Deleting it removes the plugin from all assigned users on the next publish."
                          : "Deleting removes this plugin from all assigned users on the next publish."}
                      </Text>
                    </div>
                    <Button
                      variant="destructive-primary"
                      onClick={() => setIsDeleteOpen(true)}
                    >
                      Delete plugin
                    </Button>
                  </DangerSettingsSection.Body>
                </DangerSettingsSection.Panel>
              </DangerSettingsSection>
            </>
          )}
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
                    className="bg-background border px-3 py-2 text-sm"
                    required
                  >
                    <option value="">Select an MCP server</option>
                    {availableServerOptions.map((o) => (
                      <option
                        key={serverOptionKey(o.kind, o.id)}
                        value={serverOptionKey(o.kind, o.id)}
                      >
                        {o.name}
                        {serverOptionSuffix(o)}
                      </option>
                    ))}
                  </select>
                ) : serverOptions.length > 0 ? (
                  <Text muted small>
                    All available MCP servers have already been added to this
                    plugin.
                  </Text>
                ) : (
                  <Text muted small>
                    No MCP servers available. Create an MCP server in this
                    project first.
                  </Text>
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
        <PublishDialog
          mode="manage"
          open={isManageCollaboratorsOpen}
          onOpenChange={setIsManageCollaboratorsOpen}
          onPublish={handlePublish}
          isPending={publishMutation.isPending}
        />
        <PluginAssignmentsSheet
          pluginId={pluginId!}
          pluginName={plugin.name}
          assignments={assignments}
          open={isAssignmentsOpen}
          onOpenChange={setIsAssignmentsOpen}
          onSaved={() => {
            void invalidateAll();
            offerPublish("Assignments updated");
          }}
        />
      </Page.Body>
    </Page>
  );
}

// Always-available manual re-sync for a connected marketplace. The banner only
// surfaces a Sync action when it detects unpublished changes; this button lets
// the user force a republish at any time (e.g. to recover from a failed push or
// re-run the generator), restoring the old detail page's persistent Sync
// control. Renders nothing until the project is connected — the not-connected
// publish path lives in the marketplace banner.
function MarketplaceSyncButton({
  publishStatus,
  isPending,
  onSync,
}: {
  publishStatus: PublishStatusResult | undefined;
  isPending: boolean;
  onSync: () => void;
}): JSX.Element | null {
  if (!publishStatus?.connected) return null;

  const hasUnpublishedChanges = publishStatus.upToDate === false;

  return (
    <Button variant="secondary" onClick={onSync} disabled={isPending}>
      <Button.LeftIcon>
        <Icon name="refresh-cw" className="h-4 w-4" />
      </Button.LeftIcon>
      <Button.Text>
        {isPending
          ? "Syncing..."
          : hasUnpublishedChanges
            ? "Sync changes"
            : "Sync"}
      </Button.Text>
    </Button>
  );
}

// Install affordance for the plugin overview: a download menu offering the
// preferred GitHub-marketplace install (when the marketplace is set up) and
// per-platform zip downloads, plus the install-instructions sheet.
export function PluginInstallControl({
  plugin,
  publishStatus,
  isDownloadMenuOpen,
  onDownloadMenuOpenChange,
  onDownload,
  isDownloading,
  isInstallSheetOpen,
  onInstallSheetOpenChange,
}: {
  plugin: {
    name: string;
    slug: string;
    description?: string;
    agentPluginsV1Compatible: boolean;
  };
  publishStatus: PublishStatusResult | undefined;
  isDownloadMenuOpen: boolean;
  onDownloadMenuOpenChange: (open: boolean) => void;
  onDownload: (platform: PluginPackagePlatform) => void;
  isDownloading: boolean;
  isInstallSheetOpen: boolean;
  onInstallSheetOpenChange: (open: boolean) => void;
}): JSX.Element {
  const marketplaceReady = !!(
    publishStatus?.connected &&
    publishStatus.repoOwner &&
    publishStatus.repoName
  );

  return (
    <Stack direction="horizontal" gap={2} align="center" className="shrink-0">
      <DropdownMenu
        open={isDownloadMenuOpen}
        onOpenChange={onDownloadMenuOpenChange}
      >
        <DropdownMenuTrigger asChild>
          <PluginInstallButton loading={isDownloading} />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem
            onClick={() => {
              // Defer until after the dropdown has fully closed to avoid a
              // Radix focus-trap/body-lock conflict between the closing menu
              // and the opening sheet (same pattern as MCPDetails.tsx).
              setTimeout(() => onInstallSheetOpenChange(true), 0);
            }}
            disabled={!marketplaceReady}
          >
            <div className="flex flex-col">
              <span>GitHub installation (preferred)</span>
              {!marketplaceReady && (
                <span className="text-muted-foreground text-xs">
                  Requires marketplace setup
                </span>
              )}
            </div>
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            disabled={isDownloading}
            onClick={() => onDownload("claude")}
          >
            Download as zip — Claude
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={isDownloading}
            onClick={() => onDownload("cursor")}
          >
            Download as zip — Cursor
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={isDownloading}
            onClick={() => onDownload("codex")}
          >
            Download as zip — Codex
          </DropdownMenuItem>
          {plugin.agentPluginsV1Compatible && (
            <DropdownMenuItem
              disabled={isDownloading}
              onClick={() => onDownload("agent-plugin")}
            >
              Download Agent Plugins ZIP
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
      {marketplaceReady && publishStatus && (
        <InstallInstructionsDialog
          open={isInstallSheetOpen}
          onOpenChange={onInstallSheetOpenChange}
          repoOwner={publishStatus.repoOwner!}
          repoName={publishStatus.repoName!}
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
    </Stack>
  );
}

// Shows the published freshness of a connected project under the overview
// stats: an explicit "Requires syncing" vs "Up to date" badge, paired with the
// last-published time and the manifest version currently live in the published
// repo. The timestamp shows in both states, and the live version is what plugin
// clients report for installed plugins, so it's the value to compare against
// when debugging sync lag. Renders nothing when not connected, or when
// freshness is unknown and there's nothing published to show.
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

  // Each row is omitted when its value is unknown rather than guessed. The
  // live version is rendered in its own mono cell so the long timestamp-based
  // patch (e.g. 0.1.1783944246) can't collide with adjacent text.
  const rows: { label: string; value: JSX.Element }[] = [];

  if (publishStatus.lastPublishedAt) {
    rows.push({
      label: "Last published",
      value: (
        <Text small>
          {formatDistanceToNow(publishStatus.lastPublishedAt, {
            addSuffix: true,
          })}
        </Text>
      ),
    });
  }

  // Manifest versions come straight from GitHub and occasionally carry a stray
  // BOM / zero-width / control char that renders as a tofu box — strip anything
  // outside the semver charset before display.
  const cleanVersion = publishStatus.liveVersion
    ?.replace(/[^\w.\-+]/g, "")
    .trim();
  if (cleanVersion) {
    rows.push({
      label: "Current version",
      value: (
        <div className="flex items-center gap-1">
          <Badge variant="information">
            <Badge.Text className="font-mono">{cleanVersion}</Badge.Text>
          </Badge>
          <CopyButton size="xs" text={cleanVersion} tooltip="Copy version" />
        </div>
      ),
    });
  }

  if (hasUnpublishedChanges || isUpToDate) {
    rows.push({
      label: "Sync status",
      value: hasUnpublishedChanges ? (
        <Badge variant="warning">Requires syncing</Badge>
      ) : (
        <Badge variant="success">Up to date</Badge>
      ),
    });
  }

  // Freshness unknown and nothing published yet — nothing meaningful to show.
  if (rows.length === 0) return null;

  return (
    <div className="grid w-fit grid-cols-[max-content_1fr] items-center gap-x-8 gap-y-2">
      {rows.map((row) => (
        <Fragment key={row.label}>
          <Text muted small>
            {row.label}
          </Text>
          <div>{row.value}</div>
        </Fragment>
      ))}
    </div>
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
    <Card.Entity
      className={cn(isClickable && "cursor-pointer")}
      onClick={isClickable ? handleClick : undefined}
      icon={<Network className="text-muted-foreground h-8 w-8" />}
    >
      <div className="mb-2 flex items-start justify-between gap-2">
        <Text
          variant="subheading"
          as="div"
          className="text-md group-hover:text-primary flex-1 truncate transition-colors"
          title={server.displayName}
        >
          {server.displayName}
        </Text>
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
            // Remote/unproxied MCP servers have no Gram-side tool
            // catalog, so the tool-collection badge is omitted.
            <Badge variant="neutral" className="text-xs">
              {mcpServer?.unproxiedMcpServerId
                ? "Unproxied MCP · Not proxied"
                : "Remote MCP"}
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
          variant="tertiary"
          size="sm"
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
    </Card.Entity>
  );
}
