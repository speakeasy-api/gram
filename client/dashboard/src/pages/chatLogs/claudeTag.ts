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

/** Only harness framing may precede a delivery. Quoted markup and history
 * remain ordinary text, and nonce-bearing context is opaque until its own end. */
function deliveryEnvelope(text: string): { body: string; context: string } {
  text = text.trim();
  const contexts: string[] = [];
  for (;;) {
    const header = text.match(
      /^<(system-reminder|session-context)(?:\s[^>]*|)>/,
    );
    if (!header) return { body: text, context: contexts.join("\n") };
    const nonce = contextNonce(header[0]);
    if (nonce === null) return { body: "", context: "" };
    let end = -1;
    let closingLength = 0;
    if (header[1] === "session-context") {
      const closings = text
        .slice(header[0].length)
        .matchAll(/<\/session-context(?:\s[^>]*|)>/g);
      for (const closing of closings) {
        if (contextNonce(closing[0]) === nonce) {
          end = closing.index + header[0].length;
          closingLength = closing[0].length;
          break;
        }
      }
    } else {
      const closing = `</${header[1]}>`;
      end = text.indexOf(closing, header[0].length);
      closingLength = closing.length;
    }
    if (end < 0) return { body: "", context: "" };
    if (header[1] === "session-context")
      contexts.push(text.slice(header[0].length, end));
    text = text.slice(end + closingLength).trim();
  }
}

function contextNonce(tag: string): string | undefined | null {
  const doc = new DOMParser().parseFromString(
    tag.replace(/^<\//, "<").replace(/>$/, "/>"),
    "application/xml",
  );
  if (doc.querySelector("parsererror")) return null;
  return doc.documentElement.getAttribute("nonce")?.trim();
}

function deliveryEnd(text: string, name: string): number {
  const closing = `</${name}>`;
  const token = /<!\[CDATA\[[\s\S]*?\]\]>|<\/[^>]*>/g;
  for (const match of text.matchAll(token)) {
    if (match[0] === closing) return match.index + closing.length;
  }
  return -1;
}

function deliveryXML(text: string): string {
  return text
    .split(/(<!\[CDATA\[[\s\S]*?\]\]>)/)
    .map((part) => {
      if (part.startsWith("<![CDATA["))
        return part
          .slice(9, -3)
          .replace(/&/g, "&amp;")
          .replace(/</g, "&lt;")
          .replace(/>/g, "&gt;");
      return (
        part
          .replace(
            /<(?:[@#][^<>\s]+|https?:\/\/[^<>\s]+)>/g,
            (value) => `&lt;${value.slice(1, -1)}&gt;`,
          )
          // Keep tag-like markup intact so malformed envelopes still fail XML parsing.
          .replace(/<(?![A-Za-z_:/!?])/g, "&lt;")
          .replace(
            /&(?!(?:amp|lt|gt|quot|apos|#\d+|#x[0-9a-fA-F]+);)/g,
            "&amp;",
          )
      );
    })
    .join("");
}

function deliveryMessages(element: Element): Element[] {
  return Array.from(element.children).flatMap((child): Element[] => {
    if (child.tagName === "message") return [child];
    if (
      [
        "participants",
        "system-note",
        "system-reminder",
        "session-context",
        "history",
        "reference",
        "thread_activity",
        "channel",
      ].includes(child.tagName)
    )
      return [];
    return deliveryMessages(child);
  });
}

function attribute(element: Element, ...names: string[]): string | undefined {
  for (const name of names) {
    const value = element.getAttribute(name)?.trim();
    if (value) return value;
  }
  return undefined;
}

function slackText(text: string): string {
  return text.replace(/<@[^>|]+\|([^>]+)>/g, "@$1");
}

export function parseClaudeTagWake(content: unknown): ClaudeTagWake | null {
  const { body: text, context } = deliveryEnvelope(messageText(content));
  const botId = context.match(/^You: .*bot user id `([^`]+)`/m)?.[1]?.trim();
  const channel = context.match(/^Channel: #([^\n]+) \(id: `([^`]+)`\)\r?$/m);
  let contextChannel: { id: string; name: string } | undefined;
  if (channel) contextChannel = { id: channel[2]!, name: channel[1]! };
  if (/^<standing_owner_message[\s>]/.test(text)) {
    const end = deliveryEnd(text, "standing_owner_message");
    if (end < 0) return null;
    const doc = new DOMParser().parseFromString(
      deliveryXML(text.slice(0, end)),
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
      title: slackText(el.textContent ?? "")
        .trim()
        .replace(/\s+/g, " ")
        .slice(0, 80),
      messages: [
        {
          text: slackText(el.textContent ?? ""),
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
  const end = deliveryEnd(text, "wake");
  if (end < 0) return null;
  const doc = new DOMParser().parseFromString(
    deliveryXML(text.slice(0, end)),
    "application/xml",
  );
  if (
    doc.querySelector("parsererror") ||
    doc.documentElement.tagName !== "wake"
  )
    return null;
  const channels = Array.from(doc.documentElement.children).filter(
    (el) => el.tagName === "channel" && attribute(el, "id", "channel-id"),
  );
  const messages = channels.flatMap((channel) => {
    const channelId = attribute(channel, "id", "channel-id")!;
    let channelName = attribute(channel, "name", "channel-name") ?? channelId;
    if (contextChannel && contextChannel.id === channelId)
      channelName = contextChannel.name;
    return deliveryMessages(channel)
      .filter(
        (el) =>
          el.tagName === "message" &&
          el.getAttribute("from") === "human" &&
          (!botId ||
            attribute(el, "author-id", "sender", "slack-id") !== botId),
      )
      .map((el) => ({
        text: slackText(el.textContent ?? ""),
        author:
          el.getAttribute("author") ||
          el.getAttribute("author-handle") ||
          "User",
        sender: attribute(el, "author-id", "sender", "slack-id"),
        id: attribute(el, "id", "ts") ?? null,
        timestamp: el.getAttribute("sent-at"),
        trigger: el.getAttribute("trigger") === "true",
        channel: channelName,
      }));
  });
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
        let displayName: string | undefined;
        if (message.author !== "User" && message.author !== message.sender)
          displayName = message.author;
        const participant = row.message.participants?.find(
          (participant) =>
            participant.provider === "slack" &&
            participant.providerUserId === message.sender,
        );
        const participants = message.sender
          ? [
              {
                ...participant,
                provider: "slack",
                providerUserId: message.sender,
                displayName: participant?.displayName ?? displayName,
              },
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
