import { MCPToolsetIcon } from "@/components/mcp/MCPToolsetIcon";
import { SourceMcpIcon } from "@/components/sources/SourceCard";
import { McpEntityCard } from "@/components/mcp/McpEntityCard";
import { invalidateAllResourceAudience } from "@gram/client/react-query/resourceAudience.js";
import { usePluginQueryScope } from "@/pages/plugins/usePluginQueryScope";
import { usePluginWriteAccess } from "@/hooks/usePluginWriteAccess";
import { usePluginServerQueries } from "./usePluginServerQueries";
import { useRBAC } from "@/hooks/useRBAC";
import { PluginDistributionDetail } from "./PluginDistributionDetail";
import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
import { useOrganization } from "@/contexts/Auth";
import { InputField } from "@/components/moon/input-field";
import { Page } from "@/components/page-layout";
import { MCPStatusIndicator } from "@/components/mcp/MCPStatusIndicator";
import { useRecentLabelOverride } from "@/components/command-palette/recentlyVisited";
import { RequireScope } from "@/components/require-scope";
import { ToolCollectionBadge } from "@/components/tool-collection-badge";
import { Button as UiButton } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Dialog } from "@/components/ui/Dialog";
import {
  ServerPickerSheet,
  type ServerPickerOption,
  type ServerPickerResult,
} from "@/components/mcp/ServerPickerSheet";
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
import { useAudiences } from "@gram/client/react-query/audiences";
import { useMembers } from "@gram/client/react-query/members";
import { useRoles } from "@gram/client/react-query/roles";
import { useSyncedAgentUsers } from "@gram/client/react-query/syncedAgentUsers.js";
import type { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { handleError } from "@/lib/errors";
import { Alert } from "@/components/ui/Alert";
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
import { Label } from "@/components/ui/Label";
import { SearchBar } from "@/components/ui/SearchBar";
import { Stack } from "@/components/ui/Stack";
import { Switch } from "@/components/ui/Switch";
import { useQueryClient } from "@tanstack/react-query";
import { formatDistanceToNow } from "date-fns";
import { Trash2 } from "lucide-react";
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
  pluginSectionHref,
  PLUGIN_ASSIGNMENTS_SECTION_ID,
  PLUGIN_OVERVIEW_SECTION_ID,
  PLUGIN_SETTINGS_SECTION_ID,
  PLUGIN_SKILLS_SECTION_ID,
} from "./plugin-detail-sections";
import { countPluginInstalls } from "./plugin-reach";
import {
  audienceMapByUrn,
  describePrincipal,
  memberMapByUrn,
  roleMapByUrn,
} from "./principals";
import { PublishDialog } from "./PublishDialog";
import { SectionEmptyState } from "./SectionEmptyState";
import { usePluginAssignmentsVisible } from "./use-plugin-assignments-visible";

// A selectable server for a plugin, sourced from either a toolset (Hosted) or
// a Remote- or unproxied-MCP-backed mcp_server. The kind determines
// whether it is submitted as a toolset_id or an mcp_server_id.
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
  const { hasScope, isLoading } = useRBAC();
  const organization = useOrganization();
  const canWritePlugin = usePluginWriteAccess();
  if (isLoading) return null;
  // Keep privileged queries unmounted, rather than hiding their rendered UI.
  return canWritePlugin || hasScope("org:read", organization.id) ? (
    <AdminPluginDetail />
  ) : (
    <PluginDistributionDetail />
  );
}

// Assignment metadata includes admin-only audiences and device reach. Keep its
// queries unmounted for org readers, including when cached admin data exists.
function AdminPluginDetail(): JSX.Element | null {
  const { hasScope } = useRBAC();
  const organization = useOrganization();
  return hasScope("org:admin", organization.id) ? (
    <PluginDetailWithAssignments />
  ) : (
    <PluginDetailContent />
  );
}

