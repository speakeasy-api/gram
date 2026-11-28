/**
 * Cassette format for recording chat conversations.
 *
 * A cassette captures a conversation as a JSON-serializable array of messages,
 * mirroring assistant-ui's ThreadMessage content structure (minus IDs/metadata).
 */

import type { ThreadMessage } from "@assistant-ui/react";

// ---------------------------------------------------------------------------
// Cassette types
// ---------------------------------------------------------------------------

type CassetteTextPart = { type: "text"; text: string };
type CassetteReasoningPart = { type: "reasoning"; text: string };
type CassetteToolCallPart = {
  type: "tool-call";
  toolCallId: string;
  toolName: string;
  args: unknown;
  result?: unknown;
};

type CassettePart =
  | CassetteTextPart
  | CassetteReasoningPart
  | CassetteToolCallPart;

interface CassetteMessage {
  role: "user" | "assistant";
  content: CassettePart[];
}

export interface Cassette {
  messages: CassetteMessage[];
}

// ---------------------------------------------------------------------------
// Recording: ThreadMessage[] → Cassette
// ---------------------------------------------------------------------------

/**
 * Converts assistant-ui ThreadMessages into a serializable Cassette.
 * System messages are filtered out since they aren't displayed.
 */
export function recordCassette(messages: readonly ThreadMessage[]): Cassette {
  const cassetteMessages: CassetteMessage[] = [];

  for (const msg of messages) {
    if (msg.role === "system") continue;

    const parts: CassettePart[] = [];

    for (const part of msg.content) {
      switch (part.type) {
        case "text":
          if (part.text) {
            parts.push({ type: "text", text: part.text });
          }
          break;
        case "reasoning":
          if (part.text) {
            parts.push({ type: "reasoning", text: part.text });
          }
          break;
        case "tool-call":
          parts.push({
            type: "tool-call",
            toolCallId: part.toolCallId,
            toolName: part.toolName,
            args: part.args,
            result: part.result,
          });
          break;
        // The recorder stores text, reasoning, and tool calls only.
        case "image":
        case "file":
        case "audio":
        case "source":
        case "data":
        case "generative-ui":
          break;
      }
    }

    if (parts.length > 0) {
      cassetteMessages.push({
        role: msg.role as "user" | "assistant",
        content: parts,
      });
    }
  }

  return { messages: cassetteMessages };
}
