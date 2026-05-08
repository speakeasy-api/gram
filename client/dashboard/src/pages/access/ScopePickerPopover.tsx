import { Checkbox } from "@/components/ui/checkbox";
import { Dialog } from "@/components/ui/dialog";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { RequireScope } from "@/components/require-scope";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
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
  ChevronDown,
  ChevronRight,
  Info,
  Maximize2,
  Minimize2,
  Globe,
  Plus,
  Repeat,
  Shield,
  Tag,
  Wrench,
  X,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQueries } from "@tanstack/react-query";
import { useVirtualizer } from "@tanstack/react-virtual";
import type {
  ActivePanel,
  AnnotationHint,
  CustomTab,
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
  /** Which custom tab is active */
  customTab?: CustomTab;
  onCustomTabChange?: (tab: CustomTab) => void;
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
      // Skip external MCP/catalog servers — tool names aren't resolved yet
      // TODO: re-enable once external server tool names are available
      const isExternal = t.tools.some((tool) => tool.type === "externalmcp");
      if (isExternal) continue;
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
  customTab,
  onCustomTabChange,
}: ScopePickerPopoverProps) {
  const organization = useOrganization();
  const mcpServers = useMCPServers(resourceType === "mcp");
  const [popoverOpen, setPopoverOpen] = useState(false);
  const [expanded, setExpanded] = useState(false);
  // Override for when user clicks a mode but selectors are still empty
  const [panelOverride, setPanelOverride] = useState<ActivePanel | null>(null);

  const isMcpConnect = scope === "mcp:connect";
  const collectionGroups = useCollectionGroups(mcpServers, isMcpConnect);

  const panelState = computePanelState(
    selectors,
    collectionGroups,
    resourceType,
    customTab,
  );
  // Use override only when selectors are empty (user just switched mode)
  const activePanel =
    selectors !== null && selectors.length === 0 && panelOverride
      ? panelOverride
      : panelState.activePanel;
  const label = panelState.label;

  // panelOverride persists until the user explicitly switches panels via
  // switchPanel(). The derivation above already ignores the override when
  // selectors have content, so clearing it eagerly only causes the UI to
  // jump back to "servers" when the user deselects all items.

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

  const projectList = organization.projects.map((p) => ({
    id: p.id,
    name: p.name,
  }));

  const resourceKind = resourceType === "project" ? "project" : "mcp";

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

  const switchPanel = (panel: ActivePanel) => {
    setPanelOverride(panel);
    if (panel === "all") {
      onChangeSelectors(null);
    } else {
      onChangeSelectors([]);
    }
    if (panel !== "tools") {
      onChangeAnnotations?.([]);
    }
  };

  const renderScopeOptions = ({
    includeCollection,
  }: {
    includeCollection: boolean;
  }) => (
    <div className="shrink-0 pb-1.5">
      <ScopeOption
        label={resourceType === "project" ? "All projects" : "All servers"}
        selected={activePanel === "all"}
        onClick={() => switchPanel("all")}
      />
      <ScopeOption
        label={
          resourceType === "project" ? "Specific projects" : "Specific servers"
        }
        selected={activePanel === "servers"}
        onClick={() => switchPanel("servers")}
      />
      {isMcpConnect && (
        <ScopeOption
          label="Specific tools"
          selected={activePanel === "tools"}
          onClick={() => switchPanel("tools")}
        />
      )}
      {isMcpConnect && includeCollection && (
        <ScopeOption
          label="Specific collections"
          selected={activePanel === "collection"}
          onClick={() => switchPanel("collection")}
        />
      )}
    </div>
  );

  const resourceList = activePanel === "servers" && (
    <>
      <div className="bg-border my-1 h-px" />
      <div className="max-h-[250px] overflow-y-auto">
        {resourceType === "project"
          ? projectList.map((resource) => (
              <ResourceCheckbox
                key={resource.id}
                id={resource.id}
                name={resource.name}
                checked={isResourceSelected(resource.id)}
                onToggle={toggleResource}
              />
            ))
          : mcpServers.map((group) => (
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
            ))}
      </div>
    </>
  );

  const customTabs = (toolScrollClass?: string) => (
    <Tabs
      value={customTab ?? "select"}
      className="flex min-h-0 flex-1 flex-col gap-0"
      onValueChange={(value) => {
        onChangeSelectors([]);
        onChangeAnnotations?.([]);
        onCustomTabChange?.(value as CustomTab);
      }}
    >
      <TabsList className="border-border h-auto w-full shrink-0 gap-2 rounded-none border-y bg-transparent px-1.5 py-1.5">
        <TabsTrigger
          value="select"
          className="text-muted-foreground hover:bg-muted/50 data-[state=active]:bg-muted data-[state=active]:text-foreground h-auto rounded-sm border-none px-3 py-2 text-sm shadow-none data-[state=active]:shadow-none"
        >
          <Wrench className="h-3.5 w-3.5" />
          All tools
        </TabsTrigger>
        <div className="bg-border/40 my-1 w-px self-stretch" />
        <TabsTrigger
          value="auto-groups"
          className="text-muted-foreground hover:bg-muted/50 data-[state=active]:bg-muted data-[state=active]:text-foreground h-auto rounded-sm border-none px-3 py-2 text-sm shadow-none data-[state=active]:shadow-none"
        >
          <Tag className="h-3.5 w-3.5" />
          By annotation
        </TabsTrigger>
      </TabsList>
      <TabsContent
        value="select"
        className="flex min-h-[200px] flex-1 flex-col p-0"
      >
        <ToolSelectionPanel
          mcpServers={mcpServers}
          selectors={selectors ?? []}
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
                    !(
                      s.resourceId === serverId &&
                      s.tool &&
                      toolSet.has(s.tool)
                    ),
                ),
              );
            }
          }}
          className={toolScrollClass}
        />
      </TabsContent>
      <TabsContent
        value="auto-groups"
        className="min-h-[200px] flex-1 overflow-y-auto px-2 py-1"
      >
        <AnnotationGroupPanel
          annotations={annotations ?? []}
          onChangeAnnotations={(newAnnotations) => {
            onChangeAnnotations?.(newAnnotations);
            const newSelectors = newAnnotations.map((hint) => ({
              resourceKind: "mcp" as const,
              resourceId: "*",
              disposition: ANNOTATION_TO_DISPOSITION[hint],
            }));
            onChangeSelectors(newSelectors);
          }}
          mcpServers={mcpServers}
        />
      </TabsContent>
    </Tabs>
  );

  const collectionPanel = (
    <div className="-mx-1.5 -mb-1.5 flex max-h-[min(420px,60vh)] flex-col overflow-hidden">
      <div className="border-border flex min-h-0 flex-1 flex-col overflow-y-auto border-y px-2 py-1">
        <CollectionGroupPanel
          collectionGroups={collectionGroups}
          selectors={selectors ?? []}
          onChangeSelectors={onChangeSelectors}
          onNavigate={() => setPopoverOpen(false)}
        />
      </div>
    </div>
  );

  return (
    <>
      <Popover modal={false} open={popoverOpen} onOpenChange={setPopoverOpen}>
        <PopoverTrigger asChild>
          <button
            type="button"
            className="border-input bg-background hover:bg-background inline-flex h-7 shrink-0 items-center gap-1 rounded-md border px-2 py-1 text-xs shadow-xs transition-colors"
          >
            <span className="max-w-[120px] truncate">{label}</span>
            <ChevronDown className="h-3 w-3 shrink-0 opacity-50" />
          </button>
        </PopoverTrigger>
        <PopoverContent
          align="end"
          sideOffset={8}
          className={cn(
            "p-1.5 transition-[width] duration-500",
            activePanel === "tools"
              ? "w-[680px]"
              : activePanel === "collection"
                ? "w-[360px]"
                : activePanel === "servers"
                  ? "w-96"
                  : "w-64",
          )}
          style={{
            transitionTimingFunction: "cubic-bezier(0.32, 0.72, 0, 1)",
          }}
        >
          {renderScopeOptions({ includeCollection: true })}
          {resourceList}
          {activePanel === "tools" && (
            <div className="-mx-1.5 -mb-1.5 flex max-h-[min(420px,60vh)] flex-col">
              {customTabs("max-h-[min(340px,50vh)]")}
              {(customTab ?? "select") === "select" && (
                <div className="bg-background border-border shrink-0 rounded-b-lg border-t">
                  <button
                    type="button"
                    onClick={() => {
                      setPopoverOpen(false);
                      setExpanded(true);
                    }}
                    className="text-muted-foreground hover:text-foreground hover:bg-muted/50 flex w-full cursor-pointer items-center justify-center gap-1.5 rounded-b-lg px-3 py-2.5 text-xs transition-colors"
                  >
                    <Maximize2 className="h-3 w-3" />
                    Open in full screen
                  </button>
                </div>
              )}
            </div>
          )}
          {activePanel === "collection" && collectionPanel}
        </PopoverContent>
      </Popover>

      <Dialog
        open={expanded}
        onOpenChange={(open) => {
          if (!open) setExpanded(false);
        }}
      >
        <Dialog.Content className="flex h-[85vh] w-[90vw] flex-col gap-0 overflow-hidden p-0 sm:max-w-5xl [&>.absolute]:hidden">
          <div className="bg-muted/50 border-border flex items-center justify-between border-b px-4 py-4">
            <span className="text-muted-foreground text-xs font-medium tracking-wider uppercase">
              Configure Access
            </span>
            <button
              type="button"
              onClick={() => {
                setExpanded(false);
                setPopoverOpen(true);
              }}
              className="text-muted-foreground hover:text-foreground inline-flex h-6 w-6 cursor-pointer items-center justify-center rounded-sm opacity-70 transition-opacity hover:opacity-100"
            >
              <Minimize2 className="h-4 w-4" />
            </button>
          </div>
          <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
            <div className="px-1.5 pt-1.5">
              {renderScopeOptions({ includeCollection: false })}
            </div>
            {customTabs()}
          </div>
        </Dialog.Content>
      </Dialog>
    </>
  );
}

