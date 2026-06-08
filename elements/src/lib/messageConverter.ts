/**
 * Message format converter for Gram API <-> assistant-ui.
 *
 * The Gram API returns chat messages in its own schema (GramChatMessage),
 * while assistant-ui expects messages in its internal ThreadMessage format.
 * This module bridges that gap by converting between the two formats.
 *
 * Main export: `convertGramMessagesToExported` - converts an array of Gram
 * messages into an ExportedMessageRepository with parent-child relationships
 * for conversation threading.
 */

import type {
  ExportedMessageRepository,
  ThreadMessage,
  ThreadUserMessagePart,
  ThreadAssistantMessagePart,
  TextMessagePart,
} from "@assistant-ui/react";
import { UIMessage } from "ai";

/**
 * A single text part of a multi-modal chat message.
 */
export interface GramChatTextPart {
  type: "text";
  text: string;
}

/**
 * A single image part of a multi-modal chat message. The wire shape mirrors the
 * upstream OpenAI/OpenRouter chat schema (`image_url.url`) so the converter can
 * read it without normalisation.
 */
export interface GramChatImagePart {
  type: "image_url";
  image_url?: { url?: string };
}

/**
 * A single audio part of a multi-modal chat message. Mirrors the OpenAI/OpenRouter
 * `input_audio.{data, format}` shape on the wire.
 */
export interface GramChatAudioPart {
  type: "input_audio";
  input_audio?: { data?: string; format?: string };
}

/**
 * Content part of a multi-modal chat message.
 */
export type GramChatContentPart =
  | GramChatTextPart
  | GramChatImagePart
  | GramChatAudioPart;

/**
 * Content of a chat message — either a plain string or an array of parts for
 * multi-modal messages.
 */
export type GramChatContent = string | GramChatContentPart[];

/**
 * Represents a chat message from the Gram API. Only fields actually surfaced
 * through Elements' public converters are modelled; provider-specific extras
 * remain on the wire shape but are intentionally not part of the contract.
 *
 * `tool_calls` is the JSON-encoded string the Gram chat service stores on
 * assistant rows; `tool_call_id` is the id the corresponding tool-response row
 * carries when `role === "tool"`.
 */
export interface GramChatMessage {
  id: string;
  model: string;
  created_at: Date | string;
  role: "system" | "developer" | "user" | "assistant" | "tool";
  content?: GramChatContent | null;
  name?: string;
  tool_calls?: string;
  tool_call_id?: string;
  reasoning?: string | null;
}

/**
 * Represents a chat from the Gram API.
 */
export interface GramChat {
  id: string;
  title: string;
  userId: string;
  numMessages: number;
  messages: GramChatMessage[];
  createdAt: Date | string;
  updatedAt: Date | string;
}

/**
 * Represents a chat overview from the Gram API (without full messages).
 */
export interface GramChatOverview {
  id: string;
  title: string;
  userId: string;
  numMessages: number;
  createdAt: Date | string;
  updatedAt: Date | string;
}

/**
 * Parses a date that might be a string or Date object.
 */
function parseDate(date: Date | string): Date {
  return typeof date === "string" ? new Date(date) : date;
}

/**
 * Builds content parts for a user message.
 */
function buildUserContentParts(msg: GramChatMessage): ThreadUserMessagePart[] {
  if (msg.role !== "user") {
    return [];
  }

  if (typeof msg.content === "string" || !msg.content) {
    return [
      {
        type: "text",
        text: msg.content ?? "",
      },
    ];
  }

  const parts: ThreadUserMessagePart[] = [];

  for (const item of msg.content) {
    switch (item.type) {
      case "text":
        parts.push({
          type: "text",
          text: item.text,
        });
        break;
      case "image_url": {
        const url = item.image_url?.url ?? "";
        parts.push({
          type: "image",
          image: url,
        });
        break;
      }
      case "input_audio": {
        const format = item.input_audio?.format;
        const data = item.input_audio?.data;
        if ((format === "mp3" || format === "wav") && data) {
          parts.push({
            type: "audio",
            audio: {
              data,
              format,
            },
          });
        }
        break;
      }
      default:
        parts.push({
          type: "text",
          text: "",
        });
        break;
    }
  }

  return parts;
}

/**
 * Builds content parts for an assistant message, including tool calls.
 */
