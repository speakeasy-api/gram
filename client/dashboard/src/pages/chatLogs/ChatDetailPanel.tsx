import { cn } from "@/lib/utils";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { CodeBlock } from "@/components/ui/code-block";
import type {
  ChatResolution,
  TelemetryLogRecord,
} from "@gram/client/models/components";
import { useLoadChat, useSearchLogsMutation } from "@gram/client/react-query";
import { useRiskListResults } from "@gram/client/react-query/riskListResults.js";
import { Badge, Icon, Stack } from "@speakeasy-api/moonshine";
import { format } from "date-fns";
import { useEffect, useMemo, useState } from "react";
import { Dialog } from "@/components/ui/dialog";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Button } from "@speakeasy-api/moonshine";
import type { RiskResult } from "@gram/client/models/components";
import { CircularProgress } from "./CircularProgress";
import { HookSourceIcon } from "@/pages/hooks/HookSourceIcon";

interface ChatDetailPanelProps {
  chatId: string;
  resolutions: ChatResolution[];
  onClose: () => void;
  onDelete: (chatId: string) => void;
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

interface ToolCall {
  id?: string;
  type?: string;
  name?: string;
  function?: {
    name?: string;
    arguments?: string | object;
  };
}

function formatTimestamp(nanos: string): string {
  const ms = Number(BigInt(nanos) / 1_000_000n);
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
      <div className="text-muted-foreground p-6 text-center">
        Loading telemetry logs...
      </div>
    );
  }

  if (error) {
    return (
      <div className="text-destructive p-6 text-center">
        Failed to load logs: {error.message}
      </div>
    );
  }

  if (logs.length === 0) {
    return (
      <div className="text-muted-foreground p-6 text-center">
        No telemetry logs found for this agent session.
      </div>
    );
  }

  return (
    <div className="divide-border divide-y">
      {logs.map((log) => (
        <div key={log.id} className="hover:bg-muted/30 p-4 transition-colors">
          <div className="flex items-start gap-3">
            <Badge
              variant={getSeverityBadgeVariant(log.severityText)}
              className="mt-0.5 shrink-0"
            >
              {log.severityText || "INFO"}
            </Badge>
            <div className="min-w-0 flex-1 space-y-1">
              <div className="text-sm font-medium break-words">
                {log.body.trim()}
              </div>
              <div className="text-muted-foreground flex items-center gap-3 text-xs">
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
                  <summary className="text-muted-foreground hover:text-foreground cursor-pointer text-xs">
                    Show attributes
                  </summary>
                  <pre className="bg-muted/50 mt-1 overflow-x-auto rounded p-2 text-xs">
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

function filterToolLogs(logs: TelemetryLogRecord[]): TelemetryLogRecord[] {
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
}

// Tool Calls Tab Component - filters logs to show only tool-related entries
function ToolCallsTab({
  toolLogs,
  isLoading,
  error,
}: {
  toolLogs: TelemetryLogRecord[];
  isLoading: boolean;
  error: Error | null;
}) {
  if (isLoading) {
    return (
      <div className="text-muted-foreground p-6 text-center">
        Loading tool call logs...
      </div>
    );
  }

  if (error) {
    return (
      <div className="text-destructive p-6 text-center">
        Failed to load tool calls: {error.message}
      </div>
    );
  }

  if (toolLogs.length === 0) {
    return (
      <div className="text-muted-foreground p-6 text-center">
        No tool call logs found for this agent session.
      </div>
    );
  }

  return (
    <div className="divide-border divide-y">
      {toolLogs.map((log) => {
        const attrs = log.attributes || {};
        const toolName = attrs.tool_name || attrs.function_name || "Unknown";
        const gramUrn = attrs.gram_urn;
        const status = attrs.http_status_code;

        return (
          <div key={log.id} className="hover:bg-muted/30 p-4 transition-colors">
            <div className="flex items-start gap-3">
              <div
                className={cn(
                  "flex size-8 shrink-0 items-center justify-center rounded-full",
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
              <div className="min-w-0 flex-1 space-y-1">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium">{toolName}</span>
                  {status && (
                    <Badge variant={status >= 400 ? "destructive" : "neutral"}>
                      {status}
                    </Badge>
                  )}
                </div>
                {gramUrn && (
                  <div className="text-muted-foreground font-mono text-xs">
                    {gramUrn}
                  </div>
                )}
                <div className="text-muted-foreground text-xs">
                  {formatTimestamp(log.timeUnixNano)}
                </div>
                {log.body && (
                  <div className="text-muted-foreground mt-1 text-sm">
                    {log.body.trim()}
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

function RiskBadgePopover({ results }: { results: RiskResult[] }) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button type="button" className="cursor-pointer">
          <Badge variant="destructive" className="text-xs">
            <Icon name="shield-alert" className="mr-1 size-3" />
            {results.length} {results.length === 1 ? "Risk" : "Risks"}
          </Badge>
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80">
        <div className="space-y-3">
          <div className="text-sm font-semibold">Risk Findings</div>
          <div className="divide-border max-h-60 divide-y overflow-y-auto">
            {results.map((r) => (
              <div key={r.id} className="py-2 first:pt-0 last:pb-0">
                <div className="flex items-center gap-2">
                  <Badge variant="destructive" className="shrink-0 text-[10px]">
                    {r.source}
                  </Badge>
                  {r.ruleId && (
                    <span className="text-muted-foreground truncate font-mono text-xs">
                      {r.ruleId}
                    </span>
                  )}
                </div>
                {r.description && (
                  <p className="text-muted-foreground mt-1 text-xs">
                    {r.description}
                  </p>
                )}
                {r.match && (
                  <code className="bg-destructive/10 text-destructive mt-1 inline-block rounded px-1.5 py-0.5 font-mono text-xs break-all">
                    {r.match}
                  </code>
                )}
                {r.tags && r.tags.length > 0 && (
                  <div className="mt-1 flex flex-wrap gap-1">
                    {r.tags.map((tag) => (
                      <Badge
                        key={tag}
                        variant="neutral"
                        className="text-[10px]"
                      >
                        {tag}
                      </Badge>
                    ))}
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}

export function ChatDetailPanel({
  chatId,
  resolutions,
  onClose,
  onDelete,
}: ChatDetailPanelProps) {
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
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
  const toolLogs = useMemo(() => filterToolLogs(logs), [logs]);

  // Fetch risk findings for this chat
  const { data: riskData } = useRiskListResults({ chatId });
  const riskResultsByMessage = useMemo(() => {
    const map = new Map<string, RiskResult[]>();
    for (const r of riskData?.results ?? []) {
      const existing = map.get(r.chatMessageId);
      if (existing) {
        existing.push(r);
      } else {
        map.set(r.chatMessageId, [r]);
      }
    }
    return map;
  }, [riskData]);

  if (chatLoading) {
    return <div className="p-8">Loading chat details...</div>;
  }

  if (!chat) {
    return <div className="p-8">Chat not found</div>;
  }

  const status = getOverallResolutionStatus(resolutions);
  const averageScore = getAverageScore(resolutions);
  const contextQuality = getContextQuality(averageScore);
  // Use lastMessageTimestamp if available, otherwise fall back to updatedAt
  const endTime = chat.lastMessageTimestamp ?? chat.updatedAt;
  const duration = Math.round(
    (new Date(endTime).getTime() - new Date(chat.createdAt).getTime()) / 1000,
  );

  // Filter out system messages for display count
  const nonSystemMessages = chat.messages.filter((m) => m.role !== "system");

  // Get system messages for the System Prompt tab
  const systemMessages = chat.messages.filter((m) => m.role === "system");

  // Create a map of message IDs to resolution info for showing breakpoints
  const messageResolutionMap = new Map<string, ChatResolution>();
  resolutions.forEach((res) => {
    res.messageIds.forEach((msgId) => {
      messageResolutionMap.set(msgId, res);
    });
  });

  return (
    <div className="bg-background flex h-full flex-col">
      {/* Header */}
      <div className="border-b p-6">
        <div className="mb-2 flex items-center justify-between">
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
          <div className="flex items-center gap-1">
            <button
              onClick={() => setShowDeleteConfirm(true)}
              className="hover:bg-destructive/10 text-muted-foreground hover:text-destructive rounded-md p-1 transition-colors"
              aria-label="Delete chat"
            >
              <Icon name="trash-2" className="size-5" />
            </button>
            <button
              onClick={onClose}
              className="hover:bg-muted rounded-md p-1 transition-colors"
              aria-label="Close panel"
            >
              <Icon name="x" className="size-5" />
            </button>
          </div>
        </div>
        <div className="text-muted-foreground mb-3 text-sm">
          {format(new Date(chat.createdAt), "yyyy-MM-dd HH:mm:ss")}
        </div>
        <div className="text-sm">{chat.title}</div>
      </div>

      {/* Tabs */}
      <Tabs defaultValue="overview" className="flex min-h-0 flex-1 flex-col">
        <TabsList className="h-auto w-full justify-start gap-0 rounded-none border-b bg-transparent px-6 py-0">
          <TabsTrigger
            value="overview"
            className="data-[state=active]:border-b-primary relative rounded-none border-0 border-b-2 border-transparent px-4 py-3 shadow-none data-[state=active]:bg-transparent data-[state=active]:shadow-none"
          >
            <Icon name="message-circle" className="mr-2 size-4" />
            Overview
          </TabsTrigger>
          <TabsTrigger
            value="logs"
            className="data-[state=active]:border-b-primary relative rounded-none border-0 border-b-2 border-transparent px-4 py-3 shadow-none data-[state=active]:bg-transparent data-[state=active]:shadow-none"
          >
            <Icon name="list" className="mr-2 size-4" />
            Telemetry Logs
            {logs.length > 0 && (
              <span className="bg-muted ml-1.5 rounded-full px-1.5 text-xs">
                {logs.length}
              </span>
            )}
          </TabsTrigger>
          <TabsTrigger
            value="tools"
            className="data-[state=active]:border-b-primary relative rounded-none border-0 border-b-2 border-transparent px-4 py-3 shadow-none data-[state=active]:bg-transparent data-[state=active]:shadow-none"
          >
            <Icon name="zap" className="mr-2 size-4" />
            Tool Calls
            {toolLogs.length > 0 && (
              <span className="bg-muted ml-1.5 rounded-full px-1.5 text-xs">
                {toolLogs.length}
              </span>
            )}
          </TabsTrigger>
          {systemMessages.length > 0 && (
            <TabsTrigger
              value="system"
              className="data-[state=active]:border-b-primary relative rounded-none border-0 border-b-2 border-transparent px-4 py-3 shadow-none data-[state=active]:bg-transparent data-[state=active]:shadow-none"
            >
              <Icon name="settings" className="mr-2 size-4" />
              System Prompt
            </TabsTrigger>
          )}
        </TabsList>

        {/* Overview Tab */}
        <TabsContent
          value="overview"
          className="m-0 flex-1 overflow-y-auto data-[state=inactive]:hidden"
        >
          {/* Metadata Grid */}
          <div className="bg-muted/10 border-b p-6">
            <div className="grid grid-cols-2 gap-x-8 gap-y-4">
              <div>
                <div className="text-muted-foreground mb-1 text-xs">
                  User ID:
                </div>
                <div className="text-sm font-medium">
                  {chat.externalUserId || "anonymous"}
                </div>
              </div>
              {chat.source && (
                <div>
                  <div className="text-muted-foreground mb-1 text-xs">
                    Source:
                  </div>
                  <div className="flex items-center gap-2 text-sm font-medium">
                    {chat.source.toLowerCase().includes("claude") ||
                    chat.source.toLowerCase().includes("cursor") ? (
                      <HookSourceIcon source={chat.source} className="size-4" />
                    ) : (
                      <Icon name="globe" className="size-4 opacity-60" />
                    )}
                    {chat.source}
                  </div>
                </div>
              )}
              <div>
                <div className="text-muted-foreground mb-1 text-xs">
                  Duration:
                </div>
                <div className="text-sm font-medium">{duration}s</div>
              </div>
              <div>
                <div className="text-muted-foreground mb-1 text-xs">
                  Messages:
                </div>
                <div className="text-sm font-medium">
                  {nonSystemMessages.length}
                </div>
              </div>
              <div>
                <div className="text-muted-foreground mb-1 text-xs">
                  Tool Calls:
                </div>
                <div className="text-sm font-medium">{toolLogs.length}</div>
              </div>
              <div>
                <div className="text-muted-foreground mb-1 text-xs">
                  Total Cost:
                </div>
                <div className="text-sm font-medium">
                  {chat.totalCost !== undefined && chat.totalCost > 0
                    ? `$${chat.totalCost.toFixed(4)}`
                    : "unknown"}
                </div>
              </div>
              {chat.totalInputTokens !== undefined && (
                <div>
                  <div className="text-muted-foreground mb-1 text-xs">
                    Input Tokens:
                  </div>
                  <div className="text-sm font-medium">
                    {chat.totalInputTokens.toLocaleString()}
                  </div>
                </div>
              )}
              {chat.totalOutputTokens !== undefined && (
                <div>
                  <div className="text-muted-foreground mb-1 text-xs">
                    Output Tokens:
                  </div>
                  <div className="text-sm font-medium">
                    {chat.totalOutputTokens.toLocaleString()}
                  </div>
                </div>
              )}
              {(chat.totalTokens !== undefined ||
                (chat.totalInputTokens !== undefined &&
                  chat.totalOutputTokens !== undefined)) && (
                <div>
                  <div className="text-muted-foreground mb-1 text-xs">
                    Total Tokens:
                  </div>
                  <div className="text-sm font-medium">
                    {(chat.totalTokens && chat.totalTokens > 0
                      ? chat.totalTokens
                      : (chat.totalInputTokens || 0) +
                        (chat.totalOutputTokens || 0)
                    ).toLocaleString()}
                  </div>
                </div>
              )}
              {resolutions.length > 0 && (
                <>
                  <div>
                    <div className="text-muted-foreground mb-1 text-xs">
                      Resolution Score:
                    </div>
                    <div className="text-lg font-medium">{averageScore}%</div>
                  </div>
                  <div>
                    <div className="text-muted-foreground mb-1 text-xs">
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
            <div className="border-b p-6">
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
                    <div className="min-w-0 flex-1">
                      <div className="mb-1 text-sm font-medium">
                        {resolution.userGoal}
                      </div>
                      <div className="text-muted-foreground text-xs">
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
              {nonSystemMessages.map((message) => {
                const resolution = messageResolutionMap.get(message.id);

                return (
                  <div key={message.id}>
                    {/* Resolution breakpoint */}
                    {resolution && (
                      <div className="bg-primary/10 border-primary mb-3 rounded-lg border-l-4 p-3">
                        <div className="text-xs font-semibold">
                          Resolution Point: {resolution.resolution}
                        </div>
                      </div>
                    )}

                    {/* Message - render tool calls as separate entries */}
                    {message.toolCalls ? (
                      (() => {
                        try {
                          const parsedToolCalls = JSON.parse(
                            message.toolCalls,
                          ) as ToolCall[];
                          return Array.isArray(parsedToolCalls)
                            ? parsedToolCalls.map((tc, idx: number) => (
                                <div
                                  key={tc.id || idx}
                                  className="flex items-start gap-3"
                                >
                                  <div className="bg-primary flex size-8 flex-shrink-0 items-center justify-center rounded-full">
                                    <Icon
                                      name="zap"
                                      className="text-primary-foreground size-4"
                                    />
                                  </div>
                                  <div className="min-w-0 flex-1">
                                    <div className="mb-1 flex items-center gap-2">
                                      <span className="text-sm font-semibold">
                                        Tool Call
                                      </span>
                                      {tc.id && (
                                        <code className="text-muted-foreground bg-muted rounded px-1.5 py-0.5 font-mono text-xs">
                                          {tc.id}
                                        </code>
                                      )}
                                      <span className="text-muted-foreground text-xs">
                                        {message.createdAt &&
                                          format(
                                            new Date(message.createdAt),
                                            "HH:mm:ss",
                                          )}
                                      </span>
                                    </div>
                                    <div className="bg-background overflow-hidden rounded-lg border text-sm">
                                      <div className="bg-muted/30 border-b p-3">
                                        <div className="flex items-center gap-2">
                                          <Icon
                                            name="zap"
                                            className="text-primary size-4"
                                          />
                                          <span className="font-semibold">
                                            {tc.function?.name ||
                                              tc.name ||
                                              "Tool Call"}
                                          </span>
                                        </div>
                                      </div>
                                      {tc.function?.arguments && (
                                        <CodeBlock
                                          content={
                                            typeof tc.function.arguments ===
                                            "string"
                                              ? tc.function.arguments
                                              : JSON.stringify(
                                                  tc.function.arguments,
                                                  null,
                                                  2,
                                                )
                                          }
                                          maxHeight={300}
                                        />
                                      )}
                                    </div>
                                  </div>
                                </div>
                              ))
                            : null;
                        } catch {
                          return null;
                        }
                      })()
                    ) : (
                      <div className="flex items-start gap-3">
                        {message.role === "user" && (
                          <div className="bg-primary/10 flex size-8 flex-shrink-0 items-center justify-center rounded-full">
                            <Icon name="user" className="text-primary size-4" />
                          </div>
                        )}
                        {message.role === "assistant" && (
                          <div className="bg-muted flex size-8 flex-shrink-0 items-center justify-center rounded-full">
                            <Icon name="bot" className="size-4" />
                          </div>
                        )}
                        {message.role === "tool" && (
                          <div className="bg-primary flex size-8 flex-shrink-0 items-center justify-center rounded-full">
                            <Icon
                              name="zap"
                              className="text-primary-foreground size-4"
                            />
                          </div>
                        )}

                        <div className="min-w-0 flex-1">
                          <div className="mb-1 flex items-center gap-2">
                            <span className="text-sm font-semibold capitalize">
                              {message.role === "tool"
                                ? "Tool Result"
                                : message.role}
                            </span>
                            {message.toolCallId && (
                              <code className="text-muted-foreground bg-muted rounded px-1.5 py-0.5 font-mono text-xs">
                                {message.toolCallId}
                              </code>
                            )}
                            <span className="text-muted-foreground text-xs">
                              {message.createdAt &&
                                format(new Date(message.createdAt), "HH:mm:ss")}
                            </span>
                            {riskResultsByMessage.has(message.id) && (
                              <RiskBadgePopover
                                results={riskResultsByMessage.get(message.id)!}
                              />
                            )}
                          </div>
                          <div
                            className={cn(
                              "overflow-hidden rounded-lg text-sm",
                              message.role === "user" && "bg-primary/5 p-3",
                              message.role === "assistant" && "bg-muted/50 p-3",
                              message.role === "tool" && "bg-background border",
                            )}
                          >
                            {message.role === "tool" ? (
                              <CodeBlock
                                content={message.content}
                                maxHeight={300}
                              />
                            ) : (
                              <div className="whitespace-pre-wrap">
                                {typeof message.content === "string"
                                  ? message.content.trim()
                                  : JSON.stringify(message.content)}
                              </div>
                            )}
                          </div>
                        </div>
                      </div>
                    )}
                  </div>
                );
              })}
            </Stack>
          </div>
        </TabsContent>

        {/* Telemetry Logs Tab */}
        <TabsContent
          value="logs"
          className="m-0 flex-1 overflow-y-auto data-[state=inactive]:hidden"
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
          className="m-0 flex-1 overflow-y-auto data-[state=inactive]:hidden"
        >
          <ToolCallsTab
            toolLogs={toolLogs}
            isLoading={logsLoading}
            error={logsError as Error | null}
          />
        </TabsContent>

        {/* System Prompt Tab */}
        {systemMessages.length > 0 && (
          <TabsContent
            value="system"
            className="m-0 flex-1 overflow-y-auto data-[state=inactive]:hidden"
          >
            <div className="p-6">
              <Stack direction="vertical" gap={4}>
                {systemMessages.map((message) => (
                  <div key={message.id}>
                    <div className="mb-2 flex items-center gap-2">
                      <div className="bg-muted flex size-8 flex-shrink-0 items-center justify-center rounded-full">
                        <Icon name="settings" className="size-4" />
                      </div>
                      <span className="text-sm font-semibold">
                        System Prompt
                      </span>
                      {message.createdAt && (
                        <span className="text-muted-foreground text-xs">
                          {format(new Date(message.createdAt), "HH:mm:ss")}
                        </span>
                      )}
                    </div>
                    <div className="bg-muted/30 rounded-lg border p-4">
                      <pre className="font-mono text-sm whitespace-pre-wrap">
                        {typeof message.content === "string"
                          ? message.content.trim()
                          : JSON.stringify(message.content, null, 2)}
                      </pre>
                    </div>
                  </div>
                ))}
              </Stack>
            </div>
          </TabsContent>
        )}
      </Tabs>

      <Dialog open={showDeleteConfirm} onOpenChange={setShowDeleteConfirm}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Delete chat session</Dialog.Title>
            <Dialog.Description>
              Are you sure you want to delete this chat session? This action
              cannot be undone.
            </Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Dialog.Close asChild>
              <Button variant="secondary">Cancel</Button>
            </Dialog.Close>
            <Button
              variant="destructive-primary"
              onClick={() => {
                onDelete(chatId);
                setShowDeleteConfirm(false);
              }}
            >
              Delete
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </div>
  );
}
