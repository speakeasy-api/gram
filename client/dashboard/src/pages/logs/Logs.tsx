import {Page} from "@/components/page-layout";
import {SearchBar} from "@/components/ui/search-bar";
import {Select, SelectContent, SelectItem, SelectTrigger, SelectValue,} from "@/components/ui/select";
import {Table, TableBody, TableCell, TableHead, TableHeader, TableRow,} from "@/components/ui/table";
import {
    invalidateAllListToolLogs,
    useFeaturesSetMutation,
    useListToolLogs,
    useListToolsets
} from "@gram/client/react-query";
import {FeatureName, HTTPToolLog} from "@gram/client/models/components";
import {Button, Icon} from "@speakeasy-api/moonshine";
import {useEffect, useMemo, useRef, useState} from "react";
import {useQueryClient} from "@tanstack/react-query";
import {LogDetailSheet} from "./LogDetailSheet";
import {formatTimestamp, getSourceFromUrn, getToolIcon, getToolNameFromUrn, isSuccessfulCall,} from "./utils";
import {formatDuration} from "@/lib/dates";
import {CheckIcon, XIcon} from "lucide-react";

function StatusIcon({isSuccess}: { isSuccess: boolean }) {
    if (isSuccess) {
        return <CheckIcon className="size-4 stroke-success-default"/>;
    }
    return <XIcon className="size-4 stroke-destructive-default"/>
}

