import { Checkbox } from "@/components/ui/checkbox";
import { Dialog } from "@/components/ui/dialog";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useOrganization } from "@/contexts/Auth";
import { getServerURL } from "@/lib/utils";
import { cn } from "@/lib/utils";
import { useListToolsetsForOrg } from "@gram/client/react-query/listToolsetsForOrg.js";
import {
  AlertTriangle,
  Check,
  ChevronDown,
  ChevronRight,
  Maximize2,
  SquareAsterisk,
  Globe,
  Repeat,
  Shield,
  Tag,
  Wrench,
  X,
} from "lucide-react";
import { useCallback, useMemo, useRef, useState } from "react";
import type { AnnotationHint, CustomTab, ResourceType } from "./types";

interface ScopePickerPopoverProps {
  /** The resource type determines which resource list to show */
  resourceType: ResourceType;
  /** null = unrestricted; string[] = allowlist */
  resources: string[] | null;
  onChangeResources: (resources: string[] | null) => void;
  /** Whether "Custom" mode is active (MCP scopes only) */
  customMode?: boolean;
  onCustomModeChange?: (custom: boolean) => void;
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
      group.servers.push({
        id: t.slug,
        name: t.name,
        slug: mcpUrl,
        tools: t.tools.map((tool) => ({
          id: tool.id,
          name: tool.name,
          type: tool.type,
          httpMethod: tool.httpMethod,
          annotations: tool.annotations,
        })),
      });
    }
    return [...groups.values()];
  }, [data, organization.projects]);
}