function ToolSelectionPanel({
  mcpServers,
  selectors,
  onToggleTool,
  onBatchToggleTools,
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
  const [selectedServerId, setSelectedServerId] = useState<string | null>(
    allServers[0]?.id ?? null,
  );
  const [search, setSearch] = useState("");
  const [serverSearch, setServerSearch] = useState("");
  const [leftWidth, setLeftWidth] = useState(260);
  const dragging = useRef(false);

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!dragging.current) return;
      const container = serverScrollRef.current?.parentElement?.parentElement;
      if (!container) return;
      const rect = container.getBoundingClientRect();
      const next = Math.max(
        140,
        Math.min(e.clientX - rect.left, rect.width - 180),
      );
      setLeftWidth(next);
    };
    const onUp = () => {
      dragging.current = false;
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    return () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
  }, []);

  const selectedServer = allServers.find((s) => s.id === selectedServerId);
  const tools = useMemo(
    () => selectedServer?.tools ?? [],
    [selectedServer?.tools],
  );
  const filteredTools = useMemo(
    () =>
      (search
        ? tools.filter((t) =>
            t.name.toLowerCase().includes(search.toLowerCase()),
          )
        : [...tools]
      ).sort((a, b) => a.name.localeCompare(b.name)),
    [tools, search],
  );

  const scrollRef = useRef<HTMLDivElement>(null);
  const handleWheel = useCallback((e: React.WheelEvent) => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop += e.deltaY;
    }
  }, []);

  const serverScrollRef = useRef<HTMLDivElement>(null);
  const handleServerWheel = useCallback((e: React.WheelEvent) => {
    if (serverScrollRef.current) {
      serverScrollRef.current.scrollTop += e.deltaY;
    }
  }, []);

  const filteredServers = useMemo(
    () =>
      serverSearch
        ? allServers.filter((s) =>
            `${s.projectName}/${s.name}`
              .toLowerCase()
              .includes(serverSearch.toLowerCase()),
          )
        : allServers,
    [allServers, serverSearch],
  );

  const serverVirtualizer = useVirtualizer({
    count: filteredServers.length,
    getScrollElement: () => serverScrollRef.current,
    estimateSize: () => 40,
    overscan: 5,
  });

  const toolVirtualizer = useVirtualizer({
    count: filteredTools.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 40,
    overscan: 5,
  });

  return (
    <div className={cn("flex min-h-0 flex-1", className)}>
      {/* Left column — server list */}
      <div
        className="border-border flex min-h-0 shrink-0 flex-col border-r"
        style={{ width: leftWidth }}
      >
        <div className="border-border flex h-10 shrink-0 items-center gap-2 border-b px-3">
          <Globe className="text-muted-foreground h-3 w-3 shrink-0" />
          <input
            type="text"
            placeholder="Search servers…"
            value={serverSearch}
            onChange={(e) => setServerSearch(e.target.value)}
            className="placeholder:text-muted-foreground flex-1 bg-transparent text-sm outline-none"
          />
          {serverSearch && (
            <button
              type="button"
              onClick={() => setServerSearch("")}
              className="text-muted-foreground hover:text-foreground shrink-0"
            >
              <X className="h-3 w-3" />
            </button>
          )}
        </div>
        <div
          ref={serverScrollRef}
          onWheel={handleServerWheel}
          className="min-h-0 flex-1 overflow-y-auto"
        >
          {filteredServers.length === 0 ? (
            <div className="text-muted-foreground px-3 py-3 text-sm">
              {allServers.length === 0
                ? "No servers found"
                : "No matching servers"}
            </div>
          ) : (
            <div
              style={{
                height: `${serverVirtualizer.getTotalSize()}px`,
                position: "relative",
              }}
            >
              {serverVirtualizer.getVirtualItems().map((virtualItem) => {
                const server = filteredServers[virtualItem.index];
                const isActive = selectedServerId === server.id;
                return (
                  <button
                    key={server.id}
                    type="button"
                    onClick={() => {
                      setSelectedServerId(server.id);
                      setSearch("");
                    }}
                    style={{
                      position: "absolute",
                      top: 0,
                      left: 0,
                      width: "100%",
                      height: `${virtualItem.size}px`,
                      transform: `translateY(${virtualItem.start}px)`,
                    }}
                    className={cn(
                      "hover:bg-muted/50 flex cursor-pointer items-center justify-between truncate px-3 text-sm",
                      isActive && "bg-muted font-medium",
                    )}
                  >
                    <span className="truncate">
                      <span className="text-muted-foreground/60">
                        {server.projectName.toLowerCase()}/
                      </span>
                      {server.name}
                    </span>
                    {isActive && (
                      <ChevronRight className="text-muted-foreground h-3 w-3 shrink-0" />
                    )}
                  </button>
                );
              })}
            </div>
          )}
        </div>
      </div>

      {/* Resize handle */}
      <div
        onMouseDown={(e) => {
          e.preventDefault();
          dragging.current = true;
          document.body.style.cursor = "col-resize";
          document.body.style.userSelect = "none";
        }}
        className="hover:bg-border/80 flex w-1 shrink-0 cursor-col-resize items-center justify-center transition-colors"
      />

      {/* Right column — tools for selected server */}
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <div className="border-border flex h-10 items-center gap-1 border-b px-3">
          <input
            type="text"
            placeholder="Search tools…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="placeholder:text-muted-foreground flex-1 bg-transparent text-sm outline-none"
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
        {filteredTools.length > 0 &&
          !search &&
          (() => {
            const selectedCount = filteredTools.filter((t) =>
              selectors.some(
                (s) => s.resourceId === selectedServerId && s.tool === t.name,
              ),
            ).length;
            const allSelected = selectedCount === filteredTools.length;
            const someSelected = selectedCount > 0 && !allSelected;
            return (
              <button
                type="button"
                onClick={() => {
                  if (onBatchToggleTools && selectedServerId) {
                    const toolNames = filteredTools.map((t) => t.name);
                    onBatchToggleTools(
                      selectedServerId,
                      toolNames,
                      !allSelected,
                    );
                  }
                }}
                className="bg-muted/50 border-border hover:bg-muted flex h-10 shrink-0 cursor-pointer items-center gap-2 border-b px-3 transition-colors"
              >
                <Checkbox
                  checked={
                    allSelected ? true : someSelected ? "indeterminate" : false
                  }
                  className="focus-visible:border-input pointer-events-none focus-visible:ring-0"
                  tabIndex={-1}
                />
                <span className="text-muted-foreground text-sm">
                  {allSelected
                    ? `All ${tools.length} selected`
                    : someSelected
                      ? `${selectedCount} of ${tools.length} selected`
                      : `Select all`}
                </span>
              </button>
            );
          })()}
        <div
          ref={scrollRef}
          onWheel={handleWheel}
          className="tool-scroll min-h-0 flex-1 overflow-y-auto"
        >
          {filteredTools.length === 0 ? (
            <div className="text-muted-foreground px-3 py-3 text-sm">
              {tools.length === 0 ? "No tools found" : "No matching tools"}
            </div>
          ) : (
            <div
              style={{
                height: `${toolVirtualizer.getTotalSize()}px`,
                position: "relative",
              }}
            >
              {toolVirtualizer.getVirtualItems().map((virtualItem) => {
                const tool = filteredTools[virtualItem.index];
                const isSelected = selectors.some(
                  (s) =>
                    s.resourceId === selectedServerId && s.tool === tool.name,
                );
                return (
                  <div
                    key={tool.id}
                    style={{
                      position: "absolute",
                      top: 0,
                      left: 0,
                      width: "100%",
                      height: `${virtualItem.size}px`,
                      transform: `translateY(${virtualItem.start}px)`,
                    }}
                  >
                    <ResourceCheckbox
                      id={tool.name}
                      name={tool.name}
                      checked={isSelected}
                      onToggle={() =>
                        onToggleTool(selectedServerId!, tool.name)
                      }
                      compact
                    />
                  </div>
                );
              })}
            </div>
          )}
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

function AnnotationGroupPanel({
  annotations,
  onChangeAnnotations,
  mcpServers,
}: {
  annotations: AnnotationHint[];
  onChangeAnnotations?: (annotations: AnnotationHint[]) => void;
  mcpServers: ServerGroup[];
}) {
  const allTools = useMemo(
    () =>
      mcpServers.flatMap((g) =>
        g.servers.flatMap((s) =>
          s.tools.map((t) => ({ ...t, serverName: s.name })),
        ),
      ),
    [mcpServers],
  );

  const toolsByAnnotation = useMemo(() => {
    const map = new Map<AnnotationHint, typeof allTools>();
    for (const hint of [
      "readOnlyHint",
      "destructiveHint",
      "idempotentHint",
      "openWorldHint",
    ] as AnnotationHint[]) {
      map.set(
        hint,
        allTools.filter((t) => t.annotations?.[hint] === true),
      );
    }
    return map;
  }, [allTools]);

  const toggle = (key: AnnotationHint) => {
    const has = annotations.includes(key);
    const next = has
      ? annotations.filter((a) => a !== key)
      : [...annotations, key];
    onChangeAnnotations?.(next);
  };

  return (
    <div className="py-1">
      <div className="text-muted-foreground px-2 py-2 text-sm">
        Grant access to all tools matching selected annotations:
      </div>
      {ANNOTATION_OPTIONS.map((opt) => {
        const isSelected = annotations.includes(opt.key);
        const matchCount = (toolsByAnnotation.get(opt.key) ?? []).length;
        const Icon = opt.icon;
        return (
          <button
            key={opt.key}
            type="button"
            onClick={() => toggle(opt.key)}
            className={cn(
              "hover:bg-accent flex w-full cursor-pointer items-center gap-3 rounded-sm px-3 py-2.5 text-sm outline-none",
              isSelected && "font-medium",
            )}
          >
            <Checkbox
              checked={isSelected}
              className="focus-visible:border-input pointer-events-none focus-visible:ring-0"
              tabIndex={-1}
            />
            <Icon className="text-muted-foreground h-3.5 w-3.5 shrink-0" />
            <div className="min-w-0 flex-1 text-left">
              <div>{opt.label}</div>
              <div className="text-muted-foreground text-[11px] font-normal">
                {opt.description}
              </div>
            </div>
            <span className="text-muted-foreground shrink-0 text-xs">
              {matchCount} tool{matchCount !== 1 ? "s" : ""}
            </span>
          </button>
        );
      })}
    </div>
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
  selected,
  onClick,
}: {
  label: string;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "hover:bg-accent flex w-full cursor-pointer items-center gap-2 rounded-sm px-3 py-2 text-sm",
        selected && "font-medium",
      )}
    >
      <span className="flex w-4 shrink-0 items-center justify-center">
        {selected && <Check className="h-3.5 w-3.5" />}
      </span>
      <span>{label}</span>
    </button>
  );
}
