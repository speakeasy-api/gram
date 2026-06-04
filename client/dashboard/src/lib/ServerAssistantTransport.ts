import { assistantsListMessages } from "@gram/client/funcs/assistantsListMessages";
import { assistantsSendMessage } from "@gram/client/funcs/assistantsSendMessage";
import type { GramCore } from "@gram/client/core";
import { type ChatTransport, createUIMessageStream, type UIMessage } from "ai";

export interface ServerAssistantTransportConfig {
  /** SDK client (from useGramContext). */
  client: GramCore;
  /** The assistant to converse with (the project's managed assistant). */
  assistantId: string;
  /** Project slug for the Gram-Project header. */
  projectSlug: string;
  /** Resolves the current conversation key. A fresh value starts a new thread. */
  getCorrelationId: () => string;
  /** Optional poll tuning. */
  pollIntervalMs?: number;
  pollTimeoutMs?: number;
}

/**
 * ChatTransport that routes the conversation through the persistent server-side
 * assistant instead of the client-side AI SDK: it posts the user's message via
 * `assistants.sendMessage`, then polls `assistants.listMessages` for the
 * assistant's reply and surfaces it as a single (non-streamed) message. Delivery
 * is message-level — the UI shows a thinking state until the reply lands — in
 * exchange for persistence, server-side tool execution, and cross-session memory.
 */
export class ServerAssistantTransport implements ChatTransport<UIMessage> {
  private config: ServerAssistantTransportConfig;
  /**
   * Poll cursor per chat: the highest seq surfaced for that chatId. Keyed by
   * chatId (not a single field) so that "Start fresh" — which rotates the
   * correlation id and therefore opens a new chat whose seqs restart at 1 —
   * polls the new chat from 0 instead of from the old conversation's high-water
   * mark (which would never be reached, causing a guaranteed timeout).
   */
  private lastSeqByChat = new Map<string, number>();

  constructor(config: ServerAssistantTransportConfig) {
    this.config = config;
  }

  updateConfig(config: Partial<ServerAssistantTransportConfig>) {
    this.config = { ...this.config, ...config };
  }

  async sendMessages({
    messages,
    abortSignal,
  }: {
    messages: UIMessage[];
    abortSignal?: AbortSignal;
  }) {
    const text = latestUserText(messages);
    if (!text) {
      throw new Error("No user message to send.");
    }

    const {
      client,
      assistantId,
      projectSlug,
      getCorrelationId,
      pollIntervalMs = 1500,
      pollTimeoutMs = 120_000,
    } = this.config;

    const sent = await assistantsSendMessage(client, {
      gramProject: projectSlug,
      sendMessageRequestBody: {
        assistantId,
        correlationId: getCorrelationId(),
        message: text,
        idempotencyKey: crypto.randomUUID(),
      },
    });
    if (!sent.ok) {
      throw sent.error;
    }
    const chatId = sent.value.chatId;

    const reply = await this.pollForReply({
      client,
      projectSlug,
      chatId,
      pollIntervalMs,
      pollTimeoutMs,
      abortSignal,
    });

    return createUIMessageStream<UIMessage>({
      originalMessages: messages,
      execute: ({ writer }) => {
        const id = reply.id;
        writer.write({ type: "start" });
        writer.write({ type: "text-start", id });
        writer.write({ type: "text-delta", id, delta: reply.content });
        writer.write({ type: "text-end", id });
        writer.write({ type: "finish" });
      },
    });
  }

  async reconnectToStream() {
    // The server assistant is poll-based; there is no stream to reconnect to.
    return null;
  }

  /** Polls the conversation log until an assistant message newer than the
   *  current cursor appears, then advances the cursor past it. */
  private async pollForReply({
    client,
    projectSlug,
    chatId,
    pollIntervalMs,
    pollTimeoutMs,
    abortSignal,
  }: {
    client: GramCore;
    projectSlug: string;
    chatId: string;
    pollIntervalMs: number;
    pollTimeoutMs: number;
    abortSignal?: AbortSignal;
  }): Promise<{ id: string; content: string }> {
    const deadline = Date.now() + pollTimeoutMs;

    for (;;) {
      if (abortSignal?.aborted) {
        throw new DOMException("Aborted", "AbortError");
      }

      const cursor = this.lastSeqByChat.get(chatId) ?? 0;
      const res = await assistantsListMessages(client, {
        gramProject: projectSlug,
        chatId,
        afterSeq: cursor,
      });
      if (!res.ok) {
        throw res.error;
      }

      // Advance the cursor past everything seen (user echo + any replies) and
      // surface the first assistant message in this batch.
      let nextCursor = cursor;
      let assistantReply: { id: string; content: string } | null = null;
      for (const m of res.value.messages) {
        if (m.seq > nextCursor) {
          nextCursor = m.seq;
        }
        if (!assistantReply && m.role === "assistant") {
          assistantReply = { id: m.id, content: m.content };
        }
      }
      this.lastSeqByChat.set(chatId, nextCursor);
      if (assistantReply) {
        return assistantReply;
      }

      if (Date.now() >= deadline) {
        throw new Error("Timed out waiting for the assistant's reply.");
      }
      await delay(pollIntervalMs, abortSignal);
    }
  }
}

function latestUserText(messages: UIMessage[]): string {
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i];
    if (m.role !== "user") continue;
    return m.parts
      .filter((p): p is { type: "text"; text: string } => p.type === "text")
      .map((p) => p.text)
      .join("")
      .trim();
  }
  return "";
}

function delay(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const id = setTimeout(resolve, ms);
    signal?.addEventListener(
      "abort",
      () => {
        clearTimeout(id);
        reject(new DOMException("Aborted", "AbortError"));
      },
      { once: true },
    );
  });
}
