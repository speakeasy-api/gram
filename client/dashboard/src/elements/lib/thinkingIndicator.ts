import { isPartialToolCallAnnotation } from "./toolCallAnnotation";

type PartLike = {
  readonly type: string;
  readonly text?: string;
  readonly status?: { readonly type: string };
};

type MessageLike = {
  readonly status?: { readonly type: string };
  readonly parts: readonly PartLike[];
};

/**
 * Decides whether the global "thinking" indicator shows under an assistant
 * message. While a turn runs there must be exactly one working signal on
 * screen, and the indicator fills every gap the others leave:
 *
 * - a text part that is still streaming shows the inline trailing dot
 *   (`.aui-md[data-status="running"]` in global.css);
 * - a tool run that is executing, or that ends the message so far, shimmers
 *   its heading (`ToolGroup`);
 * - a reasoning part that ends the message shimmers its trigger
 *   (`ReasoningGroup`).
 *
 * Anything else — no parts yet, or a finished text part while the model
 * decides what to do next — has no other signal, so the indicator shows.
 * Keying on the last part's type alone missed the finished-text case and left
 * a mid-turn chat looking idle.
 */
export function isThinkingIndicatorActive(message: MessageLike): boolean {
  if (message.status?.type !== "running") return false;
  const parts = message.parts;

  // ReasoningGroup shimmers only while reasoning is literally the last part.
  if (parts[parts.length - 1]?.type === "reasoning") return false;

  // An executing tool call keeps its group's heading shimmering.
  if (
    parts.some(
      (part) => part.type === "tool-call" && part.status?.type === "running",
    )
  ) {
    return false;
  }

  // Skip what renders nothing: empty text, and text still shaped like a tool
  // call annotation, which thread.tsx holds back from the prose render and
  // ToolGroup treats as the next batch's heading. Same test as both of those.
  let i = parts.length - 1;
  while (i >= 0) {
    const part = parts[i];
    if (
      part?.type === "text" &&
      ((part.text ?? "").trim().length === 0 ||
        isPartialToolCallAnnotation(part.text ?? ""))
    ) {
      i--;
      continue;
    }
    break;
  }
  const last = parts[i];
  if (!last) return true;

  // A tool run that ends the message stays in its running (shimmering) state.
  if (last.type === "tool-call") return false;

  // Streaming prose carries the trailing dot. Finished prose does not, so the
  // indicator takes over until the next part arrives.
  if (last.type === "text") return last.status?.type !== "running";

  return true;
}