export function ScopePickerPopover({
  resourceType,
  resources,
  onChangeResources,
  customMode,
  onCustomModeChange,
  annotations,
  onChangeAnnotations,
  customTab,
  onCustomTabChange,
}: ScopePickerPopoverProps) {
  const organization = useOrganization();
  const mcpServers = useMCPServers(resourceType === "mcp");
  const [popoverOpen, setPopoverOpen] = useState(false);
  const [expanded, setExpanded] = useState(false);

  // Org-scoped permissions have no resource picker — they're always org-wide
  if (resourceType === "org") {
    return (
      <span className="inline-flex items-center rounded-md border border-input bg-transparent px-2 py-1 text-xs text-muted-foreground h-7">
        All
      </span>
    );
  }

  const isUnrestricted = resources === null;
  const isMcp = resourceType === "mcp";
  const projectList = organization.projects.map((p) => ({
    id: p.id,
    name: p.name,
  }));
  const customToolCount = useMemo(() => {
    if (!customMode) return 0;
    return (resources ?? []).length;
  }, [customMode, resources]);

  const label = getLabel(resourceType, resources, customMode, customToolCount);

  const toggleResource = (id: string) => {
    if (resources === null) return;
    const has = resources.includes(id);
    const next = has ? resources.filter((r) => r !== id) : [...resources, id];
    onChangeResources(next);
  };

  const pickerContent = (
    <>
      {/* Scope mode options */}
      <div className="pb-1.5">
        <ScopeOption
          label={resourceType === "project" ? "All projects" : "All servers"}
          selected={isUnrestricted && !customMode}
          onClick={() => {
            if (customMode) {
              onCustomModeChange?.(false);
              onChangeAnnotations?.([]);
            }
            onChangeResources(null);
          }}
        />
        <ScopeOption
          label={
            resourceType === "project"
              ? "Specific projects"
              : "Specific servers"
          }
          selected={!isUnrestricted && !customMode}
          onClick={() => {
            if (customMode) {
              onCustomModeChange?.(false);
              onChangeResources([]);
              onChangeAnnotations?.([]);
            } else if (isUnrestricted) {
              onChangeResources([]);
            }
          }}
        />

        {/* Custom option for MCP scopes */}
        {isMcp && (
          <ScopeOption
            label="Specific tools"
            selected={!!customMode}
            onClick={() => {
              onCustomModeChange?.(true);
              if (isUnrestricted) onChangeResources([]);
            }}
          />
        )}
      </div>

      {/* Resource list when scoped to specific resources */}
      {!isUnrestricted && !customMode && (
        <>
          <div className="my-1 h-px bg-border" />
          {resourceType === "project"
            ? projectList.map((resource) => (
                <ResourceCheckbox
                  key={resource.id}
                  id={resource.id}
                  name={resource.name}
                  checked={resources.includes(resource.id)}
                  onToggle={toggleResource}
                />
              ))
            : mcpServers.map((group) => (
                <div key={group.projectId}>
                  <div className="px-3 py-1.5 text-xs text-muted-foreground font-medium">
                    {group.projectName}
                  </div>
                  {group.servers.map((server) => (
                    <ResourceCheckbox
                      key={server.id}
                      id={server.id}
                      name={server.name}
                      checked={resources.includes(server.id)}
                      onToggle={toggleResource}
                    />
                  ))}
                </div>
              ))}
        </>
      )}

      {/* Custom mode — tabbed fine-grained picker */}
      {customMode && (
        <>
          <Tabs
            value={customTab ?? "select"}
            className="gap-0 -mx-1.5 -mb-1.5"
            onValueChange={(value) => {
              onChangeResources([]);
              onChangeAnnotations?.([]);
              onCustomTabChange?.(value as CustomTab);
            }}
          >
            <TabsList className="w-full h-auto rounded-none bg-transparent px-1.5 py-1.5 gap-2 border-y border-border">
              <TabsTrigger
                value="select"
                className="h-auto rounded-sm border-none shadow-none px-3 py-2 text-sm text-muted-foreground hover:bg-muted/50 data-[state=active]:bg-muted data-[state=active]:text-foreground data-[state=active]:shadow-none"
              >
                <Wrench className="h-3.5 w-3.5" />
                All tools
              </TabsTrigger>
              <div className="w-px self-stretch my-1 bg-border/40" />
              <TabsTrigger
                value="auto-groups"
                className="h-auto rounded-sm border-none shadow-none px-3 py-2 text-sm text-muted-foreground hover:bg-muted/50 data-[state=active]:bg-muted data-[state=active]:text-foreground data-[state=active]:shadow-none"
              >
                <Tag className="h-3.5 w-3.5" />
                By annotation
              </TabsTrigger>
              <div className="w-px self-stretch my-1 bg-border/40" />
              <TabsTrigger
                value="http-method"
                className="h-auto rounded-sm border-none shadow-none px-3 py-2 text-sm text-muted-foreground hover:bg-muted/50 data-[state=active]:bg-muted data-[state=active]:text-foreground data-[state=active]:shadow-none"
              >
                <SquareAsterisk className="h-3.5 w-3.5" />
                By HTTP method
              </TabsTrigger>
            </TabsList>
            <TabsContent value="select" className="p-0">
              <ToolSelectionPanel
                mcpServers={mcpServers}
                resources={resources ?? []}
                onToggle={toggleResource}
              />
            </TabsContent>
            <TabsContent value="auto-groups" className="px-2 py-1">
              <AnnotationGroupPanel
                annotations={annotations ?? []}
                onChangeAnnotations={(newAnnotations) => {
                  onChangeAnnotations?.(newAnnotations);
                  // Resolve matched annotations to compound tool IDs so the
                  // backend receives real resource identifiers it can enforce.
                  const matchedIds: string[] = [];
                  for (const group of mcpServers) {
                    for (const server of group.servers) {
                      for (const tool of server.tools) {
                        if (
                          newAnnotations.some(
                            (hint) => tool.annotations?.[hint] === true,
                          )
                        ) {
                          matchedIds.push(`${server.id}:${tool.name}`);
                        }
                      }
                    }
                  }
                  onChangeResources(matchedIds);
                }}
                mcpServers={mcpServers}
              />
            </TabsContent>
            <TabsContent value="http-method" className="px-2 py-1">
              <HttpMethodGroupPanel
                mcpServers={mcpServers}
                resources={resources ?? []}
                onToggle={toggleResource}
                onChangeResources={onChangeResources}
              />
            </TabsContent>
          </Tabs>
        </>
      )}
    </>
  );

  return (
    <>
      <Popover modal={false} open={popoverOpen} onOpenChange={setPopoverOpen}>
        <PopoverTrigger asChild>
          <button
            type="button"
            className="inline-flex items-center gap-1 rounded-md border border-input bg-background px-2 py-1 text-xs shadow-xs hover:bg-background transition-colors shrink-0 h-7"
          >
            <span className="truncate max-w-[120px]">{label}</span>
            <ChevronDown className="h-3 w-3 opacity-50 shrink-0" />
          </button>
        </PopoverTrigger>
        <PopoverContent
          align="end"
          sideOffset={8}
          className={cn(
            "p-1.5 overflow-hidden transition-[width] duration-500",
            customMode ? "w-[520px]" : "w-56 max-h-[300px] overflow-y-auto",
          )}
          style={{
            transitionTimingFunction: "cubic-bezier(0.32, 0.72, 0, 1)",
          }}
        >
          {isMcp && (
            <div className="-mx-1.5 -mt-1.5 mb-1 flex items-center justify-between px-3 py-1.5 bg-muted/50 border-b border-border rounded-t-lg">
              <span className="text-[11px] font-medium text-muted-foreground uppercase tracking-wider">
                Configure Access
              </span>
              <button
                type="button"
                onClick={() => {
                  setPopoverOpen(false);
                  setExpanded(true);
                }}
                className="h-5 w-5 inline-flex items-center justify-center rounded-sm hover:bg-background/80 text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
              >
                <Maximize2 className="h-3 w-3" />
              </button>
            </div>
          )}
          {pickerContent}
        </PopoverContent>
      </Popover>

      <Dialog
        open={expanded}
        onOpenChange={(open) => {
          if (!open) setExpanded(false);
        }}
      >
        <Dialog.Content className="sm:max-w-4xl w-[90vw] max-h-[85vh] p-0 flex flex-col overflow-hidden gap-0">
          <div className="flex items-center px-4 py-3 bg-muted/50 border-b border-border pr-12">
            <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
              Configure Access
            </span>
          </div>
          <div className="p-1.5 flex-1 overflow-y-auto">{pickerContent}</div>
        </Dialog.Content>
      </Dialog>
    </>
  );
}