type AssignmentDataProps = {
  rolesData?: ReturnType<typeof useRoles>["data"];
  membersData?: ReturnType<typeof useMembers>["data"];
  audiencesData?: ReturnType<typeof useAudiences>["data"];
  syncedUsersData?: ReturnType<typeof useSyncedAgentUsers>["data"];
  isLoadingSynced?: boolean;
  installsUnavailable?: boolean;
};

function PluginDetailWithAssignments(): JSX.Element | null {
  const scope = usePluginQueryScope();
  const { data: rolesData } = useRoles(scope, undefined, {
    queryKeyHashFn: (key) => JSON.stringify([scope, key]),
  });
  const { data: membersData } = useMembers(scope, undefined, {
    queryKeyHashFn: (key) => JSON.stringify([scope, key]),
  });
  const { data: audiencesData } = useAudiences(scope);
  const showAssignments = usePluginAssignmentsVisible();
  const {
    data: syncedUsersData,
    isLoading,
    error,
  } = useSyncedAgentUsers(scope, undefined, {
    enabled: showAssignments,
    throwOnError: false,
  });
  return (
    <PluginDetailContent
      rolesData={rolesData}
      membersData={membersData}
      audiencesData={audiencesData}
      syncedUsersData={syncedUsersData}
      isLoadingSynced={isLoading}
      installsUnavailable={!isLoading && !!error}
    />
  );
}

