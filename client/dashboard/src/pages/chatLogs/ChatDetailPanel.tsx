import { cn } from "@/lib/utils";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type {
  ChatResolution,
  TelemetryLogRecord,
} from "@gram/client/models/components";
import { useLoadChat, useSearchLogsMutation } from "@gram/client/react-query";
import { Badge, Icon, Stack } from "@speakeasy-api/moonshine";
import { format } from "date-fns";
import { useEffect, useMemo } from "react";
import { CircularProgress } from "./CircularProgress";

interface ChatDetailPanelProps {
  chatId: string;
  resolutions: ChatResolution[];
  onClose: () => void;
}

function getTraceId(chatId: string): string {
  return `trace-${chatId.slice(0, 3)}`;
}

function getOverallResolutionStatus(
  resolutions: ChatResolution[],
): "success" | "failure" | "partial" | "unresolved" {
  if (resolutions.length === 0) return "unresolved";

  const hasFailure = resolutions.some((r) => r.resolution === "failure");
  const hasSuccess = resolutions.some((r) => r.resolution === "success");

  if (hasFailure) return "failure";
  if (hasSuccess) return "success";
  return "partial";
}

function getAverageScore(resolutions: ChatResolution[]): number {
  if (resolutions.length === 0) return 0;
  const sum = resolutions.reduce((acc, r) => acc + r.score, 0);
  return Math.round(sum / resolutions.length);
}

function getContextQuality(score: number): {
  label: string;
  variant: "success" | "warning" | "destructive";
} {
  if (score >= 80) return { label: "Good Context", variant: "success" };
  if (score >= 50) return { label: "Fair Context", variant: "warning" };
  return { label: "Poor Context", variant: "destructive" };
}

function getSeverityBadgeVariant(
  severity?: string,
): "destructive" | "warning" | "neutral" {
  switch (severity?.toUpperCase()) {
    case "ERROR":
    case "FATAL":
      return "destructive";
    case "WARN":
      return "warning";
    default:
      return "neutral";
  }
}

function formatTimestamp(nanos: number): string {
  const ms = nanos / 1_000_000;
  return format(new Date(ms), "HH:mm:ss.SSS");
}

