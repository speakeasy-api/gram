import {Page} from "@/components/page-layout";
import {SearchBar} from "@/components/ui/search-bar";
import {Select, SelectContent, SelectItem, SelectTrigger, SelectValue,} from "@/components/ui/select";
import {Table, TableBody, TableCell, TableHead, TableHeader, TableRow,} from "@/components/ui/table";
import {useListToolLogs} from "@gram/client/react-query";
import {HTTPToolLog} from "@gram/client/models/components";
import {cn, Icon} from "@speakeasy-api/moonshine";
import {useEffect, useRef, useState} from "react";
import {LogDetailSheet} from "./LogDetailSheet";
import {formatTimestamp, getSourceFromUrn, getToolIcon, getToolNameFromUrn, isSuccessfulCall,} from "./utils";
import {formatDuration} from "@/lib/dates.ts";

function StatusIcon({isSuccess}: { isSuccess: boolean }) {
    return (
        <div>
            <Icon name={isSuccess ? "check" : "x"}
                  className={cn("size-4", isSuccess ? 'fill-success-default' : 'fill-destructive-default')}/>
        </div>
    );
}

export default function LogsPage() {
    const [searchQuery, setSearchQuery] = useState<string>("");
    const [toolTypeFilter, setToolTypeFilter] = useState<string>("");
    const [serverNameFilter, setServerNameFilter] = useState<string>("");
    const [statusFilter, setStatusFilter] = useState<string>("");
    const [selectedLog, setSelectedLog] = useState<HTTPToolLog | null>(null);

    // Infinite scroll state
    const [allLogs, setAllLogs] = useState<HTTPToolLog[]>([]);
    const [currentCursor, setCurrentCursor] = useState<string | undefined>(undefined);
    const [isFetchingMore, setIsFetchingMore] = useState(false);
    const lastProcessedCursorRef = useRef<string | undefined>(undefined);
    const tableContainerRef = useRef<HTMLDivElement>(null);
    const perPage = 50;

    const {data, isLoading} = useListToolLogs(
        {
            perPage,
            cursor: currentCursor,
        },
        undefined,
        {
            staleTime: 30000, // Cache for 30 seconds
            refetchOnWindowFocus: false,
        },
    );

    // Update accumulated logs when new data arrives
    useEffect(() => {
        if (data?.logs && !isLoading) {
            // Check if we've already processed this cursor to avoid duplicates
            // Skip check for initial load (both undefined is OK for first load)
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

    const pagination = data?.pagination;
    const hasNextPage = pagination?.hasNextPage ?? false;

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
                                    value={searchQuery}
                                    onChange={setSearchQuery}
                                    placeholder="Search"
                                    className="w-1/3"
                                />

                                {/* Filters */}
                                <div className="flex items-center gap-2">
                                    <Select value={toolTypeFilter} onValueChange={setToolTypeFilter}>
                                        <SelectTrigger className="w-[180px]">
                                            <SelectValue placeholder="Tool Type"/>
                                        </SelectTrigger>
                                        <SelectContent>
                                            <SelectItem value="all">All Types</SelectItem>
                                            {/* Add more tool type options here */}
                                        </SelectContent>
                                    </Select>

                                    <Select
                                        value={serverNameFilter}
                                        onValueChange={setServerNameFilter}
                                    >
                                        <SelectTrigger className="w-[180px]">
                                            <SelectValue placeholder="Server Name"/>
                                        </SelectTrigger>
                                        <SelectContent>
                                            <SelectItem value="all">All Servers</SelectItem>
                                            {/* Add more server name options here */}
                                        </SelectContent>
                                    </Select>

                                    <Select value={statusFilter} onValueChange={setStatusFilter}>
                                        <SelectTrigger className="w-[180px]">
                                            <SelectValue placeholder="Status"/>
                                        </SelectTrigger>
                                        <SelectContent>
                                            <SelectItem value="all">All Statuses</SelectItem>
                                            {/* Add more status options here */}
                                        </SelectContent>
                                    </Select>
                                </div>
                            </div>

                            {/* Table */}
                            <div
                                className="border border-neutral-softest rounded-lg overflow-hidden w-full flex flex-col">
                                <div
                                    ref={tableContainerRef}
                                    className="overflow-y-auto"
                                    style={{maxHeight: 'calc(100vh - 250px)'}}
                                    onScroll={handleScroll}
                                >
                                    <Table>
                                        <TableHeader className="sticky top-0 z-10">
                                            <TableRow
                                                className="bg-surface-secondary-default border-b border-neutral-softest">
                                                <TableHead className="font-mono">TIMESTAMP</TableHead>
                                                <TableHead className="font-mono">SERVER NAME</TableHead>
                                                <TableHead className="font-mono">TOOL NAME</TableHead>
                                                <TableHead className="font-mono">STATUS</TableHead>
                                                <TableHead className="font-mono">CLIENT</TableHead>
                                                <TableHead className="font-mono">DURATION</TableHead>
                                            </TableRow>
                                        </TableHeader>
                                        <TableBody>
                                            {isLoading && allLogs.length === 0 ? (
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
                                                        No logs found
                                                    </TableCell>
                                                </TableRow>
                                            ) : allLogs.map((log) => {
                                                const ToolIcon = getToolIcon(log.toolUrn);
                                                const sourceName = getSourceFromUrn(log.toolUrn);
                                                return (
                                                    <TableRow
                                                        key={log.id}
                                                        className="cursor-pointer hover:bg-surface-secondary-default"
                                                        onClick={() => setSelectedLog(log)}
                                                    >
                                                        <TableCell className="text-muted-foreground font-mono">
                                                            {formatTimestamp(log.ts)}
                                                        </TableCell>
                                                        <TableCell className="font-medium">
                                                            <div className="flex items-center gap-2">
                                                                <ToolIcon className="size-4 shrink-0"
                                                                          strokeWidth={1.5}/>
                                                                <span>{sourceName}</span>
                                                            </div>
                                                        </TableCell>
                                                        <TableCell className="font-mono">
                          <span className="text-sm">
                            {getToolNameFromUrn(log.toolUrn)}
                          </span>
                                                        </TableCell>
                                                        <TableCell>
                                                            <div className="flex items-center justify-center">
                                                                <StatusIcon isSuccess={isSuccessfulCall(log)}/>
                                                            </div>
                                                        </TableCell>
                                                        <TableCell className="text-muted-foreground text-sm">
                                                            {log.userAgent || "-"}
                                                        </TableCell>
                                                        <TableCell className="text-muted-foreground font-mono">
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
                                    <div
                                        className="px-4 py-3 bg-surface-secondary-default border-t border-neutral-softest text-sm text-muted-foreground">
                                        Showing {allLogs.length} {allLogs.length === 1 ? "log" : "logs"}
                                        {hasNextPage && " • Scroll down to load more"}
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