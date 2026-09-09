import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
import { messageText, type TranscriptRow } from "./transcript";

interface ClaudeTagWake {
  title: string;
  messages: Array<{
    text: string;
    author: string;
    id: string | null;
    timestamp: string | null;
    trigger: boolean;
    channel: string;
  }>;
}

export function parseClaudeTagWake(content: unknown): ClaudeTagWake | null {
  const text = messageText(content).trim();
  if (!text.startsWith("<wake")) return null;
  const doc = new DOMParser().parseFromString(text, "application/xml");
  if (
    doc.querySelector("parsererror") ||
    doc.documentElement.tagName !== "wake"
  )
    return null;
  const channels = Array.from(doc.documentElement.children).filter(
    (el) => el.tagName === "channel" && el.getAttribute("id"),
  );
  const messages = channels.flatMap((channel) =>
    Array.from(channel.children)
      .filter(
        (el) => el.tagName === "message" && el.getAttribute("from") === "human",
      )
      .map((el) => ({
        text: (el.textContent ?? "").replace(/<@[^>|]+\|([^>]+)>/g, "@$1"),
        author:
          el.getAttribute("author") ||
          el.getAttribute("author-handle") ||
          "User",
        id: el.getAttribute("id"),
        timestamp: el.getAttribute("sent-at"),
        trigger: el.getAttribute("trigger") === "true",
        channel:
          channel.getAttribute("name") ||
          channel.getAttribute("channel-name") ||
          channel.getAttribute("id")!,
      })),
  );
  if (!messages.length) return null;
  return {
    messages,
    title: `Claude Tag in #${(messages.find((m) => m.trigger) ?? messages[0])!.channel.replace(/^#/, "")}`,
  };
}

export function claudeTagMetadata(messages: ChatMessage[]): {
  detected: boolean;
  title: string | undefined;
  channels: string[];
} {
  const wakes = messages
    .filter((m) => m.role === "user")
    .map((m) => parseClaudeTagWake(m.content))
    .filter((wake) => wake !== null);
  return {
    detected: wakes.length > 0,
    title: wakes[0]?.title,
    channels: [
      ...new Set(wakes.flatMap((wake) => wake.messages.map((m) => m.channel))),
    ],
  };
}

function replyText(args: unknown): string | null {
  try {
    const value: unknown = typeof args === "string" ? JSON.parse(args) : args;
    if (
      value &&
      typeof value === "object" &&
      "text" in value &&
      typeof value.text === "string"
    )
      return value.text;
  } catch {
    /* Malformed inputs stay visible in the transcript. */
  }
  return null;
}

/** Project captured rows, retaining original message IDs for risk findings and
 * navigation. Unknown formats remain visible, so extraction failures lose no text. */
export function projectClaudeTagRows(rows: TranscriptRow[]): TranscriptRow[] {
  const seen = new Set<string>();
  return rows.flatMap((row): TranscriptRow[] => {
    if (row.kind === "tool") {
      if (row.callMessage?.isRisk || row.resultMessage?.isRisk) return [row];
      const result = messageText(row.resultMessage?.content);
      if (/"isError"\s*:\s*true|"error"\s*:|\b(failed|error)\b/i.test(result))
        return [row];
      const name = row.toolCall?.function?.name ?? row.toolCall?.name;
      if (name === "mcp__slackbot__reply") {
        const text = replyText(row.toolCall?.function?.arguments);
        // Only project the reply text when we have a result confirming delivery;
        // without a result the tool may not have sent.
        if (text !== null && row.callMessage && row.resultMessage)
          return [
            {
              kind: "message",
              id: row.id,
              entryType: "assistant",
              attachments: [],
              generation: row.generation,
              message: {
                ...row.callMessage,
                role: "assistant",
                content: text,
                toolCalls: undefined,
              },
            },
          ];
      }
      if (
        name === "mcp__slackbot__react" ||
        name === "mcp__slackbot__no_reply_needed" ||
        name === "mcp__slackbot__send_later"
      )
        return [];
      return [row];
    }
    if (row.entryType === "user") {
      const wake = parseClaudeTagWake(row.message.content);
      if (!wake) return [row];
      return wake.messages.flatMap((message, index) => {
        const key = `${message.channel}:${message.id}`;
        if (message.id && seen.has(key)) return [];
        if (message.id) seen.add(key);
        const timestamp = message.timestamp
          ? new Date(message.timestamp)
          : row.message.createdAt;
        return [
          {
            ...row,
            separateTurn: true,
            id: `${row.id}:tag:${index}`,
            message: {
              ...row.message,
              content: message.text,
              userId: message.author,
              externalUserId: undefined,
              createdAt: Number.isNaN(timestamp.getTime())
                ? row.message.createdAt
                : timestamp,
            },
          },
        ];
      });
    }
    if (
      row.entryType === "assistant" &&
      /^Replied in the thread\.?$/i.test(
        messageText(row.message.content).trim(),
      )
    )
      return [];
    return [row];
  });
}