function PluginDetailContent({
  rolesData,
  membersData,
  audiencesData,
  syncedUsersData,
  isLoadingSynced = false,
  installsUnavailable = false,
}: AssignmentDataProps): JSX.Element | null {
  const { pluginId } = useParams<{ pluginId: string }>();
  const { hasScope } = useRBAC();
  const organization = useOrganization();
  const canAdmin = hasScope("org:admin", organization.id);
  const canPublish = usePluginWriteAccess();
  const location = useLocation();
  const project = useProject();
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const navigate = useNavigate();
  // Each nav entry is its own subpage, so only the section named by the path
  // renders.
  const section = activePluginSection(location.pathname, pluginId);
  const [isEditOpen, setIsEditOpen] = useState(false);
  // The server's refusal to turn auto sync off, shown next to the switch.
  const [autoSyncError, setAutoSyncError] = useState<string | null>(null);
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
  // would filter the new plugin's lists and an auto sync refusal would show
  // on a plugin it wasn't about.
  useEffect(() => {
    setServerSearch("");
    setAssignmentSearch("");
    setAutoSyncError(null);
  }, [pluginId]);

  const scope = usePluginQueryScope();
  const { data: plugin } = usePluginSuspense({ ...scope, id: pluginId! });
  useRecentLabelOverride(location.pathname, plugin.name);
  // Polled so the publish-freshness badges/banner pick up the Temporal
  // generator-rollout schedule's auto-sync without a manual refresh.
  const { data: publishStatus } = usePublishStatus(scope, undefined, {
    refetchInterval: 5_000,
  });

  const client = useSdkClient();
  const { isDownloading, download } = usePluginPackageDownload(
    client,
    pluginId!,
    setIsDownloadMenuOpen,
    scope,
  );

  const { canReadServers, toolsetsQuery, serversQuery, endpointsQuery } =
    usePluginServerQueries();
  const { data: toolsetsData, isLoading: isLoadingToolsets } = toolsetsQuery;
  const toolsets = useMemo(
    () => toolsetsData?.toolsets ?? [],
    [toolsetsData?.toolsets],
  );

  // Remote MCP-backed and built-in mcp_servers for this project. All of
  // them back the cards of already-attached plugin servers (an attached
  // server that's been disabled or lost its endpoints must still resolve for
  // display), while only non-disabled servers with at least one endpoint are
  // publishable, mirroring the backend's AddPluginServer check, so the picker
  // never offers a server the API would reject.
  const { data: mcpServersData, isLoading: isLoadingMcpServers } = serversQuery;
  const { data: mcpEndpointsData, isLoading: isLoadingMcpEndpoints } =
    endpointsQuery;
  const mcpServers = useMemo(
    () =>
      (mcpServersData?.mcpServers ?? []).filter(
        (s) =>
          !!s.remoteMcpServerId ||
          !!s.unproxiedMcpServerId ||
          !!s.builtinBackend,
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
  const roleByUrn = useMemo(
    () => roleMapByUrn(rolesData?.roles ?? []),
    [rolesData?.roles],
  );
  const memberByUrn = useMemo(
    () => memberMapByUrn(membersData?.members ?? []),
    [membersData?.members],
  );
  const audienceByUrn = useMemo(
    () => audienceMapByUrn(audiencesData?.audiences ?? []),
    [audiencesData?.audiences],
  );

  const assignmentsVisible = usePluginAssignmentsVisible();
  const showAssignments = canAdmin && assignmentsVisible;

  // Invalidate publish status too so the dirty/up-to-date affordance reflects
  // the edit the moment a mutation lands.
  const invalidateAll = async () => {
    await invalidateAllPlugin(queryClient);
    await invalidateAllPlugins(queryClient);
    await invalidateAllResourceAudience(queryClient);
    await invalidateAllPublishStatus(queryClient);
  };

  // Ref, not state: onSuccess/onError read the mode from a detached mutation
  // callback, where a state capture would be stale; it also avoids re-rendering
  // the memoized dialogs on mode switch.
  const publishModeRef = useRef<"publish" | "manage">("publish");

  const publishMutation = usePublishPluginsMutation({
    onSuccess: (data) => {
      const managing = publishModeRef.current === "manage";
      setIsPublishDialogOpen(false);
      setIsManageCollaboratorsOpen(false);
      setAutoSyncError(null);
      void invalidateAllPublishStatus(queryClient);
      toast.success(
        managing ? "Collaborators added" : "Plugins published to GitHub",
        {
          description: data.repoUrl,
          action: {
            label: "Open",
            onClick: () => {
              openSafeExternalUrl(data.repoUrl);
            },
          },
        },
      );
    },
    onError: () => {
      toast.error(
        publishModeRef.current === "manage"
          ? "Failed to add collaborators"
          : "Failed to publish plugins to GitHub",
      );
    },
  });

  // Destructure mutate so callbacks depend on the stable function rather than
  // the fresh-per-render wrapper object (mirrors Plugins.tsx).
  const { mutate: publishMutate } = publishMutation;
  // Mirror the in-flight flag into a ref so detached callbacks can gate on the
  // current pending state: a callback captured before a publish started can't
  // read a live isPending, and without this guard could fire a concurrent
  // publish that the disabled buttons prevent.
  const isPublishingRef = useRef(publishMutation.isPending);
  isPublishingRef.current = publishMutation.isPending;
  const handlePublish = useCallback(
    (githubUsernames: string[]) => {
      if (!canPublish || isPublishingRef.current) return;
      publishModeRef.current = "publish";
      publishMutate({
        security: { sessionHeaderGramSession: "" },
        request: { publishPluginsRequestBody: { githubUsernames } },
      });
    },
    [canPublish, publishMutate],
  );
  const handleAddCollaborators = useCallback(
    (githubUsernames: string[]) => {
      if (!canPublish || isPublishingRef.current) return;
      publishModeRef.current = "manage";
      publishMutate({
        security: { sessionHeaderGramSession: "" },
        request: { publishPluginsRequestBody: { githubUsernames } },
      });
    },
    [canPublish, publishMutate],
  );

  // Undefined (an older cached record) reads as on, the server's default.
  const autoSyncOff = plugin.autoSyncEnabled === false;

  // A connected marketplace republishes on its own after every plugin change,
  // so say so rather than asking for a manual sync — unless this plugin's auto
  // sync is off, when its changes wait for Sync changes. A deletion is never
  // held. A configured-but-unconnected project still needs the first-publish
  // dialog (it collects collaborators). Unconfigured projects get no nudge —
  // there's nowhere to publish to.
  const offerPublish = useCallback(
    (message: string, held: boolean = autoSyncOff) => {
      if (!canPublish || !publishStatus?.configured) return;
      if (publishStatus.connected) {
        toast.success(message, {
          description: held
            ? "Auto sync is off for this plugin, so changes publish when someone clicks Sync changes."
            : "Changes sync to your marketplace automatically.",
        });
        return;
      }
      toast.success(message, {
        action: {
          label: "Publish now",
          onClick: () => setIsPublishDialogOpen(true),
        },
      });
    },
    [
      canPublish,
      publishStatus?.configured,
      publishStatus?.connected,
      autoSyncOff,
    ],
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
      // The detail and sidebar are still mounted until navigation completes.
      // Mark their deleted record stale without issuing a guaranteed 404.
      await invalidateAllPlugin(queryClient, { refetchType: "none" });
      await invalidateAllPlugins(queryClient);
      await invalidateAllResourceAudience(queryClient);
      void invalidateAllPublishStatus(queryClient);
      offerPublish("Plugin deleted", false);
      void navigate(routes.plugins.href());
    },
  });

  const addServerMutation = useAddPluginServerMutation();

  const removeServerMutation = useRemovePluginServerMutation({
    onSuccess: () => {
      void invalidateAll();
      offerPublish("Server removed from plugin");
    },
  });

  const handleRemoveServer = (server: PluginServer) => {
    if (!canPublish) return;
    removeServerMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: { id: server.id, pluginId: pluginId! },
    });
  };

  const handleUpdate: React.FormEventHandler<HTMLFormElement> = (e) => {
    e.preventDefault();
    if (!canPublish) return;
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

  const autoSyncMutation = useUpdatePluginMutation({
    onSuccess: (updated) => {
      setAutoSyncError(null);
      void invalidateAll();
      toast.success(
        updated.autoSyncEnabled
          ? "Auto sync turned on"
          : "Auto sync turned off",
        {
          description: updated.autoSyncEnabled
            ? "Changes to this plugin, including any that were waiting, now publish automatically."
            : "Changes to this plugin now wait until someone clicks Sync changes.",
        },
      );
    },
    onError: (error) => {
      // A 412 is the server refusing to turn auto sync off, which the admin
      // fixes next to the switch. Any other failure is a toast.
      if (error instanceof GramError && error.statusCode === 412) {
        setAutoSyncError(error.message);
        return;
      }
      handleError(error, { title: "Couldn't change auto sync" });
    },
  });

  // Sends the plugin's current details back unchanged: the server refuses to
  // turn auto sync off in the same update as an edit.
  const handleAutoSyncChange = (enabled: boolean) => {
    if (!canPublish) return;
    setAutoSyncError(null);
    autoSyncMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        updatePluginForm: {
          id: pluginId!,
          name: plugin.name,
          slug: plugin.slug,
          description: plugin.description || undefined,
          autoSyncEnabled: enabled,
        },
      },
    });
  };

  const handleDelete = () => {
    if (!canPublish) return;
    deleteMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: { id: pluginId! },
    });
  };

  // Adds one at a time so each failure is reported against its own row.
  const handleAddServers = async (
    keys: string[],
  ): Promise<ServerPickerResult[]> => {
    if (!canPublish) return [];
    const results: ServerPickerResult[] = [];
    for (const key of keys) {
      const option = serverOptions.find(
        (o) => serverOptionKey(o.kind, o.id) === key,
      );
      if (!option) continue;
      try {
        await addServerMutation.mutateAsync({
          security: { sessionHeaderGramSession: "" },
          request: {
            addPluginServerForm: {
              pluginId: pluginId!,
              // Submit exactly one backend id per the toolset_id XOR
              // mcp_server_id contract.
              ...(option.kind === "mcpServer"
                ? { mcpServerId: option.id }
                : { toolsetId: option.id }),
              displayName: option.name,
              policy: "required",
            },
          },
        });
        results.push({ key });
      } catch (error) {
        results.push({
          key,
          error:
            error instanceof Error
              ? error.message
              : "Couldn't add this server. Try again.",
        });
      }
    }
    if (results.some((result) => !result.error)) await invalidateAll();
    return results;
  };

  const toolsetById = useMemo(() => {
    const map = new Map<string, ToolsetEntry>();
    for (const t of toolsets) map.set(t.id, t);
    return map;
  }, [toolsets]);

  // Hosted toolsets with a canonical MCP server (same id); undefined until servers load.
  const canonicalToolsetIds = useMemo(() => {
    if (!mcpServersData?.mcpServers) return undefined;
    return new Set(
      mcpServersData.mcpServers
        .filter((s) => s.toolsetId === s.id)
        .map((s) => s.id),
    );
  }, [mcpServersData]);

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

  // Exclude added servers; a hosted server's canonical mcp_server id is its toolset id.
  const addedToolsetIds = new Set(
    servers
      .map((s) => s.toolsetId ?? s.mcpServerId)
      .filter((id): id is string => !!id),
  );
  const addedMcpServerIds = new Set(
    servers.map((s) => s.mcpServerId).filter((id): id is string => !!id),
  );
  const pickerOptions: ServerPickerOption[] = serverOptions
    .filter((o) =>
      o.kind === "toolset"
        ? !addedToolsetIds.has(o.id)
        : !addedMcpServerIds.has(o.id),
    )
    .map((o) => {
      const toolset = o.kind === "toolset" ? toolsetById.get(o.id) : undefined;
      const mcpServer =
        o.kind === "mcpServer" ? mcpServerById.get(o.id) : undefined;
      const kindLabel = toolset
        ? "Hosted"
        : mcpServer?.builtinBackend
          ? "Built-in"
          : mcpServer?.unproxiedMcpServerId
            ? "Unproxied"
            : "Remote";
      const slug = toolset?.slug ?? mcpServer?.slug;
      // Mirrors AddPluginServer: a hosted server needs MCP on and an MCP slug.
      const mcpOff = !!toolset && !toolset.mcpEnabled;
      const noMcpAddress = !!toolset && !mcpOff && !toolset.mcpSlug;
      const noMcpServer =
        !!toolset &&
        !mcpOff &&
        !noMcpAddress &&
        !!canonicalToolsetIds &&
        !canonicalToolsetIds.has(toolset.id);
      return {
        value: serverOptionKey(o.kind, o.id),
        label: o.name,
        icon: toolset ? (
          <MCPToolsetIcon
            toolset={toolset}
            className="size-6 shrink-0"
            fallbackClassName="size-6"
          />
        ) : (
          <SourceMcpIcon
            mcpServerId={o.id}
            className="size-6 shrink-0 object-contain"
          />
        ),
        slug,
        kindLabel,
        badge: mcpOff
          ? "MCP off"
          : noMcpAddress
            ? "No MCP address"
            : noMcpServer
              ? "No MCP server"
              : undefined,
        description: mcpOff
          ? "Turn on MCP for this server before adding it to a plugin."
          : noMcpAddress
            ? "Set an MCP address for this server before adding it to a plugin."
            : noMcpServer
              ? "This toolset has no MCP server yet, so it cannot be added to a plugin."
              : `${slug ?? "No slug"} · ${kindLabel}`,
        disabled: mcpOff || noMcpAddress || noMcpServer,
      };
    });

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
            toolset={toolsetById.get(
              server.toolsetId ?? server.mcpServerId ?? "",
            )}
            mcpServer={
              server.mcpServerId
                ? mcpServerById.get(server.mcpServerId)
                : undefined
            }
            isLoading={isLoadingServers}
            metadataUnavailable={!canReadServers || !!toolsetsQuery.error}
            onRemove={canPublish ? () => handleRemoveServer(server) : undefined}
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
          audienceByUrn,
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
        audienceByUrn={audienceByUrn}
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
                  {canPublish && (
                    <MarketplaceSyncButton
                      publishStatus={publishStatus}
                      isPending={publishMutation.isPending}
                      onSync={() => handlePublish([])}
                    />
                  )}
                  <PluginInstallControl
                    plugin={{
                      name: plugin.name,
                      slug: plugin.slug,
                      description: plugin.description,
                      agentPluginsV1Compatible: plugin.agentPluginsV1Compatible,
                    }}
                    publishStatus={canAdmin ? publishStatus : undefined}
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
                  link={pluginSectionHref(
                    routes,
                    pluginId!,
                    PLUGIN_SKILLS_SECTION_ID,
                  )}
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
                      link={pluginSectionHref(
                        routes,
                        pluginId!,
                        PLUGIN_ASSIGNMENTS_SECTION_ID,
                      )}
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

          {section === PLUGIN_OVERVIEW_SECTION_ID && (
            <SettingsSection>
              <div className="flex flex-wrap items-start justify-between gap-4">
                <SettingsSection.Header>
                  <div className="flex items-center gap-2">
                    <SettingsSection.Title>MCP servers</SettingsSection.Title>
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
                  {canPublish && canReadServers && (
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
                  )}
                </div>
              </div>
              {servers.length > 0 && (
                <Page.Toolbar>
                  <Page.Toolbar.Search
                    value={serverSearch}
                    onChange={setServerSearch}
                    placeholder="Search servers"
                  />
                </Page.Toolbar>
              )}
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
                onMutated={(message) => offerPublish(message)}
              />
            </RequireScope>
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

          {section === PLUGIN_SETTINGS_SECTION_ID && canPublish && (
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
                      {autoSyncOff
                        ? "Edits wait until someone clicks Sync changes."
                        : "Edits sync to your marketplace automatically."}
                    </SettingsSection.FooterHint>
                    <SettingsSection.FooterActions>
                      <Button size="sm" onClick={() => setIsEditOpen(true)}>
                        Edit details
                      </Button>
                    </SettingsSection.FooterActions>
                  </SettingsSection.Footer>
                </SettingsSection.Panel>
              </SettingsSection>

              <SettingsSection>
                <SettingsSection.Header>
                  <SettingsSection.Title>
                    Marketplace sync
                  </SettingsSection.Title>
                </SettingsSection.Header>
                <SettingsSection.Panel>
                  <SettingsSection.Body className="space-y-3">
                    <div className="flex items-start justify-between gap-4">
                      <div className="space-y-1">
                        <Label id="plugin-auto-sync-label">
                          Sync changes automatically
                        </Label>
                        <Text small muted className="max-w-xl">
                          {autoSyncOff
                            ? "Changes to this plugin wait until someone clicks Sync changes. Everyone keeps the version last published."
                            : "Changes to this plugin publish to your marketplace on their own."}
                        </Text>
                      </div>
                      <Switch
                        aria-labelledby="plugin-auto-sync-label"
                        checked={!autoSyncOff}
                        onCheckedChange={handleAutoSyncChange}
                        disabled={autoSyncMutation.isPending}
                      />
                    </div>
                    {autoSyncError && (
                      <Alert variant="warning" title={null}>
                        <div className="flex flex-wrap items-center justify-between gap-3">
                          <span>{autoSyncError}</span>
                          {publishStatus?.connected && (
                            <Button
                              size="sm"
                              variant="secondary"
                              onClick={() => handlePublish([])}
                              disabled={publishMutation.isPending}
                            >
                              Sync changes
                            </Button>
                          )}
                        </div>
                      </Alert>
                    )}
                  </SettingsSection.Body>
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

        {canPublish && (
          <>
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
                    readOnly={autoSyncOff}
                    hint={
                      autoSyncOff
                        ? "Turn on auto sync to change the slug."
                        : undefined
                    }
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
                    Are you sure you want to delete &quot;{plugin.name}&quot;?
                    This will remove it from all assigned users on the next
                    publish.
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

            <ServerPickerSheet
              key={pluginId}
              open={isAddServerOpen}
              onOpenChange={setIsAddServerOpen}
              title="Add servers to plugin"
              description="Everyone who installs this plugin gets the servers you add."
              options={pickerOptions}
              isLoading={isLoadingServers}
              loadFailed={
                !!toolsetsQuery.error ||
                !!serversQuery.error ||
                !!endpointsQuery.error
              }
              onRetryLoad={() => {
                // Only retry what failed; a refetch ignores `enabled`, so
                // never touch a query the user can't read.
                for (const query of [
                  toolsetsQuery,
                  serversQuery,
                  endpointsQuery,
                ]) {
                  if (query.error) void query.refetch();
                }
              }}
              emptyMessage={
                serverOptions.length === 0
                  ? "No MCP servers available. Create an MCP server in this project first."
                  : "All available MCP servers have already been added to this plugin."
              }
              canWrite={canPublish}
              permissionMessage="Only people who can edit this plugin can add servers."
              onSubmit={handleAddServers}
              announceAdded={(count) => {
                const message = `${count} ${count === 1 ? "server" : "servers"} added to plugin`;
                if (canPublish && publishStatus?.configured) {
                  offerPublish(message);
                } else {
                  toast.success(message);
                }
              }}
            />
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
              onPublish={handleAddCollaborators}
              isPending={publishMutation.isPending}
            />
            {showAssignments && (
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
            )}
          </>
        )}
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
  metadataUnavailable,
  onRemove,
  lastPublishedAt,
}: {
  server: PluginServer;
  toolset: ToolsetEntry | undefined;
  mcpServer: McpServer | undefined;
  isLoading: boolean;
  metadataUnavailable: boolean;
  onRemove?: () => void;
  /** Undefined when the marketplace has never been published. */
  lastPublishedAt: Date | undefined;
}) {
  const routes = useRoutes();

  // Hosted rows resolve to their toolset; other MCP server rows are remote.
  const isRemote =
    !!server.mcpServerId &&
    !toolset &&
    (!!mcpServer || (!isLoading && !metadataUnavailable));
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
      if (mcpServer)
        routes.mcp.details.overview.goTo(mcpServerRouteParam(mcpServer));
    } else if (toolset) {
      routes.toolset.goTo(toolset.slug);
    }
  };

  return (
    <McpEntityCard
      className={cn(isClickable && "cursor-pointer")}
      onClick={isClickable ? handleClick : undefined}
      icon={
        toolset ? (
          <MCPToolsetIcon toolset={toolset} />
        ) : (
          <SourceMcpIcon
            mcpServerId={server.mcpServerId}
            className="h-8 w-8 object-contain"
          />
        )
      }
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
            // Remote/unproxied MCP servers have no Speakeasy-side tool
            // catalog, so the tool-collection badge is omitted.
            <Badge variant="neutral" className="text-xs">
              {mcpServer?.builtinBackend
                ? "Built-in"
                : mcpServer?.unproxiedMcpServerId
                  ? "Unproxied MCP · Not proxied"
                  : "Remote MCP"}
            </Badge>
          ) : server.metaMcpServerId ? (
            <Badge variant="neutral" className="text-xs">
              Gateway
            </Badge>
          ) : metadataUnavailable ? (
            <Badge variant="neutral" className="text-xs">
              Server metadata unavailable
            </Badge>
          ) : toolset ? (
            <ToolCollectionBadge toolNames={toolset.tools.map((t) => t.name)} />
          ) : isLoading ? (
            <Skeleton className="h-5 w-16" />
          ) : null}
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
        {onRemove && (
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
        )}
      </div>
    </McpEntityCard>
  );
}
