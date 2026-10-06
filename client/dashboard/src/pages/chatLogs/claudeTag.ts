import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
import { messageText, type TranscriptRow } from "./transcript";

interface ClaudeTagWake {
  title?: string;
  messages: Array<{
    text: string;
    author: string;
    id: string | null;
    timestamp: string | null;
    trigger: boolean;
    channel: string;
    sender?: string;
  }>;
}

export function parseClaudeTagWake(content: unknown): ClaudeTagWake | null {
  let text = messageText(content).trim();
  let botId: string | undefined;
  let contextChannel: { id: string; name: string } | undefined;
  const context = text.match(/^<session-context nonce="([A-Za-z0-9_-]+)">/);
  if (context) {
    const closing = `</session-context nonce="${context[1]}">`;
    const end = text.indexOf(closing, context[0].length);
    if (end < 0) return null;
    botId = text
      .slice(context[0].length, end)
      .match(/^You: .*bot user id `([^`]+)`/m)?.[1];
    const channel = text
      .slice(context[0].length, end)
      .match(/^Channel: #([^\n]+) \(id: `([^`]+)`\)\r?$/m);
    if (channel) contextChannel = { id: channel[2]!, name: channel[1]! };
    text = text.slice(end + closing.length).trim();
  }
  if (/^<standing_owner_message[\s>]/.test(text)) {
    const end = text.indexOf("</standing_owner_message>");
    if (end < 0) return null;
    const doc = new DOMParser().parseFromString(
      text.slice(0, end + "</standing_owner_message>".length),
      "application/xml",
    );
    const el = doc.documentElement;
    if (
      doc.querySelector("parsererror") ||
      el.tagName !== "standing_owner_message" ||
      !el.getAttribute("sender")
    )
      return null;
    return {
      title: (el.textContent ?? "").trim().replace(/\s+/g, " ").slice(0, 80),
      messages: [
        {
          text: el.textContent ?? "",
          author: el.getAttribute("sender")!,
          sender: el.getAttribute("sender")!,
          id: el.getAttribute("ts"),
          timestamp: el.getAttribute("sent-at"),
          trigger: el.getAttribute("originating-ask") === "true",
          channel: contextChannel?.name ?? el.getAttribute("channel-id") ?? "",
        },
      ],
    };
  }
  if (!text.startsWith("<wake")) return null;
  const end = text.indexOf("</wake>");
  if (end < 0) return null;
  const doc = new DOMParser().parseFromString(
    text.slice(0, end + "</wake>".length),
    "application/xml",
  );
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
        (el) =>
          el.tagName === "message" &&
          el.getAttribute("from") === "human" &&
          (!botId || el.getAttribute("author-id") !== botId),
      )
      .map((el) => ({
        text: (el.textContent ?? "").replace(/<@[^>|]+\|([^>]+)>/g, "@$1"),
        author:
          el.getAttribute("author") ||
          el.getAttribute("author-handle") ||
          "User",
        sender:
          el.getAttribute("author-id") ??
          el.getAttribute("sender") ??
          undefined,
        id: el.getAttribute("id"),
        timestamp: el.getAttribute("sent-at"),
        trigger: el.getAttribute("trigger") === "true",
        channel:
          (contextChannel?.id === channel.getAttribute("id")
            ? contextChannel.name
            : undefined) ||
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
      ...new Set(
        wakes.flatMap((wake) =>
          wake.messages.map((m) => m.channel).filter(Boolean),
        ),
      ),
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
        if (text !== null && row.callMessage)
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
        const participants = message.sender
          ? [
              row.message.participants?.find(
                (participant) =>
                  participant.provider === "slack" &&
                  participant.providerUserId === message.sender,
              ) ?? { provider: "slack", providerUserId: message.sender },
            ]
          : undefined;
        return [
          {
            ...row,
            separateTurn: true,
            id: `${row.id}:tag:${index}`,
            message: {
              ...row.message,
              content: message.text,
              participants,
              // Legacy wakes contain display handles rather than directory IDs.
              userId: message.sender ? row.message.userId : message.author,
              externalUserId: message.sender
                ? row.message.externalUserId
                : undefined,
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
