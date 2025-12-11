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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { ToolExecutionLog } from "@gram/client/models/components";
import { useToolExecutionLogs } from "@gram/client/react-query";
import { Badge, Button, Icon } from "@speakeasy-api/moonshine";
import { useEffect, useMemo, useRef, useState } from "react";
import { LogExecutionDetailSheet } from "./LogExecutionDetailSheet";

function getLevelBadgeVariant(
  level: string,
): "neutral" | "information" | "warning" | "destructive" {
  switch (level.toLowerCase()) {
    case "debug":
      return "neutral";
    case "info":
      return "information";
    case "warn":
    case "warning":
      return "warning";
    case "error":
      return "destructive";
    default:
      return "neutral";
  }
}

function formatTimestamp(date: Date): string {
  return new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).format(date);
}

export default function ExecutionLogsPage() {
  const [filters, setFilters] = useState<{
    searchQuery: string | null;
    deploymentFilter: string | null;
    functionFilter: string | null;
    levelFilter: string | null;
    sourceFilter: string | null;
  }>({
    searchQuery: null,
    deploymentFilter: null,
    functionFilter: null,
    levelFilter: null,
    sourceFilter: null,
  });
  const [selectedLog, setSelectedLog] = useState<ToolExecutionLog | null>(null);
  const [searchInput, setSearchInput] = useState<string>("");

  // Debounce search input - update filter 500ms after user stops typing
  useEffect(() => {
    const timeoutId = setTimeout(() => {
      setFilters((prev) => ({
        ...prev,
        searchQuery: searchInput || null,
      }));
    }, 500);

    return () => clearTimeout(timeoutId);
  }, [searchInput]);

  // Infinite scroll state
  const [allLogs, setAllLogs] = useState<ToolExecutionLog[]>([]);
  const [currentCursor, setCurrentCursor] = useState<string | undefined>(
    undefined,
  );
  const [isFetchingMore, setIsFetchingMore] = useState(false);
  const lastProcessedCursorRef = useRef<string | undefined>(undefined);
  const tableContainerRef = useRef<HTMLDivElement>(null);
  const perPage = 25;

  const listToolExecutionLogsQuery = useToolExecutionLogs(
    {
      perPage,
      cursor: currentCursor,
      deploymentId: filters.deploymentFilter || undefined,
      functionId: filters.functionFilter || undefined,
      level:
        (filters.levelFilter as "debug" | "info" | "warn" | "error" | null) ||
        undefined,
      source: (filters.sourceFilter as "stdout" | "stderr" | null) || undefined,
      sort: "desc",
    },
    undefined,
    {
      staleTime: 0,
      refetchOnWindowFocus: false,
      throwOnError: false,
    },
  );
  const { data, isLoading, error, refetch } = listToolExecutionLogsQuery;

  // Update accumulated logs when new data arrives
  useEffect(() => {
    if (data?.logs && !isLoading) {
      if (
        currentCursor !== undefined &&
        lastProcessedCursorRef.current === currentCursor
      ) {
        return;
      }

      if (currentCursor === undefined) {
        setAllLogs(data.logs);
      } else {
        setAllLogs((prev) => {
          const existingIds = new Set(prev.map((log) => log.id));
          const newLogs =
            data.logs?.filter((log) => !existingIds.has(log.id)) || [];
          return [...prev, ...newLogs];
        });
      }

      lastProcessedCursorRef.current = currentCursor;
      setIsFetchingMore(false);
    }
  }, [data, isLoading, currentCursor]);

  const pagination = data?.pagination;
  const hasNextPage = pagination?.hasNextPage ?? false;

  // Reset the cursor when filters change
  useEffect(() => {
    setCurrentCursor(undefined);
    lastProcessedCursorRef.current = undefined;
    setIsFetchingMore(false);
  }, [
    filters.deploymentFilter,
    filters.functionFilter,
    filters.levelFilter,
    filters.sourceFilter,
    filters.searchQuery,
  ]);

  // Auto-load more if container isn't scrollable after initial load
  useEffect(() => {
    const container = tableContainerRef.current;
    if (container && allLogs.length > 0 && !isFetchingMore && !isLoading) {
      setTimeout(() => {
        const isScrollable = container.scrollHeight > container.clientHeight;

        if (!isScrollable && hasNextPage && pagination?.nextPageCursor) {
          setIsFetchingMore(true);
          setCurrentCursor(pagination.nextPageCursor);
        }
      }, 100);
    }
  }, [
    allLogs.length,
    hasNextPage,
    pagination?.nextPageCursor,
    isFetchingMore,
    isLoading,
  ]);

  // Load more logs when scrolling near bottom
  const handleScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const container = e.currentTarget;
    const scrollTop = container.scrollTop;
    const scrollHeight = container.scrollHeight;
    const clientHeight = container.clientHeight;
    const distanceFromBottom = scrollHeight - (scrollTop + clientHeight);

    if (isFetchingMore || isLoading) {
      return;
    }

    if (!hasNextPage || !pagination?.nextPageCursor) {
      return;
    }

    if (distanceFromBottom < 200) {
      setIsFetchingMore(true);
      setCurrentCursor(pagination.nextPageCursor);
    }
  };

  // Get unique deployment and function IDs from visible logs for filter dropdowns
  const uniqueDeployments = useMemo(() => {
    const ids = new Set(allLogs.map((log) => log.deploymentId));
    return Array.from(ids);
  }, [allLogs]);

  const uniqueFunctions = useMemo(() => {
    const ids = new Set(allLogs.map((log) => log.functionId));
    return Array.from(ids);
  }, [allLogs]);

  // Filter logs by search query (client-side)
  const filteredLogs = useMemo(() => {
    if (!filters.searchQuery) return allLogs;
    const query = filters.searchQuery.toLowerCase();
    return allLogs.filter(
      (log) =>
        log.message?.toLowerCase().includes(query) ||
        log.rawLog.toLowerCase().includes(query),
    );
  }, [allLogs, filters.searchQuery]);

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
                  placeholder="Search messages..."
                  className="w-1/3"
                />

                <div className="flex items-center gap-2">
                  <Select
                    value={filters.deploymentFilter ?? "all"}
                    onValueChange={(value) =>
                      setFilters((prev) => ({
                        ...prev,
                        deploymentFilter: value === "all" ? null : value,
                      }))
                    }
                  >
                    <SelectTrigger className="w-[180px]">
                      <SelectValue placeholder="Deployment" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">All Deployments</SelectItem>
                      {uniqueDeployments.map((id) => (
                        <SelectItem key={id} value={id}>
                          {id.slice(0, 8)}...
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>

                  <Select
                    value={filters.functionFilter ?? "all"}
                    onValueChange={(value) =>
                      setFilters((prev) => ({
                        ...prev,
                        functionFilter: value === "all" ? null : value,
                      }))
                    }
                  >
                    <SelectTrigger className="w-[180px]">
                      <SelectValue placeholder="Function" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">All Functions</SelectItem>
                      {uniqueFunctions.map((id) => (
                        <SelectItem key={id} value={id}>
                          {id.slice(0, 8)}...
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>

                  <Select
                    value={filters.levelFilter ?? "all"}
                    onValueChange={(value) =>
                      setFilters((prev) => ({
                        ...prev,
                        levelFilter: value === "all" ? null : value,
                      }))
                    }
                  >
                    <SelectTrigger className="w-[180px]">
                      <SelectValue placeholder="Level" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">All Levels</SelectItem>
                      <SelectItem value="debug">Debug</SelectItem>
                      <SelectItem value="info">Info</SelectItem>
                      <SelectItem value="warn">Warn</SelectItem>
                      <SelectItem value="error">Error</SelectItem>
                    </SelectContent>
                  </Select>

                  <Select
                    value={filters.sourceFilter ?? "all"}
                    onValueChange={(value) =>
                      setFilters((prev) => ({
                        ...prev,
                        sourceFilter: value === "all" ? null : value,
                      }))
                    }
                  >
                    <SelectTrigger className="w-[180px]">
                      <SelectValue placeholder="Source" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">All Sources</SelectItem>
                      <SelectItem value="stdout">stdout</SelectItem>
                      <SelectItem value="stderr">stderr</SelectItem>
                    </SelectContent>
                  </Select>

                  <Button
                    onClick={() => refetch()}
                    size="sm"
                    variant="secondary"
                  >
                    <Button.LeftIcon>
                      <Icon name="refresh-cw" className="size-4" />
                    </Button.LeftIcon>
                    <Button.Text>Refresh</Button.Text>
                  </Button>
                </div>
              </div>

              {/* Table */}
              <div className="border border-neutral-softest rounded-lg overflow-hidden w-full flex flex-col relative">
                {isLoading && allLogs.length > 0 && (
                  <div className="absolute top-0 left-0 right-0 h-1 bg-primary-default/20 z-20">
                    <div className="h-full bg-primary-default animate-pulse" />
                  </div>
                )}
                <div
                  ref={tableContainerRef}
                  className="overflow-y-auto"
                  style={{ maxHeight: "calc(100vh - 250px)" }}
                  onScroll={handleScroll}
                >
                  <Table>
                    <TableHeader className="sticky top-0 z-10">
                      <TableRow className="bg-surface-secondary-default border-b border-neutral-softest">
                        <TableHead className="font-mono">TIMESTAMP</TableHead>
                        <TableHead className="font-mono">LEVEL</TableHead>
                        <TableHead className="font-mono">MESSAGE</TableHead>
                        <TableHead className="font-mono">FUNCTION ID</TableHead>
                        <TableHead className="font-mono">
                          DEPLOYMENT ID
                        </TableHead>
                        <TableHead className="font-mono">SOURCE</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {error ? (
                        <TableRow>
                          <TableCell colSpan={6} className="text-center py-8">
                            <div className="flex flex-col items-center gap-2">
                              <Icon
                                name="x"
                                className="size-6 text-destructive-default"
                              />
                              <span className="text-destructive-default font-medium">
                                Error loading logs
                              </span>
                              <span className="text-sm text-muted-foreground">
                                {error instanceof Error
                                  ? error.message
                                  : "An unexpected error occurred"}
                              </span>
                            </div>
                          </TableCell>
                        </TableRow>
                      ) : isLoading && allLogs.length === 0 ? (
                        <TableRow>
                          <TableCell
                            colSpan={6}
                            className="text-center py-8 text-muted-foreground"
                          >
                            Loading logs...
                          </TableCell>
                        </TableRow>
                      ) : filteredLogs.length === 0 ? (
                        <TableRow>
                          <TableCell
                            colSpan={6}
                            className="text-center py-8 text-muted-foreground"
                          >
                            No logs found
                          </TableCell>
                        </TableRow>
                      ) : (
                        filteredLogs.map((log) => {
                          return (
                            <TableRow
                              key={log.id}
                              className="cursor-pointer hover:bg-surface-secondary-default"
                              onClick={() => setSelectedLog(log)}
                            >
                              <TableCell className="text-muted-foreground font-mono py-4 text-xs">
                                {formatTimestamp(log.timestamp)}
                              </TableCell>
                              <TableCell className="py-4">
                                <Badge
                                  variant={getLevelBadgeVariant(log.level)}
                                  className="font-mono text-xs"
                                >
                                  {log.level.toUpperCase()}
                                </Badge>
                              </TableCell>
                              <TableCell className="font-mono py-4 max-w-md">
                                <span className="text-sm truncate block">
                                  {log.message || log.rawLog}
                                </span>
                              </TableCell>
                              <TableCell className="text-muted-foreground font-mono py-4 text-xs">
                                {log.functionId.slice(0, 8)}...
                              </TableCell>
                              <TableCell className="text-muted-foreground font-mono py-4 text-xs">
                                {log.deploymentId.slice(0, 8)}...
                              </TableCell>
                              <TableCell className="py-4">
                                <Badge
                                  variant={
                                    log.source === "stderr"
                                      ? "warning"
                                      : "neutral"
                                  }
                                  className="font-mono text-xs"
                                >
                                  {log.source}
                                </Badge>
                              </TableCell>
                            </TableRow>
                          );
                        })
                      )}

                      {isFetchingMore && (
                        <TableRow>
                          <TableCell
                            colSpan={6}
                            className="text-center py-4 text-muted-foreground"
                          >
                            <div className="flex items-center justify-center gap-2">
                              <Icon
                                name="loader-circle"
                                className="size-4 animate-spin"
                              />
                              <span>Loading more logs...</span>
                            </div>
                          </TableCell>
                        </TableRow>
                      )}
                    </TableBody>
                  </Table>
                </div>

                {filteredLogs.length > 0 && (
                  <div className="flex items-center justify-between gap-4 px-4 py-3 bg-surface-secondary-default border-t border-neutral-softest text-sm text-muted-foreground">
                    <span>
                      Showing {filteredLogs.length}{" "}
                      {filteredLogs.length === 1 ? "log" : "logs"}
                      {hasNextPage && " • Scroll down to load more"}
                    </span>
                  </div>
                )}
              </div>
            </div>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>

      <LogExecutionDetailSheet
        log={selectedLog}
        open={!!selectedLog}
        onOpenChange={(open) => !open && setSelectedLog(null)}
      />
    </Page>
  );
}
