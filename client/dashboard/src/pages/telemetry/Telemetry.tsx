import { Page } from "@/components/page-layout";
import { SearchBar } from "@/components/ui/search-bar";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  ToolCallSummary,
  TelemetryLogRecord,
  FeatureName,
} from "@gram/client/models/components";
import {
  useSearchToolCallsMutation,
  useFeaturesSetMutation,
  useListToolLogs,
  invalidateAllListToolLogs,
} from "@gram/client/react-query";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { XIcon } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { TraceRow } from "./TraceRow";
import { TelemetryLogDetailSheet } from "./TelemetryLogDetailSheet";

interface Filters {
  searchQuery: string | null;
  statusFilter: string | null;
}

export default function TelemetryPage() {
  const [filters, setFilters] = useState<Filters>({
    searchQuery: null,
    statusFilter: null,
  });
  const [searchInput, setSearchInput] = useState("");
  const [expandedTraceId, setExpandedTraceId] = useState<string | null>(null);
  const [selectedLog, setSelectedLog] = useState<TelemetryLogRecord | null>(
    null,
  );

  // Pagination state
  const [allTraces, setAllTraces] = useState<ToolCallSummary[]>([]);
  const [currentCursor, setCurrentCursor] = useState<string | undefined>(
    undefined,
  );
  const [isFetchingMore, setIsFetchingMore] = useState(false);
  const lastProcessedCursorRef = useRef<string | undefined>(undefined);
  const containerRef = useRef<HTMLDivElement>(null);
  const perPage = 25;

  const { mutate, data, isPending, error } = useSearchToolCallsMutation();

  // Check if logs are enabled using the logs list endpoint
  const queryClient = useQueryClient();
  const { data: logsData, refetch: refetchLogs } = useListToolLogs(
    { perPage: 1 },
    undefined,
    { staleTime: 0, refetchOnWindowFocus: false },
  );
  const logsEnabled = logsData?.enabled ?? true;

  const [logsMutationError, setLogsMutationError] = useState<string | null>(
    null,
  );
  const { mutateAsync: setLogsFeature, status: logsMutationStatus } =
    useFeaturesSetMutation({
      onSuccess: async () => {
        setLogsMutationError(null);
        await invalidateAllListToolLogs(queryClient);
        setCurrentCursor(undefined);
        lastProcessedCursorRef.current = undefined;
        setAllTraces([]);
        await refetchLogs();
      },
      onError: (err) => {
        const message =
          err instanceof Error ? err.message : "Failed to update logs";
        setLogsMutationError(message);
      },
    });

  const isMutatingLogs = logsMutationStatus === "pending";

  const handleSetLogs = async (enabled: boolean) => {
    setLogsMutationError(null);
    try {
      await setLogsFeature({
        request: {
          setProductFeatureRequestBody: {
            featureName: FeatureName.Logs,
            enabled,
          },
        },
      });
    } catch {
      // error state handled in onError callback
    }
  };

  // Debounce search input
  useEffect(() => {
    const timeoutId = setTimeout(() => {
      setFilters((prev) => ({
        ...prev,
        searchQuery: searchInput || null,
      }));
    }, 500);
    return () => clearTimeout(timeoutId);
  }, [searchInput]);

  // Fetch traces when filters or cursor change
  const fetchTraces = useCallback(() => {
    mutate({
      request: {
        searchToolCallsPayload: {
          filter: filters.searchQuery
            ? { gramUrn: filters.searchQuery }
            : undefined,
          cursor: currentCursor,
          limit: perPage,
          sort: "desc",
        },
      },
    });
  }, [mutate, filters.searchQuery, currentCursor]);

  // Initial fetch and filter changes
  useEffect(() => {
    fetchTraces();
  }, [fetchTraces]);

  // Update accumulated traces when new data arrives
  useEffect(() => {
    if (data?.toolCalls && !isPending) {
      if (
        currentCursor !== undefined &&
        lastProcessedCursorRef.current === currentCursor
      ) {
        return;
      }

      let filteredTraces = data.toolCalls;

      // Client-side status filter
      if (filters.statusFilter) {
        filteredTraces = filteredTraces.filter((trace) => {
          const status = trace.httpStatusCode;
          if (!status) return filters.statusFilter === "success";
          switch (filters.statusFilter) {
            case "success":
              return status >= 200 && status < 400;
            case "4xx":
              return status >= 400 && status < 500;
            case "5xx":
              return status >= 500;
            default:
              return true;
          }
        });
      }

      if (currentCursor === undefined) {
        setAllTraces(filteredTraces);
      } else {
        setAllTraces((prev) => {
          const existingIds = new Set(prev.map((t) => t.traceId));
          const newTraces = filteredTraces.filter(
            (t) => !existingIds.has(t.traceId),
          );
          return [...prev, ...newTraces];
        });
      }

      lastProcessedCursorRef.current = currentCursor;
      setIsFetchingMore(false);
    }
  }, [data, isPending, currentCursor, filters.statusFilter]);

  const nextCursor = data?.nextCursor;
  const hasNextPage = !!nextCursor;

  // Reset cursor when filters change
  useEffect(() => {
    setCurrentCursor(undefined);
    lastProcessedCursorRef.current = undefined;
    setIsFetchingMore(false);
    setAllTraces([]);
  }, [filters.searchQuery, filters.statusFilter]);

  // Handle scroll for infinite loading
  const handleScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const container = e.currentTarget;
    const scrollTop = container.scrollTop;
    const scrollHeight = container.scrollHeight;
    const clientHeight = container.clientHeight;
    const distanceFromBottom = scrollHeight - (scrollTop + clientHeight);

    if (isFetchingMore || isPending) return;
    if (!hasNextPage || !nextCursor) return;

    if (distanceFromBottom < 200) {
      setIsFetchingMore(true);
      setCurrentCursor(nextCursor);
    }
  };

  const toggleExpand = (traceId: string) => {
    setExpandedTraceId((prev) => (prev === traceId ? null : traceId));
  };

  const handleLogClick = (log: TelemetryLogRecord) => {
    setSelectedLog(log);
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          {null}
          <Page.Section.Body>
            <div className="flex flex-col gap-4">
              {/* Search and Filters Row */}
              <div className="flex items-center justify-between gap-4">
                <SearchBar
                  value={searchInput}
                  onChange={setSearchInput}
                  placeholder="Search by tool URN"
                  className="w-1/3"
                />

                <div className="flex items-center gap-2">
                  <Select
                    value={filters.statusFilter ?? "all"}
                    onValueChange={(value) =>
                      setFilters((prev) => ({
                        ...prev,
                        statusFilter: value === "all" ? null : value,
                      }))
                    }
                  >
                    <SelectTrigger className="w-[180px]">
                      <SelectValue placeholder="Status" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">All Statuses</SelectItem>
                      <SelectItem value="success">Success (2xx/3xx)</SelectItem>
                      <SelectItem value="4xx">Client Error (4xx)</SelectItem>
                      <SelectItem value="5xx">Server Error (5xx)</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </div>

              {/* Trace list container */}
              <div className="border border-border rounded-lg overflow-hidden w-full flex flex-col relative bg-surface-default">
                {/* Loading indicator */}
                {isPending && allTraces.length > 0 && (
                  <div className="absolute top-0 left-0 right-0 h-1 bg-primary-default/20 z-20">
                    <div className="h-full bg-primary-default animate-pulse" />
                  </div>
                )}

                {/* Header */}
                <div className="flex items-center gap-3 px-3 py-2.5 bg-surface-secondary-default border-b border-border text-xs font-medium text-muted-foreground uppercase tracking-wide">
                  <div className="shrink-0 w-[150px]">Timestamp</div>
                  <div className="shrink-0 w-5" />
                  <div className="flex-1">Source / Tool</div>
                  <div className="shrink-0 w-16 text-right">Status</div>
                </div>

                {/* Scrollable trace list */}
                <div
                  ref={containerRef}
                  className="overflow-y-auto"
                  style={{ maxHeight: "calc(100vh - 280px)" }}
                  onScroll={handleScroll}
                >
                  {error ? (
                    <div className="flex flex-col items-center gap-2 py-12">
                      <XIcon className="size-6 stroke-destructive-default" />
                      <span className="text-destructive-default font-medium">
                        Error loading traces
                      </span>
                      <span className="text-sm text-muted-foreground">
                        {error instanceof Error
                          ? error.message
                          : "An unexpected error occurred"}
                      </span>
                    </div>
                  ) : isPending && allTraces.length === 0 ? (
                    <div className="flex items-center justify-center gap-2 py-12 text-muted-foreground">
                      <Icon
                        name="loader-circle"
                        className="size-4 animate-spin"
                      />
                      <span>Loading traces...</span>
                    </div>
                  ) : allTraces.length === 0 ? (
                    <div className="py-12 text-center text-muted-foreground">
                      {logsEnabled ? (
                        "No traces found"
                      ) : (
                        <div className="flex flex-col items-center gap-3">
                          <span>Logs are disabled for your organization.</span>
                          <Button
                            onClick={() => handleSetLogs(true)}
                            disabled={isMutatingLogs}
                            size="sm"
                            variant="secondary"
                          >
                            <Button.LeftIcon>
                              <Icon
                                name="test-tube-diagonal"
                                className="size-4"
                              />
                            </Button.LeftIcon>
                            <Button.Text>
                              {isMutatingLogs ? "Updating Logs" : "Enable Logs"}
                            </Button.Text>
                          </Button>
                          {logsMutationError && (
                            <span className="text-sm text-destructive-default">
                              {logsMutationError}
                            </span>
                          )}
                        </div>
                      )}
                    </div>
                  ) : (
                    <>
                      {allTraces.map((trace) => (
                        <TraceRow
                          key={trace.traceId}
                          trace={trace}
                          isExpanded={expandedTraceId === trace.traceId}
                          onToggle={() => toggleExpand(trace.traceId)}
                          onLogClick={handleLogClick}
                        />
                      ))}

                      {isFetchingMore && (
                        <div className="flex items-center justify-center gap-2 py-4 text-muted-foreground border-t border-border">
                          <Icon
                            name="loader-circle"
                            className="size-4 animate-spin"
                          />
                          <span className="text-sm">
                            Loading more traces...
                          </span>
                        </div>
                      )}
                    </>
                  )}
                </div>

                {/* Footer */}
                {allTraces.length > 0 && (
                  <div className="flex items-center justify-between gap-4 px-4 py-2 bg-surface-secondary-default border-t border-border text-sm text-muted-foreground">
                    <span>
                      {allTraces.length}{" "}
                      {allTraces.length === 1 ? "trace" : "traces"}
                      {hasNextPage && " • Scroll to load more"}
                    </span>
                    {logsEnabled ? (
                      <Button
                        onClick={() => handleSetLogs(false)}
                        disabled={isMutatingLogs}
                        size="sm"
                        variant="secondary"
                      >
                        <Button.Text>
                          {isMutatingLogs ? "Updating Logs" : "Disable Logs"}
                        </Button.Text>
                      </Button>
                    ) : (
                      <Button
                        onClick={() => handleSetLogs(true)}
                        disabled={isMutatingLogs}
                        size="sm"
                        variant="secondary"
                      >
                        <Button.LeftIcon>
                          <Icon name="test-tube-diagonal" className="size-4" />
                        </Button.LeftIcon>
                        <Button.Text>
                          {isMutatingLogs ? "Updating Logs" : "Enable Logs"}
                        </Button.Text>
                      </Button>
                    )}
                  </div>
                )}
              </div>
            </div>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>

      {/* Log Detail Sheet */}
      <TelemetryLogDetailSheet
        log={selectedLog}
        open={!!selectedLog}
        onOpenChange={(open) => !open && setSelectedLog(null)}
      />
    </Page>
  );
}
