import { Page } from "@/components/page-layout";
import { SearchBar } from "@/components/ui/search-bar";
import { useSession } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import { Chat, ElementsConfig, GramElementsProvider } from "@gram-ai/elements";
import { chatSessionsCreate } from "@gram/client/funcs/chatSessionsCreate";
import { telemetrySearchToolCalls } from "@gram/client/funcs/telemetrySearchToolCalls";
import {
  FeatureName,
  TelemetryLogRecord,
} from "@gram/client/models/components";
import {
  useFeaturesSetMutation,
  useGramContext,
  useListToolsets,
} from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { Button, Icon, useMoonshineConfig } from "@speakeasy-api/moonshine";
import { useInfiniteQuery } from "@tanstack/react-query";
import { XIcon } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { LogDetailSheet } from "./LogDetailSheet";
import { TraceRow } from "./TraceRow";

const perPage = 25;

export default function LogsPage() {
  const [searchQuery, setSearchQuery] = useState<string | null>(null);
  const [searchInput, setSearchInput] = useState("");
  const [expandedTraceId, setExpandedTraceId] = useState<string | null>(null);
  const [selectedLog, setSelectedLog] = useState<TelemetryLogRecord | null>(
    null,
  );
  const containerRef = useRef<HTMLDivElement>(null);

  const client = useGramContext();
  const { theme } = useMoonshineConfig();

  const gramMcpConfig = useGramMcpConfig();

  const logsElementsConfig = useMemo<ElementsConfig>(
    () => ({
      ...gramMcpConfig,
      variant: "standalone",
      welcome: {
        title: "Explore Logs",
        subtitle: "Ask me about your logs! Powered by Elements + Gram MCP",
        suggestions: [
          {
            title: "Failing Tool Calls",
            label: "Summarize failing tool calls",
            prompt: "Summarize failing tool calls",
          },
          {
            title: "Visualize top tool calls",
            label: "Plot tool call counts",
            prompt: "Plot a chart of the top tool calls and their counts",
          },
        ],
      },
      theme: {
        colorScheme: theme === "dark" ? "dark" : "light",
      },
    }),
    [gramMcpConfig, theme],
  );

  const {
    data,
    error,
    fetchNextPage,
    hasNextPage,
    isFetching,
    isFetchingNextPage,
    refetch,
  } = useInfiniteQuery({
    queryKey: ["tool-calls", searchQuery],
    queryFn: ({ pageParam }) =>
      unwrapAsync(
        telemetrySearchToolCalls(client, {
          searchToolCallsPayload: {
            filter: searchQuery ? { gramUrn: searchQuery } : undefined,
            cursor: pageParam,
            limit: perPage,
            sort: "desc",
          },
        }),
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
  });

  // Flatten all pages into a single array of traces
  const allTraces = data?.pages.flatMap((page) => page.toolCalls) ?? [];
  const logsEnabled = data?.pages[0]?.enabled ?? true;

  const [logsMutationError, setLogsMutationError] = useState<string | null>(
    null,
  );
  const { mutateAsync: setLogsFeature, status: logsMutationStatus } =
    useFeaturesSetMutation({
      onSuccess: () => {
        setLogsMutationError(null);
        refetch();
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
      setSearchQuery(searchInput || null);
    }, 500);
    return () => clearTimeout(timeoutId);
  }, [searchInput]);

  // Handle scroll for infinite loading
  const handleScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const container = e.currentTarget;
    const scrollTop = container.scrollTop;
    const scrollHeight = container.scrollHeight;
    const clientHeight = container.clientHeight;
    const distanceFromBottom = scrollHeight - (scrollTop + clientHeight);

    if (isFetchingNextPage || isFetching) return;
    if (!hasNextPage) return;

    if (distanceFromBottom < 200) {
      fetchNextPage();
    }
  };

  const toggleExpand = (traceId: string) => {
    setExpandedTraceId((prev) => (prev === traceId ? null : traceId));
  };

  const handleLogClick = (log: TelemetryLogRecord) => {
    setSelectedLog(log);
  };

  const isLoading = isFetching && allTraces.length === 0;

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs fullWidth />
      </Page.Header>
      <Page.Body fullWidth fullHeight className="!p-0">
        <div className="flex flex-row h-full w-full">
          {/* Logs Table */}
          <div className="flex flex-col gap-4 w-1/2 min-w-0 p-8">
            {/* Search Row */}
            <div className="flex items-center gap-4">
              <SearchBar
                value={searchInput}
                onChange={setSearchInput}
                placeholder="Search by tool URN"
                className="w-1/3"
              />
            </div>

            {/* Trace list container */}
            <div className="border border-border rounded-lg overflow-hidden w-full flex flex-col flex-1 relative bg-surface-default">
              {/* Loading indicator */}
              {isFetching && allTraces.length > 0 && (
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
                className="overflow-y-auto flex-1"
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
                ) : isLoading ? (
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
                      searchQuery ? (
                        "No traces match your search"
                      ) : (
                        "No traces found"
                      )
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

                    {isFetchingNextPage && (
                      <div className="flex items-center justify-center gap-2 py-4 text-muted-foreground border-t border-border">
                        <Icon
                          name="loader-circle"
                          className="size-4 animate-spin"
                        />
                        <span className="text-sm">Loading more traces...</span>
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

          {/* Chat Panel */}
          <div className="w-1/2 border-l border-border p-8">
            <GramElementsProvider config={logsElementsConfig}>
              <Chat />
            </GramElementsProvider>
          </div>
        </div>
      </Page.Body>

      {/* Log Detail Sheet */}
      <LogDetailSheet
        log={selectedLog}
        open={!!selectedLog}
        onOpenChange={(open) => !open && setSelectedLog(null)}
      />
    </Page>
  );
}

const useGramMcpConfig = () => {
  const { projectSlug } = useSlugs();
  const client = useGramContext();
  const isLocal = process.env.NODE_ENV === "development";
  const { session } = useSession();

  // For local development, look up the gram-seed toolset in the kitchen-sink project
  const { data: toolsets } = useListToolsets(
    {
      gramProject: "kitchen-sink",
    },
    undefined,
    {
      enabled: isLocal,
      headers: {
        "gram-project": "kitchen-sink",
      },
    },
  );

  const getSession = useCallback(async (): Promise<string> => {
    const res = await chatSessionsCreate(
      client,
      {
        createRequestBody: {
          embedOrigin: window.location.origin,
        },
      },
      undefined,
      {
        headers: {
          "Gram-Project": projectSlug ?? "",
        },
      },
    );
    return res.value?.clientToken ?? "";
  }, [client, projectSlug]);

  const gramToolset = useMemo(() => {
    return toolsets?.toolsets.find((toolset) => toolset.slug === "gram-seed");
  }, [toolsets]);

  const toolsToInclude = useCallback(
    ({ toolName }: { toolName: string }) => toolName.includes("logs"),
    [],
  );

  return useMemo(() => {
    const baseConfig: ElementsConfig = {
      projectSlug: "kitchen-sink",
      tools: {
        toolsToInclude,
      },
      api: {
        url: getServerURL(),
        sessionFn: getSession,
      },
      environment: {
        GRAM_SERVER_URL: getServerURL(),
        GRAM_SESSION_HEADER_GRAM_SESSION: session,
        GRAM_APIKEY_HEADER_GRAM_KEY: "", // This must be set or else the tool call will fail
        GRAM_PROJECT_SLUG_HEADER_GRAM_PROJECT: projectSlug,
      },
    };

    if (isLocal) {
      if (toolsets && !gramToolset) {
        throw new Error("No gram-seed toolset found--have you run mise seed?");
      }

      return {
        ...baseConfig,
        ...(gramToolset && {
          mcp: `${getServerURL()}/mcp/${gramToolset?.mcpSlug}`,
        }),
      };
    }

    const mcpUrl = getServerURL().includes("app.getgram.ai")
      ? "https://app.getgram.ai/mcp/speakeasy-team-gram"
      : "https://dev.getgram.ai/mcp/speakeasy-team-gram";

    return {
      ...baseConfig,
      mcp: mcpUrl,
    };
  }, [
    getSession,
    gramToolset,
    isLocal,
    projectSlug,
    session,
    toolsets,
    toolsToInclude,
  ]);
};
