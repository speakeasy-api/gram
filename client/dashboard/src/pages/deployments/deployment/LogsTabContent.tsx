import { Heading } from "@/components/ui/heading";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
import { dateTimeFormatters } from "@/lib/dates";
import { cn } from "@/lib/utils";
import {
  useDeploymentLogsSuspense,
  useDeploymentSuspense,
} from "@gram/client/react-query";
import { Icon, Input } from "@speakeasy-api/moonshine";
import React, {
  useCallback,
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useParams } from "react-router";
import { useDeploymentSearchParams } from "./use-deployment-search-params";

type LogLevel = "WARN" | "INFO" | "DEBUG" | "ERROR" | "OK" | "SKIP";

// Uses design system tokens where available (destructive, warning, success, muted).
// INFO/DEBUG have no semantic tokens — hardcoded Tailwind is intentional.
const levelColors = {
  INFO: { dot: "bg-blue-500", text: "text-blue-700", bg: "bg-blue-50" },
  WARN: { dot: "bg-warning", text: "text-warning", bg: "bg-warning/10" },
  ERROR: {
    dot: "bg-destructive",
    text: "text-destructive",
    bg: "bg-destructive/10",
  },
  SKIP: {
    dot: "bg-muted-foreground",
    text: "text-muted-foreground",
    bg: "bg-muted",
  },
  OK: {
    dot: "bg-success",
    text: "text-success-foreground",
    bg: "bg-success/10",
  },
  DEBUG: {
    dot: "bg-muted-foreground",
    text: "text-muted-foreground",
    bg: "bg-muted",
  },
} as const;

function getLevelColors(level: LogLevel) {
  return levelColors[level] ?? levelColors.INFO;
}

type LogFocus = "all" | "warns" | "errors" | "skipped";

interface ParsedLogEntry {
  timestamp?: string;
  level: LogLevel;
  message: string;
  source?: string;
  originalMessage: string;
  originalEvent: string;
}

const TIMESTAMP_PATTERNS = [
  /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z?)\s+(.*)$/,
  /^(\d{1,2}:\d{2}:\d{2}\.\d{3})\s+(.*)$/,
];

const LEVEL_PATTERN = /^\[?(WARN|WARNING|INFO|DEBUG|ERROR|OK)\]?\s+(.*)$/i;

function formatLogTimestamp(createdAt: string): string {
  const date = new Date(createdAt);
  return dateTimeFormatters.logTimestamp.format(date);
}

function parseLogMessage(message: string, event: string): ParsedLogEntry {
  let source: string | undefined;
  let cleanMessage = message;

  const lowerMsg = message.toLowerCase();
  const isSkipped =
    lowerMsg.includes("skip") ||
    lowerMsg.includes("skipped") ||
    lowerMsg.includes("skipping");

  let timestamp: string | undefined;
  let parsedMessage = cleanMessage;

  // Extract timestamp
  for (const pattern of TIMESTAMP_PATTERNS) {
    const match = message.match(pattern);
    if (match && match[1] && match[2]) {
      timestamp = match[1];
      parsedMessage = match[2];
      cleanMessage = match[2];
      break;
    }
  }

  // Extract level from message
  const levelMatch = parsedMessage.match(LEVEL_PATTERN);
  let parsedLevel: LogLevel = "INFO";

  if (levelMatch && levelMatch[1] && levelMatch[2]) {
    const upperLevel = levelMatch[1].toUpperCase();
    parsedLevel = (upperLevel === "WARNING" ? "WARN" : upperLevel) as LogLevel;
    parsedMessage = levelMatch[2];
  }

  if (isSkipped) {
    parsedLevel = "SKIP";
  } else {
    const lowerEvent = event.toLowerCase();
    if (lowerEvent.includes("error")) parsedLevel = "ERROR";
    else if (lowerEvent.includes("warn")) parsedLevel = "WARN";

    const lowerCleanMessage = cleanMessage.toLowerCase();
    if (
      lowerCleanMessage.includes("error") ||
      lowerCleanMessage.includes("failed")
    ) {
      parsedLevel = "ERROR";
    } else if (
      lowerCleanMessage.includes("warning") ||
      lowerCleanMessage.includes("warn")
    ) {
      parsedLevel = "WARN";
    } else if (
      lowerCleanMessage.includes("success") ||
      lowerCleanMessage.includes("complete")
    ) {
      parsedLevel = "OK";
    }
  }

  return {
    timestamp,
    level: parsedLevel,
    message: parsedMessage,
    source,
    originalMessage: message,
    originalEvent: event,
  };
}

