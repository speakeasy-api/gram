import type { GramChatMessage } from "@gram-ai/elements";

// The Gram assistant runtime persists each turn's input with a
// `<message-context>…</message-context>` framing block that the backend's source
// adapter prepends when constructing the message for the runtime (EventID /
// UserID lines, MCP auth events). The model needs that block on replay, but it
// must never reach the dashboard transcript — left in, the first message of a
// reopened thread renders as a raw bubble exposing internals like MCP AuthURLs.
//
// This is a Gram-product transcript convention, so it lives in the dashboard and
// is handed to Elements via `history.transformChatMessage` rather than baked
// into the shared `@gram-ai/elements` library.
const MESSAGE_CONTEXT_RE =
  /^\s*<message-context>[\s\S]*?<\/message-context>\s*/;

function stripFraming(text: string): string {
  return text.replace(MESSAGE_CONTEXT_RE, "");
}

function userText(content: GramChatMessage["content"]): string {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  return content
    .filter(
      (part): part is { type: "text"; text: string } => part.type === "text",
    )
    .map((part) => part.text)
    .join("");
}

/**
 * Strips the backend's leading `<message-context>` framing from a persisted
 * message and drops user turns that are *only* framing (e.g. an injected
 * `assistant_mcp_auth_required` event with no human text), which would otherwise
 * render as a raw bubble. Intended for `ElementsConfig.history.transformChatMessage`.
 */
export function stripMessageContextFraming(
  message: GramChatMessage,
): GramChatMessage | null {
  // Framing only ever rides on the human turn; leave assistant/tool rows alone.
  if (message.role !== "user") {
    return message;
  }

  const text = userText(message.content);
  if (text.includes("<message-context>") && stripFraming(text).trim() === "") {
    return null;
  }

  if (typeof message.content === "string") {
    return { ...message, content: stripFraming(message.content) };
  }
  if (Array.isArray(message.content)) {
    return {
      ...message,
      content: message.content.map((part) =>
        part.type === "text"
          ? { ...part, text: stripFraming(part.text) }
          : part,
      ),
    };
  }
  return message;
}
