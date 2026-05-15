import { Eye, EyeOff } from "lucide-react";
import { cn } from "@/lib/utils";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { MultiSelect } from "@/components/ui/multi-select";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import { CodeBlock } from "@/components/ui/code-block";
import type {
  ChatMessage,
  ChatResolution,
  TelemetryLogRecord,
} from "@gram/client/models/components";
import { useLoadChat, useSearchLogsMutation } from "@gram/client/react-query";
import { useRiskListResults } from "@gram/client/react-query/riskListResults.js";
import { Badge, Icon, Stack, type IconName } from "@speakeasy-api/moonshine";
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
import { MessageContent } from "@gram-ai/elements";
import { useIsAdmin } from "@/contexts/Auth";
import { toast } from "sonner";

interface ChatDetailPanelProps {
  chatId: string;
  resolutions: ChatResolution[];
  onClose: () => void;
  onDelete: (chatId: string) => void;
  /** When true, messages without risk findings are collapsed to a single line. */
  collapseNonRisk?: boolean;
}

function getTraceId(chatId: string): string {
  return `trace-${chatId.slice(0, 3)}`;
}

function downloadJsonFile(filename: string, data: unknown) {
  const json = JSON.stringify(data, null, 2);
  const blob = new Blob([json], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

function getTraceExportSlug(chat: { id: string; title?: string | null }) {
  const titleSlug = chat.title
    ?.toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 40);

  return titleSlug || chat.id.slice(0, 8);
}

function exportTraceDataAsJson({
  chatId,
  chat,
  telemetryLogs,
  riskResults,
}: {
  chatId: string;
  chat: {
    id: string;
    title?: string | null;
    messages: ChatMessage[];
  };
  telemetryLogs: TelemetryLogRecord[];
  riskResults: RiskResult[];
}) {
  try {
    const exported = {
      schemaVersion: 1,
      exportedAt: new Date().toISOString(),
      chatId,
      chat,
      messages: chat.messages,
      telemetryLogs,
      riskResults,
    };

    downloadJsonFile(`trace-${getTraceExportSlug(chat)}.json`, exported);
  } catch {
    toast.error("Failed to export trace data");
  }
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

const FILTERABLE_ENTRY_TYPES = [
  "user",
  "assistant",
  "tool_call",
  "tool_result",
] as const;

type FilterableTraceEntryType = (typeof FILTERABLE_ENTRY_TYPES)[number];
type TraceEntryType = FilterableTraceEntryType | "system";

const DEFAULT_ENABLED_ENTRY_TYPES = [...FILTERABLE_ENTRY_TYPES];

const ENTRY_TYPE_META: Record<
  TraceEntryType,
  {
    label: string;
    icon: IconName;
    avatarClassName: string;
    iconClassName: string;
    contentClassName: string;
    collapsedClassName: string;
  }
> = {
  user: {
    label: "User",
    icon: "user",
    avatarClassName: "bg-muted",
    iconClassName: "text-muted-foreground",
    contentClassName: "border border-border p-3",
    collapsedClassName:
      "border-l-3 border-border bg-muted/10 hover:bg-muted/30",
  },
  assistant: {
    label: "Assistant",
    icon: "bot",
    avatarClassName: "bg-information-softest",
    iconClassName: "text-default-information",
    contentClassName: "border border-border p-3",
    collapsedClassName:
      "border-l-3 border-information-default bg-muted/10 hover:bg-muted/30",
  },
  tool_call: {
    label: "Tool Call",
    icon: "zap",
    avatarClassName: "bg-warning-softest",
    iconClassName: "text-warning-default",
    contentClassName: "border border-border",
    collapsedClassName:
      "border-l-3 border-warning-default bg-muted/10 hover:bg-muted/30",
  },
  tool_result: {
    label: "Tool Result",
    icon: "terminal",
    avatarClassName: "bg-success-softest",
    iconClassName: "text-success-default",
    contentClassName: "",
    collapsedClassName:
      "border-l-3 border-success-default bg-muted/10 hover:bg-muted/30",
  },
  system: {
    label: "System Prompt",
    icon: "settings",
    avatarClassName: "bg-accent",
    iconClassName: "text-muted-foreground",
    contentClassName: "border border-border p-3",
    collapsedClassName:
      "border-l-3 border-border bg-muted/10 hover:bg-muted/30",
  },
};

function parseToolCalls(toolCalls: string | undefined): ToolCall[] | null {
  if (!toolCalls) return null;

  try {
    let parsed: unknown = JSON.parse(toolCalls);
    // Handle double-encoded JSON strings.
    if (typeof parsed === "string") {
      parsed = JSON.parse(parsed);
    }
    return Array.isArray(parsed) ? (parsed as ToolCall[]) : null;
  } catch {
    return null;
  }
}

function getTraceEntryType(
  message: ChatMessage,
  parsedToolCalls: ToolCall[] | null,
): TraceEntryType {
  if (parsedToolCalls && parsedToolCalls.length > 0) return "tool_call";
  if (message.role === "tool") return "tool_result";
  if (message.role === "user") return "user";
  if (message.role === "assistant") return "assistant";
  return "system";
}

function getEntryTypeCounts(
  messages: ChatMessage[],
): Record<FilterableTraceEntryType, number> {
  const counts: Record<FilterableTraceEntryType, number> = {
    user: 0,
    assistant: 0,
    tool_call: 0,
    tool_result: 0,
  };

  for (const message of messages) {
    const entryType = getTraceEntryType(
      message,
      parseToolCalls(message.toolCalls),
    );
    if (entryType !== "system") {
      counts[entryType] += 1;
    }
  }

  return counts;
}

function isMessageVisible(
  message: ChatMessage,
  enabledEntryTypes: FilterableTraceEntryType[],
) {
  const parsedToolCalls = parseToolCalls(message.toolCalls);
  const entryType = getTraceEntryType(message, parsedToolCalls);
  return entryType === "system" || enabledEntryTypes.includes(entryType);
}

function getVisibleMessageCount(
  messages: ChatMessage[],
  enabledEntryTypes: FilterableTraceEntryType[],
) {
  return messages.filter((message) =>
    isMessageVisible(message, enabledEntryTypes),
  ).length;
}

function isFilterableEntryType(
  value: string,
): value is FilterableTraceEntryType {
  return FILTERABLE_ENTRY_TYPES.includes(value as FilterableTraceEntryType);
}

function EntryTypeFilterBar({
  value,
  counts,
  totalCount,
  visibleCount,
  onChange,
}: {
  value: FilterableTraceEntryType[];
  counts: Record<FilterableTraceEntryType, number>;
  totalCount: number;
  visibleCount: number;
  onChange: (value: FilterableTraceEntryType[]) => void;
}) {
  const options = FILTERABLE_ENTRY_TYPES.map((entryType) => {
    const meta = ENTRY_TYPE_META[entryType];

    return {
      label: `${meta.label} (${counts[entryType].toLocaleString()})`,
      value: entryType,
      icon: ({ className }: { className?: string }) => (
        <Icon name={meta.icon} className={cn(className, meta.iconClassName)} />
      ),
    };
  });

  return (
    <div className="bg-background border-b px-6 py-3">
      <div className="flex min-w-0 flex-col gap-2">
        <div className="text-sm font-medium">Entry type filter</div>
        <div>
          <MultiSelect
            options={options}
            defaultValue={value}
            onValueChange={(next) =>
              onChange(next.filter(isFilterableEntryType))
            }
            placeholder="Filter by entry type"
            maxCount={10}
            popoverClassName="min-w-[240px]"
          />
        </div>
        <div>
          <div className="text-muted-foreground text-xs">
            Showing {visibleCount.toLocaleString()} of{" "}
            {totalCount.toLocaleString()}{" "}
            {visibleCount === 1 ? "entry" : "entries"}
          </div>
        </div>
      </div>
    </div>
  );
}

function ChatMessagesList({
  messages,
  messageResolutionMap,
  riskResultsByMessage,
  collapseNonRisk,
  enabledEntryTypes,
}: {
  messages: ChatMessage[];
  messageResolutionMap: Map<string, ChatResolution>;
  riskResultsByMessage: Map<string, RiskResult[]>;
  collapseNonRisk?: boolean;
  enabledEntryTypes: FilterableTraceEntryType[];
}) {
  const visibleMessages = useMemo(
    () =>
      messages.filter((message) =>
        isMessageVisible(message, enabledEntryTypes),
      ),
    [enabledEntryTypes, messages],
  );

  const groups = useMemo(() => {
    const byGeneration = new Map<number, ChatMessage[]>();
    for (const m of visibleMessages) {
      const list = byGeneration.get(m.generation) ?? [];
      list.push(m);
      byGeneration.set(m.generation, list);
    }
    return Array.from(byGeneration.entries())
      .sort(([a], [b]) => a - b)
      .map(([generation, items]) => ({ generation, messages: items }));
  }, [visibleMessages]);

  const maxGeneration =
    groups.length > 0 ? groups[groups.length - 1]!.generation : 0;

  if (visibleMessages.length === 0) {
    return (
      <div className="text-muted-foreground rounded-lg border border-dashed p-6 text-center text-sm">
        No entries match the selected filters.
      </div>
    );
  }

  // A single segment (no compaction has ever occurred) stays flat — no accordion.
  if (maxGeneration === 0) {
    return (
      <Stack direction="vertical">
        {visibleMessages.map((message) => (
          <MessageItem
            key={message.id}
            message={message}
            resolution={messageResolutionMap.get(message.id)}
            riskResults={riskResultsByMessage.get(message.id)}
            collapseNonRisk={collapseNonRisk}
          />
        ))}
      </Stack>
    );
  }

  return (
    <Accordion type="multiple" defaultValue={[`gen-${maxGeneration}`]}>
      {groups.map(({ generation, messages: groupMessages }) => (
        <AccordionItem key={generation} value={`gen-${generation}`}>
          <AccordionTrigger>
            <div className="flex items-center gap-2">
              <span>Conversation segment {generation + 1}</span>
              <span className="text-muted-foreground text-xs font-normal">
                {groupMessages.length} message
                {groupMessages.length === 1 ? "" : "s"}
              </span>
            </div>
          </AccordionTrigger>
          <AccordionContent>
            <Stack direction="vertical">
              {groupMessages.map((message) => (
                <MessageItem
                  key={message.id}
                  message={message}
                  resolution={messageResolutionMap.get(message.id)}
                  riskResults={riskResultsByMessage.get(message.id)}
                  collapseNonRisk={collapseNonRisk}
                />
              ))}
            </Stack>
          </AccordionContent>
        </AccordionItem>
      ))}
    </Accordion>
  );
}

function MessageItem({
  message,
  resolution,
  riskResults,
  collapseNonRisk,
}: {
  message: ChatMessage;
  resolution: ChatResolution | undefined;
  riskResults: RiskResult[] | undefined;
  collapseNonRisk?: boolean;
}) {
  const hasRisk = riskResults && riskResults.length > 0;
  const hasSensitiveContent =
    riskResults?.some(
      (r) => r.source === "gitleaks" || r.source === "presidio",
    ) ?? false;
  const [expanded, setExpanded] = useState(false);
  const [contentRevealed, setContentRevealed] = useState(false);
  const isCollapsed = collapseNonRisk && !hasRisk && !expanded;

  const parsedToolCalls = useMemo(
    () => parseToolCalls(message.toolCalls),
    [message.toolCalls],
  );
  const entryType = getTraceEntryType(message, parsedToolCalls);
  const entryMeta = ENTRY_TYPE_META[entryType];

  if (isCollapsed) {
    const label =
      entryType === "tool_call"
        ? `Tool Call: ${parsedToolCalls?.[0]?.function?.name ?? "unknown"}`
        : entryMeta.label;
    const preview =
      !parsedToolCalls && typeof message.content === "string"
        ? message.content.trim().slice(0, 80)
        : "";

    return (
      <button
        type="button"
        onClick={() => setExpanded(true)}
        className={cn(
          entryMeta.collapsedClassName,
          "text-muted-foreground flex w-full items-center gap-3 truncate px-2 py-2 text-sm transition-colors",
          "border-r-muted border-t-muted last:border-b-muted border border-b-transparent",
        )}
      >
        <Icon
          name={entryMeta.icon}
          className={cn("ml-1 size-4 shrink-0", entryMeta.iconClassName)}
        />
        {message.createdAt && (
          <span className="font-mono text-xs">
            {format(new Date(message.createdAt), "HH:mm:ss")}
          </span>
        )}
        <span className="font-medium">{label}</span>
        {preview && <span className="truncate opacity-60">{preview}...</span>}
        <Icon name="chevron-down" className="ml-auto size-3 shrink-0" />
      </button>
    );
  }

  return (
    <div>
      {resolution && (
        <div className="bg-primary/10 border-primary mb-3 rounded-lg border-l-4 p-3">
          <div className="text-xs font-semibold">
            Resolution Point: {resolution.resolution}
          </div>
        </div>
      )}

      {parsedToolCalls ? (
        parsedToolCalls.map((tc, idx: number) => (
          <div key={tc.id || idx} className="flex items-start gap-3">
            <div
              className={cn(
                "flex size-8 shrink-0 items-center justify-center rounded-full",
                entryMeta.avatarClassName,
              )}
            >
              <Icon
                name={entryMeta.icon}
                className={cn("size-4", entryMeta.iconClassName)}
              />
            </div>
            <div className="min-w-0 flex-1">
              <div className="mb-1 flex items-center gap-2">
                <span className="text-muted-foreground font-mono text-xs">
                  {message.createdAt &&
                    format(new Date(message.createdAt), "HH:mm:ss")}
                </span>
                <span className="text-sm font-semibold">{entryMeta.label}</span>
                {tc.id && (
                  <code className="text-muted-foreground bg-muted rounded px-1.5 py-0.5 font-mono text-xs">
                    {tc.id}
                  </code>
                )}
                {riskResults && riskResults.length > 0 && (
                  <RiskBadgePopover results={riskResults} />
                )}
              </div>
              {hasSensitiveContent && !contentRevealed ? (
                <MaskedContent onReveal={() => setContentRevealed(true)} />
              ) : (
                <div
                  className={cn(
                    "overflow-hidden rounded-lg text-sm",
                    entryMeta.contentClassName,
                  )}
                >
                  <div className="bg-background/50 border-b p-3">
                    <div className="flex items-center gap-2">
                      <Icon
                        name={entryMeta.icon}
                        className={cn("size-4", entryMeta.iconClassName)}
                      />
                      <span className="font-semibold">
                        {tc.function?.name || tc.name || "Tool Call"}
                      </span>
                    </div>
                  </div>
                  {tc.function?.arguments && (
                    <CodeBlock
                      content={
                        typeof tc.function.arguments === "string"
                          ? tc.function.arguments
                          : JSON.stringify(tc.function.arguments, null, 2)
                      }
                      maxHeight={300}
                    />
                  )}
                </div>
              )}
            </div>
          </div>
        ))
      ) : (
        <div className="flex items-start gap-3">
          <div
            className={cn(
              "flex size-8 shrink-0 items-center justify-center rounded-full",
              entryMeta.avatarClassName,
            )}
          >
            <Icon
              name={entryMeta.icon}
              className={cn("size-4", entryMeta.iconClassName)}
            />
          </div>

          <div className="min-w-0 flex-1">
            <div className="mb-1 flex items-center gap-2">
              <span className="text-muted-foreground font-mono text-xs">
                {message.createdAt &&
                  format(new Date(message.createdAt), "HH:mm:ss")}
              </span>
              <span className="text-sm font-semibold">{entryMeta.label}</span>
              {message.toolCallId && (
                <code className="text-muted-foreground bg-muted rounded px-1.5 py-0.5 font-mono text-xs">
                  {message.toolCallId}
                </code>
              )}
              {riskResults && riskResults.length > 0 && (
                <RiskBadgePopover results={riskResults} />
              )}
            </div>
            {hasSensitiveContent && !contentRevealed ? (
              <MaskedContent onReveal={() => setContentRevealed(true)} />
            ) : entryType === "system" ? (
              <details
                className={cn(
                  "group overflow-hidden rounded-lg text-sm",
                  entryMeta.contentClassName,
                )}
              >
                <summary className="text-muted-foreground hover:bg-muted/50 flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-xs select-none">
                  <Icon
                    name="chevron-right"
                    className="size-3 transition-transform group-open:rotate-90"
                  />
                  <span>Show content</span>
                </summary>
                <div className="border-t p-3 font-mono text-xs whitespace-pre-wrap">
                  {typeof message.content === "string"
                    ? message.content.trim()
                    : JSON.stringify(message.content)}
                </div>
              </details>
            ) : (
              <div
                className={cn(
                  "overflow-hidden rounded-lg text-sm",
                  entryMeta.contentClassName,
                )}
              >
                {entryType === "tool_result" ? (
                  <CodeBlock content={message.content ?? ""} maxHeight={300} />
                ) : (
                  <MessageContent
                    content={
                      typeof message.content === "string"
                        ? message.content.trim()
                        : JSON.stringify(message.content)
                    }
                  />
                )}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
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
              <div className="text-sm font-medium wrap-break-word">
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

function MaskedContent({ onReveal }: { onReveal: () => void }) {
  return (
    <div className="bg-muted/30 flex items-center gap-2 rounded-lg border border-dashed p-3">
      <EyeOff className="text-muted-foreground h-4 w-4 shrink-0" />
      <span className="text-muted-foreground text-sm">
        This message contains sensitive data.
      </span>
      <button
        type="button"
        className="hover:text-foreground text-sm font-medium underline underline-offset-2"
        onClick={onReveal}
      >
        Click to reveal
      </button>
    </div>
  );
}

function MaskedMatchInline({ value }: { value: string }) {
  const [revealed, setRevealed] = useState(false);

  if (!revealed) {
    return (
      <button
        type="button"
        className="text-muted-foreground hover:text-foreground mt-1 inline-flex items-center gap-1 text-xs"
        onClick={() => setRevealed(true)}
      >
        <EyeOff className="h-3 w-3" />
        <span>Click to reveal</span>
      </button>
    );
  }

  return (
    <span className="mt-1 inline-flex items-center gap-1">
      <code className="bg-destructive/10 text-destructive inline-block rounded px-1.5 py-0.5 font-mono text-xs break-all">
        {value}
      </code>
      <button
        type="button"
        className="text-muted-foreground hover:text-foreground"
        onClick={() => setRevealed(false)}
      >
        <Eye className="h-3 w-3" />
      </button>
    </span>
  );
}

function RiskBadgePopover({ results }: { results: RiskResult[] }) {
  // Long messages can repeat the same secret/email many times. Collapse to
  // distinct (source, ruleId, match) so the popover lists each unique
  // finding once with an occurrence count instead of an N-row scroll of
  // identical rows.
  const grouped = new Map<string, { result: RiskResult; count: number }>();
  for (const r of results) {
    const key = `${r.source}\u0000${r.ruleId ?? ""}\u0000${r.match ?? ""}`;
    const hit = grouped.get(key);
    if (hit) {
      hit.count++;
    } else {
      grouped.set(key, { result: r, count: 1 });
    }
  }
  const unique = [...grouped.values()];

  return (
    <Popover>
      <PopoverTrigger asChild>
        <button type="button" className="cursor-pointer">
          <Badge variant="destructive" className="text-xs">
            <Icon name="shield-alert" className="mr-1 size-3" />
            {unique.length} {unique.length === 1 ? "Risk" : "Risks"}
          </Badge>
        </button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="max-h-[70vh] w-80 overflow-y-auto"
      >
        <div className="space-y-3">
          <div className="text-sm font-semibold">Risk Findings</div>
          <div className="divide-border divide-y">
            {unique.map(({ result: r, count }) => (
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
                  {count > 1 && (
                    <Badge
                      variant="neutral"
                      className="ml-auto shrink-0 text-[10px]"
                    >
                      ×{count}
                    </Badge>
                  )}
                </div>
                {r.description && (
                  <p className="text-muted-foreground mt-1 text-xs">
                    {r.description}
                  </p>
                )}
                {r.match && <MaskedMatchInline value={r.match} />}
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
  collapseNonRisk,
}: ChatDetailPanelProps) {
  const isAdmin = useIsAdmin();
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const [enabledEntryTypes, setEnabledEntryTypes] = useState<
    FilterableTraceEntryType[]
  >([...DEFAULT_ENABLED_ENTRY_TYPES]);
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

  const logs = useMemo(() => logsData?.logs || [], [logsData?.logs]);
  const toolLogs = useMemo(() => filterToolLogs(logs), [logs]);

  // Fetch risk findings for this chat
  const { data: riskData } = useRiskListResults({ chatId });
  const riskResults = useMemo(
    () => riskData?.results ?? [],
    [riskData?.results],
  );
  const riskResultsByMessage = useMemo(() => {
    const map = new Map<string, RiskResult[]>();
    for (const r of riskResults) {
      const existing = map.get(r.chatMessageId);
      if (existing) {
        existing.push(r);
      } else {
        map.set(r.chatMessageId, [r]);
      }
    }
    return map;
  }, [riskResults]);

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
  const entryTypeCounts = getEntryTypeCounts(chat.messages);
  const visibleEntryCount = getVisibleMessageCount(
    chat.messages,
    enabledEntryTypes,
  );

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
            {isAdmin && (
              <>
                <button
                  onClick={() =>
                    exportTraceDataAsJson({
                      chatId,
                      chat,
                      telemetryLogs: logs,
                      riskResults,
                    })
                  }
                  className="hover:bg-muted text-muted-foreground inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-sm transition-colors"
                  aria-label="Export data as JSON"
                >
                  <Icon name="download" className="size-4" />
                  <span>Export data</span>
                </button>
                <button
                  onClick={() => setShowDeleteConfirm(true)}
                  className="hover:bg-destructive/10 text-muted-foreground hover:text-destructive rounded-md p-1 transition-colors"
                  aria-label="Delete chat"
                >
                  <Icon name="trash-2" className="size-5" />
                </button>
              </>
            )}
            <button
              onClick={onClose}
              className="hover:bg-muted rounded-md p-1 transition-colors"
              aria-label="Close panel"
            >
              <Icon name="x" className="size-5" />
            </button>
          </div>
        </div>
        <div className="text-muted-foreground mb-3 font-mono text-sm">
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
                  {chat.messages.length}
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

          <EntryTypeFilterBar
            value={enabledEntryTypes}
            counts={entryTypeCounts}
            totalCount={chat.messages.length}
            visibleCount={visibleEntryCount}
            onChange={setEnabledEntryTypes}
          />

          {/* Chat Messages */}
          <div className="p-6">
            <ChatMessagesList
              messages={chat.messages}
              messageResolutionMap={messageResolutionMap}
              riskResultsByMessage={riskResultsByMessage}
              collapseNonRisk={collapseNonRisk}
              enabledEntryTypes={enabledEntryTypes}
            />
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