function buildAssistantContentParts(
  msg: GramChatMessage,
): ThreadAssistantMessagePart[] {
  if (msg.role !== "assistant") {
    return [];
  }

  const parts: ThreadAssistantMessagePart[] = [];

  if (typeof msg.content === "string" || !msg.content) {
    parts.push({
      type: "text",
      text: msg.content ?? "",
    });
  }

  // Accept both the OpenAI/OpenRouter shape (`{ id, function: { name, arguments } }`)
  // and the assistant-ui shape (`{ toolCallId, toolName, args }`). Tool calls
  // arrive as JSON the server stored opaquely, so we model the union here.
  type WireToolCall = {
    id?: string;
    toolCallId?: string;
    function?: { name?: string; arguments?: string | Record<string, unknown> };
    toolName?: string;
    args?: string | Record<string, unknown>;
  };

  let toolCalls = tryParseJSON<WireToolCall[]>(msg.tool_calls || "[]");
  if (!Array.isArray(toolCalls)) {
    console.warn("Invalid tool_calls format, expected an array.");
    toolCalls = [];
  }

  for (const tc of toolCalls) {
    const toolCallId = tc.id ?? tc.toolCallId;
    if (!toolCallId) {
      // assistant-ui keys tool-call state by toolCallId; if two parts in the
      // same restored thread share an empty fallback id, the second one's
      // argsText regresses from the first's and the runtime throws
      // "Tool call argsText can only be appended, not updated".
      console.warn("Dropping persisted tool call with no id:", tc);
      continue;
    }
    const args = tc.function?.arguments ?? tc.args ?? {};
    const argsText = typeof args === "string" ? args : JSON.stringify(args);
    parts.push({
      type: "tool-call",
      toolCallId,
      toolName: tc.function?.name ?? tc.toolName ?? "",
      args: typeof args === "string" ? JSON.parse(args) : args,
      argsText,
      result: undefined,
    } as ThreadAssistantMessagePart);
  }

  // Return at least an empty text part if no content
  if (parts.length === 0) {
    parts.push({
      type: "text",
      text: "",
    } as TextMessagePart);
  }

  return parts;
}

function buildSystemContentParts(msg: GramChatMessage): [TextMessagePart] {
  if (msg.role !== "system") {
    return [{ type: "text", text: "" }];
  }

  if (typeof msg.content === "string" || !msg.content) {
    return [{ type: "text", text: msg.content ?? "" }];
  }

  const text: string[] = [];

  for (const item of msg.content) {
    if (item.type !== "text") {
      continue;
    }
    text.push(item.text);
  }

  return [{ type: "text", text: text.join("\n") }];
}

/**
 * Converts a single Gram ChatMessage to a ThreadMessage.
 */
function convertGramMessageToThreadMessage(
  msg: GramChatMessage,
): ThreadMessage {
  const createdAt = parseDate(msg.created_at);

  const baseMetadata = {
    unstable_state: undefined,
    unstable_annotations: undefined,
    unstable_data: undefined,
    steps: undefined,
    submittedFeedback: undefined,
    custom: {},
  };

  if (msg.role === "user") {
    return {
      id: msg.id,
      role: "user",
      createdAt,
      content: buildUserContentParts(msg),
      attachments: [],
      metadata: baseMetadata,
    };
  }

  if (msg.role === "system") {
    return {
      id: msg.id,
      role: "system",
      createdAt,
      content: buildSystemContentParts(msg),
      metadata: baseMetadata,
    };
  }

  // Assistant message
  return {
    id: msg.id,
    role: "assistant",
    createdAt,
    content: buildAssistantContentParts(msg),
    status: { type: "complete", reason: "stop" },
    metadata: {
      unstable_state: null,
      unstable_annotations: [],
      unstable_data: [],
      steps: [],
      submittedFeedback: undefined,
      custom: {},
    },
  };
}

/**
 * Converts an array of Gram ChatMessages to an ExportedMessageRepository.
 * Creates parent-child relationships based on message order.
 *
 * Note: system, developer, and tool messages are filtered out. assistant-ui's
 * exported format only models user/assistant turns; system/developer rows are
 * pre-prompt instructions the UI doesn't render, and tool rows are folded into
 * the preceding assistant message as `tool-call` parts via `tool_calls`.
 */
export function convertGramMessagesToExported(
  messages: GramChatMessage[],
): ExportedMessageRepository {
  if (messages.length === 0) {
    return { messages: [], headId: null };
  }

  const exportedMessages: ExportedMessageRepository["messages"] = [];
  let prevId: string | null = null;

  for (const msg of messages) {
    if (
      msg.role === "system" ||
      msg.role === "developer" ||
      msg.role === "tool"
    ) {
      continue;
    }

    const threadMessage = convertGramMessageToThreadMessage(msg);
    exportedMessages.push({
      message: threadMessage,
      parentId: prevId,
      runConfig: undefined,
    });
    prevId = msg.id;
  }

  return {
    messages: exportedMessages,
    headId: prevId,
  };
}

