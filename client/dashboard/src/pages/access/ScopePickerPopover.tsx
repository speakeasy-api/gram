import { Checkbox } from "@/components/ui/checkbox";
import { RequireScope } from "@/components/require-scope";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import { cn } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import { useListCollections } from "@gram/client/react-query/listCollections.js";
import { useListToolsetsForOrg } from "@gram/client/react-query/listToolsetsForOrg.js";
import {
  AlertTriangle,
  Check,
  ChevronRight,
  Info,
  Globe,
  Plus,
  Repeat,
  Shield,
  Wrench,
  X,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQueries } from "@tanstack/react-query";
import type {
  ActivePanel,
  AnnotationHint,
  ResourceType,
  Selector,
} from "./types";
import { ANNOTATION_TO_DISPOSITION } from "./types";
import { computePanelState, type CollectionGroup } from "./computePanelState";

interface ScopePickerPopoverProps {
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
  /** Whether this picker is editing a deny rule (hides "All" option, affects descriptions). */
  isDeny?: boolean;
  /** Restrict which scope-level panels are visible (e.g. ["projects"] for deny rules).
   *  When set, auto-switches to the first allowed panel if current panel isn't in the list. */
  allowedPanels?: ActivePanel[];
  /** When editing a deny rule, pass the allow rule's selectors here.
   *  The picker will filter projects/servers/tools to only those covered by the allow. */
  allowSelectors?: Selector[] | null;
}

interface ServerTool {
  id: string;
  name: string;
  type: string;
  httpMethod?: string;
  annotations?: {
    readOnlyHint?: boolean;
    destructiveHint?: boolean;
    idempotentHint?: boolean;
    openWorldHint?: boolean;
  };
}

interface Server {
  id: string;
  name: string;
  slug: string;
  mcpSlug?: string;
  tools: ServerTool[];
}

interface ServerGroup {
  projectId: string;
  projectName: string;
  servers: Server[];
}

function useMCPServers(enabled: boolean) {
  const organization = useOrganization();
  const { data } = useListToolsetsForOrg(undefined, undefined, { enabled });

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
      // Skip toolsets containing proxy external MCP tools — proxy tools are not
      // RBAC-compatible yet. Backend encodes proxy entries with a ":proxy" name
      // suffix while direct entries get the real ":<tool-name>".
      const hasProxyExternalMcp = t.tools.some(
        (tool) => tool.type === "externalmcp" && tool.name.endsWith(":proxy"),
      );
      if (hasProxyExternalMcp) continue;
      const tools = t.tools.map((tool) => ({
        id: tool.id,
        name: tool.name,
        type: tool.type,
        httpMethod: tool.httpMethod,
        annotations: tool.annotations,
      }));
      // Skip servers with no tools
      if (tools.length === 0) continue;
      group.servers.push({
        id: t.id,
        name: t.name,
        slug: mcpUrl,
        mcpSlug: t.mcpSlug ?? undefined,
        tools,
      });
    }
    return [...groups.values()].filter((g) => g.servers.length > 0);
  }, [data, organization.projects]);
}

