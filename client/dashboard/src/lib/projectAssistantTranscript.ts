import type { GramChatMessage } from "@gram-ai/elements";

// The Project Assistant transcript carries two kinds of leading "framing" the
// user never typed and shouldn't see rendered:
//
//   <message-context>…</message-context>   — prepended by the backend's source
//       adapter when constructing the turn for the runtime (event/source
//       metadata, MCP auth events). Needed for replay; noise for display.
//   <dashboard_context>…</dashboard_context> — prepended client-side by the
//       sidebar transport to carry the active page/chart context ("Explore
//       with AI"). Steers the model; noise for display.
//
// Both ride at the START of the persisted user message (an "Explore with AI"
// turn is double-wrapped: dashboard_context inside message-context). The live
// bubble shows the clean text from the assistant-ui store, but on reload from
// history the persisted content is rendered verbatim — so strip the leading
// framing here. Each alternative pairs its own open/close tag so a mismatched
// or user-typed tag mid-message is left intact.
const LEADING_FRAMING_RE =
  /^(?:\s*<message-context>[\s\S]*?<\/message-context>\s*|\s*<dashboard_context>[\s\S]*?<\/dashboard_context>\s*)+/;

function stripFraming(text: string): string {
  return text.replace(LEADING_FRAMING_RE, "");
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

/** True when the message carries a non-text part (image, audio, …). */
function hasMediaPart(content: GramChatMessage["content"]): boolean {
  return Array.isArray(content) && content.some((part) => part.type !== "text");
}

/**
 * Strips leading transcript framing (`<message-context>`, `<dashboard_context>`)
 * from a persisted message and drops user turns that are *only* framing (e.g. an
 * injected `assistant_mcp_auth_required` event with no human text), which would
 * otherwise render as a raw bubble. Intended for
 * `ElementsConfig.history.transformChatMessage`.
 */
export function stripTranscriptFraming(
  message: GramChatMessage,
): GramChatMessage | null {
  // Framing only ever rides on the human turn; leave assistant/tool rows alone.
  if (message.role !== "user") {
    return message;
  }

  // Drop a turn only when it is *nothing but* framing. A turn that also carries
  // media (image/audio) keeps its parts — the framing text is stripped below —
  // so a user's image isn't lost just because its text part was an event block.
  const text = userText(message.content);
  if (
    !hasMediaPart(message.content) &&
    text.trim() !== "" &&
    stripFraming(text).trim() === ""
  ) {
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