export function convertGramMessagesToUIMessages(messages: GramChatMessage[]): {
  headId: string | null;
  messages: { parentId: string | null; message: UIMessage }[];
} {
  if (messages.length === 0) {
    return { messages: [], headId: null };
  }

  const toolCallResults = new Map<string, GramChatMessage>();
  for (const msg of messages) {
    if (msg.role !== "tool") {
      continue;
    }
    const id = msg.tool_call_id;
    if (typeof id !== "string") {
      continue;
    }

    toolCallResults.set(id, msg);
  }

  const uiMessages: { parentId: string | null; message: UIMessage }[] = [];
  let prevId: string | null = null;

  // Track tool call IDs across messages to deduplicate. The server accumulates
  // all tool calls from a turn into each message, so without this, every
  // assistant message in a multi-step tool use flow would show the full count.
  const seenToolCallIds = new Set<string>();

  for (const msg of messages) {
    switch (msg.role) {
      case "developer":
      case "tool":
        continue;
      case "system": {
        uiMessages.push({
          parentId: prevId,
          message: {
            id: msg.id,
            role: "system",
            parts: [
              {
                type: "text",
                text:
                  typeof msg.content === "string"
                    ? msg.content
                    : Array.isArray(msg.content)
                      ? msg.content
                          .filter((item) => item.type === "text")
                          .map((item) => item.text)
                          .join("\n")
                      : "",
              },
            ],
          },
        });
        break;
      }
      case "user": {
        seenToolCallIds.clear();
        uiMessages.push({
          parentId: prevId,
          message: {
            id: msg.id,
            role: "user",
            parts: convertGramMessagePartsToUIMessageParts(
              msg,
              toolCallResults,
            ),
          },
        });
        break;
      }
      case "assistant": {
        const uiMessage = {
          parentId: prevId,
          message: {
            id: msg.id,
            role: "assistant",
            parts: convertGramMessagePartsToUIMessageParts(
              msg,
              toolCallResults,
              seenToolCallIds,
            ),
          } satisfies UIMessage,
        };
        uiMessages.push(uiMessage);

        break;
      }
    }

    prevId = msg.id;
  }

  return {
    messages: uiMessages,
    headId: prevId,
  };
}

/**
 * Parsed shape of a single entry inside an assistant message's `tool_calls`
 * JSON string. Mirrors the OpenAI/OpenRouter tool-call wire format.
 */
interface GramToolCall {
  id: string;
  type?: "function";
  function?: { name?: string; arguments?: string | Record<string, unknown> };
}

export function convertGramMessagePartsToUIMessageParts(
  msg: GramChatMessage,
  toolResults: Map<string, GramChatMessage>,
  seenToolCallIds?: Set<string>,
): UIMessage["parts"] {
  const uiparts: UIMessage["parts"] = [];

  if (typeof msg.content === "string" && msg.content) {
    uiparts.push({
      type: "text",
      text: msg.content,
    });
  }

  const content = Array.isArray(msg.content) ? msg.content : [];
  for (const p of content) {
    switch (p.type) {
      case "text": {
        uiparts.push({
          type: "text",
          text: p.text,
        });
        break;
      }
      case "image_url": {
        const url = p.image_url?.url;
        if (!url) {
          break;
        }

        uiparts.push({
          type: "file",
          url,
          mediaType: mediaTypeFromURL(url),
        });
        break;
      }
      case "input_audio": {
        const url = p.input_audio?.data;
        if (!url) {
          break;
        }

        uiparts.push({
          type: "file",
          url,
          mediaType: mediaTypeFromURL(url),
        });
        break;
      }
    }
  }

  if (msg.role === "assistant" && msg.reasoning) {
    uiparts.push({
      type: "reasoning",
      text: msg.reasoning,
    });
  }

  if (msg.role === "assistant" && msg.tool_calls) {
    let toolCalls = tryParseJSON<GramToolCall[]>(msg.tool_calls || "[]");
    if (!Array.isArray(toolCalls)) {
      console.warn("Invalid tool_calls format, expected an array.");
      toolCalls = [];
    }

    for (const tc of toolCalls) {
      // The server accumulates all tool calls from a turn into each message's
      // tool_calls field. Deduplicate across messages so each tool call only
      // appears in the first message that references it.
      if (seenToolCallIds?.has(tc.id)) continue;
      seenToolCallIds?.add(tc.id);

      const content = toolResults.get(tc.id)?.content;
      uiparts.push({
        type: "dynamic-tool",
        toolCallId: tc.id,
        toolName: tc.function?.name ?? "",
        state: "output-available",
        input: tc.function?.arguments ?? {},
        output: typeof content === "string" ? tryParseJSON(content) : "",
      });
    }
  }

  return uiparts;
}

function mediaTypeFromURL(url: string): string {
  const unspecified = "unknown/unknown";
  if (!url.startsWith("data:")) {
    return unspecified;
  }

  const match = url.match(/^data:([^;]+);/);
  return match?.[1] || unspecified;
}

function tryParseJSON<T = unknown>(str: string): T | null {
  try {
    return JSON.parse(str) as T;
  } catch {
    return null;
  }
}
