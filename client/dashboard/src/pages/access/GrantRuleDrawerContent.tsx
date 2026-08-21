import { Checkbox } from "@/components/ui/Checkbox";
import { RequireScope } from "@/components/require-scope";
import {
  ToolSelectionPanel,
  type ToolAnnotation,
  type ToolSelectionChange,
  type ToolSelectionMode,
  type ToolSelectionServer,
  type ToolSelectionTool,
  type ToolSelectionToolRef,
} from "@/components/tool-selection/ToolSelectionPanel";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { cn, getServerURL } from "@/lib/utils";
import { mcpServerRouteParam } from "@/lib/sources";
import { useToolMetadata } from "@/hooks/useToolMetadata";
import { useOrgRoutes, useRoutes } from "@/routes";
import { useListCollections } from "@gram/client/react-query/listCollections.js";
import { useListMcpServersForOrg } from "@gram/client/react-query/listMcpServersForOrg.js";
import { useListToolsetsForOrg } from "@gram/client/react-query/listToolsetsForOrg.js";
import { ArrowUpRight, Check, Info, Plus, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router";
import { useQueries } from "@tanstack/react-query";
import type { Selector } from "@gram/client/models/components/selector.js";

import {
  selectorsAfterToolBatch,
  selectorsAfterToolToggle,
} from "./toolSelectionTransition";
import type { ActivePanel, AnnotationHint, ResourceType } from "./types";
import {
  ANNOTATION_TO_DISPOSITION,
  DISPOSITION_TO_ANNOTATION,
  isProjectSelectableResourceType,
} from "./types";
import { computePanelState, type CollectionGroup } from "./computePanelState";
import {
  mergeMcpServersIntoGroups,
  type Server,
  type ServerGroup,
  type ServerTool,
} from "./serverMerge";
import { toolMetadataToServerTools } from "./remoteToolMetadata";

interface GrantRuleDrawerContentProps {
  /** The resource type determines which resource list to show */
  resourceType: ResourceType;
  /** The scope slug this picker is for (e.g. "mcp:connect") */
  scope?: string;
  /** null = unrestricted; Selector[] = constrained */
  selectors: Selector[] | null;
  onChangeSelectors: (selectors: Selector[] | null) => void;
  /** Selected annotation hints for auto-group matching */
  annotations?: AnnotationHint[];
  onChangeAnnotations?: (annotations: AnnotationHint[]) => void;
  /** Whether this picker is editing an exception rule (hides "All" option, affects descriptions). */
  isDeny?: boolean;
  /** Restrict which scope-level panels are visible (e.g. ["projects"] for exception rules).
   *  When set, auto-switches to the first allowed panel if current panel isn't in the list. */
  allowedPanels?: ActivePanel[];
  /** When editing an exception rule, pass the allow rule's selectors here.
   *  The picker will filter projects/servers/tools to only those covered by the allow. */
  allowSelectors?: Selector[] | null;
}

function useMCPServers(enabled: boolean) {
  const organization = useOrganization();
  const { data } = useListToolsetsForOrg(undefined, undefined, { enabled });
  const { data: mcpServersData } = useListMcpServersForOrg(
    undefined,
    undefined,
    { enabled },
  );

  return useMemo((): ServerGroup[] => {
    const projectInfo = new Map(
      organization.projects.map((p) => [p.id, { name: p.name, slug: p.slug }]),
    );
    const baseUrl = getServerURL();
    const groups = new Map<string, ServerGroup>();
    for (const t of data?.toolsets ?? []) {
      const project = projectInfo.get(t.projectId);
      const projectName = project?.name ?? "Unknown";
      let group = groups.get(t.projectId);
      if (!group) {
        group = { projectId: t.projectId, projectName, servers: [] };
        groups.set(t.projectId, group);
      }
      const fullUrl = t.mcpSlug
        ? `${baseUrl}/mcp/${t.mcpSlug}`
        : `${baseUrl}/mcp/${project?.slug ?? ""}/${t.slug}/${t.defaultEnvironmentSlug ?? ""}`;
      const mcpUrl = fullUrl.replace(/^https?:\/\//, "");
      // External MCP "proxy" entries (name suffix ":proxy") represent servers
      // whose tools/list requires user auth, so we can't enumerate them at
      // deploy time. They still resolve at call-time via mcp:connect, so the
      // grant model supports them; we just don't surface the proxy entry as a
      // selectable tool in the picker.
      const tools = t.tools
        .filter(
          (tool) =>
            !(tool.type === "externalmcp" && tool.name.endsWith(":proxy")),
        )
        .map((tool) => ({
          id: tool.id,
          name: tool.name,
          type: tool.type,
          httpMethod: tool.httpMethod,
          annotations: tool.annotations,
        }));
      const isExternalMcpProxy = t.tools.some(
        (tool) => tool.type === "externalmcp" && tool.name.endsWith(":proxy"),
      );
      // Skip servers with nothing grantable: zero visible tools and not a
      // proxy server (proxy servers stay listed so users can grant at the
      // server level).
      if (tools.length === 0 && !isExternalMcpProxy) continue;
      group.servers.push({
        id: t.id,
        name: t.name,
        slug: mcpUrl,
        mcpSlug: t.mcpSlug ?? undefined,
        tools,
        dynamicTools: false,
        remoteBacked: false,
      });
    }
    // Fold in mcp_servers rows (remote/tunneled and toolset-backed servers
    // the toolset list doesn't cover). See serverMerge.ts for the grant id
    // invariant this maintains.
    return mergeMcpServersIntoGroups(
      [...groups.values()],
      mcpServersData?.mcpServers ?? [],
      new Map(organization.projects.map((p) => [p.id, p.name])),
    );
  }, [data, mcpServersData, organization.projects]);
}

export function GrantRuleDrawerContent({
  resourceType,
  scope,
  selectors,
  onChangeSelectors,
  annotations,
  onChangeAnnotations,
  isDeny: isDenyProp,
  allowedPanels,
  allowSelectors,
}: GrantRuleDrawerContentProps): JSX.Element {
  const organization = useOrganization();
  const mcpServers = useMCPServers(resourceType === "mcp");
  // Project slug per id, so the tool picker can name each remote server's
  // project when fetching its (project-scoped) stored tool metadata.
  const projectSlugById = useMemo(
    () => new Map(organization.projects.map((p) => [p.id, p.slug])),
    [organization.projects],
  );
  // Override for when user clicks a mode but selectors are still empty
  const [panelOverride, setPanelOverride] = useState<ActivePanel | null>(null);
  const [resourceSearch, setResourceSearch] = useState("");

  const resourceListRef = useRef<HTMLDivElement>(null);
  const handleResourceWheel = useCallback((e: React.WheelEvent) => {
    if (resourceListRef.current) {
      resourceListRef.current.scrollTop += e.deltaY;
    }
  }, []);

  const isMcpConnect = scope === "mcp:connect";
  const projectSelectable = isProjectSelectableResourceType(resourceType);
  const collectionGroups = useCollectionGroups(mcpServers, isMcpConnect);

  const panelState = computePanelState(
    selectors,
    collectionGroups,
    resourceType,
  );
  // Use override only when selectors are empty (user just switched mode)
  const activePanel =
    selectors !== null && selectors.length === 0 && panelOverride
      ? panelOverride
      : panelState.activePanel;

  // panelOverride persists until the user explicitly switches panels via
  // switchPanel(). The derivation above already ignores the override when
  // selectors have content, so clearing it eagerly only causes the UI to
  // jump back to "servers" when the user deselects all items.

  // Auto-switch to first allowed panel when current panel isn't permitted.
  // Fires on mount when allowedPanels constrains the picker (e.g. exception rules).
  useEffect(() => {
    if (!allowedPanels || allowedPanels.length === 0) return;
    if (allowedPanels.includes(activePanel)) return;
    switchPanel(allowedPanels[0]!);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [allowedPanels?.join(",")]);

  // Derive allowed project/server IDs from the allow rule's selectors.
  // When an exception picker is open, only show resources the allow rule covers.
  const allowFilter = useMemo(() => {
    // null or undefined = no filtering (allow covers everything)
    if (allowSelectors === null || allowSelectors === undefined) return null;
    const projectIds = new Set<string>();
    const serverIds = new Set<string>();
    for (const s of allowSelectors) {
      if (s.projectId) projectIds.add(s.projectId);
      if (s.resourceId && s.resourceId !== "*") {
        if (projectSelectable) projectIds.add(s.resourceId);
        else serverIds.add(s.resourceId);
      }
    }
    return {
      projectIds: projectIds.size > 0 ? projectIds : null,
      serverIds: serverIds.size > 0 ? serverIds : null,
    };
  }, [allowSelectors, projectSelectable]);

  const projectList = useMemo(() => {
    const seen = new Set<string>();
    const projects: { id: string; name: string }[] = [];
    // Include projects from org context
    for (const p of organization.projects) {
      if (!seen.has(p.id)) {
        seen.add(p.id);
        projects.push({ id: p.id, name: p.name });
      }
    }
    // For MCP scopes, also include projects discovered via server groups
    // (ensures project list matches what's visible in the server picker)
    for (const group of mcpServers) {
      if (!seen.has(group.projectId)) {
        seen.add(group.projectId);
        projects.push({ id: group.projectId, name: group.projectName });
      }
    }
    // Filter to only projects covered by the allow rule
    if (allowFilter?.projectIds) {
      return projects.filter((p) => allowFilter.projectIds!.has(p.id));
    }
    // If allow uses specific server IDs, derive their projects from mcpServers
    if (allowFilter?.serverIds) {
      const allowedProjectIds = new Set<string>();
      for (const group of mcpServers) {
        if (group.servers.some((s) => allowFilter.serverIds!.has(s.id))) {
          allowedProjectIds.add(group.projectId);
        }
      }
      return projects.filter((p) => allowedProjectIds.has(p.id));
    }
    return projects;
  }, [organization.projects, mcpServers, allowFilter]);

  const resourceKind = projectSelectable ? resourceType : "mcp";

  const filteredProjectList = useMemo(
    () =>
      resourceSearch
        ? projectList.filter((p) =>
            p.name.toLowerCase().includes(resourceSearch.toLowerCase()),
          )
        : projectList,
    [projectList, resourceSearch],
  );

  // Pre-filter mcpServers by allow scope, then apply search
  const scopedMcpServers = useMemo(() => {
    if (!allowFilter) return mcpServers;
    return mcpServers
      .map((group) => {
        // If allow specifies project IDs, only show groups in those projects
        if (
          allowFilter.projectIds &&
          !allowFilter.projectIds.has(group.projectId)
        )
          return { ...group, servers: [] };
        // If allow specifies server IDs, only show those servers
        if (allowFilter.serverIds) {
          return {
            ...group,
            servers: group.servers.filter((s) =>
              allowFilter.serverIds!.has(s.id),
            ),
          };
        }
        return group;
      })
      .filter((g) => g.servers.length > 0);
  }, [mcpServers, allowFilter]);

  const filteredMcpServers = useMemo(() => {
    if (!resourceSearch) return scopedMcpServers;
    const q = resourceSearch.toLowerCase();
    return scopedMcpServers
      .map((group) => ({
        ...group,
        servers: group.servers.filter(
          (s) =>
            s.name.toLowerCase().includes(q) ||
            group.projectName.toLowerCase().includes(q),
        ),
      }))
      .filter((g) => g.servers.length > 0);
  }, [scopedMcpServers, resourceSearch]);

  // The "Specific tools" picker shows servers with enumerable deploy-time tools
  // plus remote/tunneled (dynamic-tools) servers. Remote-backed ones resolve
  // their tools from the stored metadata table on expand; tunneled ones stay a
  // non-selectable row. Proxy servers (no tools/list at deploy time) appear in
  // the "Specific servers" picker for server-level grants but must not render a
  // zero-tools row here.
  const toolPanelMcpServers = useMemo(
    () =>
      scopedMcpServers
        .map((g) => ({
          ...g,
          servers: g.servers.filter(
            (s) => s.tools.length > 0 || s.dynamicTools,
          ),
        }))
        .filter((g) => g.servers.length > 0),
    [scopedMcpServers],
  );

  // Fixed-scope permissions have no resource picker — their granularity is
  // baked into the scope definition. Org scopes are always org-wide;
  // environment scopes apply to every environment in the project; chat:read is
  // granted org-wide (members are scoped to their own sessions automatically on
  // the server, so the role editor only offers the unrestricted "all sessions"
  // grant that admins receive).
  if (
    resourceType === "org" ||
    resourceType === "environment" ||
    resourceType === "chat"
  ) {
    return (
      <span className="border-input text-muted-foreground inline-flex h-7 items-center border bg-transparent px-2 py-1 text-xs">
        {resourceType === "environment"
          ? "All in project"
          : resourceType === "chat"
            ? "All sessions"
            : "All"}
      </span>
    );
  }

  // For MCP scopes, `id` is `Server.id`, which serverMerge.ts guarantees is
  // the id enforcement checks (toolset id for toolset-backed servers, the
  // mcp_servers id for remote/tunneled) — see the GRANT ID INVARIANT there.
  const toggleResource = (id: string) => {
    if (selectors === null) return;
    const has = selectors.some(
      (s) => s.resourceKind === resourceKind && s.resourceId === id,
    );
    if (has) {
      onChangeSelectors(
        selectors.filter(
          (s) => !(s.resourceKind === resourceKind && s.resourceId === id),
        ),
      );
    } else {
      onChangeSelectors([...selectors, { resourceKind, resourceId: id }]);
    }
  };

  const isResourceSelected = (id: string) =>
    selectors?.some((s) => s.resourceId === id) ?? false;

  const toggleProject = (projectId: string) => {
    if (selectors === null) return;
    const has = selectors.some((s) => s.projectId === projectId);
    if (has) {
      onChangeSelectors(selectors.filter((s) => s.projectId !== projectId));
    } else {
      onChangeSelectors([
        ...selectors,
        { resourceKind: "mcp", resourceId: "*", projectId },
      ]);
    }
  };

  const isProjectSelected = (projectId: string) =>
    selectors?.some((s) => s.projectId === projectId) ?? false;

  const switchPanel = (panel: ActivePanel) => {
    setPanelOverride(panel);
    setResourceSearch("");
    if (panel === "all") {
      onChangeSelectors(null);
    } else {
      onChangeSelectors([]);
    }
    if (panel !== "tools") {
      onChangeAnnotations?.([]);
    }
  };

  const isPanelAllowed = (panel: ActivePanel) =>
    !allowedPanels || allowedPanels.includes(panel);

  const renderScopeOptions = () => (
    <div className="shrink-0 pb-1.5">
      {!isDenyProp && isPanelAllowed("all") && (
        <ScopeOption
          label={projectSelectable ? "All projects" : "All servers"}
          description={
            projectSelectable
              ? "Give access to every project in your org"
              : "Give access to all servers in every project in your org"
          }
          selected={activePanel === "all"}
          onClick={() => switchPanel("all")}
        />
      )}
      {resourceType === "mcp" && isPanelAllowed("projects") && (
        <ScopeOption
          label="Specific projects"
          description="Give access to servers within specific projects in your org"
          selected={activePanel === "projects"}
          onClick={() => switchPanel("projects")}
        />
      )}
      {isPanelAllowed("servers") && (
        <ScopeOption
          label={projectSelectable ? "Specific projects" : "Specific servers"}
          description={
            projectSelectable
              ? "Give access to specific projects in your org"
              : "Give access to specific servers across your org"
          }
          selected={activePanel === "servers"}
          onClick={() => switchPanel("servers")}
        />
      )}
      {isMcpConnect && isPanelAllowed("tools") && (
        <ScopeOption
          label="Specific tools"
          description="Give fine-grained access to individual tools"
          selected={activePanel === "tools"}
          onClick={() => switchPanel("tools")}
        />
      )}
      {isMcpConnect && isPanelAllowed("collection") && (
        <ScopeOption
          label="Specific collections"
          description="Give access to a curated set of tools"
          selected={activePanel === "collection"}
          onClick={() => switchPanel("collection")}
        />
      )}
    </div>
  );

  const resourceList = activePanel === "servers" && (
    <>
      <div className="bg-border mt-1 h-px" />
      <div className="flex items-center gap-2 px-3 pt-2 pb-1">
        <input
          type="text"
          placeholder={
            projectSelectable ? "Search projects…" : "Search servers…"
          }
          value={resourceSearch}
          onChange={(e) => setResourceSearch(e.target.value)}
          className="placeholder:text-muted-foreground flex-1 bg-transparent text-sm outline-none"
        />
        {resourceSearch && (
          <button
            type="button"
            onClick={() => setResourceSearch("")}
            className="text-muted-foreground hover:text-foreground shrink-0"
          >
            <X className="h-3 w-3" />
          </button>
        )}
      </div>
      <div className="bg-border my-1 h-px" />
      <div
        ref={resourceListRef}
        onWheel={handleResourceWheel}
        className="h-[250px] overflow-y-auto"
      >
        {projectSelectable ? (
          filteredProjectList.length === 0 ? (
            <div className="text-muted-foreground px-3 py-3 text-sm">
              {projectList.length === 0
                ? "No projects found"
                : "No matching projects"}
            </div>
          ) : (
            filteredProjectList.map((resource) => (
              <ResourceCheckbox
                key={resource.id}
                id={resource.id}
                name={resource.name}
                checked={isResourceSelected(resource.id)}
                onToggle={toggleResource}
              />
            ))
          )
        ) : filteredMcpServers.length === 0 ? (
          <div className="text-muted-foreground px-3 py-3 text-sm">
            {scopedMcpServers.length === 0
              ? "No servers found"
              : "No matching servers"}
          </div>
        ) : (
          filteredMcpServers.map((group) => (
            <div key={group.projectId}>
              {group.servers.map((server) => (
                <ResourceCheckbox
                  key={server.id}
                  id={server.id}
                  name={
                    <>
                      <span className="text-muted-foreground/60">
                        {group.projectName.toLowerCase()}/
                      </span>
                      {server.name}
                    </>
                  }
                  checked={isResourceSelected(server.id)}
                  onToggle={toggleResource}
                />
              ))}
            </div>
          ))
        )}
      </div>
    </>
  );

  const projectPickerList = activePanel === "projects" && (
    <>
      <div className="bg-border mt-1 h-px" />
      <div className="flex items-center gap-2 px-3 pt-2 pb-1">
        <input
          type="text"
          placeholder="Search projects…"
          value={resourceSearch}
          onChange={(e) => setResourceSearch(e.target.value)}
          className="placeholder:text-muted-foreground flex-1 bg-transparent text-sm outline-none"
        />
        {resourceSearch && (
          <button
            type="button"
            onClick={() => setResourceSearch("")}
            className="text-muted-foreground hover:text-foreground shrink-0"
          >
            <X className="h-3 w-3" />
          </button>
        )}
      </div>
      <div className="bg-border my-1 h-px" />
      <div
        ref={resourceListRef}
        onWheel={handleResourceWheel}
        className="h-[250px] overflow-y-auto"
      >
        {filteredProjectList.length === 0 ? (
          <div className="text-muted-foreground px-3 py-3 text-sm">
            {projectList.length === 0
              ? "No projects found"
              : "No matching projects"}
          </div>
        ) : (
          filteredProjectList.map((project) => (
            <ResourceCheckbox
              key={project.id}
              id={project.id}
              name={project.name}
              checked={isProjectSelected(project.id)}
              onToggle={toggleProject}
            />
          ))
        )}
      </div>
    </>
  );

  const customTabs = (toolScrollClass?: string) => (
    <RoleToolSelectionPanel
      mcpServers={toolPanelMcpServers}
      projectSlugById={projectSlugById}
      selectors={selectors ?? []}
      annotations={annotations}
      onChangeAnnotations={onChangeAnnotations}
      isDeny={!!isDenyProp}
      onChangeSelectors={(sels) => onChangeSelectors(sels)}
      onToggleTool={(serverId, toolName) => {
        const sels = selectors ?? [];
        const exists = sels.some(
          (s) => s.resourceId === serverId && s.tool === toolName,
        );
        if (exists) {
          onChangeSelectors(
            sels.filter(
              (s) => !(s.resourceId === serverId && s.tool === toolName),
            ),
          );
        } else {
          onChangeSelectors([
            ...sels,
            {
              resourceKind: "mcp",
              resourceId: serverId,
              tool: toolName,
            },
          ]);
        }
      }}
      onBatchToggleTools={(serverId, toolNames, select) => {
        const sels = selectors ?? [];
        if (select) {
          const existing = new Set(
            sels
              .filter((s) => s.resourceId === serverId && s.tool)
              .map((s) => s.tool!),
          );
          const toAdd = toolNames
            .filter((name) => !existing.has(name))
            .map((name) => ({
              resourceKind: "mcp" as const,
              resourceId: serverId,
              tool: name,
            }));
          onChangeSelectors([...sels, ...toAdd]);
        } else {
          const toolSet = new Set(toolNames);
          onChangeSelectors(
            sels.filter(
              (s) =>
                !(s.resourceId === serverId && s.tool && toolSet.has(s.tool)),
            ),
          );
        }
      }}
      className={cn("min-h-[200px]", toolScrollClass)}
    />
  );

  const collectionPanel = (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <div className="border-border flex min-h-0 flex-1 flex-col overflow-y-auto border-t px-2 py-1">
        <CollectionGroupPanel
          collectionGroups={collectionGroups}
          selectors={selectors ?? []}
          onChangeSelectors={onChangeSelectors}
        />
      </div>
    </div>
  );

  return (
    <div className="flex flex-1 flex-col overflow-y-auto px-1.5 pb-1.5">
      {renderScopeOptions()}
      {resourceList}
      {projectPickerList}
      {activePanel === "tools" && (
        <div className="flex min-h-0 flex-1 flex-col">{customTabs()}</div>
      )}
      {activePanel === "collection" && collectionPanel}
    </div>
  );
}

interface RemoteToolsState {
  tools: ServerTool[];
  isLoading: boolean;
  isError: boolean;
  refetch: () => void;
}

/**
 * Fetches stored remote-MCP tool metadata for one server and reports it
 * upward. Mounted per remote-backed server so the picker issues at most one
 * metadata request per expanded server; `enabled` gates the fetch to rows the
 * admin actually opens.
 */
function RemoteToolMetadataLoader({
  serverId,
  projectSlug,
  enabled,
  onState,
}: {
  serverId: string;
  projectSlug: string | undefined;
  enabled: boolean;
  onState: (serverId: string, state: RemoteToolsState) => void;
}): null {
  const { metadataByTool, isLoading, isError, refetch } = useToolMetadata(
    serverId,
    { enabled, projectSlug },
  );
  const refetchRef = useRef(refetch);
  useEffect(() => {
    refetchRef.current = refetch;
  });
  const stableRefetch = useCallback(() => refetchRef.current(), []);
  const tools = useMemo(
    () => toolMetadataToServerTools(serverId, Object.values(metadataByTool)),
    [serverId, metadataByTool],
  );
  useEffect(() => {
    if (!enabled) return;
    onState(serverId, { tools, isLoading, isError, refetch: stableRefetch });
  }, [enabled, serverId, tools, isLoading, isError, stableRefetch, onState]);
  return null;
}

function serverToolAnnotations(tool: ServerTool): ToolAnnotation[] {
  const annotations: ToolAnnotation[] = [];
  const hints = tool.annotations;
  if (hints?.readOnlyHint) annotations.push("read_only");
  if (hints?.destructiveHint) annotations.push("destructive");
  if (hints?.idempotentHint) annotations.push("idempotent");
  if (hints?.openWorldHint) annotations.push("open_world");
  return annotations;
}

function toSelectionTool(tool: ServerTool): ToolSelectionTool {
  return { name: tool.name, annotations: serverToolAnnotations(tool) };
}

function remoteServerStatus(
  state: RemoteToolsState | undefined,
): ToolSelectionServer["status"] {
  if (!state || state.isLoading) return "loading";
  if (state.isError) return "error";
  return "ready";
}

/**
 * Dashboard adapter around the shared ToolSelectionPanel: converts server
 * groups and selectors into the panel's normalized types, lazily loads remote
 * tool metadata, and replays panel toggles as the legacy selector/annotation
 * updates so emitted grants are unchanged.
 */
function RoleToolSelectionPanel({
  mcpServers,
  projectSlugById,
  selectors,
  onToggleTool,
  onBatchToggleTools,
  annotations,
  onChangeAnnotations,
  onChangeSelectors,
  isDeny,
  className,
}: {
  mcpServers: ServerGroup[];
  projectSlugById: Map<string, string>;
  selectors: Selector[];
  onToggleTool: (serverId: string, toolName: string) => void;
  onBatchToggleTools?: (
    serverId: string,
    toolNames: string[],
    select: boolean,
  ) => void;
  annotations?: AnnotationHint[];
  onChangeAnnotations?: (annotations: AnnotationHint[]) => void;
  onChangeSelectors?: (selectors: Selector[]) => void;
  isDeny?: boolean;
  className?: string;
}): JSX.Element {
  const routes = useRoutes();

  const allServers = useMemo(
    () =>
      mcpServers
        .flatMap((g) =>
          g.servers.map((s) => ({
            ...s,
            projectName: g.projectName,
            projectSlug: projectSlugById.get(g.projectId),
          })),
        )
        .sort((a, b) =>
          `${a.projectName}/${a.name}`.localeCompare(
            `${b.projectName}/${b.name}`,
          ),
        ),
    [mcpServers, projectSlugById],
  );

  const [expandedIds, setExpandedIds] = useState<ReadonlySet<string>>(
    new Set(),
  );
  const [remoteTools, setRemoteTools] = useState<
    Record<string, RemoteToolsState>
  >({});

  const handleExpandedServersChange = useCallback((serverIds: string[]) => {
    setExpandedIds(new Set(serverIds));
  }, []);

  const handleRemoteToolsState = useCallback(
    (serverId: string, state: RemoteToolsState) => {
      setRemoteTools((prev) => ({ ...prev, [serverId]: state }));
    },
    [],
  );

  const panelServers = useMemo(
    (): ToolSelectionServer[] =>
      allServers.map((server) => {
        const namePrefix = `${server.projectName.toLowerCase()}/`;
        const isRemoteBacked = server.dynamicTools && server.remoteBacked;
        const isTunneled = server.dynamicTools && !server.remoteBacked;
        if (isTunneled) {
          return {
            id: server.id,
            name: server.name,
            namePrefix,
            tools: [],
            status: "unavailable",
            unavailableLabel: "dynamic tools",
            unavailableTooltip:
              "Tools are dynamically resolved for this server and cannot be individually permissioned.",
          };
        }
        if (isRemoteBacked) {
          const state = remoteTools[server.id];
          const status = remoteServerStatus(state);
          return {
            id: server.id,
            name: server.name,
            namePrefix,
            tools:
              status === "ready" && state
                ? state.tools.map(toSelectionTool)
                : [],
            status,
            collapsedLabel: "Expand to load tools",
            emptyLabel: "Not synced",
            emptyContent: (
              <div className="text-muted-foreground space-y-1 px-8 py-3 text-sm">
                <p>This server&apos;s tools haven&apos;t been synced yet.</p>
                <p>
                  <Link
                    to={routes.mcp.x.inspect.href(
                      mcpServerRouteParam({ id: server.id, slug: server.slug }),
                    )}
                    className="text-primary inline-flex items-center gap-1 hover:underline"
                  >
                    Connect it on the Inspect tab
                    <ArrowUpRight className="h-3 w-3" />
                  </Link>{" "}
                  to permission its tools individually.
                </p>
              </div>
            ),
            onRetry: state?.refetch,
          };
        }
        return {
          id: server.id,
          name: server.name,
          namePrefix,
          tools: server.tools.map(toSelectionTool),
          status: "ready",
        };
      }),
    [allServers, remoteTools, routes],
  );

  // Annotation counts stay pinned to deploy-time tools: lazily loaded remote
  // metadata must not change the chip counts when a row is expanded.
  const toolCountByAnnotation = useMemo(() => {
    const counts = new Map<ToolAnnotation, number>();
    for (const server of allServers) {
      for (const tool of server.tools) {
        for (const annotation of serverToolAnnotations(tool)) {
          counts.set(annotation, (counts.get(annotation) ?? 0) + 1);
        }
      }
    }
    return counts;
  }, [allServers]);

  const selectedAnnotations = useMemo(
    () => (annotations ?? []).map((hint) => ANNOTATION_TO_DISPOSITION[hint]),
    [annotations],
  );

  const selectedTools = useMemo(
    () =>
      selectors.flatMap((s): ToolSelectionToolRef[] =>
        s.tool ? [{ serverId: s.resourceId, toolName: s.tool }] : [],
      ),
    [selectors],
  );

  const hasManualTools = selectors.some(
    (s) => s.tool && s.resourceId && s.resourceId !== "*",
  );
  const mode: ToolSelectionMode = hasManualTools ? "tools" : "annotations";

  const handleSelectionChange = (change: ToolSelectionChange) => {
    switch (change.kind) {
      case "annotation-toggle": {
        if (!onChangeAnnotations || !onChangeSelectors) return;
        const hints = change.annotations.map(
          (a) => DISPOSITION_TO_ANNOTATION[a]!,
        );
        onChangeAnnotations(hints);
        onChangeSelectors(
          hints.map((hint) => ({
            resourceKind: "mcp" as const,
            resourceId: "*",
            disposition: ANNOTATION_TO_DISPOSITION[hint],
          })),
        );
        return;
      }
      case "tool-toggle": {
        if ((annotations ?? []).length > 0) {
          // Entering manual mode replaces the authoritative selectors
          // atomically: keeping the category-wide disposition selectors
          // alongside the first tool pick would silently widen the
          // persisted grant past the panel's exclusive-mode promise.
          onChangeAnnotations?.([]);
          onChangeSelectors?.(
            selectorsAfterToolToggle(
              selectors,
              change.serverId,
              change.toolName,
            ),
          );
          return;
        }
        onToggleTool(change.serverId, change.toolName);
        return;
      }
      case "tool-batch": {
        if ((annotations ?? []).length > 0) {
          onChangeAnnotations?.([]);
          onChangeSelectors?.(
            selectorsAfterToolBatch(
              selectors,
              change.serverId,
              change.toolNames,
              change.selected,
            ),
          );
          return;
        }
        onBatchToggleTools?.(
          change.serverId,
          change.toolNames,
          change.selected,
        );
        return;
      }
    }
  };

  const remoteBackedServers = allServers.filter(
    (s) => s.dynamicTools && s.remoteBacked,
  );

  return (
    <>
      {remoteBackedServers.map((server) => (
        <RemoteToolMetadataLoader
          key={server.id}
          serverId={server.id}
          projectSlug={server.projectSlug}
          enabled={expandedIds.has(server.id)}
          onState={handleRemoteToolsState}
        />
      ))}
      <ToolSelectionPanel
        servers={panelServers}
        mode={mode}
        selectedAnnotations={selectedAnnotations}
        selectedTools={selectedTools}
        onSelectionChange={handleSelectionChange}
        annotationSelectionSupported={!!onChangeAnnotations}
        annotationsDescription="Tools can be annotated with labels that provide more context about the properties of the tool, such as if it's a destructive operation. OpenAPI sources are tagged automatically based on HTTP method. You can edit annotations on the MCP tools tab."
        toolsDescription={
          isDeny
            ? "Select specific tools to exclude. Expand a server to choose which tools this role should not access."
            : "Select specific tools to allow. Expand a server to choose which tools this role can access."
        }
        toolCountByAnnotation={toolCountByAnnotation}
        onExpandedServersChange={handleExpandedServersChange}
        className={className}
      />
    </>
  );
}

/** Fetches collection groups with resolved server/tool data. */
function useCollectionGroups(
  mcpServers: ServerGroup[],
  enabled: boolean,
): CollectionGroup[] {
  const client = useSdkClient();
  const { data: collectionsData } = useListCollections({}, undefined, {
    enabled,
  });
  const collections = useMemo(
    () => collectionsData?.collections ?? [],
    [collectionsData?.collections],
  );

  const serverQueries = useQueries({
    queries: collections.map((c) => ({
      queryKey: ["collections", "listServers", c.slug],
      queryFn: () =>
        client.collections.listServers({ collectionSlug: c.slug! }),
      enabled: enabled && !!c.slug,
    })),
  });

  const mcpSlugToServer = useMemo(() => {
    const map = new Map<string, Server>();
    for (const group of mcpServers) {
      for (const server of group.servers) {
        if (server.mcpSlug) map.set(server.mcpSlug, server);
      }
    }
    return map;
  }, [mcpServers]);

  return useMemo(() => {
    return collections
      .map((c, i) => {
        const externalServers = serverQueries[i]?.data?.servers ?? [];
        const matchedServers: Server[] = [];
        for (const es of externalServers) {
          const parts = es.registrySpecifier.split("/");
          const mcpSlug = parts[parts.length - 1]!;
          const server = mcpSlugToServer.get(mcpSlug);
          if (server) matchedServers.push(server);
        }
        return {
          id: c.id!,
          name: c.name!,
          slug: c.slug,
          servers: matchedServers,
        };
      })
      .filter((g) => g.servers.some((s) => s.tools.length > 0));
  }, [collections, serverQueries, mcpSlugToServer]);
}

function CollectionGroupPanel({
  collectionGroups,
  selectors,
  onChangeSelectors,
  onNavigate,
}: {
  collectionGroups: CollectionGroup[];
  selectors: Selector[];
  onChangeSelectors: (selectors: Selector[] | null) => void;
  onNavigate?: () => void;
}) {
  const orgRoutes = useOrgRoutes();

  const goToCreateCollection = () => {
    onNavigate?.();
    orgRoutes.collections.create.goTo();
  };

  if (collectionGroups.length === 0) {
    return (
      <div className="flex flex-col items-center px-4 py-5 text-center">
        <div className="bg-muted mb-3 flex h-8 w-8 items-center justify-center rounded-full">
          <Info className="text-muted-foreground h-4 w-4" />
        </div>
        <p className="text-muted-foreground mb-4 text-xs leading-relaxed">
          Collections group MCP servers for reuse across projects.
          <br />
          Selecting one grants access to all its tools.
        </p>
        <RequireScope
          scope="org:admin"
          level="component"
          reason="You need org admin to create a collection."
        >
          {({ disabled }) => (
            <button
              type="button"
              onClick={disabled ? undefined : goToCreateCollection}
              className="border-input text-foreground hover:bg-accent inline-flex cursor-pointer items-center gap-1.5 border px-3 py-1.5 text-xs shadow-xs transition-colors"
            >
              <Plus className="h-3 w-3" />
              Create new collection
            </button>
          )}
        </RequireScope>
      </div>
    );
  }

  return (
    <div className="py-1">
      <div className="text-muted-foreground px-2 py-2 text-xs">
        Select all tools by collection:
      </div>
      {collectionGroups.map((group) => {
        const allToolSelectors: Selector[] = group.servers.flatMap((s) =>
          s.tools.map((t) => ({
            resourceKind: "mcp" as const,
            resourceId: s.id,
            tool: t.name,
          })),
        );
        const allSelected =
          allToolSelectors.length > 0 &&
          allToolSelectors.every((ts) =>
            selectors.some(
              (s) => s.resourceId === ts.resourceId && s.tool === ts.tool,
            ),
          );

        const toggleAll = () => {
          if (allSelected) {
            onChangeSelectors(
              selectors.filter(
                (s) =>
                  !allToolSelectors.some(
                    (ts) =>
                      s.resourceId === ts.resourceId && s.tool === ts.tool,
                  ),
              ),
            );
          } else {
            const toAdd = allToolSelectors.filter(
              (ts) =>
                !selectors.some(
                  (s) => s.resourceId === ts.resourceId && s.tool === ts.tool,
                ),
            );
            onChangeSelectors([...selectors, ...toAdd]);
          }
        };

        return (
          <button
            key={group.id}
            type="button"
            onClick={toggleAll}
            className="hover:bg-accent flex w-full cursor-pointer items-center gap-3 px-3 py-2.5 text-sm"
          >
            <Checkbox
              checked={allSelected}
              className="focus-visible:border-input pointer-events-none focus-visible:ring-0"
              tabIndex={-1}
            />
            <span className="min-w-0 flex-1 truncate text-left font-medium">
              {group.name}
            </span>
            <span className="text-muted-foreground shrink-0 text-xs">
              {allToolSelectors.length} tool
              {allToolSelectors.length !== 1 ? "s" : ""}
            </span>
          </button>
        );
      })}
      <div className="border-border mx-2 mt-2 border-t pt-2">
        <RequireScope
          scope="org:admin"
          level="component"
          reason="You need org admin to create a collection."
          className="w-full"
        >
          {({ disabled }) => (
            <button
              type="button"
              onClick={disabled ? undefined : goToCreateCollection}
              className="text-muted-foreground hover:text-foreground flex w-full cursor-pointer items-center justify-center gap-1.5 px-3 py-1.5 text-xs transition-colors"
            >
              <Plus className="h-3 w-3" />
              Create new collection
            </button>
          )}
        </RequireScope>
      </div>
    </div>
  );
}

function ResourceCheckbox({
  id,
  name,
  checked,
  onToggle,
  compact,
}: {
  id: string;
  name: React.ReactNode;
  checked: boolean;
  onToggle: (id: string) => void;
  compact?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={() => onToggle(id)}
      className={cn(
        "hover:bg-accent flex w-full cursor-pointer items-center gap-2 px-3",
        compact ? "h-10 text-sm" : "py-2 text-sm",
        checked && "font-medium",
      )}
    >
      <Checkbox
        checked={checked}
        className="focus-visible:border-input pointer-events-none focus-visible:ring-0"
        tabIndex={-1}
      />
      <span className="truncate">{name}</span>
    </button>
  );
}

function ScopeOption({
  label,
  description,
  selected,
  onClick,
}: {
  label: string;
  description?: string;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "hover:bg-accent flex w-full cursor-pointer items-start gap-2 px-3 py-2 text-sm",
        selected && "font-medium",
      )}
    >
      <span className="mt-0.5 flex w-4 shrink-0 items-center justify-center">
        {selected && <Check className="h-3.5 w-3.5" />}
      </span>
      <span className="flex flex-col items-start">
        <span>{label}</span>
        {description && (
          <span className="text-muted-foreground text-xs font-normal">
            {description}
          </span>
        )}
      </span>
    </button>
  );
}
