import type { ChatMessage } from "@gram/client/models/components";
import type { IconName } from "@speakeasy-api/moonshine";

export interface ToolCall {
  id?: string;
  type?: string;
  name?: string;
  function?: {
    name?: string;
    arguments?: string | object;
  };
}

export const FILTERABLE_ENTRY_TYPES = [
  "user",
  "assistant",
  "tool_call",
  "tool_result",
] as const;

export type FilterableTraceEntryType = (typeof FILTERABLE_ENTRY_TYPES)[number];
export type TraceEntryType = FilterableTraceEntryType | "system";

export const DEFAULT_ENABLED_ENTRY_TYPES = [...FILTERABLE_ENTRY_TYPES];

export const ENTRY_TYPE_META: Record<
  TraceEntryType,
  {
    label: string;
    icon: IconName;
    avatarClassName: string;
    iconClassName: string;
  }
> = {
  user: {
    label: "User",
    icon: "user",
    avatarClassName: "bg-muted",
    iconClassName: "text-muted-foreground",
  },
  assistant: {
    label: "Assistant",
    icon: "bot",
    avatarClassName: "bg-information-softest",
    iconClassName: "text-default-information",
  },
  tool_call: {
    label: "Tool Call",
    icon: "zap",
    avatarClassName: "bg-warning-softest",
    iconClassName: "text-warning-default",
  },
  tool_result: {
    label: "Tool Result",
    icon: "terminal",
    avatarClassName: "bg-success-softest",
    iconClassName: "text-success-default",
  },
  system: {
    label: "System Prompt",
    icon: "settings",
    avatarClassName: "bg-accent",
    iconClassName: "text-muted-foreground",
  },
};

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

export function getEntryTypeCounts(
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

export function isMessageVisible(
  message: ChatMessage,
  enabledEntryTypes: FilterableTraceEntryType[],
) {
  const parsedToolCalls = parseToolCalls(message.toolCalls);
  const entryType = getTraceEntryType(message, parsedToolCalls);
  return entryType === "system" || enabledEntryTypes.includes(entryType);
}

export function getVisibleMessageCount(
  messages: ChatMessage[],
  enabledEntryTypes: FilterableTraceEntryType[],
) {
  return messages.filter((message) =>
    isMessageVisible(message, enabledEntryTypes),
  ).length;
}
