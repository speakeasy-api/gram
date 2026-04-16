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
  Minimize2,
  SquareAsterisk,
  Globe,
  Repeat,
  Shield,
  SquareLibrary,
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
  const customToolCount = useMemo(() => {
    if (!customMode) return 0;
    return (resources ?? []).length;
  }, [customMode, resources]);

  // Org-scoped permissions have no resource picker — they're always org-wide
  if (resourceType === "org") {
    return (
      <span className="border-input text-muted-foreground inline-flex h-7 items-center rounded-md border bg-transparent px-2 py-1 text-xs">
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
          <div className="bg-border my-1 h-px" />
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
                  <div className="text-muted-foreground px-3 py-1.5 text-xs font-medium">
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
            className="-mx-1.5 -mb-1.5 flex min-h-0 flex-1 flex-col gap-0"
            onValueChange={(value) => {
              onChangeResources([]);
              onChangeAnnotations?.([]);
              onCustomTabChange?.(value as CustomTab);
            }}
          >
            <TabsList className="border-border h-auto w-full gap-2 rounded-none border-y bg-transparent px-1.5 py-1.5">
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
              <div className="bg-border/40 my-1 w-px self-stretch" />
              <TabsTrigger
                value="http-method"
                className="text-muted-foreground hover:bg-muted/50 data-[state=active]:bg-muted data-[state=active]:text-foreground h-auto rounded-sm border-none px-3 py-2 text-sm shadow-none data-[state=active]:shadow-none"
              >
                <SquareAsterisk className="h-3.5 w-3.5" />
                By HTTP method
              </TabsTrigger>
              <div className="bg-border/40 my-1 w-px self-stretch" />
              <TabsTrigger
                value="collection"
                className="text-muted-foreground hover:bg-muted/50 data-[state=active]:bg-muted data-[state=active]:text-foreground h-auto rounded-sm border-none px-3 py-2 text-sm shadow-none data-[state=active]:shadow-none"
              >
                <SquareLibrary className="h-3.5 w-3.5" />
                By collection
              </TabsTrigger>
            </TabsList>
            <TabsContent value="select" className="min-h-0 flex-1 p-0">
              <ToolSelectionPanel
                mcpServers={mcpServers}
                resources={resources ?? []}
                onToggle={toggleResource}
              />
            </TabsContent>
            <TabsContent
              value="auto-groups"
              className="min-h-0 flex-1 overflow-y-auto px-2 py-1"
            >
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
            <TabsContent
              value="http-method"
              className="min-h-0 flex-1 overflow-y-auto px-2 py-1"
            >
              <HttpMethodGroupPanel
                mcpServers={mcpServers}
                resources={resources ?? []}
                onToggle={toggleResource}
                onChangeResources={onChangeResources}
              />
            </TabsContent>
            <TabsContent
              value="collection"
              className="min-h-0 flex-1 overflow-y-auto px-2 py-1"
            >
              <CollectionGroupPanel
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
            "overflow-hidden p-1.5 transition-[width] duration-500",
            customMode ? "w-[620px]" : "max-h-[300px] w-56 overflow-y-auto",
          )}
          style={{
            transitionTimingFunction: "cubic-bezier(0.32, 0.72, 0, 1)",
          }}
        >
          {pickerContent}
          {isMcp && (
            <div className="border-border -mx-1.5 mt-1 -mb-1.5 rounded-b-lg border-t">
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
          <div className="flex min-h-0 flex-1 flex-col overflow-hidden p-1.5 [&_.tool-scroll]:max-h-none [&_.tool-scroll]:min-h-0 [&_.tool-scroll]:flex-1">
            {pickerContent}
          </div>
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
    <div className="flex h-full">
      {/* Left column — server list */}
      <div className="border-border w-[160px] shrink-0 overflow-y-auto border-r">
        <div className="bg-muted/50 text-muted-foreground border-border flex h-10 items-center gap-1.5 border-b px-3 text-[10px] font-medium tracking-wider uppercase">
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
                "hover:bg-muted/50 flex h-10 w-full cursor-pointer items-center justify-between truncate px-3 text-sm",
                isActive && "bg-muted font-medium",
              )}
            >
              <span className="truncate">{server.name}</span>
              {isActive && (
                <ChevronRight className="text-muted-foreground h-3 w-3 shrink-0" />
              )}
            </button>
          );
        })}
      </div>

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
        <div
          ref={scrollRef}
          onWheel={handleWheel}
          className="tool-scroll max-h-[300px] min-h-[300px] overflow-y-auto pb-2"
        >
          {filteredTools.length === 0 ? (
            <div className="text-muted-foreground px-3 py-3 text-sm">
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
      <div className="text-muted-foreground px-2 py-2 text-sm">
        Grant access to all tools matching selected annotations:
      </div>
      {ANNOTATION_OPTIONS.map((opt) => {
        const isSelected = annotations.includes(opt.key);
        const isExpanded = expanded.has(opt.key);
        const matchedTools = toolsByAnnotation.get(opt.key) ?? [];
        const Icon = opt.icon;
        return (
          <div key={opt.key} className="hover:bg-accent rounded-sm">
            <button
              type="button"
              onClick={() => toggle(opt.key)}
              className={cn(
                "flex w-full cursor-pointer items-center gap-3 px-3 py-2.5 text-sm outline-none",
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
            </button>
            {isSelected && (
              <div className="pr-3 pb-3 pl-[66px]">
                {matchedTools.length === 0 ? (
                  <span className="text-muted-foreground text-[11px]">
                    No tools matched
                  </span>
                ) : (
                  <div className="border-border bg-background overflow-hidden rounded-md border">
                    <button
                      type="button"
                      onClick={() => toggleExpanded(opt.key)}
                      className="text-muted-foreground hover:text-foreground flex w-full cursor-pointer items-center gap-1 px-2.5 py-1.5 text-xs"
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
                        className="border-border bg-popover max-h-[120px] overflow-y-auto border-t"
                      >
                        {matchedTools.map((tool) => (
                          <div
                            key={`${tool.serverName}:${tool.id}`}
                            className="text-muted-foreground border-border flex items-center justify-between gap-2 border-b px-2.5 py-1.5 text-xs last:border-b-0"
                          >
                            <span className="truncate">{tool.name}</span>
                            <span className="shrink-0 text-[10px] opacity-50">
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
      <div className="text-muted-foreground py-6 text-center text-sm">
        No HTTP tools found
      </div>
    );
  }

  return (
    <div className="py-1">
      <div className="text-muted-foreground px-2 py-2 text-sm">
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
          <div key={method} className="hover:bg-accent rounded-sm">
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
                  "inline-flex min-w-[52px] items-center justify-center rounded px-1.5 py-0.5 text-[10px] font-bold tracking-wide",
                  colors,
                )}
              >
                {method}
              </span>
              <button
                type="button"
                onClick={() => toggleExpanded(method)}
                className="flex flex-1 cursor-pointer items-center gap-2"
              >
                <span className="text-muted-foreground flex-1 text-left font-normal">
                  {selectedCount} of {tools.length} selected
                </span>
                <ChevronRight
                  className={cn(
                    "text-muted-foreground h-3.5 w-3.5 transition-transform",
                    isExpanded && "rotate-90",
                  )}
                />
              </button>
            </div>
            {isExpanded && (
              <div
                ref={scrollRef}
                onWheel={handleWheel}
                className="border-border bg-background max-h-[180px] overflow-y-auto border-t"
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
                        "hover:bg-accent flex w-full cursor-pointer items-center gap-2 px-3 py-2 text-sm",
                        isChecked && "font-medium",
                      )}
                    >
                      <Checkbox
                        checked={isChecked}
                        className="focus-visible:border-input pointer-events-none focus-visible:ring-0"
                        tabIndex={-1}
                      />
                      <span className="flex-1 truncate text-left">
                        {tool.name}
                      </span>
                      <span className="text-muted-foreground shrink-0 text-[10px] opacity-50">
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

function CollectionGroupPanel({
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

  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const toggleExpanded = (projectId: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(projectId)) next.delete(projectId);
      else next.add(projectId);
      return next;
    });
  };

  if (mcpServers.length === 0) {
    return (
      <div className="text-muted-foreground py-6 text-center text-sm">
        No collections found
      </div>
    );
  }

  return (
    <div className="py-1">
      <div className="text-muted-foreground px-2 py-2 text-sm">
        Select tools by collection:
      </div>
      {mcpServers.map((group) => {
        const isExpanded = expanded.has(group.projectId);
        const allToolIds = group.servers.flatMap((s) =>
          s.tools.map((t) => `${s.id}:${t.name}`),
        );
        const selectedCount = allToolIds.filter((id) =>
          resources.includes(id),
        ).length;
        const allSelected =
          selectedCount === allToolIds.length && allToolIds.length > 0;

        const toggleAll = () => {
          if (allSelected) {
            const removeSet = new Set(allToolIds);
            onChangeResources(resources.filter((r) => !removeSet.has(r)));
          } else {
            const existing = new Set(resources);
            const toAdd = allToolIds.filter((id) => !existing.has(id));
            onChangeResources([...resources, ...toAdd]);
          }
        };

        return (
          <div key={group.projectId} className="hover:bg-accent rounded-sm">
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
              <button
                type="button"
                onClick={() => toggleExpanded(group.projectId)}
                className="flex min-w-0 flex-1 cursor-pointer items-center gap-2"
              >
                <span className="truncate font-medium">
                  {group.projectName}
                </span>
                <span className="text-muted-foreground font-normal">
                  {selectedCount} of {allToolIds.length} selected
                </span>
                <ChevronRight
                  className={cn(
                    "text-muted-foreground ml-auto h-3.5 w-3.5 shrink-0 transition-transform",
                    isExpanded && "rotate-90",
                  )}
                />
              </button>
            </div>
            {isExpanded && (
              <div
                ref={scrollRef}
                onWheel={handleWheel}
                className="border-border bg-background max-h-[180px] overflow-y-auto border-t"
              >
                {group.servers.map((server) => (
                  <div key={server.id}>
                    <div className="text-muted-foreground bg-muted/30 px-3 py-1.5 text-[10px] font-medium tracking-wider uppercase">
                      {server.name}
                    </div>
                    {server.tools.map((tool) => {
                      const compoundId = `${server.id}:${tool.name}`;
                      const isChecked = resources.includes(compoundId);
                      return (
                        <button
                          key={compoundId}
                          type="button"
                          onClick={() => onToggle(compoundId)}
                          className={cn(
                            "hover:bg-accent flex w-full cursor-pointer items-center gap-2 px-3 py-2 text-sm",
                            isChecked && "font-medium",
                          )}
                        >
                          <Checkbox
                            checked={isChecked}
                            className="focus-visible:border-input pointer-events-none focus-visible:ring-0"
                            tabIndex={-1}
                          />
                          <span className="flex-1 truncate text-left">
                            {tool.name}
                          </span>
                        </button>
                      );
                    })}
                  </div>
                ))}
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
