import { CopilotSidebar, useCopilotState } from "@/components/copilot-sidebar";
import { SearchBar } from "@/components/ui/search-bar";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { cn } from "@/lib/utils";
import { ServiceError } from "@gram/client/models/errors/serviceerror";
import { telemetrySearchToolCalls } from "@gram/client/funcs/telemetrySearchToolCalls";
import {
  FeatureName,
  TelemetryLogRecord,
  ToolCallSummary,
} from "@gram/client/models/components";
import {
  useFeaturesSetMutation,
  useGramContext,
} from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { useInfiniteQuery } from "@tanstack/react-query";
import { XIcon } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { LogDetailSheet } from "./LogDetailSheet";
import { TraceRow } from "./TraceRow";

const perPage = 25;

// Dummy data for preview/development
const DUMMY_TRACES: ToolCallSummary[] = [
  {
    traceId: "a1b2c3d4e5f6789012345678901234ab",
    gramUrn: "urn:gram:github:repos/list",
    httpStatusCode: 200,
    logCount: 3,
    startTimeUnixNano: Date.now() * 1_000_000 - 60_000_000_000, // 1 min ago
  },
  {
    traceId: "b2c3d4e5f678901234567890123456cd",
    gramUrn: "urn:gram:stripe:customers/create",
    httpStatusCode: 201,
    logCount: 5,
    startTimeUnixNano: Date.now() * 1_000_000 - 180_000_000_000, // 3 min ago
  },
  {
    traceId: "c3d4e5f6789012345678901234567890",
    gramUrn: "urn:gram:openai:chat/completions",
    httpStatusCode: 200,
    logCount: 2,
    startTimeUnixNano: Date.now() * 1_000_000 - 300_000_000_000, // 5 min ago
  },
  {
    traceId: "d4e5f67890123456789012345678abcd",
    gramUrn: "urn:gram:slack:messages/post",
    httpStatusCode: 500,
    logCount: 4,
    startTimeUnixNano: Date.now() * 1_000_000 - 420_000_000_000, // 7 min ago
  },
  {
    traceId: "e5f6789012345678901234567890efgh",
    gramUrn: "urn:gram:postgres:query/execute",
    httpStatusCode: 200,
    logCount: 1,
    startTimeUnixNano: Date.now() * 1_000_000 - 600_000_000_000, // 10 min ago
  },
  {
    traceId: "f67890123456789012345678901234ij",
    gramUrn: "urn:gram:github:issues/create",
    httpStatusCode: 201,
    logCount: 6,
    startTimeUnixNano: Date.now() * 1_000_000 - 900_000_000_000, // 15 min ago
  },
  {
    traceId: "g7890123456789012345678901234klm",
    gramUrn: "urn:gram:aws:s3/upload",
    httpStatusCode: 403,
    logCount: 2,
    startTimeUnixNano: Date.now() * 1_000_000 - 1200_000_000_000, // 20 min ago
  },
  {
    traceId: "h890123456789012345678901234nopq",
    gramUrn: "urn:gram:anthropic:messages/create",
    httpStatusCode: 200,
    logCount: 3,
    startTimeUnixNano: Date.now() * 1_000_000 - 1500_000_000_000, // 25 min ago
  },
  {
    traceId: "i90123456789012345678901234rstuv",
    gramUrn: "urn:gram:linear:issues/list",
    httpStatusCode: 200,
    logCount: 2,
    startTimeUnixNano: Date.now() * 1_000_000 - 1800_000_000_000, // 30 min ago
  },
  {
    traceId: "j0123456789012345678901234wxyz12",
    gramUrn: "urn:gram:notion:pages/create",
    httpStatusCode: 404,
    logCount: 3,
    startTimeUnixNano: Date.now() * 1_000_000 - 2100_000_000_000, // 35 min ago
  },
  {
    traceId: "k123456789012345678901234567890a",
    gramUrn: "urn:gram:stripe:payments/process",
    httpStatusCode: 200,
    logCount: 8,
    startTimeUnixNano: Date.now() * 1_000_000 - 2400_000_000_000, // 40 min ago
  },
  {
    traceId: "l23456789012345678901234567890bc",
    gramUrn: "urn:gram:sendgrid:emails/send",
    httpStatusCode: 202,
    logCount: 2,
    startTimeUnixNano: Date.now() * 1_000_000 - 2700_000_000_000, // 45 min ago
  },
  {
    traceId: "m3456789012345678901234567890def",
    gramUrn: "urn:gram:twilio:sms/send",
    httpStatusCode: 200,
    logCount: 1,
    startTimeUnixNano: Date.now() * 1_000_000 - 3000_000_000_000, // 50 min ago
  },
  {
    traceId: "n456789012345678901234567890ghij",
    gramUrn: "urn:gram:redis:cache/get",
    httpStatusCode: 200,
    logCount: 1,
    startTimeUnixNano: Date.now() * 1_000_000 - 3300_000_000_000, // 55 min ago
  },
  {
    traceId: "o56789012345678901234567890klmno",
    gramUrn: "urn:gram:elasticsearch:search/query",
    httpStatusCode: 500,
    logCount: 4,
    startTimeUnixNano: Date.now() * 1_000_000 - 3600_000_000_000, // 1 hour ago
  },
];