function ToolSelectionPanel({
  mcpServers,
  resources,
  onToggle,
}: {
  mcpServers: ServerGroup[];
  resources: string[];
  onToggle: (id: string) => void;
}) {
  const allServers = useMemo(
    () => mcpServers.flatMap((g) => g.servers),
    [mcpServers],
  );
  const [selectedServerId, setSelectedServerId] = useState<string | null>(
    allServers[0]?.id ?? null,
  );
  const [search, setSearch] = useState("");
  const selectedServer = allServers.find((s) => s.id === selectedServerId);
  const tools = selectedServer?.tools ?? [];
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

  return (
    <div className="flex">
      {/* Left column — server list */}
      <div className="w-[160px] shrink-0 border-r border-border overflow-y-auto">
        <div className="flex items-center gap-1.5 px-3 h-10 bg-muted/50 text-[10px] font-medium text-muted-foreground uppercase tracking-wider border-b border-border">
          <Globe className="h-3 w-3" />
          Server List
        </div>
        {allServers.map((server) => {
          const isActive = selectedServerId === server.id;
          return (
            <button
              key={server.id}
              type="button"
              onClick={() => {
                setSelectedServerId(server.id);
                setSearch("");
              }}
              className={cn(
                "flex w-full items-center justify-between px-3 h-10 text-sm cursor-pointer hover:bg-muted/50 truncate",
                isActive && "bg-muted font-medium",
              )}
            >
              <span className="truncate">{server.name}</span>
              {isActive && (
                <ChevronRight className="h-3 w-3 text-muted-foreground shrink-0" />
              )}
            </button>
          );
        })}
      </div>

      {/* Right column — tools for selected server */}
      <div className="flex-1 min-w-0 flex flex-col min-h-0">
        <div className="flex items-center gap-1 px-3 h-10 border-b border-border">
          <input
            type="text"
            placeholder="Search tools…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="flex-1 bg-transparent text-sm placeholder:text-muted-foreground outline-none"
          />
          {search && (
            <button
              type="button"
              onClick={() => setSearch("")}
              className="shrink-0 text-muted-foreground hover:text-foreground"
            >
              <X className="h-3 w-3" />
            </button>
          )}
        </div>
        <div
          ref={scrollRef}
          onWheel={handleWheel}
          className="min-h-[300px] max-h-[300px] overflow-y-auto pb-2"
        >
          {filteredTools.length === 0 ? (
            <div className="px-3 py-3 text-sm text-muted-foreground">
              {tools.length === 0 ? "No tools found" : "No matching tools"}
            </div>
          ) : (
            filteredTools.map((tool) => {
              const toolId = `${selectedServerId}:${tool.name}`;
              return (
                <ResourceCheckbox
                  key={tool.id}
                  id={toolId}
                  name={tool.name}
                  checked={resources.includes(toolId)}
                  onToggle={onToggle}
                  compact
                />
              );
            })
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
  icon: React.ElementType;
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
  const [expanded, setExpanded] = useState<Set<AnnotationHint>>(new Set());
  const scrollRef = useRef<HTMLDivElement>(null);
  const handleWheel = useCallback((e: React.WheelEvent) => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop += e.deltaY;
    }
  }, []);
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
        allTools
          .filter((t) => t.annotations?.[hint] === true)
          .sort((a, b) => a.name.localeCompare(b.name)),
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

  const toggleExpanded = (key: AnnotationHint) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  return (
    <div className="py-1">
      <div className="px-2 py-2 text-sm text-muted-foreground">
        Grant access to all tools matching selected annotations:
      </div>
      {ANNOTATION_OPTIONS.map((opt) => {
        const isSelected = annotations.includes(opt.key);
        const isExpanded = expanded.has(opt.key);
        const matchedTools = toolsByAnnotation.get(opt.key) ?? [];
        const Icon = opt.icon;
        return (
          <div key={opt.key} className="rounded-sm hover:bg-accent">
            <button
              type="button"
              onClick={() => toggle(opt.key)}
              className={cn(
                "flex w-full items-center gap-3 px-3 py-2.5 text-sm cursor-pointer outline-none",
                isSelected && "font-medium",
              )}
            >
              <Checkbox
                checked={isSelected}
                className="pointer-events-none focus-visible:ring-0 focus-visible:border-input"
                tabIndex={-1}
              />
              <Icon className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
              <div className="flex-1 min-w-0 text-left">
                <div>{opt.label}</div>
                <div className="text-[11px] text-muted-foreground font-normal">
                  {opt.description}
                </div>
              </div>
            </button>
            {isSelected && (
              <div className="pl-[66px] pr-3 pb-3">
                {matchedTools.length === 0 ? (
                  <span className="text-[11px] text-muted-foreground">
                    No tools matched
                  </span>
                ) : (
                  <div className="rounded-md border border-border bg-background overflow-hidden">
                    <button
                      type="button"
                      onClick={() => toggleExpanded(opt.key)}
                      className="flex w-full items-center gap-1 px-2.5 py-1.5 text-xs text-muted-foreground hover:text-foreground cursor-pointer"
                    >
                      <ChevronRight
                        className={cn(
                          "h-3 w-3 transition-transform",
                          isExpanded && "rotate-90",
                        )}
                      />
                      {matchedTools.length} tool
                      {matchedTools.length !== 1 ? "s" : ""} matched
                    </button>
                    {isExpanded && (
                      <div
                        ref={scrollRef}
                        onWheel={handleWheel}
                        className="max-h-[120px] overflow-y-auto border-t border-border bg-popover"
                      >
                        {matchedTools.map((tool) => (
                          <div
                            key={`${tool.serverName}:${tool.id}`}
                            className="flex items-center justify-between gap-2 px-2.5 py-1.5 text-xs text-muted-foreground border-b border-border last:border-b-0"
                          >
                            <span className="truncate">{tool.name}</span>
                            <span className="text-[10px] opacity-50 shrink-0">
                              {tool.serverName}
                            </span>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

const HTTP_METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE"] as const;

const METHOD_COLORS: Record<string, string> = {
  GET: "text-blue-600 bg-blue-50",
  POST: "text-green-600 bg-green-50",
  PUT: "text-amber-600 bg-amber-50",
  PATCH: "text-orange-600 bg-orange-50",
  DELETE: "text-red-600 bg-red-50",
};

function HttpMethodGroupPanel({
  mcpServers,
  resources,
  onToggle,
  onChangeResources,
}: {
  mcpServers: ServerGroup[];
  resources: string[];
  onToggle: (id: string) => void;
  onChangeResources: (resources: string[]) => void;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const handleWheel = useCallback((e: React.WheelEvent) => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop += e.deltaY;
    }
  }, []);

  const allTools = useMemo(
    () =>
      mcpServers.flatMap((g) =>
        g.servers.flatMap((s) =>
          s.tools.map((t) => ({ ...t, serverSlug: s.id, serverName: s.name })),
        ),
      ),
    [mcpServers],
  );

  const httpTools = useMemo(
    () => allTools.filter((t) => t.type === "http"),
    [allTools],
  );

  const toolsByMethod = useMemo(() => {
    const map = new Map<string, typeof httpTools>();
    for (const tool of httpTools) {
      const method = tool.httpMethod?.toUpperCase() ?? "OTHER";
      const list = map.get(method) ?? [];
      list.push(tool);
      map.set(method, list);
    }
    // Sort tools within each method group
    for (const [key, tools] of map) {
      map.set(
        key,
        tools.sort((a, b) => a.name.localeCompare(b.name)),
      );
    }
    // Sort method groups by HTTP_METHODS order, with OTHER last
    const sorted = new Map<string, typeof httpTools>();
    for (const method of HTTP_METHODS) {
      const tools = map.get(method);
      if (tools) sorted.set(method, tools);
    }
    const other = map.get("OTHER");
    if (other) sorted.set("OTHER", other);
    return sorted;
  }, [httpTools]);

  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const toggleExpanded = (method: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(method)) next.delete(method);
      else next.add(method);
      return next;
    });
  };

  if (httpTools.length === 0) {
    return (
      <div className="py-6 text-center text-sm text-muted-foreground">
        No HTTP tools found
      </div>
    );
  }

  return (
    <div className="py-1">
      <div className="px-2 py-2 text-sm text-muted-foreground">
        Select tools by HTTP method:
      </div>
      {[...toolsByMethod.entries()].map(([method, tools]) => {
        const isExpanded = expanded.has(method);
        const compoundIds = tools.map((t) => `${t.serverSlug}:${t.id}`);
        const selectedCount = compoundIds.filter((id) =>
          resources.includes(id),
        ).length;
        const allSelected = selectedCount === tools.length && tools.length > 0;
        const colors =
          METHOD_COLORS[method] ?? "text-muted-foreground bg-muted";

        const toggleAll = () => {
          if (allSelected) {
            // Deselect all in this group
            const removeSet = new Set(compoundIds);
            onChangeResources(resources.filter((r) => !removeSet.has(r)));
          } else {
            // Select all in this group
            const existing = new Set(resources);
            const toAdd = compoundIds.filter((id) => !existing.has(id));
            onChangeResources([...resources, ...toAdd]);
          }
        };

        return (
          <div key={method} className="rounded-sm hover:bg-accent">
            <div className="flex w-full items-center gap-3 px-3 py-2.5 text-sm">
              <Checkbox
                checked={
                  allSelected
                    ? true
                    : selectedCount > 0
                      ? "indeterminate"
                      : false
                }
                onClick={(e) => {
                  e.stopPropagation();
                  toggleAll();
                }}
                className="cursor-pointer"
              />
              <span
                className={cn(
                  "inline-flex items-center justify-center rounded px-1.5 py-0.5 text-[10px] font-bold tracking-wide min-w-[52px]",
                  colors,
                )}
              >
                {method}
              </span>
              <button
                type="button"
                onClick={() => toggleExpanded(method)}
                className="flex flex-1 items-center gap-2 cursor-pointer"
              >
                <span className="flex-1 text-left text-muted-foreground font-normal">
                  {selectedCount} of {tools.length} selected
                </span>
                <ChevronRight
                  className={cn(
                    "h-3.5 w-3.5 text-muted-foreground transition-transform",
                    isExpanded && "rotate-90",
                  )}
                />
              </button>
            </div>
            {isExpanded && (
              <div
                ref={scrollRef}
                onWheel={handleWheel}
                className="max-h-[180px] overflow-y-auto border-t border-border bg-background"
              >
                {tools.map((tool) => {
                  const compoundId = `${tool.serverSlug}:${tool.id}`;
                  const isChecked = resources.includes(compoundId);
                  return (
                    <button
                      key={compoundId}
                      type="button"
                      onClick={() => onToggle(compoundId)}
                      className={cn(
                        "flex w-full items-center gap-2 px-3 py-2 text-sm hover:bg-accent cursor-pointer",
                        isChecked && "font-medium",
                      )}
                    >
                      <Checkbox
                        checked={isChecked}
                        className="pointer-events-none focus-visible:ring-0 focus-visible:border-input"
                        tabIndex={-1}
                      />
                      <span className="truncate flex-1 text-left">
                        {tool.name}
                      </span>
                      <span className="text-[10px] text-muted-foreground opacity-50 shrink-0">
                        {tool.serverName}
                      </span>
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        );
      })}
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
  name: string;
  checked: boolean;
  onToggle: (id: string) => void;
  compact?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={() => onToggle(id)}
      className={cn(
        "flex w-full items-center gap-2 px-3 hover:bg-accent cursor-pointer",
        compact ? "text-sm rounded-none h-10" : "text-sm rounded-sm py-2",
        checked && "font-medium",
      )}
    >
      <Checkbox
        checked={checked}
        className="pointer-events-none focus-visible:ring-0 focus-visible:border-input"
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
        "flex w-full items-center gap-2 rounded-sm px-3 py-2 text-sm hover:bg-accent cursor-pointer",
        selected && "font-medium",
      )}
    >
      <span className="w-4 flex items-center justify-center shrink-0">
        {selected && <Check className="h-3.5 w-3.5" />}
      </span>
      <span>{label}</span>
    </button>
  );
}

function getLabel(
  resourceType: ResourceType,
  resources: string[] | null,
  customMode?: boolean,
  customToolCount?: number,
): string {
  if (customMode) {
    const count = customToolCount ?? 0;
    if (count === 0) return "Select...";
    return `${count} tool${count === 1 ? "" : "s"} selected`;
  }
  if (resources === null) {
    return resourceType === "project" ? "All projects" : "All servers";
  }
  if (resources.length === 0) return "Select...";
  const noun = resourceType === "project" ? "project" : "server";
  return `${resources.length} ${noun}${resources.length === 1 ? "" : "s"} selected`;
}
