export interface Investigation {
  /** What the person asks, as it appears in the chat. */
  question: string;
  /** The facts on screen, sent as Additional context. */
  context: string;
}

/**
 * Context rides in the message's tagged block, which the chat shows as Additional context.
 * Names in it come from other people, so no tag inside it can close the block, whatever its case.
 */
export function investigationPrompt({
  question,
  context,
}: Investigation): string {
  const safe = context.replaceAll("<", "&lt;").replaceAll(">", "&gt;");
  return `<dashboard_context>\n${safe}\n</dashboard_context>\n\n${question}`;
}