// Telemetry Logs Tab Component
function TelemetryLogsTab({
  logs,
  isLoading,
  error,
}: {
  logs: TelemetryLogRecord[];
  isLoading: boolean;
  error: Error | null;
}) {
  if (isLoading) {
    return (
      <div className="p-6 text-center text-muted-foreground">
        Loading telemetry logs...
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-6 text-center text-destructive">
        Failed to load logs: {error.message}
      </div>
    );
  }

  if (logs.length === 0) {
    return (
      <div className="p-6 text-center text-muted-foreground">
        No telemetry logs found for this chat session.
      </div>
    );
  }

  return (
    <div className="divide-y divide-border">
      {logs.map((log) => (
        <div key={log.id} className="p-4 hover:bg-muted/30 transition-colors">
          <div className="flex items-start gap-3">
            <Badge
              variant={getSeverityBadgeVariant(log.severityText)}
              className="shrink-0 mt-0.5"
            >
              {log.severityText || "INFO"}
            </Badge>
            <div className="flex-1 min-w-0 space-y-1">
              <div className="text-sm font-medium break-words">{log.body}</div>
              <div className="flex items-center gap-3 text-xs text-muted-foreground">
                <span>{formatTimestamp(log.timeUnixNano)}</span>
                {log.service?.name && (
                  <span className="flex items-center gap-1">
                    <Icon name="server" className="size-3" />
                    {log.service.name}
                  </span>
                )}
                {log.traceId && (
                  <span className="font-mono text-[10px]">
                    {log.traceId.slice(0, 8)}...
                  </span>
                )}
              </div>
              {log.attributes && Object.keys(log.attributes).length > 0 && (
                <details className="mt-2">
                  <summary className="text-xs text-muted-foreground cursor-pointer hover:text-foreground">
                    Show attributes
                  </summary>
                  <pre className="mt-1 p-2 bg-muted/50 rounded text-xs overflow-x-auto">
                    {JSON.stringify(log.attributes, null, 2)}
                  </pre>
                </details>
              )}
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

// Tool Calls Tab Component - filters logs to show only tool-related entries
function ToolCallsTab({
  logs,
  isLoading,
  error,
}: {
  logs: TelemetryLogRecord[];
  isLoading: boolean;
  error: Error | null;
}) {
  // Filter logs to find tool-related entries
  const toolLogs = useMemo(() => {
    return logs.filter((log) => {
      const body = log.body.toLowerCase();
      const hasToolKeyword =
        body.includes("tool") ||
        body.includes("function") ||
        body.includes("mcp");
      const attrs = log.attributes || {};
      const hasToolAttr =
        attrs.tool_name || attrs.function_name || attrs.gram_urn;
      return hasToolKeyword || hasToolAttr;
    });
  }, [logs]);

  if (isLoading) {
    return (
      <div className="p-6 text-center text-muted-foreground">
        Loading tool call logs...
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-6 text-center text-destructive">
        Failed to load tool calls: {error.message}
      </div>
    );
  }

  if (toolLogs.length === 0) {
    return (
      <div className="p-6 text-center text-muted-foreground">
        No tool call logs found for this chat session.
      </div>
    );
  }

  return (
    <div className="divide-y divide-border">
      {toolLogs.map((log) => {
        const attrs = log.attributes || {};
        const toolName = attrs.tool_name || attrs.function_name || "Unknown";
        const gramUrn = attrs.gram_urn;
        const status = attrs.http_status_code;

        return (
          <div key={log.id} className="p-4 hover:bg-muted/30 transition-colors">
            <div className="flex items-start gap-3">
              <div
                className={cn(
                  "size-8 rounded-full flex items-center justify-center shrink-0",
                  status && status >= 400
                    ? "bg-destructive/10"
                    : "bg-primary/10",
                )}
              >
                <Icon
                  name="zap"
                  className={cn(
                    "size-4",
                    status && status >= 400
                      ? "text-destructive"
                      : "text-primary",
                  )}
                />
              </div>
              <div className="flex-1 min-w-0 space-y-1">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium">{toolName}</span>
                  {status && (
                    <Badge variant={status >= 400 ? "destructive" : "neutral"}>
                      {status}
                    </Badge>
                  )}
                </div>
                {gramUrn && (
                  <div className="text-xs text-muted-foreground font-mono">
                    {gramUrn}
                  </div>
                )}
                <div className="text-xs text-muted-foreground">
                  {formatTimestamp(log.timeUnixNano)}
                </div>
                {log.body && (
                  <div className="text-sm text-muted-foreground mt-1">
                    {log.body}
                  </div>
                )}
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}

export function ChatDetailPanel({
  chatId,
  resolutions,
  onClose,
}: ChatDetailPanelProps) {
  const { data: chat, isLoading: chatLoading } = useLoadChat(
    { id: chatId },
    undefined,
    {},
  );

  // Fetch telemetry logs for this chat
  const {
    mutate: searchLogs,
    data: logsData,
    isPending: logsLoading,
    error: logsError,
  } = useSearchLogsMutation();

  // Trigger log search when chatId changes
  useEffect(() => {
    searchLogs({
      request: {
        searchLogsPayload: {
          filter: {
            gramChatId: chatId,
          },
          limit: 100,
        },
      },
    });
  }, [chatId, searchLogs]);

  const logs = logsData?.logs || [];

  if (chatLoading) {
    return <div className="p-8">Loading chat details...</div>;
  }

  if (!chat) {
    return <div className="p-8">Chat not found</div>;
  }

  const status = getOverallResolutionStatus(resolutions);
  const averageScore = getAverageScore(resolutions);
  const contextQuality = getContextQuality(averageScore);
  const duration = Math.round(
    (new Date(chat.updatedAt).getTime() - new Date(chat.createdAt).getTime()) /
      1000,
  );

  // Count tool calls (messages with tool role)
  const toolCallsCount = chat.messages.filter((m) => m.role === "tool").length;

  // Create a map of message IDs to resolution info for showing breakpoints
  const messageResolutionMap = new Map<string, ChatResolution>();
  resolutions.forEach((res) => {
    res.messageIds.forEach((msgId) => {
      messageResolutionMap.set(msgId, res);
    });
  });

  return (
    <div className="h-full flex flex-col bg-background">
      {/* Header */}
      <div className="p-6 border-b">
        <div className="flex items-center justify-between mb-2">
          <div className="flex items-center gap-3">
            <h2 className="text-xl font-semibold">{getTraceId(chatId)}</h2>
            {status !== "unresolved" && (
              <Badge
                variant={
                  status === "success"
                    ? "success"
                    : status === "failure"
                      ? "destructive"
                      : "warning"
                }
              >
                <Icon name="circle-check" className="size-3" />
                {status === "success"
                  ? "Resolved"
                  : status === "failure"
                    ? "Failed"
                    : "Partial"}
              </Badge>
            )}
          </div>
          <button
            onClick={onClose}
            className="p-1 rounded-md hover:bg-muted transition-colors"
            aria-label="Close panel"
          >
            <Icon name="x" className="size-5" />
          </button>
        </div>
        <div className="text-sm text-muted-foreground mb-3">
          {format(new Date(chat.createdAt), "yyyy-MM-dd HH:mm:ss")}
        </div>
        <div className="text-sm">{chat.title}</div>
      </div>

      {/* Tabs */}
      <Tabs defaultValue="overview" className="flex-1 flex flex-col min-h-0">
        <TabsList className="w-full h-auto justify-start px-6 py-0 bg-transparent border-b rounded-none gap-0">
          <TabsTrigger
            value="overview"
            className="relative rounded-none border-0 border-b-2 border-transparent shadow-none px-4 py-3 data-[state=active]:border-b-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none"
          >
            <Icon name="message-circle" className="size-4 mr-2" />
            Overview
          </TabsTrigger>
          <TabsTrigger
            value="logs"
            className="relative rounded-none border-0 border-b-2 border-transparent shadow-none px-4 py-3 data-[state=active]:border-b-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none"
          >
            <Icon name="list" className="size-4 mr-2" />
            Telemetry Logs
            {logs.length > 0 && (
              <span className="ml-1.5 text-xs bg-muted px-1.5 rounded-full">
                {logs.length}
              </span>
            )}
          </TabsTrigger>
          <TabsTrigger
            value="tools"
            className="relative rounded-none border-0 border-b-2 border-transparent shadow-none px-4 py-3 data-[state=active]:border-b-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none"
          >
            <Icon name="zap" className="size-4 mr-2" />
            Tool Calls
            {toolCallsCount > 0 && (
              <span className="ml-1.5 text-xs bg-muted px-1.5 rounded-full">
                {toolCallsCount}
              </span>
            )}
          </TabsTrigger>
        </TabsList>

        {/* Overview Tab */}
        <TabsContent
          value="overview"
          className="flex-1 overflow-y-auto m-0 data-[state=inactive]:hidden"
        >
          {/* Metadata Grid */}
          <div className="p-6 border-b bg-muted/10">
            <div className="grid grid-cols-2 gap-x-8 gap-y-4">
              <div>
                <div className="text-xs text-muted-foreground mb-1">
                  User ID:
                </div>
                <div className="text-sm font-medium">
                  {chat.externalUserId || "anonymous"}
                </div>
              </div>
              <div>
                <div className="text-xs text-muted-foreground mb-1">
                  Duration:
                </div>
                <div className="text-sm font-medium">{duration}s</div>
              </div>
              <div>
                <div className="text-xs text-muted-foreground mb-1">
                  Messages:
                </div>
                <div className="text-sm font-medium">
                  {chat.messages.length}
                </div>
              </div>
              <div>
                <div className="text-xs text-muted-foreground mb-1">
                  Tool Calls:
                </div>
                <div className="text-sm font-medium">{toolCallsCount}</div>
              </div>
              {resolutions.length > 0 && (
                <>
                  <div>
                    <div className="text-xs text-muted-foreground mb-1">
                      Resolution Score:
                    </div>
                    <div className="text-lg font-medium">{averageScore}%</div>
                  </div>
                  <div>
                    <div className="text-xs text-muted-foreground mb-1">
                      Context Quality:
                    </div>
                    <Badge variant={contextQuality.variant}>
                      <Icon name="circle-check" className="size-3" />
                      {contextQuality.label}
                    </Badge>
                  </div>
                </>
              )}
            </div>
          </div>

          {/* Resolutions Summary */}
          {resolutions.length > 0 && (
            <div className="p-6 border-b">
              <Stack direction="vertical" gap={3}>
                {resolutions.map((resolution) => (
                  <div key={resolution.id} className="flex items-start gap-4">
                    <CircularProgress
                      score={resolution.score}
                      status={
                        resolution.resolution as
                          | "success"
                          | "failure"
                          | "partial"
                      }
                      size="sm"
                    />
                    <div className="flex-1 min-w-0">
                      <div className="text-sm font-medium mb-1">
                        {resolution.userGoal}
                      </div>
                      <div className="text-xs text-muted-foreground">
                        {resolution.resolutionNotes}
                      </div>
                    </div>
                  </div>
                ))}
              </Stack>
            </div>
          )}

          {/* Chat Messages */}
          <div className="p-6">
            <Stack direction="vertical" gap={4}>
              {chat.messages.map((message) => {
                const resolution = messageResolutionMap.get(message.id);

                return (
                  <div key={message.id}>
                    {/* Resolution breakpoint */}
                    {resolution && (
                      <div className="mb-3 p-3 rounded-lg bg-primary/10 border-l-4 border-primary">
                        <div className="text-xs font-semibold">
                          Resolution Point: {resolution.resolution}
                        </div>
                      </div>
                    )}

                    {/* Message */}
                    <div className="flex items-start gap-3">
                      {message.role === "user" && (
                        <div className="size-8 rounded-full bg-primary/10 flex items-center justify-center flex-shrink-0">
                          <Icon name="user" className="size-4 text-primary" />
                        </div>
                      )}
                      {message.role === "assistant" && (
                        <div className="size-8 rounded-full bg-muted flex items-center justify-center flex-shrink-0">
                          <Icon name="bot" className="size-4" />
                        </div>
                      )}
                      {message.role === "tool" && (
                        <div className="size-8 rounded-full bg-primary flex items-center justify-center flex-shrink-0">
                          <Icon
                            name="zap"
                            className="size-4 text-primary-foreground"
                          />
                        </div>
                      )}

                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2 mb-1">
                          <span className="text-sm font-semibold capitalize">
                            {message.role}
                          </span>
                          <span className="text-xs text-muted-foreground">
                            {message.createdAt &&
                              format(new Date(message.createdAt), "HH:mm:ss")}
                          </span>
                        </div>
                        <div
                          className={cn(
                            "p-3 rounded-lg text-sm",
                            message.role === "user" && "bg-primary/5",
                            message.role === "assistant" && "bg-muted/50",
                            message.role === "tool" && "bg-background border",
                          )}
                        >
                          {message.role === "tool" &&
                          typeof message.content === "object" &&
                          message.content !== null ? (
                            <div>
                              <div className="text-xs font-semibold mb-2">
                                Parameters:
                              </div>
                              <pre className="text-xs overflow-x-auto">
                                {JSON.stringify(message.content, null, 2)}
                              </pre>
                            </div>
                          ) : (
                            <div className="whitespace-pre-wrap">
                              {typeof message.content === "string"
                                ? message.content
                                : JSON.stringify(message.content)}
                            </div>
                          )}
                        </div>
                      </div>
                    </div>
                  </div>
                );
              })}
            </Stack>
          </div>
        </TabsContent>

        {/* Telemetry Logs Tab */}
        <TabsContent
          value="logs"
          className="flex-1 overflow-y-auto m-0 data-[state=inactive]:hidden"
        >
          <TelemetryLogsTab
            logs={logs}
            isLoading={logsLoading}
            error={logsError as Error | null}
          />
        </TabsContent>

        {/* Tool Calls Tab */}
        <TabsContent
          value="tools"
          className="flex-1 overflow-y-auto m-0 data-[state=inactive]:hidden"
        >
          <ToolCallsTab
            logs={logs}
            isLoading={logsLoading}
            error={logsError as Error | null}
          />
        </TabsContent>
      </Tabs>
    </div>
  );
}
