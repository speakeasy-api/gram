import { Checkbox } from "@/components/ui/checkbox";
import { Dialog } from "@/components/ui/dialog";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import { cn } from "@/lib/utils";
import { useListCollections } from "@gram/client/react-query/listCollections.js";
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
  Loader2,
  SquareLibrary,
  Tag,
  Wrench,
  X,
} from "lucide-react";
import { useCallback, useMemo, useRef, useState } from "react";
import { useQueries } from "@tanstack/react-query";
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
  const { data } = useListToolsetsForOrg(undefined, { enabled });

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
        mcpSlug: t.mcpSlug ?? undefined,
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

  const scopeOptions = (
    <div className="shrink-0 pb-1.5">
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
          resourceType === "project" ? "Specific projects" : "Specific servers"
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
  );

  const resourceList = !isUnrestricted && !customMode && (
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
  );

  const customTabs = (toolScrollClass?: string) => (
    <Tabs
      value={customTab ?? "select"}
      className="flex min-h-0 flex-1 flex-col gap-0"
      onValueChange={(value) => {
        onChangeResources([]);
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
      <TabsContent
        value="select"
        className="flex min-h-[200px] flex-1 flex-col p-0"
      >
        <ToolSelectionPanel
          mcpServers={mcpServers}
          resources={resources ?? []}
          onToggle={toggleResource}
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
        className="min-h-[200px] flex-1 overflow-y-auto px-2 py-1"
      >
        <HttpMethodGroupPanel
          mcpServers={mcpServers}
          resources={resources ?? []}
          onChangeResources={onChangeResources}
        />
      </TabsContent>
      <TabsContent
        value="collection"
        className="min-h-[200px] flex-1 overflow-y-auto px-2 py-1"
      >
        <CollectionGroupPanel
          mcpServers={mcpServers}
          resources={resources ?? []}
          onChangeResources={onChangeResources}
        />
      </TabsContent>
    </Tabs>
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
            customMode ? "w-[620px]" : "max-h-[300px] w-44 overflow-y-auto",
          )}
          style={{
            transitionTimingFunction: "cubic-bezier(0.32, 0.72, 0, 1)",
          }}
        >
          {scopeOptions}
          {resourceList}
          {customMode && (
            <div className="-mx-1.5 -mb-1.5 flex max-h-[min(420px,60vh)] flex-col">
              {customTabs("max-h-[min(340px,50vh)]")}
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
          <div className="flex min-h-0 flex-1 flex-col overflow-hidden p-1.5">
            {scopeOptions}
            {customTabs()}
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
  className,
}: {
  mcpServers: ServerGroup[];
  resources: string[];
  onToggle: (id: string) => void;
  className?: string;
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

  return (
    <div className={cn("flex min-h-0 flex-1", className)}>
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
          className="tool-scroll min-h-0 flex-1 overflow-y-auto pb-2"
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
  onChangeResources,
}: {
  mcpServers: ServerGroup[];
  resources: string[];
  onChangeResources: (resources: string[]) => void;
}) {
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
        Select all tools by HTTP method:
      </div>
      {[...toolsByMethod.entries()].map(([method, tools]) => {
        const compoundIds = tools.map((t) => `${t.serverSlug}:${t.name}`);
        const allSelected =
          compoundIds.length > 0 &&
          compoundIds.every((id) => resources.includes(id));
        const colors =
          METHOD_COLORS[method] ?? "text-muted-foreground bg-muted";

        const toggleAll = () => {
          if (allSelected) {
            const removeSet = new Set(compoundIds);
            onChangeResources(resources.filter((r) => !removeSet.has(r)));
          } else {
            const existing = new Set(resources);
            const toAdd = compoundIds.filter((id) => !existing.has(id));
            onChangeResources([...resources, ...toAdd]);
          }
        };

        return (
          <button
            key={method}
            type="button"
            onClick={toggleAll}
            className="hover:bg-accent flex w-full cursor-pointer items-center gap-3 rounded-sm px-3 py-2.5 text-sm"
          >
            <Checkbox
              checked={allSelected}
              className="focus-visible:border-input pointer-events-none focus-visible:ring-0"
              tabIndex={-1}
            />
            <span
              className={cn(
                "inline-flex min-w-[52px] items-center justify-center rounded px-1.5 py-0.5 text-[10px] font-bold tracking-wide",
                colors,
              )}
            >
              {method}
            </span>
            <span className="flex-1 text-left">
              {tools.length} tool{tools.length !== 1 ? "s" : ""}
            </span>
          </button>
        );
      })}
    </div>
  );
}

function CollectionGroupPanel({
  mcpServers,
  resources,
  onChangeResources,
}: {
  mcpServers: ServerGroup[];
  resources: string[];
  onChangeResources: (resources: string[]) => void;
}) {
  const client = useSdkClient();

  // Fetch org-level collections
  const { data: collectionsData, isLoading: collectionsLoading } =
    useListCollections({}, undefined);
  const collections = useMemo(
    () => collectionsData?.collections ?? [],
    [collectionsData?.collections],
  );

  // Fetch servers for each collection in parallel
  const serverQueries = useQueries({
    queries: collections.map((c) => ({
      queryKey: ["collections", "listServers", c.slug],
      queryFn: () =>
        client.collections.listServers({
          collectionSlug: c.slug!,
        }),
      enabled: !!c.slug,
    })),
  });

  // Build mcpSlug → Server lookup from the already-loaded toolset data
  const mcpSlugToServer = useMemo(() => {
    const map = new Map<string, Server>();
    for (const group of mcpServers) {
      for (const server of group.servers) {
        if (server.mcpSlug) {
          map.set(server.mcpSlug, server);
        }
      }
    }
    return map;
  }, [mcpServers]);

  // Resolve each collection's servers to internal toolset servers with tools
  const collectionGroups = useMemo(() => {
    return collections.map((c, i) => {
      const serversResponse = serverQueries[i]?.data;
      const externalServers = serversResponse?.servers ?? [];

      const matchedServers: Server[] = [];
      for (const es of externalServers) {
        const parts = es.registrySpecifier.split("/");
        const mcpSlug = parts[parts.length - 1];
        const server = mcpSlugToServer.get(mcpSlug);
        if (server) matchedServers.push(server);
      }

      return {
        id: c.id,
        name: c.name,
        slug: c.slug,
        servers: matchedServers,
      };
    });
  }, [collections, serverQueries, mcpSlugToServer]);

  if (collectionsLoading) {
    return (
      <div className="flex items-center justify-center py-6">
        <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
      </div>
    );
  }

  if (collections.length === 0) {
    return (
      <div className="text-muted-foreground py-6 text-center text-sm">
        No collections found
      </div>
    );
  }

  return (
    <div className="py-1">
      <div className="text-muted-foreground px-2 py-2 text-sm">
        Select all tools by collection:
      </div>
      {collectionGroups.map((group) => {
        const allToolIds = group.servers.flatMap((s) =>
          s.tools.map((t) => `${s.id}:${t.name}`),
        );
        const allSelected =
          allToolIds.length > 0 &&
          allToolIds.every((id) => resources.includes(id));

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
              {allToolIds.length} tool{allToolIds.length !== 1 ? "s" : ""}
            </span>
          </button>
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