export const LogsTabContent = ({
  deploymentId: propDeploymentId,
  embeddedMode = false,
  attachmentType,
}: {
  deploymentId?: string;
  embeddedMode?: boolean;
  attachmentType?: string;
} = {}) => {
  const { deploymentId: paramDeploymentId } = useParams();
  const deploymentId = propDeploymentId ?? paramDeploymentId!;
  const { data: deploymentLogs } = useDeploymentLogsSuspense(
    { deploymentId },
    undefined,
    {
      staleTime: Infinity,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
    },
  );

  const { data: deployment } = useDeploymentSuspense(
    { id: deploymentId },
    undefined,
    {
      staleTime: Infinity,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
    },
  );

  const [focus, setFocus] = useState<LogFocus>("all");
  const [selectedSource, setSelectedSource] = useState("all");
  const [searchQuery, setSearchQuery] = useState("");
  const [currentLogIndex, setCurrentLogIndex] = useState<number | null>(null);
  const [currentSearchIndex, setCurrentSearchIndex] = useState(0);
  const [showBottomFade, setShowBottomFade] = useState(false);
  const [isScrolled, setIsScrolled] = useState(false);
  const [searchInputFocused, setSearchInputFocused] = useState(false);

  const { searchParams, setSearchParams } = useDeploymentSearchParams();
  const [localGrouping, setLocalGrouping] = useState(false);

  const setGroupBySource = embeddedMode
    ? (value: boolean) => setLocalGrouping(value)
    : (value: boolean) => {
        setSearchParams((prev) => {
          if (prev.tab !== "logs") return prev;
          const next = { ...prev };
          if (value) next.grouping = "by_source";
          else next.grouping = undefined;
          return next;
        });
      };

  const groupBySource = React.useMemo(() => {
    if (embeddedMode) return localGrouping;
    if (searchParams.tab !== "logs") return false;
    return searchParams.grouping === "by_source";
  }, [embeddedMode, localGrouping, searchParams]);

  const logsContainerRef = useRef<HTMLDivElement>(null);
  const logRefs = useRef<Map<number, HTMLDivElement>>(new Map());

  // Build a map of attachmentId → asset name from the deployment data.
  // Log events store the deployment asset ID (not the uploaded assetId).
  const assetNameMap = useMemo(() => {
    const map = new Map<string, string>();
    for (const asset of deployment.openapiv3Assets ?? []) {
      map.set(asset.id, asset.name);
    }
    for (const asset of deployment.functionsAssets ?? []) {
      map.set(asset.id, asset.name);
    }
    for (const mcp of deployment.externalMcps ?? []) {
      if ("id" in mcp && "name" in mcp) {
        map.set(String(mcp.id), String(mcp.name));
      }
    }
    return map;
  }, [deployment]);

  // Build source filter options from individual assets in the log events
  const sourceOptions = useMemo(() => {
    const counts = new Map<string, number>();
    for (const event of deploymentLogs.events) {
      if (event.attachmentId) {
        counts.set(
          event.attachmentId,
          (counts.get(event.attachmentId) ?? 0) + 1,
        );
      }
    }
    return Array.from(counts.entries())
      .sort((a, b) => {
        const nameA = assetNameMap.get(a[0]) ?? a[0];
        const nameB = assetNameMap.get(b[0]) ?? b[0];
        return nameA.localeCompare(nameB);
      })
      .map(([id, count]) => ({
        value: id,
        label: assetNameMap.get(id) ?? id.slice(0, 8),
        count,
      }));
  }, [deploymentLogs.events, assetNameMap]);

  const activeSourceFilter =
    attachmentType ?? (selectedSource !== "all" ? selectedSource : undefined);

  const visibleEvents = useMemo(() => {
    if (!activeSourceFilter) return deploymentLogs.events;
    return deploymentLogs.events.filter(
      (event) => event.attachmentId === activeSourceFilter,
    );
  }, [deploymentLogs.events, activeSourceFilter]);

  const parsedLogs = useMemo(
    () =>
      visibleEvents.map((event) => parseLogMessage(event.message, event.event)),
    [visibleEvents],
  );

  useEffect(() => {
    setCurrentLogIndex(null);
  }, [parsedLogs]);

  const groupedLogs = useMemo(() => {
    if (!groupBySource) return null;

    const groups = new Map<
      string,
      { logs: ParsedLogEntry[]; indices: number[] }
    >();

    parsedLogs.forEach((log, index) => {
      const key = log.source || "Other";
      if (!groups.has(key)) {
        groups.set(key, { logs: [], indices: [] });
      }
      groups.get(key)!.logs.push(log);
      groups.get(key)!.indices.push(index);
    });

    return Array.from(groups.entries()).sort((a, b) =>
      a[0].localeCompare(b[0]),
    );
  }, [parsedLogs, groupBySource]);

  const deferredSearchQuery = useDeferredValue(searchQuery);

  const filteredIndices = useMemo(() => {
    if (focus === "all" && !deferredSearchQuery) return [];

    const indices: number[] = [];
    parsedLogs.forEach((log, index) => {
      const matchesFocus =
        focus === "all" ||
        (focus === "warns" && log.level === "WARN") ||
        (focus === "errors" && log.level === "ERROR") ||
        (focus === "skipped" && log.level === "SKIP");

      const matchesSearch =
        !deferredSearchQuery ||
        log.message.toLowerCase().includes(deferredSearchQuery.toLowerCase());

      if (matchesFocus && matchesSearch) {
        indices.push(index);
      }
    });

    return indices;
  }, [focus, deferredSearchQuery, parsedLogs]);

  const effectiveSearchIndex =
    filteredIndices.length > 0
      ? Math.min(currentSearchIndex, filteredIndices.length - 1)
      : 0;

  const scrollToLog = useCallback((index: number) => {
    const element = logRefs.current.get(index);
    if (element) {
      element.scrollIntoView({ behavior: "smooth", block: "center" });
      setCurrentLogIndex(index);
    }
  }, []);

  const navigateToResult = useCallback(
    (direction: "next" | "prev") => {
      if (filteredIndices.length === 0) return;

      let newIndex: number;
      if (direction === "next") {
        newIndex = (effectiveSearchIndex + 1) % filteredIndices.length;
      } else {
        newIndex =
          effectiveSearchIndex === 0
            ? filteredIndices.length - 1
            : effectiveSearchIndex - 1;
      }

      setCurrentSearchIndex(newIndex);
      const targetIndex = filteredIndices[newIndex];
      if (targetIndex !== undefined) {
        scrollToLog(targetIndex);
      }
    },
    [effectiveSearchIndex, filteredIndices, scrollToLog],
  );

  const handleFocusChange = (newFocus: LogFocus) => {
    setFocus(newFocus);
    setCurrentSearchIndex(0);

    if (newFocus !== "all") {
      const indices = parsedLogs
        .map((log, index) =>
          (newFocus === "warns" && log.level === "WARN") ||
          (newFocus === "errors" && log.level === "ERROR") ||
          (newFocus === "skipped" && log.level === "SKIP")
            ? index
            : -1,
        )
        .filter((i) => i !== -1);

      if (indices.length > 0 && indices[0] !== undefined) {
        scrollToLog(indices[0]);
      }
    } else {
      setCurrentLogIndex(null);
    }
  };

  const handleSearchChange = (query: string) => {
    setSearchQuery(query);
    setCurrentSearchIndex(0);

    if (query) {
      const firstMatch = parsedLogs.findIndex((log) =>
        log.message.toLowerCase().includes(query.toLowerCase()),
      );
      if (firstMatch !== -1) {
        scrollToLog(firstMatch);
      }
    } else {
      setCurrentLogIndex(null);
    }
  };

  useEffect(() => {
    const container = logsContainerRef.current;
    if (!container) return;

    const checkScroll = () => {
      const isScrollable = container.scrollHeight > container.clientHeight;
      const isAtBottom =
        Math.abs(
          container.scrollHeight - container.clientHeight - container.scrollTop,
        ) < 5;
      setShowBottomFade(isScrollable && !isAtBottom);
      setIsScrolled(container.scrollTop > 0);
    };

    // Check on mount and when logs change
    const timeoutId = setTimeout(checkScroll, 100);

    container.addEventListener("scroll", checkScroll);
    window.addEventListener("resize", checkScroll);

    return () => {
      clearTimeout(timeoutId);
      container.removeEventListener("scroll", checkScroll);
      window.removeEventListener("resize", checkScroll);
    };
  }, [parsedLogs.length, groupBySource]);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Check if the logs container or its children are focused
      const logsContainer = logsContainerRef.current;
      const activeElement = document.activeElement;
      const isWithinLogsSection = logsContainer?.contains(
        activeElement as Node,
      );
      const isSearchInputFocused =
        activeElement?.hasAttribute("data-search-input");

      if (e.key === "Escape") {
        // Only handle Escape if we're within the logs section
        if (isWithinLogsSection || isSearchInputFocused) {
          e.preventDefault();
          setFocus("all");
          setSearchQuery("");
          setCurrentLogIndex(null);
          const activeElement = document.activeElement as HTMLElement;
          if (activeElement && activeElement.tagName === "INPUT") {
            activeElement.blur();
          }
        }
        return;
      }

      if ((e.metaKey || e.ctrlKey) && e.key === "f") {
        // Only capture cmd-f if the logs section is already focused
        if (isWithinLogsSection || isSearchInputFocused) {
          e.preventDefault();
          const searchInput = document.querySelector<HTMLInputElement>(
            "[data-search-input]",
          );
          searchInput?.focus();
        }
        return;
      }

      const isInInput = document.activeElement?.tagName === "INPUT";
      if (!isInInput) {
        switch (e.key) {
          case "/": {
            e.preventDefault();
            const searchInput = document.querySelector<HTMLInputElement>(
              "[data-search-input]",
            );
            searchInput?.focus();
            break;
          }
          case "n":
            e.preventDefault();
            navigateToResult("next");
            break;
          case "N":
            if (e.shiftKey) {
              e.preventDefault();
              navigateToResult("prev");
            }
            break;
          case "j":
            e.preventDefault();
            if (
              currentLogIndex !== null &&
              currentLogIndex < parsedLogs.length - 1
            ) {
              scrollToLog(currentLogIndex + 1);
            } else if (currentLogIndex === null) {
              scrollToLog(0);
            }
            break;
          case "k":
            e.preventDefault();
            if (currentLogIndex !== null && currentLogIndex > 0) {
              scrollToLog(currentLogIndex - 1);
            }
            break;
          case "g":
            if (!e.shiftKey && !e.ctrlKey) {
              e.preventDefault();
              scrollToLog(0);
            } else if (e.ctrlKey) {
              e.preventDefault();
              setGroupBySource(!groupBySource);
            }
            break;
          case "G":
            if (e.shiftKey) {
              e.preventDefault();
              scrollToLog(parsedLogs.length - 1);
            }
            break;
          case "e":
            e.preventDefault();
            handleFocusChange("errors");
            break;
          case "w":
            e.preventDefault();
            handleFocusChange("warns");
            break;
          case "s":
            e.preventDefault();
            handleFocusChange("skipped");
            break;
          case "a":
            e.preventDefault();
            handleFocusChange("all");
            break;
        }
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [currentLogIndex, parsedLogs.length, navigateToResult, groupBySource]);

  const searchRegex = useMemo(() => {
    if (!searchQuery) return null;
    const escaped = searchQuery.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    return new RegExp(`(${escaped})`, "gi");
  }, [searchQuery]);

  const highlightMatch = useCallback(
    (text: string) => {
      if (!searchRegex) return text;
      const parts = text.split(searchRegex);
      return (
        <>
          {parts.map((part, i) =>
            part.toLowerCase() === searchQuery.toLowerCase() ? (
              <mark
                key={i}
                className="bg-yellow-200 dark:bg-yellow-800 text-inherit"
              >
                {part}
              </mark>
            ) : (
              part
            ),
          )}
        </>
      );
    },
    [searchQuery, searchRegex],
  );

  return (
    <>
      <Heading variant="h2" className="mb-4">
        Logs
      </Heading>

      {/* Filters row */}
      {!embeddedMode && sourceOptions.length > 0 && (
        <div className="mb-4 flex flex-wrap items-end gap-3">
          <div className="flex flex-col gap-1.5">
            <Type small muted>
              Source
            </Type>
            <Select value={selectedSource} onValueChange={setSelectedSource}>
              <SelectTrigger size="sm" className="min-w-[180px] bg-background">
                <SelectValue placeholder="All sources" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All sources</SelectItem>
                {sourceOptions.map((opt) => (
                  <SelectItem
                    key={opt.value}
                    value={opt.value}
                    description={`${opt.count} log${opt.count === 1 ? "" : "s"}`}
                  >
                    {opt.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>
      )}

      {/* Logs container */}
      <div className="relative bg-surface rounded-lg border border-border overflow-hidden">
        <div
          className={cn(
            "flex items-center gap-2 p-2 bg-surface/50 transition-all",
            isScrolled && "border-b border-border",
          )}
        >
          <div className="flex items-center gap-3 text-[11px] text-muted-foreground">
            {searchQuery ? (
              <>
                <span className="flex items-center gap-1">
                  <kbd className="bg-muted px-1 py-0.5 rounded-sm font-mono text-[10px]">
                    N
                  </kbd>
                  <span>/</span>
                  <kbd className="bg-muted px-1 py-0.5 rounded-sm font-mono text-[10px]">
                    ⇧N
                  </kbd>
                  <span className="ml-0.5">results</span>
                </span>
                <span className="flex items-center gap-1">
                  <kbd className="bg-muted px-1 py-0.5 rounded-sm font-mono text-[10px]">
                    ESC
                  </kbd>
                  <span>clear</span>
                </span>
              </>
            ) : (
              <>
                <span className="flex items-center gap-1">
                  <kbd className="bg-muted px-1 py-0.5 rounded-sm font-mono text-[10px]">
                    J
                  </kbd>
                  <span>/</span>
                  <kbd className="bg-muted px-1 py-0.5 rounded-sm font-mono text-[10px]">
                    K
                  </kbd>
                  <span className="ml-0.5">navigate</span>
                </span>
                <span className="flex items-center gap-1">
                  <kbd className="bg-muted px-1 py-0.5 rounded-sm font-mono text-[10px]">
                    G
                  </kbd>
                  <span>first</span>
                </span>
                <span className="flex items-center gap-1">
                  <kbd className="bg-muted px-1 py-0.5 rounded-sm font-mono text-[10px]">
                    ⇧G
                  </kbd>
                  <span>last</span>
                </span>
              </>
            )}
          </div>
          <div className="ml-auto">
            <div className="relative">
              <Icon
                name="search"
                className="absolute left-2 top-1/2 -translate-y-1/2 size-3 text-muted-foreground pointer-events-none"
              />
              <Input
                data-search-input
                type="text"
                placeholder="Search logs"
                value={searchQuery}
                onChange={(e) => handleSearchChange(e.target.value)}
                onFocus={() => setSearchInputFocused(true)}
                onBlur={() => setSearchInputFocused(false)}
                className="pl-7 pr-16 w-48 text-xs py-1 rounded-sm"
              />
              {searchQuery || searchInputFocused ? (
                filteredIndices.length > 0 ? (
                  <div className="absolute right-1 top-1/2 -translate-y-1/2 flex items-center gap-0.5">
                    <span className="text-[10px] text-muted-foreground bg-muted px-1 py-0.5 rounded-sm">
                      ESC
                    </span>
                    <span className="text-[10px] text-muted-foreground mx-0.5">
                      {effectiveSearchIndex + 1}/{filteredIndices.length}
                    </span>
                    <div className="flex items-center">
                      <button
                        onClick={() => navigateToResult("prev")}
                        className="p-0.5 hover:bg-muted rounded-sm opacity-60 hover:opacity-100 transition-opacity"
                        title="Previous (Shift+N)"
                      >
                        <Icon name="chevron-up" className="size-2.5" />
                      </button>
                      <button
                        onClick={() => navigateToResult("next")}
                        className="p-0.5 hover:bg-muted rounded-sm opacity-60 hover:opacity-100 transition-opacity"
                        title="Next (N)"
                      >
                        <Icon name="chevron-down" className="size-2.5" />
                      </button>
                    </div>
                  </div>
                ) : (
                  <div className="absolute right-1.5 top-1/2 -translate-y-1/2 flex items-center gap-0.5">
                    <span className="text-[10px] text-muted-foreground bg-muted px-1 py-0.5 rounded-sm">
                      ESC
                    </span>
                    <span className="text-[10px] text-muted-foreground ml-0.5">
                      0/0
                    </span>
                  </div>
                )
              ) : (
                <div className="absolute right-2 top-1/2 -translate-y-1/2 flex items-center">
                  <span className="text-[10px] text-muted-foreground bg-muted px-1 py-0.5 rounded-sm font-mono">
                    /
                  </span>
                </div>
              )}
            </div>
          </div>
        </div>

        <div
          ref={logsContainerRef}
          tabIndex={0}
          className="font-mono text-xs max-h-[500px] overflow-y-auto pb-2 focus:outline-none"
        >
          {parsedLogs.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
              <Icon name="file-text" className="size-8 mb-3 opacity-30" />
              <p className="text-sm font-sans">No logs to display</p>
            </div>
          ) : groupBySource && groupedLogs ? (
            // Grouped view
            groupedLogs.map(([source, group]) => (
              <details key={source} className="group" open>
                <summary className="px-3 py-3 cursor-pointer hover:bg-muted/30 flex items-center gap-2 border-b border-border">
                  <Icon
                    name="chevron-right"
                    className="size-3 group-open:rotate-90 transition-transform"
                  />
                  <span className="font-sans font-medium">{source}</span>
                  <span className="text-muted-foreground font-sans text-xs">
                    ({group.logs.length})
                  </span>
                </summary>
                <div>
                  {group.logs.map((log, localIndex) => {
                    const globalIndex = group.indices[localIndex];
                    if (globalIndex === undefined) return null;

                    const isHighlighted = globalIndex === currentLogIndex;
                    const isError = log.level === "ERROR";
                    const isWarn = log.level === "WARN";
                    const isSkipped = log.level === "SKIP";

                    return (
                      <div
                        ref={(el) => {
                          if (el) logRefs.current.set(globalIndex, el);
                        }}
                        key={
                          visibleEvents[globalIndex]?.id ||
                          `fallback-${globalIndex}`
                        }
                        className={cn(
                          "px-3 py-1.5 transition-colors relative",
                          "hover:bg-muted/20",
                          isError && "bg-destructive/10 text-destructive",
                          isWarn && "bg-warning/10 text-warning",
                          isSkipped && "bg-muted/50 text-muted-foreground",
                          isHighlighted &&
                            "border-l-4 border-l-foreground pl-2",
                        )}
                      >
                        <div className="flex items-center gap-2.5 min-w-0">
                          <span
                            className={cn(
                              "size-1.5 shrink-0 rounded-full",
                              getLevelColors(log.level).dot,
                            )}
                          />
                          <span
                            className={cn(
                              "text-[11px] tabular-nums shrink-0",
                              isError
                                ? "text-destructive"
                                : isWarn
                                  ? "text-warning"
                                  : "text-muted-foreground/60",
                            )}
                          >
                            {formatLogTimestamp(
                              visibleEvents[globalIndex]!.createdAt,
                            )}
                          </span>
                          <span
                            className={cn(
                              "shrink-0 rounded px-1.5 py-0.5 font-mono text-[10px] font-medium uppercase",
                              getLevelColors(log.level).bg,
                              getLevelColors(log.level).text,
                            )}
                          >
                            {log.level}
                          </span>
                          <span className="min-w-0 flex-1">
                            {highlightMatch(log.message)}
                          </span>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </details>
            ))
          ) : (
            // Flat view
            parsedLogs.map((log, index) => {
              const isHighlighted = index === currentLogIndex;
              const isError = log.level === "ERROR";
              const isWarn = log.level === "WARN";
              const isSkipped = log.level === "SKIP";

              return (
                <div
                  ref={(el) => {
                    if (el) logRefs.current.set(index, el);
                  }}
                  key={visibleEvents[index]?.id || `fallback-${index}`}
                  className={cn(
                    "px-3 py-1.5 transition-colors relative",
                    "hover:bg-muted/20",
                    isError && "bg-destructive/10 text-destructive",
                    isWarn && "bg-warning/10 text-warning",
                    isSkipped && "bg-muted/50 text-muted-foreground",
                    isHighlighted && "border-l-4 border-l-foreground pl-2",
                  )}
                >
                  <div className="flex items-center gap-2.5 min-w-0">
                    <span
                      className={cn(
                        "size-1.5 shrink-0 rounded-full",
                        getLevelColors(log.level).dot,
                      )}
                    />
                    <span
                      className={cn(
                        "text-[11px] tabular-nums shrink-0",
                        isError
                          ? "text-destructive"
                          : isWarn
                            ? "text-warning"
                            : "text-muted-foreground/60",
                      )}
                    >
                      {formatLogTimestamp(visibleEvents[index]!.createdAt)}
                    </span>
                    <span
                      className={cn(
                        "shrink-0 rounded px-1.5 py-0.5 font-mono text-[10px] font-medium uppercase",
                        getLevelColors(log.level).bg,
                        getLevelColors(log.level).text,
                      )}
                    >
                      {log.level}
                    </span>
                    <span className="min-w-0 flex-1">
                      {highlightMatch(log.message)}
                    </span>
                  </div>
                </div>
              );
            })
          )}
        </div>

        {showBottomFade && (
          <div className="absolute bottom-0 left-0 right-0 h-12 bg-gradient-to-t from-background to-transparent pointer-events-none rounded-b-lg" />
        )}
      </div>
    </>
  );
};
