import type { ChatMessage } from "@gram/client/models/components";

export interface ToolCall {
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

export type FilterableTraceEntryType = (typeof FILTERABLE_ENTRY_TYPES)[number];
export type TraceEntryType = FilterableTraceEntryType | "system";

export function parseToolCalls(
  toolCalls: string | undefined,
): ToolCall[] | null {
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

export function getTraceEntryType(
  message: ChatMessage,
  parsedToolCalls: ToolCall[] | null,
): TraceEntryType {
  if (parsedToolCalls && parsedToolCalls.length > 0) return "tool_call";
  if (message.role === "tool") return "tool_result";
  if (message.role === "user") return "user";
  if (message.role === "assistant") return "assistant";
  return "system";
}

function isMessageVisible(
  message: ChatMessage,
  enabledEntryTypes: FilterableTraceEntryType[],
) {
  const parsedToolCalls = parseToolCalls(message.toolCalls);
  const entryType = getTraceEntryType(message, parsedToolCalls);
  return entryType === "system" || enabledEntryTypes.includes(entryType);
}

function messageHasRiskResults(
  message: ChatMessage,
  riskResultsByMessage: ReadonlyMap<string, readonly unknown[]>,
) {
  return (riskResultsByMessage.get(message.id)?.length ?? 0) > 0;
}

function isMessageVisibleWithRisk({
  message,
  enabledEntryTypes,
  riskOnly,
  riskResultsByMessage,
}: {
  message: ChatMessage;
  enabledEntryTypes: FilterableTraceEntryType[];
  riskOnly: boolean;
  riskResultsByMessage: ReadonlyMap<string, readonly unknown[]>;
}) {
  if (!isMessageVisible(message, enabledEntryTypes)) return false;
  if (!riskOnly) return true;
  return messageHasRiskResults(message, riskResultsByMessage);
}

export function getVisibleMessages({
  messages,
  enabledEntryTypes,
  riskOnly,
  riskResultsByMessage,
}: {
  messages: ChatMessage[];
  enabledEntryTypes: FilterableTraceEntryType[];
  riskOnly: boolean;
  riskResultsByMessage: ReadonlyMap<string, readonly unknown[]>;
}): ChatMessage[] {
  return messages.filter((message) =>
    isMessageVisibleWithRisk({
      message,
      enabledEntryTypes,
      riskOnly,
      riskResultsByMessage,
    }),
  );
}

export function getRiskEntryCount(
  messages: ChatMessage[],
  riskResultsByMessage: ReadonlyMap<string, readonly unknown[]>,
): number {
  return messages.filter((message) =>
    messageHasRiskResults(message, riskResultsByMessage),
  ).length;
}