// Flag to use dummy data for development preview
export const USE_DUMMY_DATA = true;

// Dummy span/log data for each trace
export const DUMMY_LOGS_BY_TRACE: Record<string, TelemetryLogRecord[]> = {
  a1b2c3d4e5f6789012345678901234ab: [
    {
      id: "log-1-1",
      traceId: "a1b2c3d4e5f6789012345678901234ab",
      spanId: "span1234567890ab",
      body: "Initiating GitHub API request to list repositories",
      severityText: "INFO",
      timeUnixNano: Date.now() * 1_000_000 - 60_000_000_000,
      observedTimeUnixNano: Date.now() * 1_000_000 - 60_000_000_000,
      service: { name: "gram-mcp", version: "1.0.0" },
      attributes: {
        "http.method": "GET",
        "http.url": "https://api.github.com/user/repos",
      },
    },
    {
      id: "log-1-2",
      traceId: "a1b2c3d4e5f6789012345678901234ab",
      spanId: "span1234567890ab",
      body: "Successfully authenticated with GitHub token",
      severityText: "DEBUG",
      timeUnixNano: Date.now() * 1_000_000 - 59_500_000_000,
      observedTimeUnixNano: Date.now() * 1_000_000 - 59_500_000_000,
      service: { name: "gram-mcp", version: "1.0.0" },
    },
    {
      id: "log-1-3",
      traceId: "a1b2c3d4e5f6789012345678901234ab",
      spanId: "span1234567890ab",
      body: "Received 42 repositories from GitHub API",
      severityText: "INFO",
      timeUnixNano: Date.now() * 1_000_000 - 59_000_000_000,
      observedTimeUnixNano: Date.now() * 1_000_000 - 59_000_000_000,
      service: { name: "gram-mcp", version: "1.0.0" },
      attributes: { "response.count": 42 },
    },
  ],
  b2c3d4e5f678901234567890123456cd: [
    {
      id: "log-2-1",
      traceId: "b2c3d4e5f678901234567890123456cd",
      spanId: "span2345678901bc",
      body: "Creating new Stripe customer",
      severityText: "INFO",
      timeUnixNano: Date.now() * 1_000_000 - 180_000_000_000,
      observedTimeUnixNano: Date.now() * 1_000_000 - 180_000_000_000,
      service: { name: "gram-mcp", version: "1.0.0" },
    },
    {
      id: "log-2-2",
      traceId: "b2c3d4e5f678901234567890123456cd",
      spanId: "span2345678901bc",
      body: "Validating customer email: user@example.com",
      severityText: "DEBUG",
      timeUnixNano: Date.now() * 1_000_000 - 179_800_000_000,
      observedTimeUnixNano: Date.now() * 1_000_000 - 179_800_000_000,
      service: { name: "gram-mcp", version: "1.0.0" },
    },
    {
      id: "log-2-3",
      traceId: "b2c3d4e5f678901234567890123456cd",
      spanId: "span2345678901bc",
      body: "Stripe API request: POST /v1/customers",
      severityText: "INFO",
      timeUnixNano: Date.now() * 1_000_000 - 179_500_000_000,
      observedTimeUnixNano: Date.now() * 1_000_000 - 179_500_000_000,
      service: { name: "gram-mcp", version: "1.0.0" },
      attributes: { "stripe.api_version": "2023-10-16" },
    },
    {
      id: "log-2-4",
      traceId: "b2c3d4e5f678901234567890123456cd",
      spanId: "span2345678901bc",
      body: "Customer created successfully: cus_abc123def456",
      severityText: "INFO",
      timeUnixNano: Date.now() * 1_000_000 - 179_000_000_000,
      observedTimeUnixNano: Date.now() * 1_000_000 - 179_000_000_000,
      service: { name: "gram-mcp", version: "1.0.0" },
    },
    {
      id: "log-2-5",
      traceId: "b2c3d4e5f678901234567890123456cd",
      spanId: "span2345678901bc",
      body: "Sending welcome email to customer",
      severityText: "DEBUG",
      timeUnixNano: Date.now() * 1_000_000 - 178_500_000_000,
      observedTimeUnixNano: Date.now() * 1_000_000 - 178_500_000_000,
      service: { name: "gram-mcp", version: "1.0.0" },
    },
  ],
  d4e5f67890123456789012345678abcd: [
    {
      id: "log-4-1",
      traceId: "d4e5f67890123456789012345678abcd",
      spanId: "span4567890123de",
      body: "Posting message to Slack channel #general",
      severityText: "INFO",
      timeUnixNano: Date.now() * 1_000_000 - 420_000_000_000,
      observedTimeUnixNano: Date.now() * 1_000_000 - 420_000_000_000,
      service: { name: "gram-mcp", version: "1.0.0" },
    },
    {
      id: "log-4-2",
      traceId: "d4e5f67890123456789012345678abcd",
      spanId: "span4567890123de",
      body: "Slack API rate limit exceeded",
      severityText: "WARN",
      timeUnixNano: Date.now() * 1_000_000 - 419_500_000_000,
      observedTimeUnixNano: Date.now() * 1_000_000 - 419_500_000_000,
      service: { name: "gram-mcp", version: "1.0.0" },
      attributes: { retry_after: 30 },
    },
    {
      id: "log-4-3",
      traceId: "d4e5f67890123456789012345678abcd",
      spanId: "span4567890123de",
      body: "Retrying request after 30 seconds...",
      severityText: "INFO",
      timeUnixNano: Date.now() * 1_000_000 - 389_500_000_000,
      observedTimeUnixNano: Date.now() * 1_000_000 - 389_500_000_000,
      service: { name: "gram-mcp", version: "1.0.0" },
    },
    {
      id: "log-4-4",
      traceId: "d4e5f67890123456789012345678abcd",
      spanId: "span4567890123de",
      body: "Internal server error from Slack API: Connection reset by peer",
      severityText: "ERROR",
      timeUnixNano: Date.now() * 1_000_000 - 389_000_000_000,
      observedTimeUnixNano: Date.now() * 1_000_000 - 389_000_000_000,
      service: { name: "gram-mcp", version: "1.0.0" },
      attributes: { "error.code": "ECONNRESET" },
    },
  ],
  g7890123456789012345678901234klm: [
    {
      id: "log-7-1",
      traceId: "g7890123456789012345678901234klm",
      spanId: "span7890123456gh",
      body: "Uploading file to S3 bucket: my-bucket/uploads/file.pdf",
      severityText: "INFO",
      timeUnixNano: Date.now() * 1_000_000 - 1200_000_000_000,
      observedTimeUnixNano: Date.now() * 1_000_000 - 1200_000_000_000,
      service: { name: "gram-mcp", version: "1.0.0" },
    },
    {
      id: "log-7-2",
      traceId: "g7890123456789012345678901234klm",
      spanId: "span7890123456gh",
      body: "Access Denied: User does not have s3:PutObject permission",
      severityText: "ERROR",
      timeUnixNano: Date.now() * 1_000_000 - 1199_500_000_000,
      observedTimeUnixNano: Date.now() * 1_000_000 - 1199_500_000_000,
      service: { name: "gram-mcp", version: "1.0.0" },
      attributes: {
        "aws.error_code": "AccessDenied",
        "aws.bucket": "my-bucket",
      },
    },
  ],
  o56789012345678901234567890klmno: [
    {
      id: "log-15-1",
      traceId: "o56789012345678901234567890klmno",
      spanId: "spano56789012345",
      body: "Executing Elasticsearch query on index: logs-*",
      severityText: "INFO",
      timeUnixNano: Date.now() * 1_000_000 - 3600_000_000_000,
      observedTimeUnixNano: Date.now() * 1_000_000 - 3600_000_000_000,
      service: { name: "gram-mcp", version: "1.0.0" },
    },
    {
      id: "log-15-2",
      traceId: "o56789012345678901234567890klmno",
      spanId: "spano56789012345",
      body: "Query timeout after 30 seconds",
      severityText: "WARN",
      timeUnixNano: Date.now() * 1_000_000 - 3570_000_000_000,
      observedTimeUnixNano: Date.now() * 1_000_000 - 3570_000_000_000,
      service: { name: "gram-mcp", version: "1.0.0" },
    },
    {
      id: "log-15-3",
      traceId: "o56789012345678901234567890klmno",
      spanId: "spano56789012345",
      body: "Elasticsearch cluster unhealthy: 2/5 nodes responding",
      severityText: "ERROR",
      timeUnixNano: Date.now() * 1_000_000 - 3569_500_000_000,
      observedTimeUnixNano: Date.now() * 1_000_000 - 3569_500_000_000,
      service: { name: "gram-mcp", version: "1.0.0" },
      attributes: {
        "es.cluster": "production",
        "es.nodes_available": 2,
        "es.nodes_total": 5,
      },
    },
    {
      id: "log-15-4",
      traceId: "o56789012345678901234567890klmno",
      spanId: "spano56789012345",
      body: "Request failed: ServiceUnavailable",
      severityText: "ERROR",
      timeUnixNano: Date.now() * 1_000_000 - 3569_000_000_000,
      observedTimeUnixNano: Date.now() * 1_000_000 - 3569_000_000_000,
      service: { name: "gram-mcp", version: "1.0.0" },
    },
  ],
};