export function ScopePickerPopover({
  resourceType,
  scope,
  selectors,
  onChangeSelectors,
  annotations,
  onChangeAnnotations,
  isDeny: isDenyProp,
  allowedPanels,
  allowSelectors,
}: ScopePickerPopoverProps) {
  const organization = useOrganization();
  const mcpServers = useMCPServers(resourceType === "mcp");
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
  // Fires on mount when allowedPanels constrains the picker (e.g. deny rules).
  useEffect(() => {
    if (!allowedPanels || allowedPanels.length === 0) return;
    if (allowedPanels.includes(activePanel)) return;
    switchPanel(allowedPanels[0]);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [allowedPanels?.join(",")]);

  // Derive allowed project/server IDs from the allow rule's selectors.
  // When deny picker is open, only show resources the allow rule covers.
  const allowFilter = useMemo(() => {
    // null or undefined = no filtering (allow covers everything)
    if (allowSelectors === null || allowSelectors === undefined) return null;
    const projectIds = new Set<string>();
    const serverIds = new Set<string>();
    for (const s of allowSelectors) {
      if (s.projectId) projectIds.add(s.projectId);
      if (s.resourceId && s.resourceId !== "*") serverIds.add(s.resourceId);
    }
    return {
      projectIds: projectIds.size > 0 ? projectIds : null,
      serverIds: serverIds.size > 0 ? serverIds : null,
    };
  }, [allowSelectors]);

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

  const resourceKind = resourceType === "project" ? "project" : "mcp";

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

  // Fixed-scope permissions have no resource picker — their granularity is
  // baked into the scope definition. Org scopes are always org-wide;
  // environment scopes apply to every environment in the project.
  if (resourceType === "org" || resourceType === "environment") {
    return (
      <span className="border-input text-muted-foreground inline-flex h-7 items-center rounded-md border bg-transparent px-2 py-1 text-xs">
        {resourceType === "environment" ? "All in project" : "All"}
      </span>
    );
  }

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
          label={resourceType === "project" ? "All projects" : "All servers"}
          description={
            resourceType === "project"
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
          label={
            resourceType === "project"
              ? "Specific projects"
              : "Specific servers"
          }
          description={
            resourceType === "project"
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
            resourceType === "project" ? "Search projects…" : "Search servers…"
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
        {resourceType === "project" ? (
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
    <ToolSelectionPanel
      mcpServers={scopedMcpServers}
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

function ToolSelectionPanel({
  mcpServers,
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
}) {
  const allServers = useMemo(
    () =>
      mcpServers
        .flatMap((g) =>
          g.servers.map((s) => ({ ...s, projectName: g.projectName })),
        )
        .sort((a, b) =>
          `${a.projectName}/${a.name}`.localeCompare(
            `${b.projectName}/${b.name}`,
          ),
        ),
    [mcpServers],
  );

  const [search, setSearch] = useState("");
  // Auto-expand if only one server; otherwise all collapsed
  const [expandedServers, setExpandedServers] = useState<Set<string>>(
    () => new Set(allServers.length === 1 ? [allServers[0].id] : []),
  );

  const toggleExpanded = (serverId: string) => {
    setExpandedServers((prev) =>
      prev.has(serverId) ? new Set() : new Set([serverId]),
    );
  };

  const q = search.toLowerCase();

  // Check if any annotation filters are active
  const hasAnnotationFilter = (annotations ?? []).length > 0;

  // Compute matching tools per annotation
  const allTools = useMemo(
    () =>
      allServers.flatMap((s) => s.tools.map((t) => ({ ...t, serverId: s.id }))),
    [allServers],
  );

  const toolCountByAnnotation = useMemo(() => {
    const counts = new Map<AnnotationHint, number>();
    for (const hint of [
      "readOnlyHint",
      "destructiveHint",
      "idempotentHint",
      "openWorldHint",
    ] as AnnotationHint[]) {
      counts.set(hint, allTools.filter((t) => t.annotations?.[hint]).length);
    }
    return counts;
  }, [allTools]);

  const toggleAnnotation = (key: AnnotationHint) => {
    if (!onChangeAnnotations || !onChangeSelectors) return;
    const current = annotations ?? [];
    const has = current.includes(key);
    const next = has ? current.filter((a) => a !== key) : [...current, key];
    onChangeAnnotations(next);
    // Sync selectors to annotation-based selectors
    const newSelectors = next.map((hint) => ({
      resourceKind: "mcp" as const,
      resourceId: "*",
      disposition: ANNOTATION_TO_DISPOSITION[hint],
    }));
    onChangeSelectors(newSelectors);
    // Collapse all server accordions when switching to annotation mode
    if (next.length > 0) {
      setExpandedServers(new Set());
      setSearch("");
    }
  };

  // Filter servers and tools by search query
  const filteredServers = useMemo(() => {
    if (!q) return allServers;
    return allServers
      .map((server) => ({
        ...server,
        tools: server.tools.filter((t) => t.name.toLowerCase().includes(q)),
      }))
      .filter(
        (s) =>
          s.tools.length > 0 ||
          s.name.toLowerCase().includes(q) ||
          s.projectName.toLowerCase().includes(q),
      );
  }, [allServers, q]);

  // Auto-expand servers when searching
  useEffect(() => {
    if (q) {
      setExpandedServers(new Set(filteredServers.map((s) => s.id)));
    }
  }, [q, filteredServers]);

  const scrollRef = useRef<HTMLDivElement>(null);
  const handleWheel = useCallback((e: React.WheelEvent) => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop += e.deltaY;
    }
  }, []);

  // Determine active mode: annotation-based or manual tool selection
  const hasManualTools = selectors.some(
    (s) => s.tool && s.resourceId && s.resourceId !== "*",
  );

  return (
    <div className={cn("flex min-h-0 flex-1 flex-col", className)}>
      <div
        ref={scrollRef}
        onWheel={handleWheel}
        className="min-h-0 flex-1 overflow-y-auto"
      >
        {/* ── Section 1: By annotation ── */}
        {onChangeAnnotations && (
          <div className={cn(hasManualTools && "opacity-60")}>
            <div className="px-3 pt-5 pb-3">
              <div className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
                By annotation
              </div>
              <div className="text-muted-foreground/70 mt-1.5 text-xs leading-snug">
                Tools can be annotated with labels that provide more context
                about the properties of the tool, such as if it's a destructive
                operation. OpenAPI sources are tagged automatically based on
                HTTP method. You can edit annotations on the MCP tools tab.
              </div>
            </div>
            <div className="flex flex-wrap gap-2 px-3 pb-4">
              {ANNOTATION_OPTIONS.map((opt) => {
                const isActive = (annotations ?? []).includes(opt.key);
                const count = toolCountByAnnotation.get(opt.key) ?? 0;
                if (count === 0) return null;
                const Icon = opt.icon;
                return (
                  <button
                    key={opt.key}
                    type="button"
                    onClick={() => toggleAnnotation(opt.key)}
                    className={cn(
                      "border-input hover:bg-accent inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs transition-colors",
                      isActive &&
                        "border-primary bg-primary/5 text-primary font-medium",
                    )}
                  >
                    <Icon className="h-3 w-3" />
                    {opt.label}
                    <span className="text-muted-foreground ml-0.5">
                      {count}
                    </span>
                  </button>
                );
              })}
            </div>
          </div>
        )}

        {/* ── OR divider ── */}
        {onChangeAnnotations && (
          <div className="flex items-center gap-3 px-3 py-3">
            <div className="bg-border h-px flex-1" />
            <span className="text-muted-foreground text-[11px] font-medium uppercase">
              or
            </span>
            <div className="bg-border h-px flex-1" />
          </div>
        )}

        {/* ── Section 2: By server (manual) ── */}
        <div className={cn(hasAnnotationFilter && "opacity-60")}>
          <div className="px-3 pt-1 pb-3">
            <div className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
              By server
            </div>
            <div className="text-muted-foreground/70 mt-1.5 text-xs leading-snug">
              {isDeny
                ? "Select specific tools to deny. Expand a server to choose which tools this role should not access."
                : "Select specific tools to allow. Expand a server to choose which tools this role can access."}
            </div>
          </div>

          {/* Search */}
          <div className="flex items-center gap-2 px-3 pb-3">
            <div className="border-input flex h-8 flex-1 items-center gap-2 rounded-md border px-2">
              <Wrench className="text-muted-foreground h-3 w-3 shrink-0" />
              <input
                type="text"
                placeholder="Search tools and servers…"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="placeholder:text-muted-foreground flex-1 bg-transparent text-xs outline-none"
              />
              {search && (
                <button
                  type="button"
                  onClick={() => setSearch("")}
                  className="text-muted-foreground hover:text-foreground shrink-0"
                >
                  <X className="h-3 w-3" />
                </button>
              )}
            </div>
          </div>

          {/* Server accordion */}
          <div className="border-border border-t">
            {filteredServers.length === 0 ? (
              <div className="text-muted-foreground px-3 py-3 text-sm">
                {allServers.length === 0
                  ? "No servers found"
                  : "No matching tools or servers"}
              </div>
            ) : (
              filteredServers.map((server) => {
                const isExpanded = expandedServers.has(server.id);
                const serverTools = server.tools
                  .slice()
                  .sort((a, b) => a.name.localeCompare(b.name));

                const selectedCount = serverTools.filter((t) =>
                  selectors.some(
                    (s) => s.resourceId === server.id && s.tool === t.name,
                  ),
                ).length;
                const allSelected =
                  serverTools.length > 0 &&
                  selectedCount === serverTools.length;
                const someSelected = selectedCount > 0 && !allSelected;

                return (
                  <div
                    key={server.id}
                    className="border-border border-b last:border-b-0"
                  >
                    {/* Server header */}
                    <div
                      role="button"
                      tabIndex={0}
                      onClick={() => toggleExpanded(server.id)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" || e.key === " ") {
                          e.preventDefault();
                          toggleExpanded(server.id);
                        }
                      }}
                      className="hover:bg-muted/50 flex cursor-pointer items-center"
                    >
                      <div className="flex min-w-0 flex-1 items-center gap-2 px-3 py-2.5 text-sm">
                        <ChevronRight
                          className={cn(
                            "text-muted-foreground h-3 w-3 shrink-0 transition-transform",
                            isExpanded && "rotate-90",
                          )}
                        />
                        <span className="min-w-0 truncate">
                          <HighlightMatch
                            text={`${server.projectName.toLowerCase()}/`}
                            query={q}
                            className="text-muted-foreground/60"
                          />
                          <HighlightMatch
                            text={server.name}
                            query={q}
                            className="font-medium"
                          />
                        </span>
                      </div>
                      <div className="flex shrink-0 items-center gap-2 pr-3">
                        <span className="text-muted-foreground text-xs">
                          {selectedCount > 0
                            ? `${selectedCount} of ${serverTools.length} selected`
                            : `${serverTools.length} ${serverTools.length === 1 ? "tool" : "tools"} available`}
                        </span>
                        {
                          <Checkbox
                            checked={
                              allSelected
                                ? true
                                : someSelected
                                  ? "indeterminate"
                                  : false
                            }
                            onClick={(e) => {
                              e.stopPropagation();
                              if (hasAnnotationFilter && onChangeAnnotations) {
                                onChangeAnnotations([]);
                              }
                              if (onBatchToggleTools) {
                                onBatchToggleTools(
                                  server.id,
                                  serverTools.map((t) => t.name),
                                  !allSelected,
                                );
                              }
                            }}
                            className="focus-visible:border-input pointer-events-auto cursor-pointer focus-visible:ring-0"
                          />
                        }
                      </div>
                    </div>

                    {/* Expanded tool list */}
                    {isExpanded && (
                      <div className="bg-muted/30 border-border max-h-[300px] overflow-y-auto border-t">
                        {serverTools.map((tool) => {
                          const isSelected = selectors.some(
                            (s) =>
                              s.resourceId === server.id &&
                              s.tool === tool.name,
                          );
                          return (
                            <button
                              key={tool.id}
                              type="button"
                              onClick={() => {
                                if (
                                  hasAnnotationFilter &&
                                  onChangeAnnotations
                                ) {
                                  onChangeAnnotations([]);
                                }
                                onToggleTool(server.id, tool.name);
                              }}
                              className="hover:bg-accent flex w-full cursor-pointer items-center gap-2 py-1.5 pr-3 pl-8 text-sm"
                            >
                              <Checkbox
                                checked={isSelected}
                                className="focus-visible:border-input pointer-events-none focus-visible:ring-0"
                                tabIndex={-1}
                              />
                              <HighlightMatch
                                text={tool.name}
                                query={q}
                                className="truncate"
                              />
                            </button>
                          );
                        })}
                      </div>
                    )}
                  </div>
                );
              })
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

const ANNOTATION_OPTIONS: {
  key: AnnotationHint;
  label: string;
  description: string;
  icon: React.ComponentType<{ className?: string }>;
}[] = [
  {
    key: "readOnlyHint",
    label: "Read-only",
    description: "Tools that don't modify their environment",
    icon: Shield,
  },
  {
    key: "destructiveHint",
    label: "Destructive",
    description: "Tools that perform destructive updates",
    icon: AlertTriangle,
  },
  {
    key: "idempotentHint",
    label: "Idempotent",
    description: "Repeated calls have no additional effect",
    icon: Repeat,
  },
  {
    key: "openWorldHint",
    label: "Open-world",
    description: "Tools that interact with external entities",
    icon: Globe,
  },
];

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
          const mcpSlug = parts[parts.length - 1];
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
              className="border-input text-foreground hover:bg-accent inline-flex cursor-pointer items-center gap-1.5 rounded-md border px-3 py-1.5 text-xs shadow-xs transition-colors"
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
            className="hover:bg-accent flex w-full cursor-pointer items-center gap-3 rounded-sm px-3 py-2.5 text-sm"
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
              className="text-muted-foreground hover:text-foreground flex w-full cursor-pointer items-center justify-center gap-1.5 rounded-sm px-3 py-1.5 text-xs transition-colors"
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
        compact ? "h-10 rounded-none text-sm" : "rounded-sm py-2 text-sm",
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
        "hover:bg-accent flex w-full cursor-pointer items-start gap-2 rounded-sm px-3 py-2 text-sm",
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

/** Highlights substring matches with a yellow background. */
function HighlightMatch({
  text,
  query,
  className,
}: {
  text: string;
  query: string;
  className?: string;
}) {
  if (!query) return <span className={className}>{text}</span>;
  const idx = text.toLowerCase().indexOf(query.toLowerCase());
  if (idx === -1) return <span className={className}>{text}</span>;
  return (
    <span className={className}>
      {text.slice(0, idx)}
      <mark className="rounded-sm bg-yellow-200 dark:bg-yellow-800/60">
        {text.slice(idx, idx + query.length)}
      </mark>
      {text.slice(idx + query.length)}
    </span>
  );
}
