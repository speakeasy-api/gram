import { Heading } from "@/components/ui/heading";
import { dateTimeFormatters } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { useDeploymentLogsSuspense } from "@gram/client/react-query";
import { Icon, Input } from "@speakeasy-api/moonshine";
import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useParams } from "react-router";
import { useDeploymentSearchParams } from "./use-deployment-search-params";

type LogLevel = "WARN" | "INFO" | "DEBUG" | "ERROR" | "OK" | "SKIP";
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
}: { deploymentId?: string; embeddedMode?: boolean } = {}) => {
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

  const [focus, setFocus] = useState<LogFocus>("all");
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

  const parsedLogs = useMemo(
    () =>
      deploymentLogs.events.map((event) =>
        parseLogMessage(event.message, event.event),
      ),
    [deploymentLogs.events],
  );

  const logStats = useMemo(() => {
    const stats = { warns: 0, errors: 0, skipped: 0 };
    parsedLogs.forEach((log) => {
      if (log.level === "WARN") stats.warns++;
      if (log.level === "ERROR") stats.errors++;
      if (log.level === "SKIP") stats.skipped++;
    });
    return stats;
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

  const filteredIndices = useMemo(() => {
    if (focus === "all" && !searchQuery) return [];

    const indices: number[] = [];
    parsedLogs.forEach((log, index) => {
      const matchesFocus =
        focus === "all" ||
        (focus === "warns" && log.level === "WARN") ||
        (focus === "errors" && log.level === "ERROR") ||
        (focus === "skipped" && log.level === "SKIP");

      const matchesSearch =
        !searchQuery ||
        log.message.toLowerCase().includes(searchQuery.toLowerCase());

      if (matchesFocus && matchesSearch) {
        indices.push(index);
      }
    });

    return indices;
  }, [focus, searchQuery, parsedLogs]);

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
        newIndex = (currentSearchIndex + 1) % filteredIndices.length;
      } else {
        newIndex =
          currentSearchIndex === 0
            ? filteredIndices.length - 1
            : currentSearchIndex - 1;
      }

      setCurrentSearchIndex(newIndex);
      const targetIndex = filteredIndices[newIndex];
      if (targetIndex !== undefined) {
        scrollToLog(targetIndex);
      }
    },
    [currentSearchIndex, filteredIndices, scrollToLog],
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

  const highlightMatch = (text: string) => {
    if (!searchQuery) return text;

    const parts = text.split(new RegExp(`(${searchQuery})`, "gi"));
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
  };

  return (
    <>
      <Heading variant="h2" className="mb-6">
        Logs
      </Heading>

      {/* Logs container */}
      <div className="relative bg-surface rounded-lg border border-border overflow-hidden">
        <div
          className={cn(
            "flex items-center gap-2 p-2 bg-surface/50 transition-all",
            isScrolled && "border-b border-border",
          )}
        >
          <button
            onClick={() => handleFocusChange("all")}
            className={cn(
              "flex items-center gap-2 p-1 text-xs font-mono uppercase rounded-sm border border-border transition-colors",
              focus === "all" ? "bg-btn-secondary" : "hover:bg-muted/50",
            )}
          >
            <Icon
              name="list"
              className={cn(
                "size-3",
                focus === "all" ? "" : "text-muted-foreground",
              )}
            />
            <span>All logs</span>
            <span className="text-muted-foreground opacity-60">
              {parsedLogs.length}
            </span>
          </button>

          {logStats.warns > 0 && (
            <button
              onClick={() => handleFocusChange("warns")}
              className={cn(
                "flex items-center gap-2 p-1 text-xs font-mono uppercase rounded-sm border border-border transition-colors",
                focus === "warns" ? "bg-btn-secondary" : "hover:bg-muted/50",
              )}
            >
              <Icon
                name="triangle-alert"
                className={cn(
                  "size-3",
                  focus === "warns" ? "text-warning" : "text-muted-foreground",
                )}
              />
              <span>Warns</span>
              <span className="text-muted-foreground opacity-60">
                {logStats.warns}
              </span>
            </button>
          )}

          {logStats.errors > 0 && (
            <button
              onClick={() => handleFocusChange("errors")}
              className={cn(
                "flex items-center gap-2 p-1 text-xs font-mono uppercase rounded-sm border border-border transition-colors",
                focus === "errors" ? "bg-btn-secondary" : "hover:bg-muted/50",
              )}
            >
              <Icon
                name="circle-x"
                className={cn(
                  "size-3",
                  focus === "errors"
                    ? "text-destructive"
                    : "text-muted-foreground",
                )}
              />
              <span>Errors</span>
              <span className="text-muted-foreground opacity-60">
                {logStats.errors}
              </span>
            </button>
          )}

          {logStats.skipped > 0 && (
            <button
              onClick={() => handleFocusChange("skipped")}
              className={cn(
                "flex items-center gap-2 p-1 text-xs font-mono uppercase rounded-sm border border-border transition-colors",
                focus === "skipped" ? "bg-btn-secondary" : "hover:bg-muted/50",
              )}
            >
              <Icon
                name="skip-forward"
                className={cn(
                  "size-3",
                  focus === "skipped" ? "" : "text-muted-foreground",
                )}
              />
              <span>Skipped</span>
              <span className="text-muted-foreground opacity-60">
                {logStats.skipped}
              </span>
            </button>
          )}

          <div className="ml-auto flex items-center gap-2">
            <button
              onClick={() => setGroupBySource(!groupBySource)}
              className={cn(
                "flex items-center gap-2 p-1 text-xs font-mono uppercase rounded-sm border border-border transition-colors",
                groupBySource ? "bg-btn-secondary" : "hover:bg-muted/50",
              )}
            >
              <Icon
                name="layers"
                className={cn(
                  "size-3",
                  groupBySource ? "" : "text-muted-foreground",
                )}
              />
              <span>{groupBySource ? "Grouped" : "Group"}</span>
            </button>

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
                      {currentSearchIndex + 1}/{filteredIndices.length}
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
                  <span className="text-[10px] text-muted-foreground bg-muted px-1 py-0.5 rounded-sm">
                    ⌘F
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
                          deploymentLogs.events[globalIndex]?.id ||
                          `fallback-${globalIndex}`
                        }
                        className={cn(
                          "px-3 py-2 transition-colors relative",
                          "hover:bg-muted/20",
                          isError && "bg-destructive/10 text-destructive",
                          isWarn && "bg-warning/10 text-warning",
                          isSkipped && "bg-muted/50 text-muted-foreground",
                          isHighlighted &&
                            "border-l-4 border-l-foreground pl-2",
                        )}
                      >
                        <div className="flex items-start gap-4">
                          <span
                            className={cn(
                              "text-muted-foreground tabular-nums",
                              (isError || isWarn) && "text-inherit",
                            )}
                          >
                            {formatLogTimestamp(
                              deploymentLogs.events[globalIndex]!.createdAt,
                            )}
                          </span>
                          <span
                            className={cn(
                              "font-medium uppercase",
                              isError && "text-destructive",
                              isWarn && "text-warning",
                              isSkipped && "text-muted-foreground",
                              !isError &&
                                !isWarn &&
                                !isSkipped &&
                                "text-muted-foreground",
                            )}
                          >
                            [{log.level}]
                          </span>
                          <span className="flex-1">
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
                  key={deploymentLogs.events[index]?.id || `fallback-${index}`}
                  className={cn(
                    "px-3 py-2 transition-colors relative",
                    "hover:bg-muted/20",
                    isError && "bg-destructive/10 text-destructive",
                    isWarn && "bg-warning/10 text-warning",
                    isSkipped && "bg-muted/50 text-muted-foreground",
                    isHighlighted && "border-l-4 border-l-foreground pl-2",
                  )}
                >
                  <div className="flex items-start gap-4">
                    <span
                      className={cn(
                        "text-muted-foreground tabular-nums",
                        (isError || isWarn) && "text-inherit",
                      )}
                    >
                      {formatLogTimestamp(
                        deploymentLogs.events[index]!.createdAt,
                      )}
                    </span>
                    <span
                      className={cn(
                        "font-medium uppercase",
                        isError && "text-destructive",
                        isWarn && "text-warning",
                        isSkipped && "text-muted-foreground",
                        !isError &&
                          !isWarn &&
                          !isSkipped &&
                          "text-muted-foreground",
                      )}
                    >
                      [{log.level}]
                    </span>
                    <span className="flex-1">
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

      <div className="mt-2 flex items-center justify-between text-xs text-muted-foreground">
        <div className="flex items-center gap-4">
          {searchQuery ? (
            <>
              <div className="flex items-center gap-1">
                <kbd className="bg-muted px-1.5 py-0.5 rounded-sm">N</kbd>
                <span>/</span>
                <kbd className="bg-muted px-1.5 py-0.5 rounded-sm">⇧N</kbd>
                <span className="ml-1">navigate results</span>
              </div>
              <div className="flex items-center gap-1">
                <kbd className="bg-muted px-1.5 py-0.5 rounded-sm">ESC</kbd>
                <span>clear search</span>
              </div>
            </>
          ) : (
            <>
              <div className="flex items-center gap-1">
                <kbd className="bg-muted px-1.5 py-0.5 rounded-sm">⌘F</kbd>
                <span>or</span>
                <kbd className="bg-muted px-1.5 py-0.5 rounded-sm">/</kbd>
                <span>to search</span>
              </div>
              {!groupBySource && (
                <div className="flex items-center gap-1">
                  <kbd className="bg-muted px-1.5 py-0.5 rounded-sm">^G</kbd>
                  <span>group by source</span>
                </div>
              )}
              <div className="flex items-center gap-1">
                <kbd className="bg-muted px-1.5 py-0.5 rounded-sm">J</kbd>
                <span>/</span>
                <kbd className="bg-muted px-1.5 py-0.5 rounded-sm">K</kbd>
                <span className="ml-1">navigate logs</span>
              </div>
            </>
          )}
        </div>
      </div>
    </>
  );
};