export default function LogsPage() {
  // Copilot config - filter to logs-related tools only
  const logsToolFilter = useCallback(
    ({ toolName }: { toolName: string }) =>
      toolName.toLowerCase().includes("logs"),
    [],
  );
  const mcpConfig = useObservabilityMcpConfig({
    toolsToInclude: logsToolFilter,
  });

  return (
    <CopilotSidebar
      mcpConfig={mcpConfig}
      title="Explore Logs"
      subtitle="Ask me about your logs! Powered by Elements + Gram MCP"
      suggestions={[
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
        {
          title: "Recent Errors",
          label: "Find recent errors",
          prompt: "Search for recent error logs and summarize what's happening",
        },
      ]}
    >
      <LogsContent />
    </CopilotSidebar>
  );
}

function LogsContent() {
  const { isExpanded: isCopilotOpen } = useCopilotState();
  const [searchQuery, setSearchQuery] = useState<string | null>(null);
  const [searchInput, setSearchInput] = useState("");
  const [expandedTraceId, setExpandedTraceId] = useState<string | null>(null);
  const [selectedLog, setSelectedLog] = useState<TelemetryLogRecord | null>(
    null,
  );
  const containerRef = useRef<HTMLDivElement>(null);

  const client = useGramContext();

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
    throwOnError: false,
  });

  // Flatten all pages into a single array of traces (use dummy data if enabled)
  const apiTraces = data?.pages.flatMap((page) => page.toolCalls) ?? [];
  const allTraces = USE_DUMMY_DATA ? DUMMY_TRACES : apiTraces;
  const logsEnabled = USE_DUMMY_DATA ? true : (data?.pages[0]?.enabled ?? true);

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

  const handleSetLogs = (enabled: boolean) => {
    setLogsMutationError(null);
    setLogsFeature({
      request: {
        setProductFeatureRequestBody: {
          featureName: FeatureName.Logs,
          enabled,
        },
      },
    });
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

  const isLoading = !USE_DUMMY_DATA && isFetching && allTraces.length === 0;

  return (
    <div className="flex flex-col h-full w-full overflow-hidden">
      {/* Header section */}
      <div className="p-6 border-b shrink-0">
        <div
          className={cn(
            "flex gap-4 mb-4 transition-all duration-300",
            isCopilotOpen
              ? "flex-col items-stretch"
              : "flex-row items-center justify-between",
          )}
        >
          <div className="min-w-0">
            <h1 className="text-2xl font-semibold mb-1">Logs</h1>
            <p className="text-sm text-muted-foreground">
              Browse raw tool call traces and telemetry data
            </p>
          </div>
        </div>
        {/* Search Row */}
        <SearchBar
          value={searchInput}
          onChange={setSearchInput}
          placeholder="Search by tool URN"
          className="max-w-md"
        />
      </div>

      {/* Content section */}
      <div className="flex-1 overflow-hidden relative min-h-0">
        {/* Trace list container */}
        <div className="h-full flex flex-col bg-background">
          {/* Loading indicator */}
          {isFetching && allTraces.length > 0 && (
            <div className="absolute top-0 left-0 right-0 h-1 bg-primary/20 z-20">
              <div className="h-full bg-primary animate-pulse" />
            </div>
          )}

          {/* Header */}
          <div className="flex items-center gap-3 px-5 py-2.5 bg-muted/30 border-b text-xs font-medium text-muted-foreground uppercase tracking-wide shrink-0">
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
            <TraceListContent
              error={USE_DUMMY_DATA ? null : error}
              isLoading={isLoading}
              logsEnabled={logsEnabled}
              allTraces={allTraces}
              searchQuery={searchQuery}
              expandedTraceId={expandedTraceId}
              isFetchingNextPage={USE_DUMMY_DATA ? false : isFetchingNextPage}
              isMutatingLogs={isMutatingLogs}
              logsMutationError={logsMutationError}
              onEnableLogs={() => handleSetLogs(true)}
              onToggleExpand={toggleExpand}
              onLogClick={handleLogClick}
            />
          </div>

          {/* Footer */}
          {allTraces.length > 0 && (
            <div className="flex items-center justify-between gap-4 px-5 py-3 bg-muted/30 border-t text-sm text-muted-foreground shrink-0">
              <span>
                {allTraces.length} {allTraces.length === 1 ? "trace" : "traces"}
                {!USE_DUMMY_DATA && hasNextPage && " • Scroll to load more"}
              </span>
              {logsEnabled ? (
                <Button
                  onClick={() => handleSetLogs(false)}
                  disabled={isMutatingLogs}
                  size="sm"
                  variant="secondary"
                >
                  <Button.Text>
                    {isMutatingLogs ? "Updating..." : "Disable Logs"}
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
                    {isMutatingLogs ? "Updating..." : "Enable Logs"}
                  </Button.Text>
                </Button>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Log Detail Sheet */}
      <LogDetailSheet
        log={selectedLog}
        open={!!selectedLog}
        onOpenChange={(open) => !open && setSelectedLog(null)}
      />
    </div>
  );
}

function TraceListContent({
  error,
  isLoading,
  logsEnabled,
  allTraces,
  searchQuery,
  expandedTraceId,
  isFetchingNextPage,
  isMutatingLogs,
  logsMutationError,
  onEnableLogs,
  onToggleExpand,
  onLogClick,
}: {
  error: Error | null;
  isLoading: boolean;
  logsEnabled: boolean;
  allTraces: ToolCallSummary[];
  searchQuery: string | null;
  expandedTraceId: string | null;
  isFetchingNextPage: boolean;
  isMutatingLogs: boolean;
  logsMutationError: string | null;
  onEnableLogs: () => void;
  onToggleExpand: (traceId: string) => void;
  onLogClick: (log: TelemetryLogRecord) => void;
}) {
  if (error instanceof ServiceError && error.statusCode === 404) {
    return (
      <LogsDisabledState
        onEnableLogs={onEnableLogs}
        isMutating={isMutatingLogs}
        mutationError={logsMutationError}
      />
    );
  }

  if (error) {
    return (
      <LogsError
        error={
          error instanceof Error
            ? error
            : new Error("An unexpected error occurred")
        }
      />
    );
  }

  if (isLoading) {
    return <LogsLoading />;
  }

  if (allTraces.length === 0) {
    if (!logsEnabled) {
      return (
        <LogsDisabledState
          onEnableLogs={onEnableLogs}
          isMutating={isMutatingLogs}
          mutationError={logsMutationError}
        />
      );
    }
    return <LogsEmptyState searchQuery={searchQuery} />;
  }

  return (
    <>
      {allTraces.map((trace) => (
        <TraceRow
          key={trace.traceId}
          trace={trace}
          isExpanded={expandedTraceId === trace.traceId}
          onToggle={() => onToggleExpand(trace.traceId)}
          onLogClick={onLogClick}
        />
      ))}

      {isFetchingNextPage && (
        <div className="flex items-center justify-center gap-2 py-4 text-muted-foreground border-t">
          <Icon name="loader-circle" className="size-4 animate-spin" />
          <span className="text-sm">Loading more traces...</span>
        </div>
      )}
    </>
  );
}

function LogsDisabledState({
  onEnableLogs,
  isMutating,
  mutationError,
}: {
  onEnableLogs: () => void;
  isMutating: boolean;
  mutationError: string | null;
}) {
  return (
    <div className="py-12 text-center text-muted-foreground">
      <div className="flex flex-col items-center gap-3">
        <div className="size-12 rounded-full bg-muted flex items-center justify-center mb-2">
          <Icon name="scroll-text" className="size-6 text-muted-foreground" />
        </div>
        <span className="font-medium text-foreground">
          Logs are disabled for your organization
        </span>
        <span className="text-sm max-w-sm">
          Enable logs to capture tool call traces and telemetry data for
          debugging and analysis.
        </span>
        <Button
          onClick={onEnableLogs}
          disabled={isMutating}
          size="sm"
          className="mt-2"
        >
          <Button.LeftIcon>
            <Icon name="test-tube-diagonal" className="size-4" />
          </Button.LeftIcon>
          <Button.Text>
            {isMutating ? "Enabling..." : "Enable Logs"}
          </Button.Text>
        </Button>
        {mutationError && (
          <span className="text-sm text-destructive">{mutationError}</span>
        )}
      </div>
    </div>
  );
}

function LogsError({ error }: { error: Error }) {
  return (
    <div className="flex flex-col items-center gap-3 py-12">
      <div className="size-12 rounded-full bg-destructive/10 flex items-center justify-center">
        <XIcon className="size-6 text-destructive" />
      </div>
      <span className="font-medium text-foreground">Error loading traces</span>
      <span className="text-sm text-muted-foreground max-w-sm text-center">
        {error.message}
      </span>
    </div>
  );
}

function LogsLoading() {
  return (
    <div className="flex items-center justify-center gap-2 py-12 text-muted-foreground">
      <Icon name="loader-circle" className="size-5 animate-spin" />
      <span>Loading traces...</span>
    </div>
  );
}

function LogsEmptyState({ searchQuery }: { searchQuery: string | null }) {
  return (
    <div className="py-12 text-center">
      <div className="flex flex-col items-center gap-3">
        <div className="size-12 rounded-full bg-muted flex items-center justify-center">
          <Icon name="inbox" className="size-6 text-muted-foreground" />
        </div>
        <span className="font-medium text-foreground">
          {searchQuery ? "No matching traces" : "No traces found"}
        </span>
        <span className="text-sm text-muted-foreground max-w-sm">
          {searchQuery
            ? "Try adjusting your search query"
            : "Traces will appear here when tool calls are made"}
        </span>
      </div>
    </div>
  );
}