export default function LogsPage() {
    const [filters, setFilters] = useState<{
        searchQuery: string | null;
        toolTypeFilter: string | null;
        sourceFilter: string | null;
        statusFilter: string | null;
    }>({
        searchQuery: null,
        toolTypeFilter: null,
        sourceFilter: null,
        statusFilter: null,
    });
    const [selectedLog, setSelectedLog] = useState<HTTPToolLog | null>(null);
    const [searchInput, setSearchInput] = useState<string>("");

    // Debounce search input - update filter 500ms after user stops typing
    useEffect(() => {
        const timeoutId = setTimeout(() => {
            setFilters(prev => ({
                ...prev,
                searchQuery: searchInput || null
            }));
        }, 500);

        return () => clearTimeout(timeoutId);
    }, [searchInput]);

    // Fetch toolsets for source dropdown
    const {data: toolsetsData} = useListToolsets();
    const sources = useMemo(() => {
        return toolsetsData?.toolsets?.map(t =>
            ({slug: t.slug, id: t.id, toolUrns: t.toolUrns}))
            .filter(Boolean) || [];
    }, [toolsetsData]);

    // Get tool URNs for selected source
    const selectedToolUrns = useMemo(() => {
        if (!filters.sourceFilter) return undefined;
        const selectedToolset = sources.find(s => s.slug === filters.sourceFilter);
        return selectedToolset?.toolUrns && selectedToolset.toolUrns.length > 0
            ? selectedToolset.toolUrns
            : undefined;
    }, [filters.sourceFilter, sources]);

    // Infinite scroll state
    const [allLogs, setAllLogs] = useState<HTTPToolLog[]>([]);
    const [currentCursor, setCurrentCursor] = useState<string | undefined>(undefined);
    const [isFetchingMore, setIsFetchingMore] = useState(false);
    const lastProcessedCursorRef = useRef<string | undefined>(undefined);
    const tableContainerRef = useRef<HTMLDivElement>(null);
    const perPage = 25;

    const queryClient = useQueryClient();
    const listToolLogsQuery = useListToolLogs(
        {
            perPage,
            cursor: currentCursor,
            toolType: (filters.toolTypeFilter as "http" | "function" | "prompt" | undefined) || undefined,
            toolUrns: selectedToolUrns,
            status: (filters.statusFilter as "success" | "failure" | undefined) || undefined,
            toolName: filters.searchQuery || undefined,
        },
        undefined,
        {
            staleTime: 0, // Don't cache to ensure fresh data on filter changes
            refetchOnWindowFocus: false,
        },
    );
    const {data, isLoading, error, refetch} = listToolLogsQuery;

    const [logsMutationError, setLogsMutationError] = useState<string | null>(null);
    const {mutateAsync: setLogsFeature, status: logsMutationStatus} = useFeaturesSetMutation({
        onSuccess: async () => {
            setLogsMutationError(null);
            await invalidateAllListToolLogs(queryClient);
            setCurrentCursor(undefined);
            lastProcessedCursorRef.current = undefined;
            setAllLogs([]);
            await refetch();
        },
        onError: (err) => {
            const message = err instanceof Error ? err.message : "Failed to update logs";
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

    // Update accumulated logs when new data arrives
    useEffect(() => {
        if (data?.logs && !isLoading) {
            // Check if we've already processed this cursor to avoid duplicates
            // Skip check for an initial load (both undefined is OK for the first load)
            if (currentCursor !== undefined && lastProcessedCursorRef.current === currentCursor) {
                return;
            }

            if (currentCursor === undefined) {
                // Initial load - replace all logs
                setAllLogs(data.logs);
            } else {
                // Append new logs for infinite scroll, deduplicating by ID
                setAllLogs((prev) => {
                    const existingIds = new Set(prev.map((log) => log.id));
                    const newLogs = data.logs.filter((log) => !existingIds.has(log.id));
                    return [...prev, ...newLogs];
                });
            }

            lastProcessedCursorRef.current = currentCursor;
            setIsFetchingMore(false);
        }
    }, [data, isLoading, currentCursor]);

    const logsEnabled = data?.enabled ?? true;
    const pagination = data?.pagination;
    const hasNextPage = pagination?.hasNextPage ?? false;

    // Reset the cursor when filters change (but keep existing logs visible until new data arrives)
    useEffect(() => {
        setCurrentCursor(undefined);
        lastProcessedCursorRef.current = undefined;
        setIsFetchingMore(false);
    }, [filters.toolTypeFilter, filters.sourceFilter, filters.statusFilter, filters.searchQuery]);

    // Auto-load more if container isn't scrollable after initial load
    useEffect(() => {
        const container = tableContainerRef.current;
        if (container && allLogs.length > 0 && !isFetchingMore && !isLoading) {
            setTimeout(() => {
                const isScrollable = container.scrollHeight > container.clientHeight;

                // If not scrollable and we have more data, load it automatically
                if (!isScrollable && hasNextPage && pagination?.nextPageCursor) {
                    setIsFetchingMore(true);
                    setCurrentCursor(pagination.nextPageCursor);
                }
            }, 100);
        }
    }, [allLogs.length, hasNextPage, pagination?.nextPageCursor, isFetchingMore, isLoading]);

    // Load more logs when scrolling near bottom
    const handleScroll = (e: React.UIEvent<HTMLDivElement>) => {
        const container = e.currentTarget;
        const scrollTop = container.scrollTop;
        const scrollHeight = container.scrollHeight;
        const clientHeight = container.clientHeight;
        const distanceFromBottom = scrollHeight - (scrollTop + clientHeight);

        // Check if we're near the bottom and can load more
        if (isFetchingMore || isLoading) {
            return;
        }

        if (!hasNextPage || !pagination?.nextPageCursor) {
            return;
        }

        // Trigger when within 200px of bottom
        if (distanceFromBottom < 200) {
            setIsFetchingMore(true);
            setCurrentCursor(pagination.nextPageCursor);
        }
    };

    return (
        <Page>
            <Page.Header>
                <Page.Header.Title>Logs</Page.Header.Title>
            </Page.Header>
            <Page.Body>
                <Page.Section>
                    {null}
                    <Page.Section.Body>
                        <div className="flex flex-col gap-4">
                            {/* Search and Filters Row */}
                            <div className="flex items-center justify-between gap-4">{/* Search Input */}
                                <SearchBar
                                    value={searchInput}
                                    onChange={setSearchInput}
                                    placeholder="Search"
                                    className="w-1/3"/>

                                {/* Filters */}
                                <div className="flex items-center gap-2">
                                    <Select
                                        value={filters.toolTypeFilter ?? "all"}
                                        onValueChange={(value) => setFilters(prev => ({
                                            ...prev,
                                            toolTypeFilter: value === "all" ? null : value
                                        }))}>
                                        <SelectTrigger className="w-[180px]">
                                            <SelectValue placeholder="Tool Type"/>
                                        </SelectTrigger>
                                        <SelectContent>
                                            <SelectItem value="all">All Types</SelectItem>
                                            <SelectItem value="http">HTTP</SelectItem>
                                            <SelectItem value="function">Function</SelectItem>
                                            <SelectItem value="prompt">Custom</SelectItem>
                                        </SelectContent>
                                    </Select>

                                    <Select
                                        value={filters.sourceFilter ?? "all"}
                                        onValueChange={(value) => setFilters(prev => ({
                                            ...prev,
                                            sourceFilter: value === "all" ? null : value
                                        }))}>
                                        <SelectTrigger className="w-[180px]">
                                            <SelectValue placeholder="Source"/>
                                        </SelectTrigger>
                                        <SelectContent>
                                            <SelectItem value="all">All Sources</SelectItem>
                                            {sources.map((source) => (
                                                <SelectItem key={source.id} value={source.slug}>
                                                    {source.slug}
                                                </SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>

                                    <div className="flex flex-col gap-2">
                                        <Select
                                            value={filters.statusFilter ?? "all"}
                                            onValueChange={(value) => setFilters(prev => ({
                                                ...prev,
                                                statusFilter: value === "all" ? null : value
                                            }))}>
                                            <SelectTrigger className="w-[180px]">
                                                <SelectValue placeholder="Status"/>
                                            </SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="all">All Statuses</SelectItem>
                                                <SelectItem value="success">Success</SelectItem>
                                                <SelectItem value="failure">Failure</SelectItem>
                                            </SelectContent>
                                        </Select>
                                        {logsEnabled && logsMutationError && (
                                            <span className="text-xs text-destructive-default">
                                                {logsMutationError}
                                            </span>
                                        )}
                                    </div>
                                </div>
                            </div>

                            {/* Table */}
                            <div
                                className="border border-neutral-softest rounded-lg overflow-hidden w-full flex flex-col relative">
                                {/* Loading indicator when filtering with existing data */}
                                {isLoading && allLogs.length > 0 && (
                                    <div className="absolute top-0 left-0 right-0 h-1 bg-primary-default/20 z-20">
                                        <div className="h-full bg-primary-default animate-pulse"/>
                                    </div>
                                )}
                                <div
                                    ref={tableContainerRef}
                                    className="overflow-y-auto"
                                    style={{maxHeight: 'calc(100vh - 250px)'}}
                                    onScroll={handleScroll}>
                                    <Table>
                                        <TableHeader className="sticky top-0 z-10">
                                            <TableRow
                                                className="bg-surface-secondary-default border-b border-neutral-softest">
                                                <TableHead className="font-mono">TIMESTAMP</TableHead>
                                                <TableHead className="font-mono">SOURCE NAME</TableHead>
                                                <TableHead className="font-mono">TOOL NAME</TableHead>
                                                <TableHead className="font-mono">STATUS</TableHead>
                                                <TableHead className="font-mono">CLIENT</TableHead>
                                                <TableHead className="font-mono">DURATION</TableHead>
                                            </TableRow>
                                        </TableHeader>
                                        <TableBody>
                                            {error ? (
                                                <TableRow>
                                                    <TableCell colSpan={6}
                                                               className="text-center py-8">
                                                        <div className="flex flex-col items-center gap-2">
                                                            <XIcon className="size-6 stroke-destructive-default"/>
                                                            <span className="text-destructive-default font-medium">
                                                                Error loading logs
                                                            </span>
                                                            <span className="text-sm text-muted-foreground">
                                                                {error instanceof Error ? error.message : "An unexpected error occurred"}
                                                            </span>
                                                        </div>
                                                    </TableCell>
                                                </TableRow>
                                            ) : isLoading &&allLogs.length === 0 ? (
                                                <TableRow>
                                                    <TableCell colSpan={6}
                                                               className="text-center py-8 text-muted-foreground">
                                                        Loading logs...
                                                    </TableCell>
                                                </TableRow>
                                            ) : allLogs.length === 0 ? (
                                                <TableRow>
                                                    <TableCell colSpan={6}
                                                               className="text-center py-8 text-muted-foreground">
                                                        {logsEnabled ? (
                                                            "No logs found"
                                                        ) : (
                                                            <div className="flex flex-col items-center gap-3">
                                                                <span>
                                                                    Logs are disabled for your organization.
                                                                </span>
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
                                                                {logsMutationError && (
                                                                    <span className="text-sm text-destructive-default">
                                                                        {logsMutationError}
                                                                    </span>
                                                                )}
                                                            </div>
                                                        )}
                                                    </TableCell>
                                                </TableRow>
                                            ) : allLogs.map((log) => {
                                                const ToolIcon = getToolIcon(log.toolUrn);
                                                const sourceName = getSourceFromUrn(log.toolUrn);
                                                return (
                                                    <TableRow
                                                        key={log.id}
                                                        className="cursor-pointer hover:bg-surface-secondary-default"
                                                        onClick={() => setSelectedLog(log)}>
                                                        <TableCell className="text-muted-foreground font-mono py-4">
                                                            {formatTimestamp(log.ts)}
                                                        </TableCell>
                                                        <TableCell className="font-medium py-4">
                                                            <div className="flex items-center gap-2">
                                                                <ToolIcon className="size-4 shrink-0"
                                                                          strokeWidth={1.5}/>
                                                                <span>{sourceName}</span>
                                                            </div>
                                                        </TableCell>
                                                        <TableCell className="font-mono py-4">
                                                          <span className="text-sm">
                                                            {getToolNameFromUrn(log.toolUrn)}
                                                          </span>
                                                        </TableCell>
                                                        <TableCell className="py-4">
                                                            <div className="flex items-center justify-center">
                                                                <StatusIcon isSuccess={isSuccessfulCall(log)}/>
                                                            </div>
                                                        </TableCell>
                                                        <TableCell
                                                            className="text-muted-foreground flex text-sm py-4">
                                                            {log.userAgent || "-"}
                                                        </TableCell>
                                                        <TableCell className="text-muted-foreground font-mono py-4">
                                                            {formatDuration(log.durationMs)}
                                                        </TableCell>
                                                    </TableRow>
                                                );
                                            })}

                                            {/* Loading indicator at bottom when fetching more */}
                                            {isFetchingMore && (
                                                <TableRow>
                                                    <TableCell colSpan={6}
                                                               className="text-center py-4 text-muted-foreground">
                                                        <div className="flex items-center justify-center gap-2">
                                                            <Icon name="loader-circle" className="size-4 animate-spin"/>
                                                            <span>Loading more logs...</span>
                                                        </div>
                                                    </TableCell>
                                                </TableRow>
                                            )}
                                        </TableBody>
                                    </Table>
                                </div>

                                {/* Footer with total count */}
                                {allLogs.length > 0 && (
                                    <div className="flex items-center justify-between gap-4 px-4 py-3 bg-surface-secondary-default border-t border-neutral-softest text-sm text-muted-foreground">
                                        <span>
                                            Showing {allLogs.length} {allLogs.length === 1 ? "log" : "logs"}
                                            {hasNextPage && " • Scroll down to load more"}
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

            <LogDetailSheet
                log={selectedLog}
                open={!!selectedLog}
                onOpenChange={(open) => !open && setSelectedLog(null)}
            />
        </Page>
    );
}