import { AlertTriangle, Check, ChevronRight, Wrench, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";

import { Checkbox } from "@/components/ui/Checkbox";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";

import { ANNOTATION_OPTIONS, type ToolAnnotation } from "./annotations";

export type { ToolAnnotation };

export type ToolSelectionMode = "annotations" | "tools";

export interface ToolSelectionTool {
  name: string;
  annotations: ToolAnnotation[];
}

export interface ToolSelectionServer {
  id: string;
  name: string;
  /** Muted prefix rendered before the name (e.g. "project/"). */
  namePrefix?: string;
  tools: ToolSelectionTool[];
  status: "loading" | "ready" | "error" | "unavailable";
  /** Trailing label for collapsed rows whose tools load lazily on expand. */
  collapsedLabel?: string;
  /** Trailing label when ready with zero tools. */
  emptyLabel?: string;
  /** Expanded body when ready with zero tools. */
  emptyContent?: React.ReactNode;
  unavailableLabel?: string;
  unavailableTooltip?: React.ReactNode;
  onRetry?: () => void;
}

export interface ToolSelectionToolRef {
  serverId: string;
  toolName: string;
}

/**
 * Every change carries the full normalized next selection. Annotation and
 * manual tool selection are mutually exclusive: toggling one axis clears the
 * other. The `kind` discriminant lets adapters that keep their own selection
 * encoding replay the exact toggle instead of the normalized state.
 */
export type ToolSelectionChange = {
  mode: ToolSelectionMode;
  annotations: ToolAnnotation[];
  tools: ToolSelectionToolRef[];
} & (
  | { kind: "annotation-toggle"; annotation: ToolAnnotation }
  | {
      kind: "tool-toggle";
      serverId: string;
      toolName: string;
      selected: boolean;
    }
  | {
      kind: "tool-batch";
      serverId: string;
      toolNames: string[];
      selected: boolean;
    }
);

export interface ToolSelectionPanelProps {
  servers: ToolSelectionServer[];
  mode: ToolSelectionMode;
  selectedAnnotations: readonly ToolAnnotation[];
  selectedTools: readonly ToolSelectionToolRef[];
  onSelectionChange: (change: ToolSelectionChange) => void;
  annotationSelectionSupported?: boolean;
  annotationsDescription?: React.ReactNode;
  toolsDescription?: React.ReactNode;
  /** Overrides the counts derived from `servers` (e.g. deploy-time-only counts). */
  toolCountByAnnotation?: ReadonlyMap<ToolAnnotation, number>;
  searchPlaceholder?: string;
  /** "By server" reads oddly when the panel holds one server's tools. */
  toolsTabLabel?: string;
  /**
   * Drop the server header when there is only one: a row that names the
   * server you are already looking at, and expands the only group there is,
   * is a click between you and the tools.
   */
  flattenSingleServer?: boolean;
  onExpandedServersChange?: (serverIds: string[]) => void;
  className?: string;
}

export function ToolSelectionPanel({
  servers,
  mode,
  selectedAnnotations,
  selectedTools,
  onSelectionChange,
  annotationSelectionSupported = true,
  annotationsDescription,
  toolsDescription,
  toolCountByAnnotation,
  searchPlaceholder = "Search tools and servers…",
  toolsTabLabel = "By server",
  flattenSingleServer = false,
  onExpandedServersChange,
  className,
}: ToolSelectionPanelProps): JSX.Element {
  const [search, setSearch] = useState("");
  const [expandedServers, setExpandedServers] = useState<ReadonlySet<string>>(
    () => new Set(servers.length === 1 ? [servers[0]!.id] : []),
  );

  useEffect(() => {
    onExpandedServersChange?.([...expandedServers]);
  }, [expandedServers, onExpandedServersChange]);

  const toggleExpanded = useCallback((serverId: string) => {
    setExpandedServers((prev) =>
      prev.has(serverId) ? new Set() : new Set([serverId]),
    );
  }, []);

  const q = search.toLowerCase();

  const annotationCounts = useMemo(() => {
    if (toolCountByAnnotation) return toolCountByAnnotation;
    const counts = new Map<ToolAnnotation, number>();
    for (const server of servers) {
      for (const tool of server.tools) {
        for (const annotation of tool.annotations) {
          counts.set(annotation, (counts.get(annotation) ?? 0) + 1);
        }
      }
    }
    return counts;
  }, [servers, toolCountByAnnotation]);

  const toggleAnnotation = (annotation: ToolAnnotation) => {
    const has = selectedAnnotations.includes(annotation);
    const annotations = has
      ? selectedAnnotations.filter((a) => a !== annotation)
      : [...selectedAnnotations, annotation];
    onSelectionChange({
      kind: "annotation-toggle",
      annotation,
      mode: "annotations",
      annotations,
      tools: [],
    });
    if (annotations.length > 0) {
      setExpandedServers(new Set());
      setSearch("");
    }
  };

  const toggleTool = (serverId: string, toolName: string) => {
    const has = selectedTools.some(
      (t) => t.serverId === serverId && t.toolName === toolName,
    );
    const tools = has
      ? selectedTools.filter(
          (t) => !(t.serverId === serverId && t.toolName === toolName),
        )
      : [...selectedTools, { serverId, toolName }];
    onSelectionChange({
      kind: "tool-toggle",
      serverId,
      toolName,
      selected: !has,
      mode: "tools",
      annotations: [],
      tools,
    });
  };

  const batchToggleTools = (
    serverId: string,
    toolNames: string[],
    selected: boolean,
  ) => {
    let tools: ToolSelectionToolRef[];
    if (selected) {
      const existing = new Set(
        selectedTools
          .filter((t) => t.serverId === serverId)
          .map((t) => t.toolName),
      );
      tools = [
        ...selectedTools,
        ...toolNames
          .filter((name) => !existing.has(name))
          .map((name) => ({ serverId, toolName: name })),
      ];
    } else {
      const removed = new Set(toolNames);
      tools = selectedTools.filter(
        (t) => !(t.serverId === serverId && removed.has(t.toolName)),
      );
    }
    onSelectionChange({
      kind: "tool-batch",
      serverId,
      toolNames,
      selected,
      mode: "tools",
      annotations: [],
      tools,
    });
  };

  const filteredServers = useMemo(() => {
    if (!q) return servers;
    return servers
      .map((server) => {
        // A server-name/prefix match keeps the full tool list — the user
        // found the server, not a subset of its tools.
        if (
          server.name.toLowerCase().includes(q) ||
          (server.namePrefix ?? "").toLowerCase().includes(q)
        ) {
          return server;
        }
        return {
          ...server,
          tools: server.tools.filter((t) => t.name.toLowerCase().includes(q)),
        };
      })
      .filter((s) => s.tools.length > 0);
  }, [servers, q]);

  useEffect(() => {
    if (q) {
      setExpandedServers(new Set(filteredServers.map((s) => s.id)));
    }
  }, [q, filteredServers]);

  const annotationSectionVisible = annotationSelectionSupported;
  // Whether any annotation is actually counted, not whether tools happen to be
  // loaded: a server that resolves its tools per caller reports no counts, and
  // expanding one must not start hiding the chips it can still be narrowed by.
  const hasAnnotationCounts = Array.from(annotationCounts.values()).some(
    (count) => count > 0,
  );

  // Annotation and per-tool selection are alternatives, not a stack: showing
  // both meant the panel scrolled past the choice you were making. One switch,
  // one visible pane, no "or" divider.
  const [pane, setPane] = useState<"annotations" | "tools">(() => {
    if (!annotationSelectionSupported) return "tools";
    if (mode === "annotations") return "annotations";
    if (mode === "tools" || selectedTools.length > 0) return "tools";
    return selectedAnnotations.length > 0 ? "annotations" : "tools";
  });
  const activePane = annotationSectionVisible ? pane : "tools";

  return (
    <div className={cn("flex min-h-0 flex-1 flex-col", className)}>
      {annotationSectionVisible && (
        <div className="border-border flex shrink-0 items-center gap-1 border-b px-3">
          {[
            { key: "annotations" as const, label: "By annotation" },
            { key: "tools" as const, label: toolsTabLabel },
          ].map((option) => (
            <button
              key={option.key}
              type="button"
              onClick={() => setPane(option.key)}
              className={cn(
                "text-muted-foreground relative px-1 py-2.5 font-mono text-[11px] tracking-[0.08em] uppercase",
                activePane === option.key &&
                  "text-foreground after:bg-primary after:absolute after:right-0 after:bottom-0 after:left-0 after:h-0.5",
                option.key === "tools" && "ml-4",
              )}
            >
              {option.label}
              {option.key === "annotations" && selectedAnnotations.length > 0
                ? ` (${selectedAnnotations.length})`
                : ""}
              {option.key === "tools" && selectedTools.length > 0
                ? ` (${selectedTools.length})`
                : ""}
            </button>
          ))}
        </div>
      )}

      <div className="min-h-0 flex-1 overflow-y-auto">
        {activePane === "annotations" ? (
          <div>
            {annotationsDescription && (
              <div className="text-muted-foreground/70 px-3 pt-3 pb-2 text-xs leading-snug">
                {annotationsDescription}
              </div>
            )}
            {/* One row per annotation, saying what picking it would cover: a
                wall of chips made the reader guess. The marker follows the
                filter bar's dimension squares. */}
            <div className="border-border divide-border mx-3 mt-3 divide-y border">
              {ANNOTATION_OPTIONS.map((opt) => {
                const isActive = selectedAnnotations.includes(opt.key);
                const count = annotationCounts.get(opt.key) ?? 0;
                // Counts come from a deploy-time catalogue. A server that
                // resolves its tools per caller has none, and hiding every
                // row would leave the pane empty even though annotations are
                // exactly what such a server can be narrowed by.
                if (count === 0 && hasAnnotationCounts) return null;
                return (
                  <button
                    key={opt.key}
                    type="button"
                    onClick={() => toggleAnnotation(opt.key)}
                    role="checkbox"
                    aria-checked={isActive}
                    className={cn(
                      "hover:bg-muted/40 flex w-full items-center gap-3 px-3 py-2.5 text-left transition-colors",
                      isActive && "bg-muted/60",
                    )}
                  >
                    <span
                      aria-hidden="true"
                      className={cn(
                        "border-primary flex size-4 shrink-0 items-center justify-center border",
                        isActive && "bg-primary text-primary-foreground",
                      )}
                    >
                      {isActive && <Check className="size-3" />}
                    </span>
                    <span
                      aria-hidden="true"
                      className={cn(
                        "size-2 shrink-0",
                        !isActive && "opacity-40",
                      )}
                      style={{ backgroundColor: opt.color }}
                    />
                    <span className="min-w-0 flex-1">
                      <span className="block text-sm">{opt.label}</span>
                      <span className="text-muted-foreground block text-xs">
                        {opt.description}
                      </span>
                    </span>
                    {count > 0 && (
                      <span className="text-muted-foreground shrink-0 text-xs">
                        {count} tools
                      </span>
                    )}
                  </button>
                );
              })}
            </div>
          </div>
        ) : (
          <div>
            {toolsDescription && (
              <div className="text-muted-foreground/70 px-3 pt-3 pb-2 text-xs leading-snug">
                {toolsDescription}
              </div>
            )}

            <div className="flex items-center gap-2 px-3 pt-3 pb-3">
              <div className="border-input flex h-9 flex-1 items-center gap-2 border px-2.5">
                <Wrench className="text-muted-foreground h-3 w-3 shrink-0" />
                <input
                  type="text"
                  placeholder={searchPlaceholder}
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

            <div className="border-border mx-3 mb-3 border">
              {filteredServers.length === 0 ? (
                <div className="text-muted-foreground px-3 py-3 text-sm">
                  {servers.length === 0
                    ? "No servers found"
                    : "No matching tools or servers"}
                </div>
              ) : (
                filteredServers.map((server) => (
                  <ServerRow
                    key={server.id}
                    server={server}
                    selectedTools={selectedTools}
                    query={q}
                    isExpanded={expandedServers.has(server.id)}
                    onToggleExpanded={toggleExpanded}
                    onToggleTool={toggleTool}
                    onBatchToggleTools={batchToggleTools}
                    flat={flattenSingleServer && servers.length === 1}
                  />
                ))
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function rowCountLabel(
  server: ToolSelectionServer,
  isExpanded: boolean,
  selectedCount: number,
  total: number,
): string {
  if (server.collapsedLabel && !isExpanded) {
    return selectedCount > 0
      ? `${selectedCount} selected`
      : server.collapsedLabel;
  }
  if (server.status === "loading") return "Loading…";
  if (server.status === "error") return "Couldn't load";
  if (server.status === "ready" && total === 0 && server.emptyLabel) {
    return server.emptyLabel;
  }
  if (selectedCount > 0) return `${selectedCount} of ${total} selected`;
  return `${total} ${total === 1 ? "tool" : "tools"} available`;
}

function ServerRow({
  server,
  selectedTools,
  query,
  isExpanded,
  onToggleExpanded,
  onToggleTool,
  onBatchToggleTools,
  flat = false,
}: {
  server: ToolSelectionServer;
  selectedTools: readonly ToolSelectionToolRef[];
  query: string;
  isExpanded: boolean;
  onToggleExpanded: (serverId: string) => void;
  onToggleTool: (serverId: string, toolName: string) => void;
  onBatchToggleTools: (
    serverId: string,
    toolNames: string[],
    select: boolean,
  ) => void;
  /** Render the tools without the server header above them. */
  flat?: boolean;
}): JSX.Element {
  const serverTools = useMemo(
    () => server.tools.slice().sort((a, b) => a.name.localeCompare(b.name)),
    [server.tools],
  );

  const q = query.toLowerCase();

  const selectedCount = serverTools.filter((tool) =>
    selectedTools.some(
      (t) => t.serverId === server.id && t.toolName === tool.name,
    ),
  ).length;
  const total = serverTools.length;
  const allSelected =
    total > 0 &&
    serverTools.every((t) =>
      selectedTools.some(
        (s) => s.serverId === server.id && s.toolName === t.name,
      ),
    );
  const someSelected = selectedCount > 0 && !allSelected;

  const isLoading = server.status === "loading";
  const isError = server.status === "error";
  const showEmpty = server.status === "ready" && total === 0;

  if (server.status === "unavailable") {
    const row = (
      <div
        tabIndex={0}
        aria-disabled="true"
        className="border-border focus-visible:ring-ring flex cursor-not-allowed items-center border-b px-3 py-2.5 text-sm opacity-50 focus-visible:ring-1 focus-visible:outline-none last:border-b-0"
      >
        <span className="min-w-0 flex-1 truncate">
          {server.namePrefix && (
            <HighlightMatch
              text={server.namePrefix}
              query={q}
              className="text-muted-foreground/60"
            />
          )}
          <HighlightMatch
            text={server.name}
            query={q}
            className="font-medium"
          />
        </span>
        <span className="text-muted-foreground shrink-0 text-xs">
          {server.unavailableLabel ?? "dynamic tools"}
        </span>
      </div>
    );
    if (!server.unavailableTooltip) return row;
    return (
      <Tooltip>
        <TooltipTrigger asChild>{row}</TooltipTrigger>
        <TooltipContent side="bottom" className="max-w-xs">
          {server.unavailableTooltip}
        </TooltipContent>
      </Tooltip>
    );
  }

  let expandedBody: React.ReactNode;
  if (isLoading) {
    expandedBody = (
      <div className="text-muted-foreground px-8 py-3 text-sm">
        Loading tools…
      </div>
    );
  } else if (isError) {
    expandedBody = (
      <div className="text-muted-foreground space-y-1 px-8 py-3 text-sm">
        <p className="flex items-center gap-1.5">
          <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
          Couldn&apos;t load this server&apos;s tools.
        </p>
        {server.onRetry && (
          <button
            type="button"
            onClick={server.onRetry}
            className="text-primary hover:underline"
          >
            Retry
          </button>
        )}
      </div>
    );
  } else if (showEmpty && server.emptyContent) {
    expandedBody = server.emptyContent;
  } else if (total === 0) {
    expandedBody = (
      <div className="text-muted-foreground px-8 py-3 text-sm">
        No tools available
      </div>
    );
  } else {
    expandedBody = serverTools.map((tool) => {
      const isSelected = selectedTools.some(
        (s) => s.serverId === server.id && s.toolName === tool.name,
      );
      return (
        <button
          key={tool.name}
          type="button"
          onClick={() => onToggleTool(server.id, tool.name)}
          className={cn(
            "hover:bg-accent flex w-full cursor-pointer items-center gap-2 py-1.5 pr-3 text-sm",
            // Tools sit under their server's chevron. A flattened single
            // server has no header above them, so the indent would leave
            // them floating away from the edge they belong to.
            flat ? "pl-3" : "pl-8",
          )}
        >
          <Checkbox
            checked={isSelected}
            className="focus-visible:border-input pointer-events-none focus-visible:ring-0"
            tabIndex={-1}
          />
          <HighlightMatch text={tool.name} query={q} className="truncate" />
        </button>
      );
    });
  }

  if (flat) {
    return (
      <div>
        {total > 0 && (
          <div className="border-border text-muted-foreground flex items-center justify-between border-b px-3 py-2 text-xs">
            <span>
              {selectedCount > 0
                ? `${selectedCount} of ${total} selected`
                : `${total} tools`}
            </span>
            {total > 0 && (
              <button
                type="button"
                onClick={() =>
                  onBatchToggleTools(
                    server.id,
                    serverTools.map((t) => t.name),
                    !allSelected,
                  )
                }
                className="hover:text-foreground underline decoration-dotted underline-offset-4"
              >
                {allSelected ? "Clear all" : "Select all"}
              </button>
            )}
          </div>
        )}
        <div>{expandedBody}</div>
      </div>
    );
  }

  return (
    <div className="border-border border-b last:border-b-0">
      <div
        role="button"
        aria-expanded={isExpanded}
        tabIndex={0}
        onClick={() => onToggleExpanded(server.id)}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onToggleExpanded(server.id);
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
            {server.namePrefix && (
              <HighlightMatch
                text={server.namePrefix}
                query={q}
                className="text-muted-foreground/60"
              />
            )}
            <HighlightMatch
              text={server.name}
              query={q}
              className="font-medium"
            />
          </span>
        </div>
        <div className="flex shrink-0 items-center gap-2 pr-3">
          <span className="text-muted-foreground text-xs">
            {rowCountLabel(server, isExpanded, selectedCount, total)}
          </span>
          {total > 0 && (
            <Checkbox
              checked={
                allSelected ? true : someSelected ? "indeterminate" : false
              }
              onClick={(e) => {
                e.stopPropagation();
                onBatchToggleTools(
                  server.id,
                  serverTools.map((t) => t.name),
                  !allSelected,
                );
              }}
              className="focus-visible:border-input pointer-events-auto cursor-pointer focus-visible:ring-0"
            />
          )}
        </div>
      </div>

      {isExpanded && (
        <div className="bg-muted/30 border-border max-h-[300px] overflow-y-auto border-t">
          {expandedBody}
        </div>
      )}
    </div>
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
}): JSX.Element {
  if (!query) return <span className={className}>{text}</span>;
  const idx = text.toLowerCase().indexOf(query.toLowerCase());
  if (idx === -1) return <span className={className}>{text}</span>;
  return (
    <span className={className}>
      {text.slice(0, idx)}
      <mark className="bg-yellow-200 dark:bg-yellow-800/60">
        {text.slice(idx, idx + query.length)}
      </mark>
      {text.slice(idx + query.length)}
    </span>
  );
}
